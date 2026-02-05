# Bootstrap Semantic Analyzer Deep Dive

## Architecture Overview

The bootstrap semantic analyzer (`bootstrap/sema.ease`) implements type checking and name resolution for the Ease compiler, written in Ease itself.

### Design Constraints

The implementation uses **parallel arrays** instead of arrays-of-structs to work around current compiler limitations with struct arrays.

### Core Data Structure

```ease
struct Sema {
    // Type storage (20 parallel arrays)
    type_tags: []int,
    type_names: []string,
    type_elem: []int,
    type_fields_start: []int,
    type_fields_count: []int,
    type_params_start: []int,
    type_params_count: []int,
    type_result: []int,

    field_names: []string,
    field_types: []int,

    // Symbol storage
    sym_names: []string,
    sym_types: []int,
    sym_scopes: []int,
    sym_kinds: []int,
    sym_mutable: []int,

    // Scope storage
    scope_parents: []int,
    scope_names: []string,

    // State
    errors: []string,
    current_scope: []int,
    current_func: []int,
}
```

**Size**: This struct contains 20 array fields (fat pointers). Each array is 24 bytes (ptr + len + cap), so the struct is **480 bytes minimum**, plus any actual array data.

## Type System

### Builtin Types

Types are identified by index, stored in parallel arrays:

- **Type 0**: INVALID (tag=0)
- **Type 1**: INT (tag=1)
- **Type 2**: BOOL (tag=2)
- **Type 3**: STRING (tag=3)
- **Type 4**: UNIT (tag=4)

For builtin types: **type_index == type_tag** (convenient simplification)

### Type Storage

Each type `i` has properties stored across arrays:
- `type_tags[i]` - kind of type (TY_INT, TY_STRUCT, TY_ARRAY, etc.)
- `type_names[i]` - name (for structs)
- `type_elem[i]` - element type index (for arrays/slices)
- `type_fields_start[i]`, `type_fields_count[i]` - field range (for structs)
- `type_params_start[i]`, `type_params_count[i]`, `type_result[i]` - signature (for functions)

## Analysis Flow

### Test Case: `test_binary_ops()`

Creates AST for `1 + 2`:
```
Node 0: EXPR_INT (value=1)
Node 1: EXPR_INT (value=2)
Node 2: EXPR_BINARY (op=TK_PLUS, left=0, right=1)
```

### Expected Execution Trace

1. **`analyze_expr(s, nodes, 2)`** (binary expression)
   - Identifies EXPR_BINARY, calls `analyze_binary`

2. **`analyze_binary(s, nodes, 2)`**
   - Extracts op=TK_PLUS, left=0, right=1
   - Calls `analyze_expr(s, nodes, 0)` → returns type 1 (int)
   - Calls `analyze_expr(s, nodes, 1)` → returns type 1 (int)
   - Checks if op == TK_PLUS → YES
   - Checks if left_type == 3 (string) → NO (it's 1=int)
   - Calls **`is_numeric(s, 1)`** ← **CRASH OCCURS HERE**

3. **`is_numeric(s, 1)`** (should execute)
   - Check t >= len(s.type_tags) → 1 < 5, OK
   - Access s.type_tags[1] → should be 1 (TY_INT)
   - Return 1 == 1 → true

4. **Back in `analyze_binary`** (never reaches)
   - Should print "[checking types_equal]"
   - Should check types match
   - Should return type 1

## The Critical Bug

### Actual Output
```
[checking numeric]exit status 255
```

The crash happens **immediately after printing "[checking numeric]"** and **before calling `is_numeric`** returns successfully.

### Root Cause Analysis

**Pass-by-Value Problem:**

Every function in `bootstrap/sema.ease` receives Sema by VALUE:
```ease
fn is_numeric(s: Sema, t: int) -> bool { ... }
fn analyze_binary(s: Sema, nodes: []AstNode, idx: int) -> int { ... }
fn analyze_expr(s: Sema, nodes: []AstNode, idx: int) -> int { ... }
```

This means:
1. **Massive copying**: Each call copies all 480+ bytes of struct + array metadata
2. **Nested calls**: `analyze_binary` → `is_numeric` creates stack frames with multiple Sema copies
3. **Return values**: Returning from these functions may trigger sret (struct return) protocol for the arrays

### Comparison with Go Implementation

The production Go implementation uses **pointer receivers**:
```go
func (a *Analyzer) analyzeExpr(expr ast.Expr) types.Type { ... }
func (a *Analyzer) analyzeBinary(binExpr *ast.BinaryExpr) types.Type { ... }
```

This passes only an 8-byte pointer, not 480+ bytes of data.

### Known Compiler Issue

From CLAUDE.md Known Issues:
> **Array operations on returned structs**: When a struct containing an array is returned from a function, then passed to another function that reads from AND pushes to that array, it crashes
> - Pattern: `struct S { arr: []int }; fn make() -> S { ... }; fn use(s: S) { let x = s.arr[0]; push(s.arr, x+1); }`

The bootstrap sema hits this pattern:
- Large struct with many arrays
- Nested function calls (analyze_binary → is_numeric)
- Potential sret buffer corruption when stack frames overlap

### Why the Crash Happens

1. **`analyze_binary`** receives a Sema struct by value (480+ bytes on stack)
2. It calls **`is_numeric(s, 1)`**, passing Sema by value AGAIN
3. This creates a nested sret scenario:
   - Caller allocates return buffer for is_numeric at low stack offset
   - is_numeric's stack frame may extend upward
   - If is_numeric makes calls or has local variables, it can overwrite its own return buffer
4. When is_numeric tries to return, the corrupted struct causes a crash (exit 255)

## Possible Solutions

### 1. Eliminate Pass-by-Value (Ideal but Hard)

Restructure to avoid passing Sema around:
- Make Sema a global or use a different architecture
- Break functions into smaller pieces that don't nest

### 2. Reduce Struct Size

Split Sema into multiple smaller structs:
```ease
struct TypeInfo { type_tags: []int, type_names: []string, ... }
struct SymbolTable { sym_names: []string, ... }
struct SemaContext { types: TypeInfo, symbols: SymbolTable, ... }
```

### 3. Use Array Indices Instead of Struct Passing

Store Sema in a global array and pass only an index:
```ease
let mut global_sema_storage = []Sema{}

fn analyze_binary(sema_idx: int, nodes: []AstNode, idx: int) -> int {
    let s = global_sema_storage[sema_idx]
    ...
}
```

### 4. Simplify Call Patterns

Inline simple functions like `is_numeric` to avoid nested struct passing:
```ease
// Instead of calling is_numeric(s, left_type)
let is_num = left_type >= 0 && left_type < len(s.type_tags) && s.type_tags[left_type] == TY_INT()
if !is_num { ... }
```

### 5. Work Around Compiler Bug

Fix the underlying sret buffer corruption in the Ease compiler itself (more involved).

## Recommended Approach

**Short term**: Use solution #4 (inline simple checks) to unblock testing

**Medium term**: Implement solution #3 (global storage with indices)

**Long term**: Fix compiler's struct return handling for large structs with nested calls
