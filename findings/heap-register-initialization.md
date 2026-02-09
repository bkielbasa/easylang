# Heap Register Initialization Requirement

**Date**: February 9, 2026
**Status**: ✅ Identified and Fixed

## Problem

Self-compiled bootstrap compiler binaries failed immediately with "Cannot allocate memory" error, even for trivial programs. The Go-compiled bootstrap compiler worked fine, but binaries it generated that then tried to compile other programs would crash.

## Root Cause

The bootstrap compiler was not initializing heap management registers (X25 and X26) at the start of the main function. These registers track the bump allocator state:

- **X25** (heap_ptr): Current position in heap allocation
- **X26** (heap_end): End of current heap region

The Go compiler's ARM64 backend initializes these to 0 at program startup, indicating "heap not yet allocated". Without this initialization, the registers contain undefined values, causing heap_alloc to malfunction.

## Investigation Process

### 1. Symptom Analysis

```bash
$ lldb ./tmp/test_output -o "process launch -- tmp/test_trivial.ease"
error: Cannot allocate memory
```

Even the simplest program failed immediately, suggesting the issue was in startup code, not program logic.

### 2. Comparing Generated Code

**Go-compiled bootstrap compiler binary:**
```assembly
_main:
0000000100026608  mov   x17, #0x1af0
000000010002660c  sub   sp, sp, x17
0000000100026610  stp   x29, x30, [sp]
0000000100026614  mov   x29, sp
0000000100026618  mov   x27, x0
000000010002661c  mov   x28, x1
0000000100026620  mov   x25, #0x0       ← Heap ptr init
0000000100026624  mov   x26, #0x0       ← Heap end init
0000000100026628  mov   x16, #0x0
...
```

**Self-compiled bootstrap compiler binary:**
```assembly
0000000100000210  mov   x1, #0x1
0000000100000214  mov   x0, x1
0000000100000218  ret
... (starts with helper functions, no heap init)
```

The self-compiled binary never initialized X25/X26!

### 3. Why This Matters

The bootstrap compiler's heap_alloc implementation checks X25 to see if the heap is initialized:

```ease
// Pseudocode for heap_alloc
if X25 == 0 {
    // First allocation: mmap 1MB
    X0 = mmap(NULL, 1MB, PROT_READ|WRITE, MAP_ANON|PRIVATE, -1, 0)
    X25 = X0          // heap_ptr = start of region
    X26 = X0 + 1MB    // heap_end = end of region
}
// ... rest of allocation logic
```

If X25 contains garbage (not 0), the check fails and heap_alloc tries to use the garbage value as a heap pointer, leading to invalid memory access and "Cannot allocate memory".

## Solution

### Code Changes

**Pass 2: Generate heap init** (bootstrap/compiler.ease:2770-2785):
```ease
if instr.op == OP_LABEL() {
    if instr.str_val != "" {
        push(func_names, instr.str_val)
        push(func_positions, instr_count)
        if instr.str_val == "main" {
            main_pos = instr_count
            // Initialize heap registers for main function
            let mov_x25_zero = encode_mov_reg(25, 31)  // MOV X25, XZR
            push(code, mov_x25_zero)
            instr_count = instr_count + 1
            let mov_x26_zero = encode_mov_reg(26, 31)  // MOV X26, XZR
            push(code, mov_x26_zero)
            instr_count = instr_count + 1
        }
    }
}
```

**Pass 1: Count heap init instructions** (bootstrap/compiler.ease:2698-2712):
```ease
if instr.op == OP_LABEL() {
    push(label_ids, instr.dest)
    push(label_positions, instr_count)
    if instr.str_val != "" {
        current_func = instr.str_val
        // Main function has 2 extra instructions for heap init
        if current_func == "main" {
            instr_count = instr_count + 2  // MOV X25, XZR + MOV X26, XZR
        }
    }
}
```

### Generated Code

After fix, simple programs now start with:
```assembly
0000000100000210  mov   x25, xzr    ; Initialize heap_ptr = 0
0000000100000214  mov   x26, xzr    ; Initialize heap_end = 0
0000000100000218  mov   x1, #0x2a   ; Program logic starts here
000000010000021c  mov   x0, x1
0000000100000220  mov   x16, #0x1
...
```

## Test Results

### Before Fix
```bash
$ ./tmp/bootstrap_compiler tmp/test_trivial.ease  # Creates binary
$ lldb ./tmp/test_output -o "process launch"
error: Cannot allocate memory  # FAIL
```

### After Fix
```bash
$ go run cmd/ease/main.go build bootstrap/compiler.ease -o tmp/bootstrap_compiler
$ ./tmp/bootstrap_compiler tmp/test_trivial.ease
$ lldb ./tmp/test_output -o "process launch" -o "exit"
Process 12345 exited with status = 42 (0x0000002a)  # SUCCESS
```

Simple programs compiled by the fixed bootstrap compiler now execute correctly!

## Why Go Compiler Worked

The Go compiler's ARM64 backend has standard function prologue code that:
1. Sets up stack frame
2. Saves frame pointer and link register
3. **Initializes special registers like X25/X26**
4. Preserves callee-saved registers

The bootstrap compiler had to manually implement this prologue logic, and initially missed the heap register initialization.

## Remaining Issue

While simple programs now work, the self-compiled bootstrap compiler (when compiling itself) still fails with memory errors. This suggests additional issues in:
- Complex heap allocation patterns
- Function call conventions for multi-argument functions
- Some other codegen bug that only manifests in large programs

## Technical Notes

### Register Usage Convention

**ARM64 calling convention (AAPCS64):**
- X0-X7: Arguments and return values
- X8: Indirect result location
- X9-X15: Temporary (caller-saved)
- X16-X17: Intra-procedure-call temporary
- X18: Platform register (reserved)
- X19-X28: Callee-saved
- X29: Frame pointer
- X30: Link register
- XZR: Zero register (reads as 0, writes discarded)

**Ease compiler usage:**
- X25: heap_ptr (current allocation position)
- X26: heap_end (end of current heap region)
- X27-X28: Used by Go runtime (argc/argv in generated code)

### Why XZR for Initialization

Using `MOV X25, XZR` instead of `MOV X25, #0`:
- Same result (register set to 0)
- XZR encodes more efficiently (simpler instruction)
- Standard ARM64 idiom for zeroing registers

## Lessons Learned

1. **Runtime state matters**: Compilers must initialize all assumed state, not just program logic
2. **Register conventions are critical**: Missing one detail breaks everything
3. **Test incrementally**: Simple programs caught this before complex scenarios confused debugging
4. **Compare working vs broken**: Disassembling both binaries revealed the exact difference
5. **Self-hosting reveals bugs**: Issues invisible in simple tests appear when compiler compiles itself

## Future Work

1. **Investigate remaining self-compilation failure**: Why does compiler.ease compilation still fail?
2. **Add register initialization tests**: Verify all special registers are set up correctly
3. **Document register usage**: Create comprehensive guide for bootstrap compiler's register allocation
4. **Function prologue/epilogue**: Standardize and test thoroughly

## References

- ARM AAPCS64: https://github.com/ARM-software/abi-aa/blob/main/aapcs64/aapcs64.rst
- Go ARM64 backend: src/cmd/internal/obj/arm64/
- bootstrap/compiler.ease lines 2698-2785 (implementation)
