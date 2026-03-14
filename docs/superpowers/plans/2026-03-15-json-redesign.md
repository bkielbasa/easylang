# JSON Redesign + Runtime Reflection Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the JSON package with a unified Value API and add runtime reflection for struct-based Marshal/Unmarshal, powered by compiler-emitted metadata.

**Architecture:** Two layers — (1) a `reflect` package backed by 5 new IR opcodes and compiler-emitted struct metadata globals, (2) a redesigned `json` package with unified `Value` type, plus `json.Marshal`/`json.Unmarshal` compiler builtins (2 more IR opcodes) using inline codegen against the metadata.

**Tech Stack:** Ease (self-hosting compiler), LLVM IR codegen

**Spec:** `docs/superpowers/specs/2026-03-15-json-redesign-design.md`

---

## File Structure

### New files
- `bootstrap/ease/reflect/reflect.ease` — reflect stdlib package (stub functions; compiler intercepts calls)
- `bootstrap/ease/sema/sema.ease` — add `LookupTypeInstantiation` function
- `tests/reflect_test.ease` — reflect package tests

### Modified files
- `bootstrap/ease/ir/ir.ease` — new opcode constants (88-94), extend `RegisterStruct` signature, new `g_struct_reg_field_types` global
- `bootstrap/compiler.ease` — collect field types and pass to `RegisterStruct`
- `bootstrap/ease/irgen/irgen.ease` — IR generation for reflect builtins and json.Marshal/Unmarshal
- `bootstrap/ease/llvm/llvm.ease` — emit struct metadata globals, codegen for 7 new opcodes, collect_vregs updates
- `bootstrap/ease/json/json.ease` — new Value struct, constructors, unified API, MarshalStruct/UnmarshalStruct helpers
- `tests/json_test.ease` — new Value-based API tests + keep backward compat tests
- `bootstrap/seed.ll` — updated after final convergence

---

## Chunk 1: Extend RegisterStruct with Field Types

### Task 1: Extend RegisterStruct to accept field types

**Files:**
- Modify: `bootstrap/ease/ir/ir.ease:137-151`
- Modify: `bootstrap/compiler.ease:815-837`

- [ ] **Step 1: Add `g_struct_reg_field_types` global array**

In `bootstrap/ease/ir/ir.ease`, after line 140 (`g_struct_reg_field_names := []string{}`), add:

```ease
g_struct_reg_field_types := []int{}
```

- [ ] **Step 2: Update `RegisterStruct` signature and body**

In `bootstrap/ease/ir/ir.ease`, replace the function at lines 142-151:

```ease
fn RegisterStruct(name: string, field_names: []string, field_types: []int) {
    g_struct_reg_names = append(g_struct_reg_names, name)
    g_struct_reg_field_counts = append(g_struct_reg_field_counts, len(field_names))
    g_struct_reg_field_starts = append(g_struct_reg_field_starts, len(g_struct_reg_field_names))
    i := 0
    for i < len(field_names) {
        g_struct_reg_field_names = append(g_struct_reg_field_names, field_names[i])
        g_struct_reg_field_types = append(g_struct_reg_field_types, field_types[i])
        i = i + 1
    }
}
```

- [ ] **Step 3: Update the call site in compiler.ease**

In `bootstrap/compiler.ease`, in the struct registration loop (~lines 815-837). Field types are collected from `nodes[fi].type_tag` — this comes directly from the AST, NOT from `sema.register_field_type` (which is keyed by field name alone and would collide if two structs have same-named fields of different types).

Before the field loop (around where `field_names_list` is initialized), add:

```ease
field_types_list := []int{}
```

Inside the field loop (after `field_names_list = append(field_names_list, nodes[fi].str_val)`), add:

```ease
field_types_list = append(field_types_list, nodes[fi].type_tag)
```

Update the `RegisterStruct` call from:
```ease
RegisterStruct(struct_name, field_names_list)
```
to:
```ease
RegisterStruct(struct_name, field_names_list, field_types_list)
```

- [ ] **Step 4: Verify bootstrap convergence**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease2
./tmp/ease2 bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease3
diff <(./tmp/ease2 bootstrap/compiler.ease 2>&1) <(./tmp/ease3 bootstrap/compiler.ease 2>&1)
```

Expected: gen2 == gen3 (convergence). If not, run gen4.

- [ ] **Step 5: Run existing tests to confirm no regression**

```bash
./tmp/ease test tests/
```

Expected: All 219+ tests pass.

- [ ] **Step 6: Update seed.ll**

Copy the converged output to seed.ll:
```bash
cp tmp/output.ll bootstrap/seed.ll
```

Verify SEED OK:
```bash
clang -O1 bootstrap/seed.ll -o tmp/ease_seed && ./tmp/ease_seed bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease_check
diff <(./tmp/ease_check bootstrap/compiler.ease 2>&1) <(./tmp/ease_seed bootstrap/compiler.ease 2>&1)
```

- [ ] **Step 7: Commit**

```bash
git add bootstrap/ease/ir/ir.ease bootstrap/compiler.ease bootstrap/seed.ll
git commit -m "feat(reflect): Extend RegisterStruct to store field types"
```

---

## Chunk 2: Emit Reflect Metadata as LLVM Globals

### Task 2: Emit struct metadata into compiled binary

**Files:**
- Modify: `bootstrap/ease/llvm/llvm.ease:337-368` (emit_llvm_ir, after string constant emission)

- [ ] **Step 1: Add metadata emission function**

In `bootstrap/ease/llvm/llvm.ease`, add a new function before `emit_llvm_ir` (around line 335):

```ease
fn emit_reflect_metadata(fd: int) {
    si := 0
    for si < len(g_struct_reg_names) {
        sname := g_struct_reg_names[si]
        count := g_struct_reg_field_counts[si]
        start := g_struct_reg_field_starts[si]

        // Emit field name string constants as null-terminated C strings
        fi := 0
        for fi < count {
            fname := g_struct_reg_field_names[start + fi]
            write_str(fd, "@reflect." + sname + ".fname." + strconv.Itoa(fi) + " = private unnamed_addr constant [" + strconv.Itoa(len(fname) + 1) + " x i8] c\"" + fname + "\\00\"\n")
            fi = fi + 1
        }

        // Emit field count
        write_str(fd, "@reflect." + sname + ".count = private constant i64 " + strconv.Itoa(count) + "\n")

        // Emit field names pointer array
        if count > 0 {
            write_str(fd, "@reflect." + sname + ".names = private constant [" + strconv.Itoa(count) + " x ptr] [")
            fi = 0
            for fi < count {
                if fi > 0 { write_str(fd, ", ") }
                write_str(fd, "ptr @reflect." + sname + ".fname." + strconv.Itoa(fi))
                fi = fi + 1
            }
            write_str(fd, "]\n")
        }

        // Emit field types array
        if count > 0 {
            write_str(fd, "@reflect." + sname + ".types = private constant [" + strconv.Itoa(count) + " x i64] [")
            fi = 0
            for fi < count {
                if fi > 0 { write_str(fd, ", ") }
                ftype := g_struct_reg_field_types[start + fi]
                write_str(fd, "i64 " + strconv.Itoa(ftype))
                fi = fi + 1
            }
            write_str(fd, "]\n")
        }

        si = si + 1
    }
}
```

- [ ] **Step 2: Call emit_reflect_metadata from emit_llvm_ir**

In `emit_llvm_ir()`, after the string constant emission loop (around line 368, after the `for` loop that emits `@.str.N` globals), add:

```ease
emit_reflect_metadata(fd)
```

- [ ] **Step 3: Verify bootstrap convergence**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease2
./tmp/ease2 bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease3
diff <(./tmp/ease2 bootstrap/compiler.ease 2>&1) <(./tmp/ease3 bootstrap/compiler.ease 2>&1)
```

- [ ] **Step 4: Inspect emitted metadata**

```bash
grep "@reflect\." tmp/output.ll | head -30
```

Expected: See `@reflect.Doc.count`, `@reflect.Doc.names`, `@reflect.Doc.types` (and similar for other structs).

- [ ] **Step 5: Run tests**

```bash
./tmp/ease test tests/
```

Expected: All tests still pass (metadata is emitted but not yet used).

- [ ] **Step 6: Update seed.ll and commit**

```bash
cp tmp/output.ll bootstrap/seed.ll
git add bootstrap/ease/llvm/llvm.ease bootstrap/seed.ll
git commit -m "feat(reflect): Emit struct metadata as LLVM globals"
```

---

## Chunk 3: Reflect IR Opcodes + Codegen

### Task 3: Define reflect opcodes in IR

**Files:**
- Modify: `bootstrap/ease/ir/ir.ease:129` (after OP_ARRAY_SLICE)

- [ ] **Step 1: Add opcode constants**

After `const OP_ARRAY_SLICE = 87`, add:

```ease
const OP_REFLECT_NUM_FIELD = 88
const OP_REFLECT_FIELD_NAME = 89
const OP_REFLECT_FIELD_TYPE = 90
const OP_REFLECT_FIELD_VALUE = 91
const OP_REFLECT_SET_FIELD_VALUE = 92
```

- [ ] **Step 2: Commit opcode definitions**

```bash
git add bootstrap/ease/ir/ir.ease
git commit -m "feat(reflect): Add reflect IR opcode constants 88-92"
```

### Task 4: Add reflect opcodes to collect_vregs

**Files:**
- Modify: `bootstrap/ease/llvm/llvm.ease:294-335` (collect_vregs function)

The `collect_vregs()` function has 4 vreg collection blocks. Note: arg1 uses an **exclusion** pattern (collects all opcodes EXCEPT those listed), so new opcodes are automatically included for arg1 — no change needed.

- [ ] **Step 1: Add to dest vreg collection (line ~306)**

Add the 4 result-producing reflect opcodes to the OR-chain. Do NOT add `OP_REFLECT_SET_FIELD_VALUE` — it's a void operation (like `OP_STORE` and `OP_POKE`, which are not in the dest list):

```ease
|| op == OP_REFLECT_NUM_FIELD || op == OP_REFLECT_FIELD_NAME || op == OP_REFLECT_FIELD_TYPE || op == OP_REFLECT_FIELD_VALUE
```

- [ ] **Step 2: Add to arg2 vreg collection (line ~323)**

Add opcodes that use arg2 (field index):

```ease
|| op == OP_REFLECT_FIELD_NAME || op == OP_REFLECT_FIELD_TYPE || op == OP_REFLECT_FIELD_VALUE || op == OP_REFLECT_SET_FIELD_VALUE
```

- [ ] **Step 3: Add to arg3 vreg collection (line ~327-331)**

Add `OP_REFLECT_SET_FIELD_VALUE` (uses arg3 for the value). Follow the existing pattern at line 331:

```ease
if op == OP_REFLECT_SET_FIELD_VALUE { arg3_vregs = append(arg3_vregs, instrs[i].arg3) }
```

- [ ] **Step 4: Commit**

```bash
git add bootstrap/ease/llvm/llvm.ease
git commit -m "feat(reflect): Register reflect opcodes in collect_vregs"
```

### Task 5: Implement reflect opcode codegen

**Files:**
- Modify: `bootstrap/ease/llvm/llvm.ease` (after OP_ARRAY_SLICE handler, around line 870+)

Ease strings are i64-encoded pointers to null-terminated C strings. The `ptrtoint ptr` conversion used here matches exactly how `OP_LOADCONST` handles string constants (line ~674 of llvm.ease).

- [ ] **Step 1: Implement OP_REFLECT_NUM_FIELD codegen**

```ease
if terminated == 0 && instr.op == OP_REFLECT_NUM_FIELD {
    sname := instr.str_val
    val := tmp_name(ti, 1)
    write_str(fd, "  " + val + " = load i64, ptr @reflect." + sname + ".count\n")
    emit_store_vreg(fd, instr.dest, val)
}
```

- [ ] **Step 2: Implement OP_REFLECT_FIELD_NAME codegen**

```ease
if terminated == 0 && instr.op == OP_REFLECT_FIELD_NAME {
    sname := instr.str_val
    idx := emit_load_vreg(fd, instr.arg2, ti, 0)
    gep := tmp_name(ti, 1)
    write_str(fd, "  " + gep + " = getelementptr [0 x ptr], ptr @reflect." + sname + ".names, i64 0, i64 " + idx + "\n")
    cstr := tmp_name(ti, 2)
    write_str(fd, "  " + cstr + " = load ptr, ptr " + gep + "\n")
    str_val := tmp_name(ti, 3)
    write_str(fd, "  " + str_val + " = ptrtoint ptr " + cstr + " to i64\n")
    emit_store_vreg(fd, instr.dest, str_val)
}
```

- [ ] **Step 3: Implement OP_REFLECT_FIELD_TYPE codegen**

```ease
if terminated == 0 && instr.op == OP_REFLECT_FIELD_TYPE {
    sname := instr.str_val
    idx := emit_load_vreg(fd, instr.arg2, ti, 0)
    gep := tmp_name(ti, 1)
    write_str(fd, "  " + gep + " = getelementptr [0 x i64], ptr @reflect." + sname + ".types, i64 0, i64 " + idx + "\n")
    val := tmp_name(ti, 2)
    write_str(fd, "  " + val + " = load i64, ptr " + gep + "\n")
    emit_store_vreg(fd, instr.dest, val)
}
```

- [ ] **Step 4: Implement OP_REFLECT_FIELD_VALUE codegen**

```ease
if terminated == 0 && instr.op == OP_REFLECT_FIELD_VALUE {
    base := emit_load_vreg(fd, instr.arg1, ti, 0)
    idx := emit_load_vreg(fd, instr.arg2, ti, 1)
    offset := tmp_name(ti, 2)
    write_str(fd, "  " + offset + " = mul i64 " + idx + ", 8\n")
    addr := tmp_name(ti, 3)
    write_str(fd, "  " + addr + " = add i64 " + base + ", " + offset + "\n")
    ptr := tmp_name(ti, 4)
    write_str(fd, "  " + ptr + " = inttoptr i64 " + addr + " to ptr\n")
    val := tmp_name(ti, 5)
    write_str(fd, "  " + val + " = load i64, ptr " + ptr + "\n")
    emit_store_vreg(fd, instr.dest, val)
}
```

- [ ] **Step 5: Implement OP_REFLECT_SET_FIELD_VALUE codegen**

Note: No `emit_store_vreg` — this is a void operation (like `OP_STORE`).

```ease
if terminated == 0 && instr.op == OP_REFLECT_SET_FIELD_VALUE {
    base := emit_load_vreg(fd, instr.arg1, ti, 0)
    idx := emit_load_vreg(fd, instr.arg2, ti, 1)
    val := emit_load_vreg(fd, instr.arg3, ti, 2)
    offset := tmp_name(ti, 3)
    write_str(fd, "  " + offset + " = mul i64 " + idx + ", 8\n")
    addr := tmp_name(ti, 4)
    write_str(fd, "  " + addr + " = add i64 " + base + ", " + offset + "\n")
    ptr := tmp_name(ti, 5)
    write_str(fd, "  " + ptr + " = inttoptr i64 " + addr + " to ptr\n")
    write_str(fd, "  store i64 " + val + ", ptr " + ptr + "\n")
}
```

- [ ] **Step 6: Bootstrap convergence**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease2
./tmp/ease2 bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease3
diff <(./tmp/ease2 bootstrap/compiler.ease 2>&1) <(./tmp/ease3 bootstrap/compiler.ease 2>&1)
```

Run gen4 if needed.

- [ ] **Step 7: Run tests**

```bash
./tmp/ease test tests/
```

Expected: All tests pass (opcodes exist but no IR generation path yet).

- [ ] **Step 8: Update seed.ll and commit**

```bash
cp tmp/output.ll bootstrap/seed.ll
git add bootstrap/ease/llvm/llvm.ease bootstrap/seed.ll
git commit -m "feat(reflect): Implement LLVM codegen for reflect opcodes 88-92"
```

---

## Chunk 4: Reflect IR Generation + Stdlib + Tests

### Task 6: Add reflect builtin IR generation

**Files:**
- Modify: `bootstrap/ease/irgen/irgen.ease:1612-1692` (builtin dispatch)

Add these BEFORE the fallthrough to `gen_ir_call_user` (~line 1692). Use `gen_ir_eval_args` for consistency with existing builtins (e.g., `strconv.FloatToStr` at line ~1673).

- [ ] **Step 1: Add all 5 reflect IR generation handlers**

```ease
if func_name == "reflect.NumField" {
    arg_vregs := gen_ir_eval_args(ast_idx, nodes, instrs, symtab_names, symtab_vregs)
    sname := get_vreg_struct_name(arg_vregs[0])
    result_vreg := len(instrs)
    instrs = append(instrs, IRInstr { op: OP_REFLECT_NUM_FIELD, dest: result_vreg, arg1: arg_vregs[0], arg2: 0, arg3: 0, str_val: sname })
    set_vreg_type(result_vreg, TYPE_INT)
    return result_vreg
}

if func_name == "reflect.FieldName" {
    arg_vregs := gen_ir_eval_args(ast_idx, nodes, instrs, symtab_names, symtab_vregs)
    sname := get_vreg_struct_name(arg_vregs[0])
    result_vreg := len(instrs)
    instrs = append(instrs, IRInstr { op: OP_REFLECT_FIELD_NAME, dest: result_vreg, arg1: arg_vregs[0], arg2: arg_vregs[1], arg3: 0, str_val: sname })
    set_vreg_type(result_vreg, TYPE_STRING)
    return result_vreg
}

if func_name == "reflect.FieldType" {
    arg_vregs := gen_ir_eval_args(ast_idx, nodes, instrs, symtab_names, symtab_vregs)
    sname := get_vreg_struct_name(arg_vregs[0])
    result_vreg := len(instrs)
    instrs = append(instrs, IRInstr { op: OP_REFLECT_FIELD_TYPE, dest: result_vreg, arg1: arg_vregs[0], arg2: arg_vregs[1], arg3: 0, str_val: sname })
    set_vreg_type(result_vreg, TYPE_INT)
    return result_vreg
}

if func_name == "reflect.FieldValue" {
    arg_vregs := gen_ir_eval_args(ast_idx, nodes, instrs, symtab_names, symtab_vregs)
    sname := get_vreg_struct_name(arg_vregs[0])
    result_vreg := len(instrs)
    instrs = append(instrs, IRInstr { op: OP_REFLECT_FIELD_VALUE, dest: result_vreg, arg1: arg_vregs[0], arg2: arg_vregs[1], arg3: 0, str_val: sname })
    set_vreg_type(result_vreg, TYPE_INT)
    return result_vreg
}

if func_name == "reflect.SetFieldValue" {
    arg_vregs := gen_ir_eval_args(ast_idx, nodes, instrs, symtab_names, symtab_vregs)
    sname := get_vreg_struct_name(arg_vregs[0])
    result_vreg := len(instrs)
    instrs = append(instrs, IRInstr { op: OP_REFLECT_SET_FIELD_VALUE, dest: result_vreg, arg1: arg_vregs[0], arg2: arg_vregs[1], arg3: arg_vregs[2], str_val: sname })
    set_vreg_type(result_vreg, TYPE_INT)
    return result_vreg
}
```

- [ ] **Step 2: Commit IR generation**

```bash
git add bootstrap/ease/irgen/irgen.ease
git commit -m "feat(reflect): Add IR generation for reflect builtins"
```

### Task 7: Create reflect stdlib package

**Files:**
- Create: `bootstrap/ease/reflect/reflect.ease`

- [ ] **Step 1: Create the reflect package**

```bash
mkdir -p bootstrap/ease/reflect
```

These are compiler builtins — the compiler intercepts calls to `reflect.*` and emits IR opcodes directly. The function bodies are never executed; they exist only so the import system has something to resolve.

```ease
package reflect

// Type tag constants (mirror ast.ease TYPE_* constants)
const INT = 1
const BOOL = 2
const STRING = 3
const STRUCT = 7
const FLOAT = 12

fn NumField(v: int) -> int { return 0 }
fn FieldName(v: int, i: int) -> string { return "" }
fn FieldType(v: int, i: int) -> int { return 0 }
fn FieldValue(v: int, i: int) -> int { return 0 }
fn SetFieldValue(v: int, i: int, val: int) { }
```

- [ ] **Step 2: Commit reflect package**

```bash
git add bootstrap/ease/reflect/reflect.ease
git commit -m "feat(reflect): Add reflect stdlib package"
```

### Task 8: Write and run reflect tests

**Files:**
- Create: `tests/reflect_test.ease`

- [ ] **Step 1: Write reflect tests**

```ease
package main

import (
    "testing"
    "reflect"
)

struct ReflectUser {
    name: string,
    age: int,
    active: int,
}

fn TestReflectNumField(t: testing.T) {
    u := ReflectUser { name: "alice", age: 30, active: 1 }
    n := reflect.NumField(u)
    if n != 3 { t.Fatal("expected 3 fields, got " + strconv.Itoa(n)) }
}

fn TestReflectFieldName(t: testing.T) {
    u := ReflectUser { name: "alice", age: 30, active: 1 }
    n0 := reflect.FieldName(u, 0)
    if n0 != "name" { t.Fatal("expected field 0 = name, got " + n0) }
    n1 := reflect.FieldName(u, 1)
    if n1 != "age" { t.Fatal("expected field 1 = age, got " + n1) }
    n2 := reflect.FieldName(u, 2)
    if n2 != "active" { t.Fatal("expected field 2 = active, got " + n2) }
}

fn TestReflectFieldType(t: testing.T) {
    u := ReflectUser { name: "alice", age: 30, active: 1 }
    t0 := reflect.FieldType(u, 0)
    if t0 != reflect.STRING { t.Fatal("expected STRING for field 0") }
    t1 := reflect.FieldType(u, 1)
    if t1 != reflect.INT { t.Fatal("expected INT for field 1") }
}

fn TestReflectFieldValue(t: testing.T) {
    u := ReflectUser { name: "alice", age: 30, active: 1 }
    age_val := reflect.FieldValue(u, 1)
    if age_val != 30 { t.Fatal("expected age = 30, got " + strconv.Itoa(age_val)) }
    active_val := reflect.FieldValue(u, 2)
    if active_val != 1 { t.Fatal("expected active = 1") }
}

fn TestReflectSetFieldValue(t: testing.T) {
    u := ReflectUser { name: "alice", age: 30, active: 1 }
    reflect.SetFieldValue(u, 1, 99)
    if u.age != 99 { t.Fatal("expected age = 99 after SetFieldValue") }
}

fn TestReflectFieldValueString(t: testing.T) {
    u := ReflectUser { name: "alice", age: 30, active: 1 }
    name_val := reflect.FieldValue(u, 0)
    u2 := ReflectUser { name: "", age: 0, active: 0 }
    reflect.SetFieldValue(u2, 0, name_val)
    if u2.name != "alice" { t.Fatal("expected name = alice after copy via reflect") }
}
```

- [ ] **Step 2: Run tests**

```bash
./tmp/ease test tests/reflect_test.ease
```

Expected: All 6 tests pass.

- [ ] **Step 3: Run full test suite**

```bash
./tmp/ease test tests/
```

Expected: All tests pass.

- [ ] **Step 4: Bootstrap convergence and seed update**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease2
./tmp/ease2 bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease3
diff <(./tmp/ease2 bootstrap/compiler.ease 2>&1) <(./tmp/ease3 bootstrap/compiler.ease 2>&1)
cp tmp/output.ll bootstrap/seed.ll
```

- [ ] **Step 5: Commit**

```bash
git add tests/reflect_test.ease bootstrap/ease/irgen/irgen.ease bootstrap/seed.ll
git commit -m "feat(reflect): Add reflect tests and verify field introspection"
```

---

## Chunk 5: JSON Value Type + New API

### Task 9: Write Value API tests first (TDD)

**Files:**
- Modify: `tests/json_test.ease`

- [ ] **Step 1: Add Value API tests**

Append to `tests/json_test.ease`. These tests use the new API that will be implemented in Task 10.

```ease
fn TestJsonValueStr(t: testing.T) {
    v := json.Str("hello")
    if v.Type() != json.TYPE_STRING { t.Fatal("expected TYPE_STRING") }
    if v.String() != "hello" { t.Fatal("expected hello") }
}

fn TestJsonValueNum(t: testing.T) {
    v := json.Num(42)
    if v.Type() != json.TYPE_NUM { t.Fatal("expected TYPE_NUM") }
    if v.Int() != 42 { t.Fatal("expected 42") }
}

fn TestJsonValueBool(t: testing.T) {
    v := json.Bool(1)
    if v.Type() != json.TYPE_BOOL { t.Fatal("expected TYPE_BOOL") }
    if v.Bool() != 1 { t.Fatal("expected 1") }
}

fn TestJsonValueNull(t: testing.T) {
    v := json.Null()
    if v.Type() != json.TYPE_NULL { t.Fatal("expected TYPE_NULL") }
    if v.IsNull() != 1 { t.Fatal("expected IsNull() == 1") }
}

fn TestJsonValueObject(t: testing.T) {
    doc := json.Object()
    doc.Set("name", json.Str("alice"))
    doc.Set("age", json.Num(30))
    if doc.Get("name").String() != "alice" { t.Fatal("expected alice") }
    if doc.Get("age").Int() != 30 { t.Fatal("expected 30") }
    if doc.Has("name") != 1 { t.Fatal("expected Has(name) == 1") }
    if doc.Has("missing") != 0 { t.Fatal("expected Has(missing) == 0") }
}

fn TestJsonValueSetUpdate(t: testing.T) {
    doc := json.Object()
    doc.Set("key", json.Str("old"))
    doc.Set("key", json.Str("new"))
    if doc.Get("key").String() != "new" { t.Fatal("expected updated value") }
    if doc.Len() != 1 { t.Fatal("expected len 1 after update") }
}

fn TestJsonValueArray(t: testing.T) {
    arr := json.Array()
    arr.Push(json.Str("a"))
    arr.Push(json.Num(1))
    if arr.Len() != 2 { t.Fatal("expected len 2") }
    if arr.At(0).String() != "a" { t.Fatal("expected a") }
    if arr.At(1).Int() != 1 { t.Fatal("expected 1") }
}

fn TestJsonValueNested(t: testing.T) {
    doc := json.Object()
    doc.Set("name", json.Str("ease"))
    inner := json.Object()
    inner.Set("type", json.Str("lang"))
    doc.Set("meta", inner)
    m := doc.Get("meta")
    if m.Get("type").String() != "lang" { t.Fatal("expected lang") }
}

fn TestJsonValueMarshal(t: testing.T) {
    doc := json.Object()
    doc.Set("key", json.Str("value"))
    doc.Set("num", json.Num(42))
    output := doc.Marshal()
    expected := "{\"key\":\"value\",\"num\":42}"
    if output != expected { t.Fatal("expected " + expected + ", got " + output) }
}

fn TestJsonValueArrayMarshal(t: testing.T) {
    arr := json.Array()
    arr.Push(json.Str("hello"))
    arr.Push(json.Num(1))
    arr.Push(json.Bool(1))
    output := arr.Marshal()
    expected := "[\"hello\",1,true]"
    if output != expected { t.Fatal("expected " + expected + ", got " + output) }
}

fn TestJsonGetNonexistent(t: testing.T) {
    doc := json.Object()
    v := doc.Get("missing")
    if v.IsNull() != 1 { t.Fatal("expected null for missing key") }
}

fn TestJsonAtOutOfBounds(t: testing.T) {
    arr := json.Array()
    arr.Push(json.Num(1))
    v := arr.At(5)
    if v.IsNull() != 1 { t.Fatal("expected null for out of bounds") }
}
```

- [ ] **Step 2: Verify tests fail (TDD red phase)**

```bash
./tmp/ease test tests/json_test.ease
```

Expected: Compilation errors (json.Str, json.Value, etc. not found yet).

- [ ] **Step 3: Commit test file**

```bash
git add tests/json_test.ease
git commit -m "test(json): Add Value API tests (TDD red phase)"
```

### Task 10: Implement Value struct and API

**Files:**
- Modify: `bootstrap/ease/json/json.ease`

**CRITICAL**: The new `TYPE_*` constants MUST match the existing internal `json_*` constants exactly, or the internal parallel-array storage will corrupt marshal output when mixing old and new APIs:

| Existing internal | Value | New public constant |
|---|---|---|
| `json_null = 0` | 0 | `TYPE_NULL = 0` |
| `json_string = 1` | 1 | `TYPE_STRING = 1` |
| `json_int = 2` | 2 | `TYPE_NUM = 2` |
| `json_bool = 3` | 3 | `TYPE_BOOL = 3` |
| `json_object = 4` | 4 | `TYPE_OBJECT = 4` |
| `json_array = 5` | 5 | `TYPE_ARRAY = 5` |

- [ ] **Step 1: Add Value struct and type constants**

At the top of `bootstrap/ease/json/json.ease`, after the existing internal constants (line 9) and before `struct Doc` (line 11), add:

```ease
// Public type constants (must match internal json_* constants above)
const TYPE_NULL = 0
const TYPE_STRING = 1
const TYPE_NUM = 2
const TYPE_BOOL = 3
const TYPE_OBJECT = 4
const TYPE_ARRAY = 5

struct Value {
    type_tag: int,
    data: int,
}
```

- [ ] **Step 2: Add constructor functions**

After the existing constructors (`New`, `NewArray`), add:

```ease
fn Str(s: string) -> Value {
    return Value { type_tag: TYPE_STRING, data: s }
}

fn Num(n: int) -> Value {
    return Value { type_tag: TYPE_NUM, data: n }
}

fn Bool(b: int) -> Value {
    return Value { type_tag: TYPE_BOOL, data: b }
}

fn Null() -> Value {
    return Value { type_tag: TYPE_NULL, data: 0 }
}

fn Object() -> Value {
    doc_id := g_doc_count
    g_doc_count = g_doc_count + 1
    g_doc_is_array = append(g_doc_is_array, 0)
    return Value { type_tag: TYPE_OBJECT, data: doc_id }
}

fn Array() -> Value {
    doc_id := g_doc_count
    g_doc_count = g_doc_count + 1
    g_doc_is_array = append(g_doc_is_array, 1)
    return Value { type_tag: TYPE_ARRAY, data: doc_id }
}
```

- [ ] **Step 3: Add Value getter methods**

```ease
fn (v: Value) String() -> string {
    if v.type_tag != TYPE_STRING { return "" }
    return v.data
}

fn (v: Value) Int() -> int {
    if v.type_tag != TYPE_NUM { return 0 }
    return v.data
}

fn (v: Value) Bool() -> int {
    if v.type_tag != TYPE_BOOL { return 0 }
    return v.data
}

fn (v: Value) Type() -> int {
    return v.type_tag
}

fn (v: Value) IsNull() -> int {
    if v.type_tag == TYPE_NULL { return 1 }
    return 0
}
```

- [ ] **Step 4: Add Value object/array methods**

```ease
fn (v: Value) Set(key: string, val: Value) {
    if v.type_tag != TYPE_OBJECT { return }
    doc_id := v.data
    // Update existing key if present
    i := 0
    for i < len(g_entry_doc) {
        if g_entry_doc[i] == doc_id && g_entry_key[i] == key {
            g_entry_type[i] = val.type_tag
            if val.type_tag == TYPE_STRING {
                g_entry_str[i] = val.data
            } else {
                g_entry_int[i] = val.data
            }
            return
        }
        i = i + 1
    }
    // New entry
    g_entry_doc = append(g_entry_doc, doc_id)
    g_entry_key = append(g_entry_key, key)
    g_entry_type = append(g_entry_type, val.type_tag)
    if val.type_tag == TYPE_STRING {
        g_entry_str = append(g_entry_str, val.data)
        g_entry_int = append(g_entry_int, 0)
    } else {
        g_entry_str = append(g_entry_str, "")
        g_entry_int = append(g_entry_int, val.data)
    }
}

fn (v: Value) Get(key: string) -> Value {
    if v.type_tag != TYPE_OBJECT { return Value { type_tag: TYPE_NULL, data: 0 } }
    doc_id := v.data
    i := 0
    for i < len(g_entry_doc) {
        if g_entry_doc[i] == doc_id && g_entry_key[i] == key {
            etype := g_entry_type[i]
            if etype == TYPE_STRING {
                return Value { type_tag: TYPE_STRING, data: g_entry_str[i] }
            }
            return Value { type_tag: etype, data: g_entry_int[i] }
        }
        i = i + 1
    }
    return Value { type_tag: TYPE_NULL, data: 0 }
}

fn (v: Value) Has(key: string) -> int {
    if v.type_tag != TYPE_OBJECT { return 0 }
    doc_id := v.data
    i := 0
    for i < len(g_entry_doc) {
        if g_entry_doc[i] == doc_id && g_entry_key[i] == key { return 1 }
        i = i + 1
    }
    return 0
}

fn (v: Value) At(index: int) -> Value {
    if v.type_tag != TYPE_ARRAY { return Value { type_tag: TYPE_NULL, data: 0 } }
    doc_id := v.data
    count := 0
    i := 0
    for i < len(g_entry_doc) {
        if g_entry_doc[i] == doc_id {
            if count == index {
                etype := g_entry_type[i]
                if etype == TYPE_STRING {
                    return Value { type_tag: TYPE_STRING, data: g_entry_str[i] }
                }
                return Value { type_tag: etype, data: g_entry_int[i] }
            }
            count = count + 1
        }
        i = i + 1
    }
    return Value { type_tag: TYPE_NULL, data: 0 }
}

fn (v: Value) Len() -> int {
    if v.type_tag != TYPE_OBJECT && v.type_tag != TYPE_ARRAY { return 0 }
    doc_id := v.data
    count := 0
    i := 0
    for i < len(g_entry_doc) {
        if g_entry_doc[i] == doc_id { count = count + 1 }
        i = i + 1
    }
    return count
}

fn (v: Value) Push(val: Value) {
    if v.type_tag != TYPE_ARRAY { return }
    doc_id := v.data
    g_entry_doc = append(g_entry_doc, doc_id)
    g_entry_key = append(g_entry_key, "")
    g_entry_type = append(g_entry_type, val.type_tag)
    if val.type_tag == TYPE_STRING {
        g_entry_str = append(g_entry_str, val.data)
        g_entry_int = append(g_entry_int, 0)
    } else {
        g_entry_str = append(g_entry_str, "")
        g_entry_int = append(g_entry_int, val.data)
    }
}
```

- [ ] **Step 5: Add Value.Marshal method**

```ease
fn (v: Value) Marshal() -> string {
    if v.type_tag == TYPE_OBJECT || v.type_tag == TYPE_ARRAY {
        d := Doc { id: v.data }
        return d.Marshal()
    }
    if v.type_tag == TYPE_STRING { return "\"" + EscapeString(v.data) + "\"" }
    if v.type_tag == TYPE_NUM { return strconv.Itoa(v.data) }
    if v.type_tag == TYPE_BOOL {
        if v.data != 0 { return "true" }
        return "false"
    }
    return "null"
}
```

- [ ] **Step 6: Add ParseValue function**

```ease
fn ParseValue(input: string) -> Value {
    doc := Parse(input)
    if g_doc_is_array[doc.id] == 1 {
        return Value { type_tag: TYPE_ARRAY, data: doc.id }
    }
    return Value { type_tag: TYPE_OBJECT, data: doc.id }
}
```

- [ ] **Step 7: Run tests (TDD green phase)**

```bash
./tmp/ease test tests/json_test.ease
```

Expected: All old tests pass + all new Value tests pass.

- [ ] **Step 8: Run full suite**

```bash
./tmp/ease test tests/
```

- [ ] **Step 9: Commit**

```bash
git add bootstrap/ease/json/json.ease tests/json_test.ease
git commit -m "feat(json): Add unified Value type with constructors and methods"
```

---

## Chunk 6: Parse round-trip tests + backward compat

### Task 11: Add parse round-trip tests

**Files:**
- Modify: `tests/json_test.ease`

- [ ] **Step 1: Add parse tests**

```ease
fn TestJsonParseValue(t: testing.T) {
    doc := json.ParseValue("{\"name\":\"alice\",\"age\":30}")
    if doc.Get("name").String() != "alice" { t.Fatal("expected alice") }
    if doc.Get("age").Int() != 30 { t.Fatal("expected 30") }
}

fn TestJsonParseValueArray(t: testing.T) {
    arr := json.ParseValue("[1,2,3]")
    if arr.Len() != 3 { t.Fatal("expected len 3") }
    if arr.At(0).Int() != 1 { t.Fatal("expected 1") }
    if arr.At(2).Int() != 3 { t.Fatal("expected 3") }
}

fn TestJsonParseValueNested(t: testing.T) {
    doc := json.ParseValue("{\"inner\":{\"key\":\"val\"}}")
    inner := doc.Get("inner")
    if inner.Get("key").String() != "val" { t.Fatal("expected val") }
}

fn TestJsonRoundTrip(t: testing.T) {
    doc := json.Object()
    doc.Set("name", json.Str("ease"))
    doc.Set("version", json.Num(1))
    s := doc.Marshal()
    parsed := json.ParseValue(s)
    if parsed.Get("name").String() != "ease" { t.Fatal("round trip name failed") }
    if parsed.Get("version").Int() != 1 { t.Fatal("round trip version failed") }
}
```

- [ ] **Step 2: Run tests**

```bash
./tmp/ease test tests/json_test.ease
```

Expected: All tests pass.

- [ ] **Step 3: Verify old API backward compatibility**

All 13 original tests (`TestJsonNewObject`, `TestJsonSetInt`, etc.) should still pass since the old `Doc` methods are unchanged.

- [ ] **Step 4: Commit**

```bash
git add tests/json_test.ease
git commit -m "feat(json): Add ParseValue and round-trip tests"
```

---

## Chunk 7: json.Marshal and json.Unmarshal Compiler Builtins

### Task 12: Add Marshal/Unmarshal opcode constants

**Files:**
- Modify: `bootstrap/ease/ir/ir.ease`

- [ ] **Step 1: Add opcode constants**

After `const OP_REFLECT_SET_FIELD_VALUE = 92`, add:

```ease
const OP_JSON_MARSHAL = 93
const OP_JSON_UNMARSHAL = 94
```

- [ ] **Step 2: Commit**

```bash
git add bootstrap/ease/ir/ir.ease
git commit -m "feat(json): Add Marshal/Unmarshal IR opcode constants"
```

### Task 13: Add type instantiation lookup to sema

**Files:**
- Modify: `bootstrap/ease/sema/sema.ease`

For `json.Unmarshal[User](s)`, the parser calls `RecordTypeInstantiation("Unmarshal", 0, "User", line)`. We need a way to look up the concrete type by decl_name during IR generation.

- [ ] **Step 1: Add LookupTypeInstantiation function**

In `bootstrap/ease/sema/sema.ease`, after `RecordTypeInstantiation` (line ~923), add:

```ease
fn LookupTypeInstantiation(decl_name: string, param_idx: int) -> string {
    // Search backwards to find the most recent instantiation
    i := len(g_type_inst_decl_names) - 1
    for i >= 0 {
        if g_type_inst_decl_names[i] == decl_name && g_type_inst_param_idx[i] == param_idx {
            return g_type_inst_concrete[i]
        }
        i = i - 1
    }
    return ""
}
```

Note: Searching backwards returns the most recent instantiation, which handles cases where `Unmarshal` is called multiple times with different types. Since IR generation processes statements in order, the most recent recording corresponds to the current call being generated.

- [ ] **Step 2: Commit**

```bash
git add bootstrap/ease/sema/sema.ease
git commit -m "feat(json): Add type instantiation lookup for Unmarshal"
```

### Task 14: Add Marshal/Unmarshal IR generation

**Files:**
- Modify: `bootstrap/ease/irgen/irgen.ease`

- [ ] **Step 1: Add json.Marshal IR generation**

In the builtin dispatch chain (after the reflect builtins added in Task 6):

```ease
if func_name == "json.Marshal" {
    arg_vregs := gen_ir_eval_args(ast_idx, nodes, instrs, symtab_names, symtab_vregs)
    sname := get_vreg_struct_name(arg_vregs[0])
    result_vreg := len(instrs)
    instrs = append(instrs, IRInstr { op: OP_JSON_MARSHAL, dest: result_vreg, arg1: arg_vregs[0], arg2: 0, arg3: 0, str_val: sname })
    set_vreg_type(result_vreg, TYPE_STRING)
    return result_vreg
}
```

- [ ] **Step 2: Add json.Unmarshal IR generation**

```ease
if func_name == "json.Unmarshal" {
    arg_vregs := gen_ir_eval_args(ast_idx, nodes, instrs, symtab_names, symtab_vregs)
    // Get the type argument from the type instantiation registry
    // The parser recorded: RecordTypeInstantiation("Unmarshal", 0, "User", line)
    sname := sema.LookupTypeInstantiation("Unmarshal", 0)
    result_vreg := len(instrs)
    instrs = append(instrs, IRInstr { op: OP_JSON_UNMARSHAL, dest: result_vreg, arg1: arg_vregs[0], arg2: 0, arg3: 0, str_val: sname })
    set_vreg_type(result_vreg, TYPE_STRUCT)
    set_vreg_struct_name(result_vreg, sname)
    return result_vreg
}
```

- [ ] **Step 3: Commit**

```bash
git add bootstrap/ease/irgen/irgen.ease
git commit -m "feat(json): Add IR generation for Marshal/Unmarshal builtins"
```

### Task 15: Add Marshal/Unmarshal codegen + collect_vregs

**Files:**
- Modify: `bootstrap/ease/llvm/llvm.ease`

The codegen generates the JSON serialization/deserialization inline, using compile-time-known struct metadata. This avoids the runtime type resolution problem — everything is resolved at compile time.

- [ ] **Step 1: Add to collect_vregs**

Add `OP_JSON_MARSHAL` and `OP_JSON_UNMARSHAL` to the dest vreg list (line ~306):

```ease
|| op == OP_JSON_MARSHAL || op == OP_JSON_UNMARSHAL
```

arg1 is automatically included (not in exclusion list).

- [ ] **Step 2: Implement OP_JSON_MARSHAL codegen**

This generates an unrolled loop that builds the JSON string by directly referencing the struct's reflect metadata. The field count is known at compile time from the struct registry.

```ease
if terminated == 0 && instr.op == OP_JSON_MARSHAL {
    sname := instr.str_val
    struct_ptr := emit_load_vreg(fd, instr.arg1, ti, 0)

    // Get field count from struct registry (compile time)
    count := 0
    start := 0
    si := 0
    for si < len(g_struct_reg_names) {
        if g_struct_reg_names[si] == sname {
            count = g_struct_reg_field_counts[si]
            start = g_struct_reg_field_starts[si]
        }
        si = si + 1
    }

    // Start with "{"
    result := tmp_name(ti, 1)
    write_str(fd, "  " + result + " = call i64 @ease_make_string(ptr @.str.json.lbrace)\n")

    tmpi := 2
    fi := 0
    for fi < count {
        fname := g_struct_reg_field_names[start + fi]
        ftype := g_struct_reg_field_types[start + fi]

        // Add comma separator for fields after the first
        if fi > 0 {
            comma := tmp_name(ti, tmpi)
            write_str(fd, "  " + comma + " = call i64 @ease_make_string(ptr @.str.json.comma)\n")
            tmpi = tmpi + 1
            new_result := tmp_name(ti, tmpi)
            write_str(fd, "  " + new_result + " = call i64 @str_concat(i64 " + result + ", i64 " + comma + ")\n")
            result = new_result
            tmpi = tmpi + 1
        }

        // Add "fieldname":
        key_str := tmp_name(ti, tmpi)
        write_str(fd, "  " + key_str + " = ptrtoint ptr @reflect." + sname + ".fname." + strconv.Itoa(fi) + " to i64\n")
        tmpi = tmpi + 1
        quoted_key := tmp_name(ti, tmpi)
        write_str(fd, "  " + quoted_key + " = call i64 @json_quote_key(i64 " + key_str + ")\n")
        tmpi = tmpi + 1
        new_result := tmp_name(ti, tmpi)
        write_str(fd, "  " + new_result + " = call i64 @str_concat(i64 " + result + ", i64 " + quoted_key + ")\n")
        result = new_result
        tmpi = tmpi + 1

        // Load field value from struct_ptr + fi * 8
        offset_str := strconv.Itoa(fi * 8)
        field_addr := tmp_name(ti, tmpi)
        write_str(fd, "  " + field_addr + " = add i64 " + struct_ptr + ", " + offset_str + "\n")
        tmpi = tmpi + 1
        field_ptr := tmp_name(ti, tmpi)
        write_str(fd, "  " + field_ptr + " = inttoptr i64 " + field_addr + " to ptr\n")
        tmpi = tmpi + 1
        field_val := tmp_name(ti, tmpi)
        write_str(fd, "  " + field_val + " = load i64, ptr " + field_ptr + "\n")
        tmpi = tmpi + 1

        // Format based on type
        formatted := tmp_name(ti, tmpi)
        if ftype == TYPE_STRING {
            write_str(fd, "  " + formatted + " = call i64 @json_quote_string(i64 " + field_val + ")\n")
        }
        if ftype == TYPE_INT {
            write_str(fd, "  " + formatted + " = call i64 @strconv_Itoa(i64 " + field_val + ")\n")
        }
        if ftype == TYPE_BOOL {
            write_str(fd, "  " + formatted + " = call i64 @json_format_bool(i64 " + field_val + ")\n")
        }
        tmpi = tmpi + 1

        new_result2 := tmp_name(ti, tmpi)
        write_str(fd, "  " + new_result2 + " = call i64 @str_concat(i64 " + result + ", i64 " + formatted + ")\n")
        result = new_result2
        tmpi = tmpi + 1

        fi = fi + 1
    }

    // Append "}"
    rbrace := tmp_name(ti, tmpi)
    write_str(fd, "  " + rbrace + " = call i64 @ease_make_string(ptr @.str.json.rbrace)\n")
    tmpi = tmpi + 1
    final_result := tmp_name(ti, tmpi)
    write_str(fd, "  " + final_result + " = call i64 @str_concat(i64 " + result + ", i64 " + rbrace + ")\n")
    emit_store_vreg(fd, instr.dest, final_result)
}
```

**Important**: This codegen requires helper functions that must be declared/defined in the json module:
- `@json_quote_key(i64 str) -> i64` — returns `"key":` (quoted key with colon)
- `@json_quote_string(i64 str) -> i64` — returns `"value"` (quoted and escaped string)
- `@json_format_bool(i64 val) -> i64` — returns `"true"` or `"false"`
- `@ease_make_string(ptr cstr) -> i64` — creates Ease string from C string (may use ptrtoint)
- `@str_concat(i64, i64) -> i64` — existing string concatenation
- `@strconv_Itoa(i64) -> i64` — existing int-to-string

Also emit the constant strings for `{`, `}`, `,` if they don't already exist:
```
@.str.json.lbrace = private unnamed_addr constant [2 x i8] c"{\00"
@.str.json.rbrace = private unnamed_addr constant [2 x i8] c"}\00"
@.str.json.comma = private unnamed_addr constant [2 x i8] c",\00"
```

The implementer should add these string constants to the emit section, and add the helper functions (`json_quote_key`, `json_quote_string`, `json_format_bool`) to `bootstrap/ease/json/json.ease`.

- [ ] **Step 3: Implement OP_JSON_UNMARSHAL codegen**

Uses the `GetField` runtime helper (defined in Step 4) to avoid type branching in LLVM IR. Each field calls `@json_GetField(doc_id, key, type_tag) -> i64` which handles all types internally.

```ease
if terminated == 0 && instr.op == OP_JSON_UNMARSHAL {
    sname := instr.str_val
    json_str := emit_load_vreg(fd, instr.arg1, ti, 0)

    // Allocate struct
    size := struct_size(sname)
    struct_ptr := tmp_name(ti, 1)
    write_str(fd, "  " + struct_ptr + " = call ptr @malloc(i64 " + strconv.Itoa(size) + ")\n")
    struct_i64 := tmp_name(ti, 2)
    write_str(fd, "  " + struct_i64 + " = ptrtoint ptr " + struct_ptr + " to i64\n")

    // Parse JSON to a Doc
    doc_val := tmp_name(ti, 3)
    write_str(fd, "  " + doc_val + " = call i64 @json_Parse(i64 " + json_str + ")\n")

    // For each field, look up key in parsed doc and store value
    count := 0
    start := 0
    si := 0
    for si < len(g_struct_reg_names) {
        if g_struct_reg_names[si] == sname {
            count = g_struct_reg_field_counts[si]
            start = g_struct_reg_field_starts[si]
        }
        si = si + 1
    }

    tmpi := 4
    fi := 0
    for fi < count {
        ftype := g_struct_reg_field_types[start + fi]

        // Get field name as string
        key_str := tmp_name(ti, tmpi)
        write_str(fd, "  " + key_str + " = ptrtoint ptr @reflect." + sname + ".fname." + strconv.Itoa(fi) + " to i64\n")
        tmpi = tmpi + 1

        // Call GetField(doc_id, key, type_tag) — single helper handles all types
        type_tag_str := strconv.Itoa(ftype)
        field_val := tmp_name(ti, tmpi)
        write_str(fd, "  " + field_val + " = call i64 @json_GetField(i64 " + doc_val + ", i64 " + key_str + ", i64 " + type_tag_str + ")\n")
        tmpi = tmpi + 1

        // Store at struct_ptr + fi * 8
        offset_str := strconv.Itoa(fi * 8)
        store_addr := tmp_name(ti, tmpi)
        write_str(fd, "  " + store_addr + " = add i64 " + struct_i64 + ", " + offset_str + "\n")
        tmpi = tmpi + 1
        store_ptr := tmp_name(ti, tmpi)
        write_str(fd, "  " + store_ptr + " = inttoptr i64 " + store_addr + " to ptr\n")
        tmpi = tmpi + 1
        write_str(fd, "  store i64 " + field_val + ", ptr " + store_ptr + "\n")

        fi = fi + 1
    }

    emit_store_vreg(fd, instr.dest, struct_i64)
}
```

- [ ] **Step 4: Add json helper functions**

In `bootstrap/ease/json/json.ease`, add:

```ease
// Helper: quote a key for JSON output: "key":
fn QuoteKey(key: string) -> string {
    return "\"" + key + "\":"
}

// Helper: quote and escape a string value: "value"
fn QuoteString(s: string) -> string {
    return "\"" + EscapeString(s) + "\""
}

// Helper: format bool as JSON
fn FormatBool(val: int) -> string {
    if val != 0 { return "true" }
    return "false"
}

// Helper: get field value from parsed doc by key and type
fn GetField(doc_id: int, key: string, type_tag: int) -> int {
    d := Doc { id: doc_id }
    if type_tag == TYPE_STRING { return d.GetString(key) }
    if type_tag == TYPE_INT { return d.GetInt(key) }
    if type_tag == TYPE_BOOL { return d.GetBool(key) }
    return 0
}
```

- [ ] **Step 5: Bootstrap convergence**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease2
./tmp/ease2 bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease3
diff <(./tmp/ease2 bootstrap/compiler.ease 2>&1) <(./tmp/ease3 bootstrap/compiler.ease 2>&1)
```

Run gen4 if needed. Do NOT update seed.ll yet — that happens in Task 17.

- [ ] **Step 6: Commit**

```bash
git add bootstrap/ease/llvm/llvm.ease bootstrap/ease/json/json.ease bootstrap/ease/irgen/irgen.ease
git commit -m "feat(json): Implement Marshal/Unmarshal codegen and helpers"
```

### Task 16: Write Marshal/Unmarshal tests

**Files:**
- Modify: `tests/json_test.ease`

- [ ] **Step 1: Add marshal test struct and tests**

```ease
struct MarshalUser {
    name: string,
    age: int,
}

fn TestJsonMarshalStruct(t: testing.T) {
    u := MarshalUser { name: "alice", age: 30 }
    s := json.Marshal(u)
    expected := "{\"name\":\"alice\",\"age\":30}"
    if s != expected { t.Fatal("expected " + expected + ", got " + s) }
}

fn TestJsonUnmarshalStruct(t: testing.T) {
    s := "{\"name\":\"bob\",\"age\":25}"
    u := json.Unmarshal[MarshalUser](s)
    if u.name != "bob" { t.Fatal("expected name = bob, got " + u.name) }
    if u.age != 25 { t.Fatal("expected age = 25") }
}

fn TestJsonUnmarshalMissingFields(t: testing.T) {
    s := "{\"name\":\"carol\"}"
    u := json.Unmarshal[MarshalUser](s)
    if u.name != "carol" { t.Fatal("expected name = carol") }
    if u.age != 0 { t.Fatal("expected age = 0 for missing field") }
}
```

- [ ] **Step 2: Run tests**

```bash
./tmp/ease test tests/json_test.ease
```

Expected: All tests pass.

- [ ] **Step 3: Run full suite**

```bash
./tmp/ease test tests/
```

- [ ] **Step 4: Commit**

```bash
git add tests/json_test.ease
git commit -m "feat(json): Add tests for struct Marshal/Unmarshal"
```

---

## Chunk 8: Final Convergence + Docs

### Task 17: Final bootstrap convergence

**Files:**
- Modify: `bootstrap/seed.ll`

- [ ] **Step 1: Full convergence check**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease2
./tmp/ease2 bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease3
diff <(./tmp/ease2 bootstrap/compiler.ease 2>&1) <(./tmp/ease3 bootstrap/compiler.ease 2>&1)
```

If gen2 != gen3, run gen4:
```bash
./tmp/ease3 bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease4
diff <(./tmp/ease3 bootstrap/compiler.ease 2>&1) <(./tmp/ease4 bootstrap/compiler.ease 2>&1)
```

- [ ] **Step 2: Update seed.ll with converged output**

```bash
cp tmp/output.ll bootstrap/seed.ll
```

Verify SEED OK:
```bash
clang -O1 bootstrap/seed.ll -o tmp/ease_seed
./tmp/ease_seed bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease_check
diff <(./tmp/ease_check bootstrap/compiler.ease 2>&1) <(./tmp/ease_seed bootstrap/compiler.ease 2>&1)
```

- [ ] **Step 3: Run full test suite one final time**

```bash
./tmp/ease test tests/
```

Expected: All tests pass (should be ~240+ tests now).

- [ ] **Step 4: Commit seed**

```bash
git add bootstrap/seed.ll
git commit -m "chore: Update seed.ll for json redesign + reflect convergence"
```

### Task 18: Update documentation

**Files:**
- Modify: `docs/implementation-status.md`

- [ ] **Step 1: Update implementation-status.md**

In the "Standard Library" line (line 15), add `reflect` to the stdlib list:
```
- Standard library (strings, strconv, io, os, testing, time, result, json, reflect, path, lsp — all pure Ease implementations)
```

Update the test count line (line 25) to the new total.

In the "Future Work" section, add:
```
- [x] reflect - runtime struct field introspection (NumField, FieldName, FieldType, FieldValue, SetFieldValue)
```

- [ ] **Step 2: Commit docs**

```bash
git add docs/implementation-status.md
git commit -m "docs: Update status for json redesign + reflect package"
```
