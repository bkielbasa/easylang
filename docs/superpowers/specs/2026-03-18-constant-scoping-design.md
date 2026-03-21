# Constant Scoping Fix — Design

## Goal

Fix constant scoping so constants follow Go-style rules: bare name access within a package, qualified `pkg.CONST` access cross-package. Uppercase = exported, lowercase = private. This prevents imported module constants from shadowing each other or the compiler's own constants.

## Problem

All constants from all modules are stored in a single flat global registry (`g_const_names` in sema.ease). When module A defines `const TYPE_STRING = 3` and imported module B defines `const TYPE_STRING = 1`, module B's constant shadows module A's because the symtab is searched backwards and the later-registered constant is found first.

This caused a real bug: adding `const TYPE_STRING = 1` to json.ease shadowed the compiler's own `const TYPE_STRING = 3` (from ast.ease), causing struct field types to be misidentified and producing corrupt binaries.

## Design

### Layer 1: Module Tracking in Constant Registry

**sema.ease** — add a module name array parallel to the existing constant arrays:

```ease
g_const_modules := []string{}  // "" for local, "token" for imported, etc.
```

Changes:
- Add `g_const_modules` global array, appended in parallel with existing arrays.
- `register_const()` — does NOT change signature. Instead, use a global `g_current_const_module` that the caller sets before invoking `parse_source_file`. `register_const` reads `g_current_const_module` and appends it to `g_const_modules`.
- `RegisterQualifiedConst()` — store the same module name as the source constant.
- Add `ConstModule(i: int) -> string` accessor.
- Add `SetCurrentConstModule(mod: string)` setter.
- `ConstCount()`, `ConstName()`, `ConstType()`, `ConstIntValue()`, `ConstStrValue()` — unchanged.

**compiler.ease** — set module name before parsing each source file:

- Before calling `parse_source_file` for the main file: `sema.SetCurrentConstModule("")`
- Before calling `parse_source_file` for an imported module: `sema.SetCurrentConstModule(mod_name)`
- In `load_dir_package`: set module before each file parse in the directory loop.
- In `load_single_module`: set module before the single file parse.
- The qualified const registration (already done in `load_dir_package` and `load_single_module`) continues as-is — `RegisterQualifiedConst` copies the module name from the source entry.

### Layer 2: Filtered Symtab Construction

**compiler.ease** `generate_all_ir()` — replace the current "add all constants" loop:

Current:
```ease
ci := 0
for ci < sema.ConstCount() {
    symtab_names = append(symtab_names, sema.ConstName(ci))
    symtab_vregs = append(symtab_vregs, -2000 - ci)
    ci = ci + 1
}
```

New logic:
```
ci := 0
for ci < sema.ConstCount() {
    cname := sema.ConstName(ci)
    cmod := sema.ConstModule(ci)
    if cmod == "" || cmod == current_function_module {
        // Local constant or same-module constant → add with bare name
        symtab_names = append(symtab_names, cname)
        symtab_vregs = append(symtab_vregs, -2000 - ci)
    } else if contains_dot(cname) {
        // Qualified constant from imported module (e.g., "reflect.INT") → add as-is
        symtab_names = append(symtab_names, cname)
        symtab_vregs = append(symtab_vregs, -2000 - ci)
    }
    // else: bare name from different module → skip (not visible)
    ci = ci + 1
}
```

**Module derivation** — `current_function_module` is extracted from `g_func_source_files[gen_i]`:

| Source file path | Module name | Rule |
|------------------|-------------|------|
| `bootstrap/compiler.ease` | `""` (empty) | Main file — no directory package |
| `bootstrap/ease/token/token.ease` | `"token"` | Directory package — last directory component before filename |
| `bootstrap/ease/irgen/irgen.ease` | `"irgen"` | Directory package — same rule |
| `bootstrap/ease/sema/sema.ease` | `"sema"` | Directory package — same rule |
| `tests/const_test.ease` | `""` (empty) | Test file — treated as main package |
| `bootstrap/ease/json/json.ease` | `"json"` | Stdlib package |

Extraction logic: if path contains `ease/X/Y.ease`, module = `X`. Otherwise module = `""`.

This covers all three compilation modes:
- **Self-compile**: compiler.ease is main (`""`), sub-packages are `token`, `ast`, `ir`, `sema`, `irgen`, `llvm`, `lsp`
- **User compile**: user's main file is `""`, imported packages get their directory name
- **Test mode**: test file is `""`, imported packages get their directory name

### Layer 3: Compiler Source Update

All cross-module bare constant references in the compiler source must be qualified. ~1,391 references total.

| Package | Constants | Prefix | Approx. refs | Example |
|---------|-----------|--------|-------------|---------|
| token | `TK_*` (~70 consts) | `token.` | ~548 | `TK_IDENT` → `token.TK_IDENT` |
| ast | `TYPE_*`, `EXPR_*`, `DECL_*`, `STMT_*` (~40 consts) | `ast.` | ~430 | `TYPE_STRING` → `ast.TYPE_STRING` |
| ir | `OP_*` (~95 consts) | `ir.` | ~413 | `OP_ADD` → `ir.OP_ADD` |

**Files and their cross-module references:**

| File | Module | Uses from other modules |
|------|--------|------------------------|
| `bootstrap/ease/parser/parser.ease` | `parser` | `token.TK_*`, `ast.EXPR_*`, `ast.DECL_*`, `ast.STMT_*`, `ast.TYPE_*` |
| `bootstrap/ease/irgen/irgen.ease` | `irgen` | `token.TK_*`, `ast.EXPR_*`, `ast.TYPE_*`, `ir.OP_*` |
| `bootstrap/ease/llvm/llvm.ease` | `llvm` | `ir.OP_*` |
| `bootstrap/ease/sema/sema.ease` | `sema` | `ast.TYPE_*` |
| `bootstrap/compiler.ease` | `""` (main) | `token.TK_*`, `ast.EXPR_*`, `ast.DECL_*`, `ast.TYPE_*` |
| `bootstrap/ease/lsp/lsp.ease` | `lsp` | `token.TK_*`, `ast.EXPR_*`, `ast.DECL_*` |

Constants within their own package remain bare (e.g., `TK_IDENT` used inside `token/token.ease` stays as `TK_IDENT`).

**How sub-packages access cross-module constants without import statements:**

The compiler's sub-packages (parser, irgen, llvm, sema, lsp) don't have their own `import` statements — all imports are handled by `compiler.ease`. This works because:
1. All module constants are registered globally in sema's registry during compilation
2. `RegisterQualifiedConst` creates qualified entries (e.g., `"ast.TYPE_STRING"`) that are globally visible
3. The symtab filter (Layer 2) adds ALL qualified constants (names containing `.`) to every function's symtab
4. So `irgen.ease` can reference `ast.TYPE_STRING` without importing `ast` — the qualified constant is already in the global registry from when `compiler.ease` loaded the `ast` package

**Rewrite strategy:**

For each file, use find-and-replace with word boundaries. The constants have unique prefixes (`TK_`, `TYPE_`, `EXPR_`, `DECL_`, `STMT_`, `OP_`) that don't collide with variable names. The rewrite is mechanical:
1. For each file, determine its module (from directory name)
2. For each constant prefix used in that file, check if the prefix belongs to a different module
3. If cross-module, prefix all occurrences with the owning module name + `.`
4. Skip occurrences that are already qualified (contain a `.` before the constant name)
5. Skip occurrences inside the defining module (bare names stay bare in their own package)

### Layer 4: Cross-Package Constant Resolution (already done)

`gen_ir_field` in irgen.ease already handles qualified constant access for `EXPR_FIELD` nodes (e.g., `reflect.INT`). The parser sees `ast.TYPE_STRING` and produces an `EXPR_FIELD` with left=`EXPR_IDENT("ast")`, field=`"TYPE_STRING"`. `gen_ir_field` looks up `"ast.TYPE_STRING"` in the symtab and finds the qualified constant entry. No changes needed.

## Bootstrap Strategy

This is a "big bang" change. The bootstrap gap:

1. **seed.ll** → clang → old binary (no scoping)
2. Old binary compiles new source (with qualified names) → gen1.ll
   - Old binary doesn't scope constants, so bare names from imports are still visible
   - But the new source uses qualified names, which also resolve correctly
   - gen1.ll is valid but may differ from gen2.ll
3. gen1.ll → clang → gen1 binary (has scoping)
4. gen1 binary compiles new source → gen2.ll
   - Now scoping is enforced, qualified names resolve correctly
5. gen2.ll → clang → gen2 binary
6. gen2 binary compiles new source → gen3.ll
7. gen2.ll == gen3.ll → **converged**

Update seed.ll to gen2.ll after convergence.

## Scope

### In scope
- Module tracking in constant registry (`g_const_modules` array)
- Filtered symtab construction based on module ownership
- Qualifying all cross-module constant references in compiler source
- Tests for constant scoping (shadowing prevention, cross-package access, private constants)
- Bootstrap convergence

### Out of scope
- Wildcard imports (`import "reflect" use *`)
- Compile-time error for accessing private cross-package constants (silently invisible for now)
- Refactoring the constant registry to use a different data structure

## LSP Considerations

The LSP server (`lsp.ease`) uses `TK_*`, `EXPR_*`, and `DECL_*` constants from other modules. After the rewrite, these become `token.TK_*`, `ast.EXPR_*`, `ast.DECL_*`. The LSP module is loaded the same way as other sub-packages — its constants will be scoped to `"lsp"` and it accesses cross-module constants via qualified names. No special LSP handling needed.

## Testing

**Existing tests (must continue passing):**
- `TestCrossPackageConst` — `reflect.INT` resolves to 1
- `TestCrossPackageConstInStructLit` — `reflect.STRING` in struct literal
- `TestJsonCrossPackageTypeConsts` — `json.JSON_STRING`, `json.JSON_NUM`, `json.JSON_OBJECT`
- All 251 existing tests

**New tests:**
- `TestConstShadowingPrevention` — create two test modules with same-named exported constant, verify each resolves to its own value via qualified access
- `TestPrivateConstNotAccessible` — lowercase constant from imported module should not be visible (compile error or zero value)

**Bootstrap verification:**
- gen2.ll == gen3.ll (convergence)
