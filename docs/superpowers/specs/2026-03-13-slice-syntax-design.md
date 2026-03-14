# Slice Syntax Design

## Goal

Add Go-style slice expressions (`arr[start:end]`, `arr[start:]`, `arr[:end]`, `arr[:]`) to Ease arrays with shared underlying memory semantics.

## Syntax

```ease
arr[start:end]     // elements from start to end-1
arr[start:]        // elements from start to len(arr)-1
arr[:end]          // elements from 0 to end-1
arr[:]             // full slice (same data, new fat pointer)
```

Three-index `arr[start:end:cap]` is out of scope for now.

## Semantics

- **Shared memory**: Result is a new fat pointer into the same underlying data buffer. Mutations through the slice affect the original array.
- **New fat pointer fields**: `new_data = data_ptr + start * 8`, `new_len = end - start`, `new_cap = old_cap - start`
- **Defaults**: Missing start = 0, missing end = len(arr)
- **No bounds checking**: Consistent with current `arr[i]` behavior. `arr[3:1]` produces a negative length — this is accepted (no runtime check).
- **Slice of slice**: `arr[1:3][0:1]` works naturally since the result is a standard fat pointer.

## Architecture

### Token Layer
No new tokens. `:` is already `TK_COLON` (16).

### AST Layer
New constant in `bootstrap/ease/ast/ast.ease`:
```ease
const EXPR_SLICE = 61    // next available after EXPR_TO_STRING = 60
```

Three children via existing node fields:
- `left`: base array expression (node index)
- `right`: start expression (node index, or -1 for default 0)
- `extra`: end expression (node index, or -1 for default len)

### Parser Layer
In the postfix expression loop of `parse_primary()`, inside the `is_generic_args == 0` branch that currently handles `EXPR_INDEX` creation:

**Before** parsing the first expression inside `[...]`, check if the next token is `TK_COLON`:
- `[:]` — colon then `]`, both defaults → `EXPR_SLICE` with right=-1, extra=-1
- `[:expr]` — colon then expression → parse end expr, `EXPR_SLICE` with right=-1, extra=end_node

**If the first token is not `TK_COLON`**, parse the first expression, then check for `TK_COLON`:
- `[expr:]` — start provided, end defaults → `EXPR_SLICE` with right=start_node, extra=-1
- `[expr:expr]` — both provided → `EXPR_SLICE` with right=start_node, extra=end_node
- `[expr]` — no colon → existing `EXPR_INDEX` path (unchanged)

### IR Layer
New constant in `bootstrap/ease/ir/ir.ease`:
```ease
const OP_ARRAY_SLICE = 87    // next available after OP_BOOL_TO_STR = 86
```

Operands: `(base_vreg, start_vreg, end_vreg)`.
For defaulted bounds (-1 in AST), emit constant `0` (for start) or `OP_ARRAY_LEN` (for end) before the slice op.

### Sema Layer
The result vreg of `OP_ARRAY_SLICE` inherits the array type of the base expression. In irgen, after emitting the slice op: `set_vreg_type(result, get_vreg_type(base_vreg))`. This ensures downstream type-dependent operations (string element access, etc.) work correctly on the slice result.

### Codegen Layer (LLVM IR)
Handle `OP_ARRAY_SLICE`:
1. Load fat pointer from base_vreg (24 bytes: data_ptr, len, cap)
2. Compute `new_data = data_ptr + start * 8`
3. Compute `new_len = end - start`
4. Compute `new_cap = cap - start`
5. Allocate new 24-byte fat pointer via `@malloc` (consistent with `OP_ARRAY_PUSH` which also uses `@malloc` directly; all fields are written immediately so no zero-init needed)
6. Store `[new_data, new_len, new_cap]`
7. Result vreg = pointer to new fat pointer

### String Slicing
Out of scope. String slicing (`s[1:3]`) is a natural follow-up but is not part of this design.

## Testing

New tests in `tests/arrays_test.ease`:
- `TestSliceBasic` — `arr[1:3]` returns correct elements
- `TestSliceFromStart` — `arr[:2]` defaults start to 0
- `TestSliceToEnd` — `arr[2:]` defaults end to len
- `TestSliceFull` — `arr[:]` returns full slice
- `TestSliceSharedMemory` — mutating slice element affects original
- `TestSliceLen` — `len(slice)` returns correct length
- `TestSliceAppend` — `append(slice, val)` works on slices
- `TestSliceOfStrings` — slicing `[]string` arrays
- `TestSliceOfSlice` — `arr[1:4][0:2]` chained slicing

## Validation

After tests pass:
1. Restore `bootstrap/ease/path/` from `tmp/path_backup`
2. Verify `parts[0:len(parts)-1]` compiles and works
3. Verify self-hosting convergence (gen1 == gen2)
