# Constant Scoping Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement Go-style constant scoping so constants are only visible within their own module (bare name) or via qualified access (`pkg.CONST`), preventing cross-module shadowing bugs.

**Architecture:** Add a parallel `g_const_modules` array to track which module each constant belongs to. Add a parallel `g_func_modules` array to track each function's owning module. Filter the symtab in `generate_all_ir` so each function only sees its own module's constants (bare) plus all qualified constants (dotted). Then mechanically rewrite ~1,300 cross-module constant references in the compiler source to use qualified names.

**Spec:** `docs/superpowers/specs/2026-03-18-constant-scoping-design.md`

---

### Task 1: Add Module Tracking to Constant Registry (sema.ease)

**Files:**
- Modify: `bootstrap/ease/sema/sema.ease:450-528` (constant registry section)

- [ ] **Step 1: Add `g_const_modules` array and `g_current_const_module` global**

In `bootstrap/ease/sema/sema.ease`, after line 453 (`g_const_types := []int{}`), add:

```ease
g_const_modules := []string{}
g_current_const_module := ""
```

- [ ] **Step 2: Append module in `register_const`**

In `register_const` (line 455), right after `g_const_types = append(g_const_types, cnode.type_tag)` (line 458), add:

```ease
    g_const_modules = append(g_const_modules, g_current_const_module)
```

- [ ] **Step 3: Copy module in `RegisterQualifiedConst`**

In `RegisterQualifiedConst` (line 523), after the existing appends, add:

```ease
    g_const_modules = append(g_const_modules, g_const_modules[idx])
```

- [ ] **Step 4: Add accessor and setter functions**

After `RegisterQualifiedConst`, add:

```ease
fn ConstModule(i: int) -> string {
    if i < len(g_const_modules) {
        return g_const_modules[i]
    }
    return ""
}

fn SetCurrentConstModule(mod: string) {
    g_current_const_module = mod
}
```

- [ ] **Step 5: Verify compilation**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease_gen1
```

Expected: compiles successfully (no behavioral change yet).

- [ ] **Step 6: Commit**

```bash
git add bootstrap/ease/sema/sema.ease
git commit -m "feat(sema): Add module tracking to constant registry"
```

---

### Task 2: Set Module Before Parsing + Track Function Modules (compiler.ease)

**Files:**
- Modify: `bootstrap/compiler.ease` (load_dir_package, load_single_module, main compile path, run_test_mode, generate_all_ir)

- [ ] **Step 1: Add `g_func_modules` global array**

Near the existing `g_func_source_files` global (line 28), add:

```ease
g_func_modules := []string{}
```

- [ ] **Step 2: Set module and track func modules in `load_dir_package`**

In `load_dir_package` (line 681), before `entries := os.ListDir(resolved)` (line 686), add:

```ease
    sema.SetCurrentConstModule(mod_name)
```

In the function registration loop (line 718-728), after `g_func_source_files = append(...)` (line 723), add:

```ease
        g_func_modules = append(g_func_modules, mod_name)
```

- [ ] **Step 3: Set module and track func modules in `load_single_module`**

In `load_single_module` (line 740), before `mod_source := os.ReadFile(resolved)` (line 746), add:

```ease
    sema.SetCurrentConstModule(mod_name)
```

In the function registration loop (line 760-768), after `g_func_source_files = append(...)` (line 765), add:

```ease
        g_func_modules = append(g_func_modules, mod_name)
```

- [ ] **Step 4: Set empty module for main file and track main func modules**

In the main compile path, before `parse_source_file(source, nodes, ...)` (line 1493), add:

```ease
    sema.SetCurrentConstModule("")
```

In the loop that populates `g_func_source_files` for the main file (line 1495-1498), after the `g_func_source_files` append (line 1496), add:

```ease
        g_func_modules = append(g_func_modules, "")
```

After `load_stdlib(...)` (line 1503), add:

```ease
    sema.SetCurrentConstModule("")
```

- [ ] **Step 5: Set empty module and track func modules in `run_test_mode`**

In `run_test_mode`, before the source file parsing loop (line 1075), add:

```ease
    sema.SetCurrentConstModule("")
```

Inside the source file `g_func_source_files` loop (line 1080-1082), after the append (line 1081), add:

```ease
            g_func_modules = append(g_func_modules, "")
```

Before the test file parsing loop (line 1091), add:

```ease
    sema.SetCurrentConstModule("")
```

Inside the test file `g_func_source_files` loop (line 1097-1100), after the append (line 1099), add:

```ease
            g_func_modules = append(g_func_modules, "")
```

- [ ] **Step 6: Verify compilation — no behavioral change**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease_gen1
./tmp/ease_gen1 test tests
```

Expected: all 251 tests pass (module tracking recorded but not yet used for filtering).

- [ ] **Step 7: Commit**

```bash
git add bootstrap/compiler.ease
git commit -m "feat(compiler): Track module for constants and functions"
```

---

### Task 3: Filter Symtab by Module in `generate_all_ir` (compiler.ease)

**Files:**
- Modify: `bootstrap/compiler.ease:1012-1017` (constant loop in `generate_all_ir`)
- Modify: `tests/const_test.ease` (add scoping test)

- [ ] **Step 1: Write test for constant shadowing prevention**

Add `"json"` to the import block in `tests/const_test.ease` (alongside existing `"testing"` and `"reflect"`).

Then add test:

```ease
fn TestConstShadowingPrevention(t: testing.T) {
    // Both reflect and json define constants with small int values.
    // If scoping works, local TYPE_A/TYPE_B/TYPE_C should NOT be shadowed.
    if TYPE_A != 1 { t.Fatal("local TYPE_A should be 1") }
    if TYPE_B != 2 { t.Fatal("local TYPE_B should be 2") }
    if TYPE_C != 3 { t.Fatal("local TYPE_C should be 3") }
    // Cross-package access still works
    if reflect.INT != 1 { t.Fatal("reflect.INT should be 1") }
    if json.JSON_STRING != 1 { t.Fatal("json.JSON_STRING should be 1") }
}
```

- [ ] **Step 2: Run test to verify it passes before filtering**

```bash
./tmp/ease test tests
```

Expected: PASS.

- [ ] **Step 3: Replace the constant symtab loop with filtered version**

In `generate_all_ir` (line 987), replace lines 1012-1017:

```ease
        ci := 0
        for ci < sema.ConstCount() {
            symtab_names = append(symtab_names, sema.ConstName(ci))
            symtab_vregs = append(symtab_vregs, -2000 - ci)
            ci = ci + 1
        }
```

With:

```ease
        // Determine current function's module for constant scoping
        current_func_module := ""
        if gen_i < len(g_func_modules) {
            current_func_module = g_func_modules[gen_i]
        }
        ci := 0
        for ci < sema.ConstCount() {
            cname := sema.ConstName(ci)
            cmod := sema.ConstModule(ci)
            if cmod == "" || cmod == current_func_module {
                // Local constant or same-module constant -> visible with bare name
                symtab_names = append(symtab_names, cname)
                symtab_vregs = append(symtab_vregs, -2000 - ci)
            } else if strings.IndexOf(cname, ".") >= 0 {
                // Qualified constant (e.g. "reflect.INT") -> always visible
                symtab_names = append(symtab_names, cname)
                symtab_vregs = append(symtab_vregs, -2000 - ci)
            }
            // else: bare name from different module -> NOT visible (scoped out)
            ci = ci + 1
        }
```

- [ ] **Step 4: Verify compilation and tests**

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease_gen1
./tmp/ease_gen1 test tests
```

Expected: all tests pass. The old binary compiles new source (no scoping enforced). ease_gen1 WILL enforce scoping but cannot yet compile the compiler source (bare cross-module names). That's expected — Task 4 fixes this.

- [ ] **Step 5: Commit**

```bash
git add bootstrap/compiler.ease tests/const_test.ease
git commit -m "feat(compiler): Add filtered constant symtab with module scoping"
```

---

### Task 4: Qualify Cross-Module Constants in Compiler Source

This is the largest task — mechanically rewriting ~1,300 bare constant references to use qualified names. The rewrite is per-file, per-prefix. Constants stay bare within their defining module.

**IMPORTANT:** macOS BSD sed does NOT support `\b` word boundaries. Use `[[:<:]]` (word start boundary) instead.

**Files:**
- Modify: `bootstrap/ease/parser/parser.ease` (~559 refs: TK_->token.TK_, EXPR_->ast.EXPR_, DECL_->ast.DECL_, STMT_->ast.STMT_, TYPE_->ast.TYPE_)
- Modify: `bootstrap/ease/irgen/irgen.ease` (~445 refs: TK_->token.TK_, EXPR_->ast.EXPR_, DECL_->ast.DECL_, STMT_->ast.STMT_, TYPE_->ast.TYPE_, OP_->ir.OP_)
- Modify: `bootstrap/ease/llvm/llvm.ease` (~104 refs: OP_->ir.OP_)
- Modify: `bootstrap/ease/sema/sema.ease` (~32 refs: TYPE_->ast.TYPE_, EXPR_->ast.EXPR_, DECL_->ast.DECL_)
- Modify: `bootstrap/ease/lexer/lexer.ease` (~90 refs: TK_->token.TK_)
- Modify: `bootstrap/ease/doc/doc.ease` (~5 refs: TK_->token.TK_)
- Modify: `bootstrap/compiler.ease` (~68 refs: TK_->token.TK_, EXPR_->ast.EXPR_, DECL_->ast.DECL_, TYPE_->ast.TYPE_, OP_->ir.OP_)

**Prefix-to-module mapping:**
- `TK_` -> `token` (defined in `token/token.ease`)
- `EXPR_`, `DECL_`, `STMT_`, `TYPE_` -> `ast` (defined in `ast/ast.ease`)
- `OP_` -> `ir` (defined in `ir/ir.ease`)

**Rewrite rules:**
- In a file belonging to module X, constants defined in module X stay bare
- Constants from other modules get prefixed: `TK_FOO` -> `token.TK_FOO`
- Skip already-qualified references (double-qualify cleanup handles these)
- `token/token.ease` and `ast/ast.ease` and `ir/ir.ease` are NOT modified (they define these constants)

- [ ] **Step 4a: Qualify constants in parser/parser.ease**

parser.ease is in module `parser`. All TK_/EXPR_/DECL_/STMT_/TYPE_ are cross-module.

```bash
cd /Users/bklimczak/Projects/easylang
sed -i '' 's/[[:<:]]TK_/token.TK_/g' bootstrap/ease/parser/parser.ease
sed -i '' 's/token\.token\.TK_/token.TK_/g' bootstrap/ease/parser/parser.ease
sed -i '' 's/[[:<:]]EXPR_/ast.EXPR_/g' bootstrap/ease/parser/parser.ease
sed -i '' 's/ast\.ast\.EXPR_/ast.EXPR_/g' bootstrap/ease/parser/parser.ease
sed -i '' 's/[[:<:]]DECL_/ast.DECL_/g' bootstrap/ease/parser/parser.ease
sed -i '' 's/ast\.ast\.DECL_/ast.DECL_/g' bootstrap/ease/parser/parser.ease
sed -i '' 's/[[:<:]]STMT_/ast.STMT_/g' bootstrap/ease/parser/parser.ease
sed -i '' 's/ast\.ast\.STMT_/ast.STMT_/g' bootstrap/ease/parser/parser.ease
sed -i '' 's/[[:<:]]TYPE_/ast.TYPE_/g' bootstrap/ease/parser/parser.ease
sed -i '' 's/ast\.ast\.TYPE_/ast.TYPE_/g' bootstrap/ease/parser/parser.ease
```

Verify no bare cross-module refs remain:

```bash
grep -c '[^.]TK_\|[^.]EXPR_\|[^.]DECL_\|[^.]STMT_\|[^.]TYPE_' bootstrap/ease/parser/parser.ease
```

Expected: 0 (or only false positives in comments).

- [ ] **Step 4b: Qualify constants in irgen/irgen.ease**

irgen.ease is in module `irgen`. All TK_/EXPR_/DECL_/STMT_/TYPE_/OP_ are cross-module.

```bash
sed -i '' 's/[[:<:]]TK_/token.TK_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/token\.token\.TK_/token.TK_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/[[:<:]]EXPR_/ast.EXPR_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/ast\.ast\.EXPR_/ast.EXPR_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/[[:<:]]DECL_/ast.DECL_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/ast\.ast\.DECL_/ast.DECL_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/[[:<:]]STMT_/ast.STMT_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/ast\.ast\.STMT_/ast.STMT_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/[[:<:]]TYPE_/ast.TYPE_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/ast\.ast\.TYPE_/ast.TYPE_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/[[:<:]]OP_/ir.OP_/g' bootstrap/ease/irgen/irgen.ease
sed -i '' 's/ir\.ir\.OP_/ir.OP_/g' bootstrap/ease/irgen/irgen.ease
```

- [ ] **Step 4c: Qualify constants in llvm/llvm.ease**

llvm.ease is in module `llvm`. All OP_ refs are from ir package.

```bash
sed -i '' 's/[[:<:]]OP_/ir.OP_/g' bootstrap/ease/llvm/llvm.ease
sed -i '' 's/ir\.ir\.OP_/ir.OP_/g' bootstrap/ease/llvm/llvm.ease
```

- [ ] **Step 4d: Qualify constants in sema/sema.ease**

sema.ease is in module `sema`. TYPE_/EXPR_/DECL_ are from ast package. sema.ease does NOT define any TYPE_/EXPR_/DECL_ constants of its own (verified), so all occurrences are cross-module.

```bash
sed -i '' 's/[[:<:]]TYPE_/ast.TYPE_/g' bootstrap/ease/sema/sema.ease
sed -i '' 's/ast\.ast\.TYPE_/ast.TYPE_/g' bootstrap/ease/sema/sema.ease
sed -i '' 's/[[:<:]]EXPR_/ast.EXPR_/g' bootstrap/ease/sema/sema.ease
sed -i '' 's/ast\.ast\.EXPR_/ast.EXPR_/g' bootstrap/ease/sema/sema.ease
sed -i '' 's/[[:<:]]DECL_/ast.DECL_/g' bootstrap/ease/sema/sema.ease
sed -i '' 's/ast\.ast\.DECL_/ast.DECL_/g' bootstrap/ease/sema/sema.ease
```

- [ ] **Step 4e: Qualify constants in lexer/lexer.ease**

lexer.ease is in module `lexer`. All TK_ refs are from the token package.

```bash
sed -i '' 's/[[:<:]]TK_/token.TK_/g' bootstrap/ease/lexer/lexer.ease
sed -i '' 's/token\.token\.TK_/token.TK_/g' bootstrap/ease/lexer/lexer.ease
```

- [ ] **Step 4f: Qualify constants in doc/doc.ease**

doc.ease is in module `doc`. TK_ refs are from the token package.

```bash
sed -i '' 's/[[:<:]]TK_/token.TK_/g' bootstrap/ease/doc/doc.ease
sed -i '' 's/token\.token\.TK_/token.TK_/g' bootstrap/ease/doc/doc.ease
```

- [ ] **Step 4g: Qualify constants in compiler.ease**

compiler.ease is the main file (module `""`). All TK_/EXPR_/DECL_/TYPE_/OP_ are cross-module.

```bash
sed -i '' 's/[[:<:]]TK_/token.TK_/g' bootstrap/compiler.ease
sed -i '' 's/token\.token\.TK_/token.TK_/g' bootstrap/compiler.ease
sed -i '' 's/[[:<:]]EXPR_/ast.EXPR_/g' bootstrap/compiler.ease
sed -i '' 's/ast\.ast\.EXPR_/ast.EXPR_/g' bootstrap/compiler.ease
sed -i '' 's/[[:<:]]DECL_/ast.DECL_/g' bootstrap/compiler.ease
sed -i '' 's/ast\.ast\.DECL_/ast.DECL_/g' bootstrap/compiler.ease
sed -i '' 's/[[:<:]]TYPE_/ast.TYPE_/g' bootstrap/compiler.ease
sed -i '' 's/ast\.ast\.TYPE_/ast.TYPE_/g' bootstrap/compiler.ease
sed -i '' 's/[[:<:]]OP_/ir.OP_/g' bootstrap/compiler.ease
sed -i '' 's/ir\.ir\.OP_/ir.OP_/g' bootstrap/compiler.ease
```

- [ ] **Step 4h: Verify the old binary can still compile the new source**

The old binary (./tmp/ease) doesn't enforce scoping, so qualified names like `token.TK_IDENT` resolve via the EXPR_FIELD path in irgen. The new source uses only qualified names for cross-module refs, so both paths work.

```bash
./tmp/ease bootstrap/compiler.ease && clang -O1 tmp/output.ll -o tmp/ease_gen1
```

Expected: compiles successfully.

- [ ] **Step 4i: Verify all tests still pass**

```bash
./tmp/ease test tests
```

Expected: all tests pass (tests don't use internal compiler constants).

- [ ] **Step 4j: Commit the rewrite**

```bash
git add bootstrap/ease/parser/parser.ease bootstrap/ease/irgen/irgen.ease bootstrap/ease/llvm/llvm.ease bootstrap/ease/sema/sema.ease bootstrap/ease/lexer/lexer.ease bootstrap/ease/doc/doc.ease bootstrap/compiler.ease
git commit -m "refactor(compiler): Qualify all cross-module constant references"
```

---

### Task 5: Bootstrap Convergence

**Files:**
- Modify: `bootstrap/seed.ll` (regenerated)

- [ ] **Step 1: Generate gen1 binary (old binary compiles new source)**

```bash
./tmp/ease bootstrap/compiler.ease
cp tmp/output.ll tmp/gen1.ll
clang -O1 tmp/gen1.ll -o tmp/ease_gen1
```

- [ ] **Step 2: Generate gen2 (gen1 compiles new source — scoping now enforced)**

```bash
./tmp/ease_gen1 bootstrap/compiler.ease
cp tmp/output.ll tmp/gen2.ll
clang -O1 tmp/gen2.ll -o tmp/ease_gen2
```

Expected: compiles successfully. If it fails, there are unqualified cross-module refs that gen1 (which enforces scoping) can't resolve. Fix them in Task 4 and retry.

- [ ] **Step 3: Generate gen3 (gen2 compiles new source — verify convergence)**

```bash
./tmp/ease_gen2 bootstrap/compiler.ease
cp tmp/output.ll tmp/gen3.ll
```

- [ ] **Step 4: Verify convergence: gen2.ll == gen3.ll**

```bash
diff tmp/gen2.ll tmp/gen3.ll
```

Expected: no differences (byte-identical).

- [ ] **Step 5: Run full test suite with converged binary**

```bash
./tmp/ease_gen2 test tests
```

Expected: all tests pass (251+).

- [ ] **Step 6: Update seed.ll and install new binary**

```bash
cp tmp/gen2.ll bootstrap/seed.ll
cp tmp/ease_gen2 tmp/ease
```

- [ ] **Step 7: Verify seed.ll bootstrap works end-to-end**

```bash
clang -O1 bootstrap/seed.ll -o tmp/ease_seed
./tmp/ease_seed bootstrap/compiler.ease
diff tmp/output.ll tmp/gen2.ll
```

Expected: identical output.

- [ ] **Step 8: Commit**

```bash
git add bootstrap/seed.ll
git commit -m "chore: Update seed.ll for constant scoping convergence"
```

---

### Task 6: Add Scoping-Specific Tests and Update Docs

**Files:**
- Modify: `tests/const_test.ease`
- Modify: `docs/implementation-status.md`

- [ ] **Step 1: Add test for local const not shadowed by imports**

Add to `tests/const_test.ease`:

```ease
fn TestLocalConstNotShadowedByImport(t: testing.T) {
    // json has private const json_string = 1 (lowercase = private)
    // It should NOT be visible here. Our local TYPE_A = 1 must resolve correctly.
    v := TYPE_A
    if v != 1 { t.Fatal("TYPE_A should be 1, not shadowed by imports") }
    // Verify multiple cross-package const types work
    if reflect.STRING != 3 { t.Fatal("reflect.STRING should be 3") }
    if json.JSON_ARRAY != 5 { t.Fatal("json.JSON_ARRAY should be 5") }
}
```

- [ ] **Step 2: Run tests**

```bash
./tmp/ease test tests
```

Expected: all tests pass.

- [ ] **Step 3: Update implementation-status.md**

Update the const keyword line:

```markdown
- [x] **`const` keyword** — Compile-time constants with Go-style module scoping (bare name within package, `pkg.CONST` cross-package)
```

Update test count to reflect new total.

- [ ] **Step 4: Commit**

```bash
git add tests/const_test.ease docs/implementation-status.md
git commit -m "test: Add constant scoping tests and update docs"
```
