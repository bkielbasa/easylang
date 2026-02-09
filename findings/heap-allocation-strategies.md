# Heap Allocation Strategies: Bump Allocator vs Direct mmap

## The Problem

When a program needs dynamic memory, there are multiple strategies for managing heap allocation. This document compares two approaches: calling `mmap` for every allocation vs using a bump allocator.

## Strategy 1: Direct mmap Per Allocation (Naive)

### How it Works
```assembly
; Every heap_alloc(size) call:
MOV X0, #0              ; addr = NULL
MOV X1, size            ; len = requested size
MOV X2, #3              ; PROT_READ | PROT_WRITE
MOV X3, #0x1002         ; MAP_ANON | MAP_PRIVATE
MOV X4, #-1             ; fd = -1
MOV X5, #0              ; offset = 0
MOV X16, #0x20000C5     ; mmap syscall
SVC #0x80               ; Make syscall
```

### Problems
1. **Syscall overhead**: Every allocation requires an expensive kernel call
2. **Memory fragmentation**: Each mmap creates a separate memory region
3. **Page alignment**: mmap works in page-sized chunks (4KB on macOS)
4. **Inefficient for small allocations**: Allocating 8 bytes still uses 4KB

### When to Use
- Large allocations (> 1MB)
- Isolated memory regions (security)
- Memory you might want to `munmap` independently

## Strategy 2: Bump Allocator (Go Compiler's Approach)

### Core Concept

Allocate a large memory region once, then "bump" a pointer forward for each allocation:

```
Heap region (1MB):
[███████░░░░░░░░░░░░░░░░░░░░]
 ^      ^
 start  current_ptr

After allocating 100 bytes:
[███████████░░░░░░░░░░░░░░░░]
 ^          ^
 start      current_ptr (bumped forward)
```

### Implementation with Registers

Go's compiler uses special registers to track heap state:
- **X25**: Heap current pointer (next free byte)
- **X26**: Heap end pointer (limit of current region)

### Complete Algorithm

```assembly
heap_alloc:
    ; 1. Align requested size to 8 bytes
    ADD X19, X_size, #7
    LSR X19, X19, #3
    LSL X19, X19, #3        ; X19 = aligned size

    ; 2. Check if heap is initialized
    CMP X25, XZR
    B.NE heap_initialized

    ; 3. First time: allocate 1MB region
    MOV X0, #0
    MOV X1, #0x100000       ; 1MB
    MOV X2, #3
    MOV X3, #0x1002
    MOV X4, #-1
    MOV X5, #0
    MOV X16, #0x20000C5
    SVC #0x80

    ; 4. Check for mmap failure
    CMN X0, #1              ; Compare with -1
    B.NE mmap_ok
    MOV X0, #0              ; Return NULL on failure
    RET

mmap_ok:
    ; 5. Initialize heap state
    MOV X25, X0             ; heap_ptr = mmap result
    MOV X17, #0x100000
    ADD X26, X0, X17        ; heap_end = start + 1MB

heap_initialized:
    ; 6. Check if we have space
    ADD X17, X25, X19       ; X17 = current + size
    CMP X17, X26
    B.LE have_space

    ; 7. Out of space: allocate another region
    ; (Simplified: would need better strategy)
    MOV X0, #0
    ...

have_space:
    ; 8. Bump allocate
    MOV X0, X25             ; Return current pointer
    ADD X25, X25, X19       ; Bump pointer forward
    RET
```

### Key Features

1. **Amortized O(1) allocation**: Most allocations just increment a pointer
2. **Batch syscalls**: One mmap call serves many allocations
3. **Cache-friendly**: Consecutive allocations are contiguous in memory
4. **No per-allocation metadata**: No headers or bookkeeping per object

### Limitations

1. **No individual deallocation**: Can't free individual allocations
2. **Memory waste**: Can't reclaim space until entire region is freed
3. **Need garbage collection**: Or manual region-based deallocation
4. **Fragmentation across regions**: Multiple regions if you run out of space

## Performance Comparison

### Allocating 1000 x 64-byte chunks

**Direct mmap**:
- 1000 syscalls
- 1000 x 4KB = 4MB of memory (due to page alignment)
- Estimated time: ~1ms on macOS (1μs per syscall)

**Bump allocator**:
- 1 syscall (1MB region)
- 64KB of memory actually used
- Estimated time: ~1μs + 1000 x (pointer bump) = ~2μs total

**Speedup**: ~500x faster

## Hybrid Approach: Size Classes

Many production allocators combine strategies:

```
Small allocations (< 32KB):  Bump allocator
Large allocations (> 32KB):  Direct mmap
```

This balances efficiency and fragmentation.

## Function Prologue Requirements

For bump allocator to work, **registers must be initialized**:

```assembly
_function_entry:
    ; Save frame pointer and link register
    STP X29, X30, [SP, #-0x80]!
    MOV X29, SP

    ; Initialize heap state (if not done)
    MOV X25, #0              ; First-time flag
    MOV X26, #0

    ; ... rest of function
```

Without proper initialization, X25/X26 contain **garbage values**, causing:
- Incorrect heap pointer arithmetic
- Crashes when trying to allocate
- Memory corruption

## Why the Naive Approach Failed

In our bootstrap compiler, we tried direct mmap without:
1. ✅ Correct syscall parameters (fixed with MOVZ/MOVK)
2. ❌ Error checking after mmap
3. ❌ Function prologue to initialize stack
4. ❌ Register preservation across syscalls

Result: mmap failed (returned error code 22 = EINVAL), we didn't check for error, and tried to use the error code as a memory address → **crash**.

## Lessons Learned

1. **Bump allocators are the standard** for modern language runtimes
2. **Syscalls are expensive** - batch them when possible
3. **Always check syscall return values** - errors are common
4. **Function prologues matter** - they set up the execution environment
5. **Register discipline is critical** - know which registers are callee-saved
6. **Alignment requirements** - ARM64 stack must be 16-byte aligned

## References

- [TCMalloc](https://google.github.io/tcmalloc/) - Google's high-performance allocator
- [mimalloc](https://github.com/microsoft/mimalloc) - Microsoft's allocator
- ARM64 Calling Convention (AAPCS64)
- macOS mmap(2) man page
