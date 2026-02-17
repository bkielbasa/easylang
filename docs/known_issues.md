# Known Issues

## Global Array Limit (SIGBUS with 10+ arrays)

**Status**: RESOLVED - Fixed by LLVM backend migration

**Description**:
Programs with 10 or more global array variables used to crash with SIGBUS (exit code 138) during runtime initialization with the ARM64 backend. This was fixed when the LLVM backend became the default codegen target.

**Regression test**: `testdata/global_arrays.ease` - tests 15 global arrays with initialization and access.

Both LLVM and ARM64 backends now handle 30+ global arrays correctly.

## Struct Return Buffer Corruption with Nested Calls

**Status**: Documented workaround exists

**Description**:
When a function returns a large struct (>100 bytes), the sret buffer is allocated at a low offset in the caller's stack frame. If the caller then makes nested function calls with large stack frames, those frames can extend upward and overwrite the sret buffer, corrupting the returned struct data.

**Workaround**: Keep structs small or avoid returning them from functions that will be called recursively.

**See**: CLAUDE.md Known Issues section
