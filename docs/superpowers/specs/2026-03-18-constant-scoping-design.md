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
- `register_const()` → accept a `module_name: string` parameter. Store it in `g_const_modules`.
- `RegisterQualifiedConst()` → also store the module name.
- Add `ConstModule(i: int) -> string` accessor.
- `ConstCount()`, `ConstName()`, `ConstType()`, `ConstIntValue()`, `ConstStrValue()` — unchanged.

**compiler.ease** — pass module name when registering constants:

- In `parse_source_file` call sites: pass `""` for the main file's constants, pass `mod_name` for imported module constants.
- In `load_dir_package` and `load_single_module`: the `register_const` calls happen inside `parse_source_file`. Either pass the module name through, or set a global "current module" that `register_const` reads.

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

The `current_function_module` is derived from `g_func_source_files[gen_i]` — extract the module name from the source file path.

### Layer 3: Compiler Source Update

All cross-module bare constant references in the compiler source must be qualified:

| Package | Constants | Prefix | Example |
|---------|-----------|--------|---------|
| token | `TK_*` (~70) | `token.` | `TK_IDENT` → `token.TK_IDENT` |
| ast | `TYPE_*`, `EXPR_*`, `DECL_*`, `STMT_*` (~40) | `ast.` | `TYPE_STRING` → `ast.TYPE_STRING` |
| ir | `OP_*` (~95) | `ir.` | `OP_ADD` → `ir.OP_ADD` |

Files that need updating:
- `bootstrap/ease/parser/parser.ease` — uses token.TK_*, ast.EXPR_*, ast.DECL_*, ast.STMT_*, ast.TYPE_*
- `bootstrap/ease/irgen/irgen.ease` — uses token.TK_*, ast.EXPR_*, ast.TYPE_*, ir.OP_*
- `bootstrap/ease/llvm/llvm.ease` — uses ir.OP_*
- `bootstrap/ease/sema/sema.ease` — uses ast.TYPE_*
- `bootstrap/compiler.ease` — uses token.TK_*, ast.EXPR_*, ast.DECL_*, ast.TYPE_*
- `bootstrap/ease/lsp/lsp.ease` — uses token.TK_*, ast.EXPR_*, ast.DECL_*

Constants within their own package remain bare (e.g., `TK_IDENT` used inside `token.ease` stays as `TK_IDENT`).

### Layer 4: Cross-Package Constant Resolution (already done)

`gen_ir_field` in irgen.ease already handles qualified constant access for `EXPR_FIELD` nodes (e.g., `reflect.INT`). No changes needed.

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

## Testing

- `TestCrossPackageConst` — existing: `reflect.INT` resolves to 1
- `TestCrossPackageConstInStructLit` — existing: `reflect.STRING` in struct literal
- **New**: `TestConstShadowingPrevention` — two modules with same-named constants don't conflict
- **New**: `TestPrivateConstNotAccessible` — lowercase constants from imported modules aren't visible
- All 251 existing tests must pass
- Bootstrap convergence: gen2 == gen3
