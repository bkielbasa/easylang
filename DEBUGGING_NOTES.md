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

## Status

**BLOCKED**: Cannot use bootstrap compiler to compile source files from disk.
**IMPACT**: High - prevents self-compilation testing.
**PRIORITY**: Critical - needs resolution before self-hosting milestone.

---

*Last updated: Feb 9, 2026*
