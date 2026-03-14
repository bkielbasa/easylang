# Slice Syntax Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Go-style slice expressions (`arr[start:end]`, `arr[start:]`, `arr[:end]`, `arr[:]`) with shared memory semantics.

**Architecture:** New `EXPR_SLICE` AST node parsed in the `[...]` postfix handler, lowered to `OP_ARRAY_SLICE` IR opcode, compiled to LLVM IR that creates a new 24-byte fat pointer sharing the original data buffer. The parser checks for `:` inside brackets to distinguish slices from index access.

**Tech Stack:** Ease self-hosting compiler (parser → irgen → LLVM IR codegen)

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `bootstrap/ease/ast/ast.ease` | Modify (line 59) | Add `EXPR_SLICE = 61` constant |
| `bootstrap/ease/ir/ir.ease` | Modify (line 128) | Add `OP_ARRAY_SLICE = 87` constant |
| `bootstrap/ease/parser/parser.ease` | Modify (lines 2976-3010) | Parse `arr[start:end]` variants, emit `EXPR_SLICE` |
| `bootstrap/ease/irgen/irgen.ease` | Modify (lines 1721-1723) | Add `gen_ir_slice()` function, dispatch from `EXPR_SLICE` |
| `bootstrap/ease/llvm/llvm.ease` | Modify (after OP_ARRAY_LEN handler ~line 829) | Emit LLVM IR for `OP_ARRAY_SLICE` |
| `tests/arrays_test.ease` | Modify | Add 9 slice tests |

---

## Chunk 1: Constants and Tests

### Task 1: Add AST and IR constants

**Files:**
- Modify: `bootstrap/ease/ast/ast.ease:59`
- Modify: `bootstrap/ease/ir/ir.ease:128`

- [ ] **Step 1: Add EXPR_SLICE constant**

In `bootstrap/ease/ast/ast.ease`, after line 59 (`const EXPR_TO_STRING = 60`), add:

```ease
const EXPR_SLICE = 61
```

- [ ] **Step 2: Add OP_ARRAY_SLICE constant**

In `bootstrap/ease/ir/ir.ease`, after line 128 (`const OP_BOOL_TO_STR = 86`), add:

```ease
const OP_ARRAY_SLICE = 87
```

- [ ] **Step 3: Verify compiler still builds**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease 2>&1 | grep -v warning
```

Expected: builds without errors (new constants are unused so far).

- [ ] **Step 4: Commit**

```bash
git add bootstrap/ease/ast/ast.ease bootstrap/ease/ir/ir.ease
git commit -m "feat(lang): Add EXPR_SLICE and OP_ARRAY_SLICE constants"
```

### Task 2: Write failing slice tests

**Files:**
- Modify: `tests/arrays_test.ease`

- [ ] **Step 1: Add all slice tests**

Append to `tests/arrays_test.ease` (after the last test, before the final blank line):

```ease
fn TestSliceBasic(t: testing.T) {
    arr := []int{10, 20, 30, 40, 50}
    s := arr[1:3]
    if len(s) != 2 { t.Fatal("expected len 2, got " + strconv.Itoa(len(s))) }
    if s[0] != 20 { t.Fatal("expected s[0]=20") }
    if s[1] != 30 { t.Fatal("expected s[1]=30") }
}

fn TestSliceFromStart(t: testing.T) {
    arr := []int{10, 20, 30, 40, 50}
    s := arr[:2]
    if len(s) != 2 { t.Fatal("expected len 2") }
    if s[0] != 10 { t.Fatal("expected s[0]=10") }
    if s[1] != 20 { t.Fatal("expected s[1]=20") }
}

fn TestSliceToEnd(t: testing.T) {
    arr := []int{10, 20, 30, 40, 50}
    s := arr[3:]
    if len(s) != 2 { t.Fatal("expected len 2") }
    if s[0] != 40 { t.Fatal("expected s[0]=40") }
    if s[1] != 50 { t.Fatal("expected s[1]=50") }
}

fn TestSliceFull(t: testing.T) {
    arr := []int{10, 20, 30}
    s := arr[:]
    if len(s) != 3 { t.Fatal("expected len 3") }
    if s[0] != 10 { t.Fatal("expected s[0]=10") }
}

fn TestSliceSharedMemory(t: testing.T) {
    arr := []int{10, 20, 30, 40, 50}
    s := arr[1:4]
    s[0] = 99
    if arr[1] != 99 { t.Fatal("expected arr[1]=99 after slice mutation") }
}

fn TestSliceLen(t: testing.T) {
    arr := []int{10, 20, 30, 40, 50}
    s := arr[1:4]
    if len(s) != 3 { t.Fatal("expected len 3, got " + strconv.Itoa(len(s))) }
}

fn TestSliceAppend(t: testing.T) {
    arr := []int{10, 20, 30}
    s := arr[0:2]
    s = append(s, 99)
    if len(s) != 3 { t.Fatal("expected len 3") }
    if s[2] != 99 { t.Fatal("expected s[2]=99") }
}

fn TestSliceOfStrings(t: testing.T) {
    arr := []string{"a", "b", "c", "d"}
    s := arr[1:3]
    if len(s) != 2 { t.Fatal("expected len 2") }
    if s[0] != "b" { t.Fatal("expected s[0]=b") }
    if s[1] != "c" { t.Fatal("expected s[1]=c") }
}

fn TestSliceOfSlice(t: testing.T) {
    arr := []int{10, 20, 30, 40, 50}
    s1 := arr[1:4]
    s2 := s1[0:2]
    if len(s2) != 2 { t.Fatal("expected len 2") }
    if s2[0] != 20 { t.Fatal("expected s2[0]=20") }
    if s2[1] != 30 { t.Fatal("expected s2[1]=30") }
}
```

- [ ] **Step 2: Verify tests fail (compiler can't parse slice syntax yet)**

```bash
./tmp/ease test tests/ 2>&1 | tail -5
```

Expected: Compilation error (parser rejects `arr[1:3]` because it expects `]` after index expression but finds `:`).

- [ ] **Step 3: Commit failing tests**

```bash
git add tests/arrays_test.ease
git commit -m "test(lang): Add failing slice syntax tests"
```

---

## Chunk 2: Parser

### Task 3: Parse slice syntax

**Files:**
- Modify: `bootstrap/ease/parser/parser.ease:2976-3010`

The current code at lines 2976-3010 handles `EXPR_INDEX`. We need to modify it to detect `:` and emit `EXPR_SLICE` instead.

- [ ] **Step 1: Replace the array indexing block**

Find the block starting at line 2976 (`if is_generic_args == 0 {`) through line 3010 (the closing `}`). Replace the ENTIRE content between `if is_generic_args == 0 {` and its closing `}` (line 3010) with:

```ease
                    if is_generic_args == 0 {
                    // Array indexing or slice
                    array_node := current_node
                    p = lex_end(src, p, next_kind)  // Skip '['
                    p = lex_skip(src, p)

                    // Check for [:end] or [:] — colon before any expression
                    if lex_kind(src, p) == TK_COLON {
                        // Slice with default start
                        p = lex_end(src, p, TK_COLON)
                        p = lex_skip(src, p)
                        end_node := -1
                        if lex_kind(src, p) != TK_RBRACKET {
                            // [:end]
                            end_result := parse_expr_with_pos(src, p, nodes)
                            if end_result.node_idx < 0 {
                                return ParseResult { node_idx: -1, new_pos: p }
                            }
                            end_node = end_result.node_idx
                            p = end_result.new_pos
                            p = lex_skip(src, p)
                        }
                        // Expect ']'
                        if lex_kind(src, p) != TK_RBRACKET {
                            return ParseResult { node_idx: -1, new_pos: p }
                        }
                        p = lex_end(src, p, TK_RBRACKET)
                        slice_node := AstNode {
                            tag: EXPR_SLICE,
                            int_val: 0,
                            str_val: "",
                            left: array_node,
                            right: -1,
                            extra: end_node,
                            type_tag: TYPE_UNKNOWN
                        }
                        nodes = append(nodes, slice_node)
                        current_node = len(nodes) - 1
                    } else {
                        // Parse first expression (could be index or slice start)
                        index_result := parse_expr_with_pos(src, p, nodes)
                        if index_result.node_idx < 0 {
                            return ParseResult { node_idx: -1, new_pos: p }
                        }
                        p = index_result.new_pos
                        p = lex_skip(src, p)

                        if lex_kind(src, p) == TK_COLON {
                            // Slice: arr[start:end] or arr[start:]
                            p = lex_end(src, p, TK_COLON)
                            p = lex_skip(src, p)
                            end_node := -1
                            if lex_kind(src, p) != TK_RBRACKET {
                                // arr[start:end]
                                end_result := parse_expr_with_pos(src, p, nodes)
                                if end_result.node_idx < 0 {
                                    return ParseResult { node_idx: -1, new_pos: p }
                                }
                                end_node = end_result.node_idx
                                p = end_result.new_pos
                                p = lex_skip(src, p)
                            }
                            // Expect ']'
                            if lex_kind(src, p) != TK_RBRACKET {
                                return ParseResult { node_idx: -1, new_pos: p }
                            }
                            p = lex_end(src, p, TK_RBRACKET)
                            slice_node := AstNode {
                                tag: EXPR_SLICE,
                                int_val: 0,
                                str_val: "",
                                left: array_node,
                                right: index_result.node_idx,
                                extra: end_node,
                                type_tag: TYPE_UNKNOWN
                            }
                            nodes = append(nodes, slice_node)
                            current_node = len(nodes) - 1
                        } else {
                            // Regular index access: arr[idx]
                            if lex_kind(src, p) != TK_RBRACKET {
                                return ParseResult { node_idx: -1, new_pos: p }
                            }
                            p = lex_end(src, p, TK_RBRACKET)
                            index_node := AstNode {
                                tag: EXPR_INDEX,
                                int_val: 0,
                                str_val: "",
                                left: array_node,
                                right: index_result.node_idx,
                                extra: -1,
                                type_tag: TYPE_UNKNOWN
                            }
                            nodes = append(nodes, index_node)
                            current_node = len(nodes) - 1
                        }
                    }
                    }
```

**Key design decisions:**
- `EXPR_SLICE` node: `left` = base array, `right` = start (-1 for default 0), `extra` = end (-1 for default len)
- Checks for `TK_COLON` BEFORE parsing first expression to handle `[:end]` and `[:]`
- Falls through to existing `EXPR_INDEX` if no colon found

- [ ] **Step 2: Verify compiler builds**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease 2>&1 | grep -v warning
```

Expected: builds (parser now accepts slice syntax but irgen doesn't handle EXPR_SLICE yet).

- [ ] **Step 3: Commit**

```bash
git add bootstrap/ease/parser/parser.ease
git commit -m "feat(lang): Parse slice syntax arr[start:end] in parser"
```

---

## Chunk 3: IR Generation

### Task 4: Generate IR for slice expressions

**Files:**
- Modify: `bootstrap/ease/irgen/irgen.ease`

- [ ] **Step 1: Add gen_ir_slice function**

Add this function in `irgen.ease` right after the `gen_ir_index` function (after line 497, `return res` + closing `}`):

```ease
fn gen_ir_slice(node: AstNode, nodes: []AstNode, instrs: []IRInstr, st_names: []string, st_vregs: []int) -> int {
    // Generate base array vreg
    arr := gen_ir_from_ast_with_symtab(node.left, nodes, instrs, st_names, st_vregs)

    // Generate start vreg (default 0 if node.right == -1)
    start := 0
    if node.right >= 0 {
        start = gen_ir_from_ast_with_symtab(node.right, nodes, instrs, st_names, st_vregs)
    } else {
        start = len(instrs)
        instrs = append(instrs, IRInstr { op: OP_LOADCONST, dest: start, arg1: 0, arg2: 0, arg3: 0, str_val: "" })
        set_vreg_type(start, TYPE_INT)
    }

    // Generate end vreg (default len(arr) if node.extra == -1)
    end := 0
    if node.extra >= 0 {
        end = gen_ir_from_ast_with_symtab(node.extra, nodes, instrs, st_names, st_vregs)
    } else {
        end = len(instrs)
        instrs = append(instrs, IRInstr { op: OP_ARRAY_LEN, dest: end, arg1: arr, arg2: 0, arg3: 0, str_val: "" })
        set_vreg_type(end, TYPE_INT)
    }

    // Emit OP_ARRAY_SLICE: dest=result, arg1=arr, arg2=start, arg3=end
    res := len(instrs)
    instrs = append(instrs, IRInstr { op: OP_ARRAY_SLICE, dest: res, arg1: arr, arg2: start, arg3: end, str_val: "" })

    // Result inherits array type from base
    set_vreg_type(res, get_vreg_type(arr))
    sn := get_vreg_struct_name(arr)
    if len(sn) > 0 {
        set_vreg_struct_name(res, sn)
    }

    return res
}
```

- [ ] **Step 2: Add EXPR_SLICE dispatch in gen_ir_from_ast_with_symtab**

Find line 1721-1723 in `irgen.ease`:
```ease
    if node.tag == EXPR_INDEX {
        return gen_ir_index(node, nodes, instrs, symtab_names, symtab_vregs)
    }
```

Add right after it (after the closing `}`):

```ease
    if node.tag == EXPR_SLICE {
        return gen_ir_slice(node, nodes, instrs, symtab_names, symtab_vregs)
    }
```

- [ ] **Step 3: Verify compiler builds**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease 2>&1 | grep -v warning
```

Expected: builds. Slice IR is now generated but codegen doesn't handle `OP_ARRAY_SLICE` yet.

- [ ] **Step 4: Commit**

```bash
git add bootstrap/ease/irgen/irgen.ease
git commit -m "feat(lang): Generate IR for slice expressions"
```

---

## Chunk 4: LLVM Codegen

### Task 5: Emit LLVM IR for OP_ARRAY_SLICE

**Files:**
- Modify: `bootstrap/ease/llvm/llvm.ease`

- [ ] **Step 1: Add OP_ARRAY_SLICE handler**

Find the `OP_ARRAY_LEN` handler block ending at line 829. Add the following block right after it (after the closing `}`):

```ease
        if terminated == 0 && instr.op == OP_ARRAY_SLICE {
            // Slice: create new fat pointer sharing underlying data
            // arg1=array fat ptr, arg2=start vreg, arg3=end vreg
            tis := strconv.Itoa(ti)
            // Load fat pointer fields
            fp := emit_load_ptr(fd, instr.arg1, ti, 0, 1)
            dp := "%sl.dp." + tis
            write_str(fd, "  " + dp + " = load i64, ptr " + fp + "\n")
            cap_addr := "%sl.ca." + tis
            write_str(fd, "  " + cap_addr + " = getelementptr i8, ptr " + fp + ", i64 16\n")
            old_cap := "%sl.oc." + tis
            write_str(fd, "  " + old_cap + " = load i64, ptr " + cap_addr + "\n")
            // Load start and end
            start := emit_load_vreg(fd, instr.arg2, ti, 2)
            end := emit_load_vreg(fd, instr.arg3, ti, 3)
            // new_data = data_ptr + start * 8
            scaled := "%sl.sc." + tis
            write_str(fd, "  " + scaled + " = mul i64 " + start + ", 8\n")
            new_dp := "%sl.nd." + tis
            write_str(fd, "  " + new_dp + " = add i64 " + dp + ", " + scaled + "\n")
            // new_len = end - start
            new_len := "%sl.nl." + tis
            write_str(fd, "  " + new_len + " = sub i64 " + end + ", " + start + "\n")
            // new_cap = old_cap - start
            new_cap := "%sl.nc." + tis
            write_str(fd, "  " + new_cap + " = sub i64 " + old_cap + ", " + start + "\n")
            // Allocate new 24-byte fat pointer
            new_fp := "%sl.fp." + tis
            write_str(fd, "  " + new_fp + " = call ptr @malloc(i64 24)\n")
            // Store data_ptr, len, cap
            write_str(fd, "  store i64 " + new_dp + ", ptr " + new_fp + "\n")
            len_ptr := "%sl.lp." + tis
            write_str(fd, "  " + len_ptr + " = getelementptr i8, ptr " + new_fp + ", i64 8\n")
            write_str(fd, "  store i64 " + new_len + ", ptr " + len_ptr + "\n")
            cap_ptr := "%sl.cp." + tis
            write_str(fd, "  " + cap_ptr + " = getelementptr i8, ptr " + new_fp + ", i64 16\n")
            write_str(fd, "  store i64 " + new_cap + ", ptr " + cap_ptr + "\n")
            // Result = fat pointer as i64
            result := "%sl.r." + tis
            write_str(fd, "  " + result + " = ptrtoint ptr " + new_fp + " to i64\n")
            emit_store_vreg(fd, instr.dest, result)
        }
```

**How this works:**
1. Loads the existing fat pointer's data_ptr (offset 0) and cap (offset 16)
2. Computes `new_data = data_ptr + start * 8` (pointer into original buffer)
3. Computes `new_len = end - start` and `new_cap = cap - start`
4. Allocates a new 24-byte fat pointer via `@malloc`
5. Stores `[new_data, new_len, new_cap]` into the new fat pointer
6. Returns the new fat pointer as i64

The new fat pointer's data field points INTO the original array's buffer — this is what gives shared memory semantics.

- [ ] **Step 2: Build compiler and run tests**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease 2>&1 | grep -v warning && ./tmp/ease test tests/ 2>&1 | tail -20
```

Expected: all tests pass including the 9 new slice tests.

- [ ] **Step 3: Verify self-hosting convergence**

```bash
./tmp/ease bootstrap/compiler.ease && cp tmp/output.ll tmp/gen1.ll && clang -O1 tmp/gen1.ll -o tmp/ease_gen1 2>&1 | grep -v warning && ./tmp/ease_gen1 bootstrap/compiler.ease && cp tmp/output.ll tmp/gen2.ll && diff tmp/gen1.ll tmp/gen2.ll && echo "CONVERGED"
```

Expected: `CONVERGED`

- [ ] **Step 4: Commit**

```bash
git add bootstrap/ease/llvm/llvm.ease
git commit -m "feat(lang): Add slice syntax codegen (arr[start:end] with shared memory)"
```

---

## Chunk 5: Restore path module and update docs

### Task 6: Restore path module from backup

**Files:**
- Restore: `bootstrap/ease/path/` from `tmp/path_backup/`
- Restore: `tests/path_test.ease` from `tmp/path_test_backup.ease`

- [ ] **Step 1: Restore path module**

```bash
cp -r tmp/path_backup/ bootstrap/ease/path/
cp tmp/path_test_backup.ease tests/path_test.ease
```

- [ ] **Step 2: Verify path module compiles and tests pass**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease 2>&1 | grep -v warning && ./tmp/ease test tests/ 2>&1 | tail -20
```

Expected: all tests pass (including path tests that use `parts[0:len(parts)-1]`).

- [ ] **Step 3: Verify self-hosting convergence again**

```bash
./tmp/ease bootstrap/compiler.ease && cp tmp/output.ll tmp/gen1.ll && clang -O1 tmp/gen1.ll -o tmp/ease_gen1 2>&1 | grep -v warning && ./tmp/ease_gen1 bootstrap/compiler.ease && cp tmp/output.ll tmp/gen2.ll && diff tmp/gen1.ll tmp/gen2.ll && echo "CONVERGED"
```

Expected: `CONVERGED`

- [ ] **Step 4: Commit**

```bash
git add bootstrap/ease/path/ tests/path_test.ease
git commit -m "feat(stdlib): Restore path module (unblocked by slice syntax)"
```

### Task 7: Update documentation

**Files:**
- Modify: `docs/language.md`
- Modify: `docs/implementation-status.md`

- [ ] **Step 1: Add slice syntax to language.md**

Add a new section after the "Loops" section in `docs/language.md`:

```markdown
## Slices (Go-style, implemented)
```ease
arr[start:end]     // elements from start to end-1
arr[start:]        // elements from start to len(arr)-1
arr[:end]          // elements from 0 to end-1
arr[:]             // full slice (same data, new fat pointer)
```
- Slices share underlying memory with the original array (Go semantics)
- Mutations through a slice affect the original array
- `len()` and `append()` work on slices
- Slices of slices work naturally: `arr[1:4][0:2]`
```

- [ ] **Step 2: Update implementation-status.md**

Update the test count and mark path module as done in `docs/implementation-status.md`.

- [ ] **Step 3: Commit**

```bash
git add docs/language.md docs/implementation-status.md
git commit -m "docs: Add slice syntax documentation"
```

### Task 8: Update seed

- [ ] **Step 1: Update seed.ll**

```bash
cp tmp/output.ll bootstrap/seed.ll
```

- [ ] **Step 2: Verify seed works**

```bash
clang -O1 bootstrap/seed.ll -o tmp/ease_seed 2>&1 | grep -v warning && ./tmp/ease_seed bootstrap/compiler.ease && diff tmp/output.ll bootstrap/seed.ll && echo "SEED OK"
```

Expected: `SEED OK` — the seed produces identical output.

- [ ] **Step 3: Commit seed**

```bash
git add bootstrap/seed.ll
git commit -m "chore: Update seed.ll with slice syntax support"
```
