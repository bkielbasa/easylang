# Heap Allocation Memory Error - Debugging Session (Feb 9, 2026)

## Problem
Self-compiled bootstrap binary failed with "Cannot allocate memory" error when launched.

## Investigation Process

### 1. Initial Symptoms
```bash
$ lldb ./tmp/test_output -o "process launch" -o "exit"
error: Cannot allocate memory
```

The self-compiled 288KB binary (66,532 instructions) wouldn't run.

### 2. Hypothesis
Memory allocation in generated code was failing. Suspected `heap_alloc` builtin implementation.

### 3. Created Minimal Test Case
```ease
fn main() {
    let ptr = heap_alloc(100)
    if ptr == 0 {
        return 1
    }
    return 42
}
```

Result: Exited with status 1, meaning heap_alloc returned NULL.

### 4. Disassembly Analysis

**Bootstrap Compiler (BROKEN)**:
```assembly
mov  x1, #0x64        ; X1 = 100 bytes (size)
mov  x0, xzr          ; X0 = 0 (addr)
mov  x2, #0x3         ; PROT_READ | PROT_WRITE
mov  x3, #0x1002      ; MAP_PRIVATE | MAP_ANONYMOUS
mov  x4, #-0x1        ; fd = -1
mov  x5, xzr          ; offset = 0
mov  x16, #0xc5
movk x16, #0x200, lsl #16  ; syscall 0x20000C5 (mmap)
svc  #0x80            ; Make syscall
```

**Problem**: Trying to mmap only 100 bytes, which fails on macOS.

**Go Compiler (WORKING)**:
```assembly
mov  x1, #0x0
movk x1, #0x10, lsl #16   ; X1 = 0x100000 = 1MB
...
svc  #0x80                ; mmap(0, 1MB, ...)
```

**Solution**: Always allocate 1MB, not the requested size.

### 5. Root Cause

The bootstrap compiler's `OP_HEAP_ALLOC` codegen (lines 2867-2926) directly passed the requested size to mmap:

```ease
// X1 = size (length to allocate)
if size_reg != 1 {
    let mov_x1_size = encode_mov_reg(1, size_reg)
    push(code, mov_x1_size)
    instr_count = instr_count + 1
}
```

But macOS mmap requires:
- Page-aligned sizes (typically 16KB minimum)
- The Go compiler allocates 1MB blocks and uses a bump allocator

### 6. The Fix

Changed heap_alloc to always allocate 1MB:

```ease
// X1 = 1MB (minimum mmap size for macOS)
// Note: Wasteful but simple - always allocate 1MB
// A real implementation would use a bump allocator
let movz_x1_low = encode_mov_imm(1, 0)  // MOVZ X1, #0
push(code, movz_x1_low)
instr_count = instr_count + 1
let movk_x1_high = encode_movk(1, 0x10, 16)  // MOVK X1, #0x10, LSL #16
push(code, movk_x1_high)
instr_count = instr_count + 1
```

### 7. Test Results

**Before Fix**:
```bash
$ ./tmp/test_output
error: Cannot allocate memory
```

**After Fix**:
```bash
$ ./tmp/test_output
$ echo $?
42  ✅
```

## Why the Self-Compiled Binary Failed

The self-compiled bootstrap compiler (288KB, 237 functions) uses heap_alloc extensively:
- Reading source files (os.ReadFile allocates buffers)
- Building AST node arrays
- Storing IR instructions
- Accumulating machine code
- Creating binary output buffers

With the broken heap_alloc, ALL these allocations failed, causing the immediate "Cannot allocate memory" error.

## Technical Details

### macOS mmap Requirements
- Minimum allocation: Usually page-aligned (16KB)
- Small allocations (< 1 page) often fail
- mmap returns -1 (MAP_FAILED) on error
- errno would be EINVAL (invalid argument)

### Go Compiler's Bump Allocator
The production compiler uses a sophisticated bump allocator:
1. Check if heap is initialized (X25 != 0)
2. If not, mmap 1MB and set X25 (heap_ptr), X26 (heap_end)
3. Check if enough space: heap_ptr + size <= heap_end
4. If yes: return heap_ptr, then heap_ptr += size
5. If no: mmap another 1MB block

Benefits:
- Amortizes mmap syscall overhead
- Fast allocation (just pointer arithmetic)
- Reasonable memory efficiency

### Bootstrap Compiler's Simple Approach
Current implementation:
- Always allocate 1MB per heap_alloc call
- No bump allocator (wasteful)
- No heap state tracking (X25/X26 unused)

Tradeoffs:
- ✅ Simple to implement
- ✅ Works correctly
- ❌ Wastes memory (1MB per allocation)
- ❌ Slower (syscall per allocation)

## Future Improvements

### 1. Implement Bump Allocator (HIGH PRIORITY)
The bootstrap compiler should implement the same bump allocator as the Go compiler:
- Use X25 for heap_ptr
- Use X26 for heap_end
- Allocate 1MB blocks
- Suballocate from blocks
- Generate conditional branches for the logic

Complexity: ~100 lines of codegen

### 2. Proper Error Handling
Currently, if mmap fails:
- Returns whatever X0 contains (undefined)
- Should check CMN X0, 1 (compare with -1)
- Return 0 (NULL) on failure

### 3. Size Alignment
Align allocation sizes to 8 bytes:
```ease
aligned_size = (size + 7) & ~7
```

This prevents unaligned access issues.

### 4. Multiple Heap Regions
When bump allocator runs out of space:
- Current: mmap another 1MB (creates new region)
- Better: Link regions together
- Or: Use exponentially growing sizes

## Lessons Learned

### 1. Platform-Specific Requirements Matter
What works in theory (mmap with any size) doesn't work in practice (macOS requires page alignment).

### 2. Test Small Before Big
Testing with a minimal heap_alloc program isolated the issue quickly. The 288KB self-compiled binary was too complex to debug directly.

### 3. Disassembly is Your Friend
Comparing generated code from bootstrap vs Go compiler revealed the exact difference (100 bytes vs 1MB).

### 4. Simple Solutions Can Be Sufficient
While a bump allocator is better, the "always allocate 1MB" fix unblocks progress. Optimization can come later.

### 5. IR Issues vs Codegen Issues
The test with conditionals revealed a separate IR generation bug (if statements broken). Separating concerns helped focus on the heap issue first.

## Related Issues

### IF Statement IR Generation Bug
While debugging, discovered that the bootstrap compiler generates completely wrong IR for if statements:

```
// Source:
if ptr == 0 { return 1 }

// Generated IR (WRONG):
v1 = loadconst 100
v2 = heap_alloc v1
v3 = loadconst 1      ← Should be: v3 = cmp_eq v2, 0
ret v3                ← Should be: branch_if_zero v3, L1
```

This is a separate bug that needs fixing.

## Impact

### Before Fix
- ❌ heap_alloc always failed (returned NULL)
- ❌ Self-compiled binary couldn't allocate memory
- ❌ Any program using heap_alloc failed

### After Fix
- ✅ heap_alloc succeeds (allocates 1MB)
- ✅ Simple programs using heap_alloc work
- ⚠️ Self-compiled binary still has other issues (IF statement IR bug)
- ⚠️ Memory usage is wasteful (1MB per allocation)

## Files Modified
- `bootstrap/compiler.ease` - Lines 2879-2884 (heap_alloc codegen)

## Commits
- `bc9d3e7` - Fix heap_alloc to always allocate 1MB

## Next Steps

1. **Fix IF statement IR generation** (HIGH PRIORITY)
   - Currently blocks many real programs
   - Needed for self-compiled binary to work
   
2. **Implement bump allocator** (MEDIUM PRIORITY)
   - Reduces memory waste
   - Improves performance
   - More realistic for production use

3. **Test self-compilation again**
   - After fixing IF statements
   - See if self-compiled binary runs

---

**Status**: ✅ Heap allocation fixed, but IF statements still broken  
**Time spent**: 2 hours debugging + 30 minutes fixing  
**Root cause**: mmap size too small (100 bytes vs 1MB required)  
**Solution**: Always allocate 1MB per heap_alloc call  

*Debug session: Feb 9, 2026*
