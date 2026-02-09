# Bootstrap Compiler Debugging Notes

## Critical Issue: os.ReadFile Hangs (Feb 9, 2026)

### Symptom
When the bootstrap compiler (compiled by Go compiler) tries to read a source file, it hangs indefinitely:
```bash
$ ./bootstrap_compiler tmp/test.ease
=== Bootstrap Ease Compiler ===
Reading file: tmp/test.ease
[HANGS HERE - never prints "File loaded successfully"]
```

### What Works
- Bootstrap compiler with hardcoded test program (no file reading): ✅ WORKS
- Bootstrap compiler compilation itself: ✅ WORKS
- Generated binaries from bootstrap compiler: ✅ WORK CORRECTLY

### What Doesn't Work
- Any attempt to read a file via os.ReadFile: ❌ HANGS

### Investigation Results

1. **Not file-specific**: Happens with any .ease file (test_return.ease, test_multiarg.ease, etc.)
2. **Not path-specific**: Happens with both relative and absolute paths
3. **Not size-specific**: Even tiny 61-byte files hang
4. **Not my recent changes**: Reverting commits shows hang exists in earlier versions too
5. **Introduced during multi-arg work**: The hang was introduced sometime during or before commit cf31755

### Technical Details

The Go compiler implements `os.ReadFile` as a builtin that generates ARM64 code including:
- Open syscall
- **emitBumpAlloc** call (allocates 1MB buffer using X25/X26 heap state)
- Read loop
- Close syscall

The hang happens early (right after printing "Reading file:"), suggesting it's in the open or initial read operation.

### Hypothesis

Possible causes:
1. **Heap state initialization**: emitBumpAlloc requires X25/X26 to be initialized in function prologue
2. **Syscall issue**: open() syscall might be failing and code doesn't handle error
3. **Infinite loop**: Read loop might not terminate correctly
4. **Register corruption**: Some register used by file reading is being clobbered

### Attempted Fixes

- ❌ Clean rebuild
- ❌ Different test files
- ❌ Absolute paths
- ❌ Reverting to earlier commits
- ❌ Checking for code errors (none found)

### Current Workaround

The bootstrap compiler works fine with the hardcoded test program:
```bash
$ ./bootstrap_compiler   # No arguments
=== Bootstrap Ease Compiler ===
No file specified, using test program
[COMPILES SUCCESSFULLY]
```

### Next Steps

1. Debug with lldb to see exact hang location
2. Check if Go compiler's os.ReadFile implementation changed
3. Verify X25/X26 initialization in function prologues
4. Add error checking after syscalls
5. Consider implementing os.ReadFile in bootstrap compiler itself (not as Go builtin)

### Files Affected

- All versions of bootstrap compiler after commit cf31755
- Does NOT affect Go compiler itself
- Does NOT affect generated binaries from bootstrap compiler

## Resolution ✅ (Feb 9, 2026)

### Root Cause Found
X25 and X26 (heap state registers) were **only initialized in main() function**:
```go
// pkg/codegen/arm64/emit.go:545
if e.fn.Name == "main" {
    e.asm.MOV(X27, X0)  // Save argc
    e.asm.MOV(X28, X1)  // Save argv
    e.asm.MOVimm(X25, 0)  // ← ONLY HERE!
    e.asm.MOVimm(X26, 0)  // ← ONLY HERE!
}
```

But `os.ReadFile` (and other builtins) use `emitBumpAlloc` which checks X25:
```go
// emitBumpAlloc checks if heap is initialized
e.asm.CMP(X25, XZR)  // ← X25 was garbage in non-main functions!
```

### The Fix
Move X25/X26 initialization outside the `if main` block:
```go
// Initialize heap state for ALL functions
e.asm.MOVimm(X25, 0)
e.asm.MOVimm(X26, 0)
```

### Why This Worked
- Bootstrap compiler's top-level code is NOT in main()
- When it called os.ReadFile, X25 contained garbage
- emitBumpAlloc's check (CMP X25, XZR) had undefined behavior
- This caused hang/crash when trying to initialize heap

### Test Results
After fix (commit 664a63b):
- ✅ test_simple.ease - Compiles successfully
- ✅ test_multiarg.ease - Compiles successfully
- ✅ test_minimal_heap.ease - Compiles successfully
- ✅ test_heap_alloc.ease - Compiles successfully (poke/peek work!)

## Status

**RESOLVED**: Bootstrap compiler now reads files correctly! ✅
**IMPACT**: Self-hosting milestone unblocked.
**COMMIT**: 664a63b - FIX: Critical heap register initialization bug

---

*Last updated: Feb 9, 2026*
*Resolution time: ~2 hours from bug discovery to fix*
