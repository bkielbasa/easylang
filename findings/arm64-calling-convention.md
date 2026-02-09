# ARM64 Calling Convention (AAPCS64)

## Overview

The ARM64 calling convention (AAPCS64 - Application Binary Interface for ARM Architecture 64-bit) defines how functions interact with each other, how parameters are passed, and how the stack is managed.

## Register Usage

### Argument Registers (X0-X7)
```
X0 - First argument / Return value
X1 - Second argument
X2 - Third argument
X3 - Fourth argument
X4 - Fifth argument
X5 - Sixth argument
X6 - Seventh argument
X7 - Eighth argument
```

Arguments beyond 8 are passed on the stack.

### Return Value
```
X0 - Integer/pointer return value (64-bit)
X1 - Second part for 128-bit returns
```

### Caller-Saved (Volatile) Registers
```
X0-X18  - Caller must save if needed after call
```
These registers can be freely used by the callee without preservation.

### Callee-Saved (Non-Volatile) Registers
```
X19-X28 - Callee must preserve
X29 (FP) - Frame Pointer
X30 (LR) - Link Register
SP      - Stack Pointer
```
If a function uses these, it must save and restore them.

### Special Purpose Registers
```
X29 (FP) - Frame Pointer (points to saved FP/LR)
X30 (LR) - Link Register (return address)
SP      - Stack Pointer (must be 16-byte aligned)
XZR     - Zero Register (reads as 0, writes discarded)
X31     - Can be XZR or SP depending on context
```

## Function Prologue and Epilogue

### Typical Prologue
```assembly
_function:
    ; Save frame pointer and link register
    STP X29, X30, [SP, #-0x80]!   ; Pre-decrement SP by 0x80
    MOV X29, SP                    ; Set up frame pointer

    ; Save callee-saved registers if used
    STP X19, X20, [SP, #0x10]
    STP X21, X22, [SP, #0x20]

    ; Allocate local variables (already done by STP #-0x80)
    ; ...function body...
```

### Typical Epilogue
```assembly
    ; Restore callee-saved registers
    LDP X21, X22, [SP, #0x20]
    LDP X19, X20, [SP, #0x10]

    ; Restore frame pointer and return
    LDP X29, X30, [SP], #0x80     ; Post-increment SP by 0x80
    RET                            ; Return to address in X30
```

## Stack Frame Layout

```
High addresses
┌─────────────────┐
│ Previous Frame  │
├─────────────────┤ ← FP points here after prologue
│ Saved X30 (LR)  │
├─────────────────┤
│ Saved X29 (FP)  │
├─────────────────┤
│ Saved X19-X28   │ (if used)
├─────────────────┤
│ Local Variables │
├─────────────────┤
│ Spilled Temps   │
├─────────────────┤
│ Outgoing Args   │ (args 9+)
└─────────────────┘ ← SP
Low addresses
```

## Stack Alignment Requirement

**CRITICAL**: The stack pointer (SP) must be **16-byte aligned** at all times.

### Why This Matters
```assembly
; ❌ WRONG - Not aligned
SUB SP, SP, #20  ; SP now misaligned!

; ✅ CORRECT - Aligned to 16 bytes
SUB SP, SP, #32  ; Always use multiples of 16
```

Misaligned stack causes:
- **Bus errors** on some ARM implementations
- **Undefined behavior** in syscalls
- **Crashes** in library calls

## Calling a Function

### Example: add(10, 32)

```assembly
; Setup arguments
MOV X0, #10    ; First argument
MOV X1, #32    ; Second argument

; Call function
BL add         ; Branch with Link (saves return address in X30)

; Result now in X0
MOV X19, X0    ; Save result if needed
```

### What BL Does
```
1. PC + 4 → X30          (save return address)
2. target_address → PC   (jump to function)
```

## Returning from Function

### Simple Return
```assembly
add:
    ADD X0, X0, X1  ; Compute result in X0
    RET             ; Return (PC ← X30)
```

### Return with Epilogue
```assembly
add:
    STP X29, X30, [SP, #-16]!
    MOV X29, SP

    ; ... function body ...
    ADD X0, X0, X1

    LDP X29, X30, [SP], #16
    RET
```

## Special Case: main Function

The `main` function is special because it's called by the OS, not by other code.

### Approach 1: Exit Syscall (No RET)
```assembly
main:
    ; ... do work ...

    ; Exit via syscall (don't use RET)
    MOV X0, #0          ; Exit code
    MOV X16, #1         ; exit syscall
    SVC #0x80           ; Make syscall
    ; Never returns
```

### Approach 2: Return to C Runtime
```assembly
main:
    STP X29, X30, [SP, #-16]!
    MOV X29, SP

    ; ... do work ...

    ; Return to C runtime which calls exit()
    MOV X0, #0          ; Return value
    LDP X29, X30, [SP], #16
    RET                 ; Return to __start or dyld
```

## Parameter Passing Examples

### 1. Two Parameters
```assembly
; Call: add(5, 10)
MOV X0, #5
MOV X1, #10
BL add

; Implementation:
add:
    ADD X0, X0, X1
    RET
```

### 2. Four Parameters
```assembly
; Call: func(1, 2, 3, 4)
MOV X0, #1
MOV X1, #2
MOV X2, #3
MOV X3, #4
BL func

func:
    ; X0, X1, X2, X3 contain arguments
    RET
```

### 3. Nine Parameters (Uses Stack)
```assembly
; Call: func(1, 2, 3, 4, 5, 6, 7, 8, 9)
MOV X0, #1       ; Args 1-8 in registers
MOV X1, #2
; ... X2-X7 ...
STR X8, [SP, #0] ; Arg 9 on stack
BL func

func:
    ; Access 9th argument from stack
    LDR X9, [SP, #0]
    RET
```

## Syscall Convention (Different!)

System calls use a different convention:

### Registers
```
X0-X7 - Arguments (same as functions)
X16   - Syscall number
X0    - Return value / Error code
```

### Example: write(1, "Hi", 2)
```assembly
MOV X0, #1              ; fd = stdout
ADRP X1, msg@PAGE       ; buf = "Hi"
ADD X1, X1, msg@PAGEOFF
MOV X2, #2              ; count = 2
MOV X16, #4             ; syscall number (write)
SVC #0x80               ; Supervisor call

; Check result
CMP X0, #0
B.LT error              ; Negative = error
```

## Common Mistakes

### ❌ Forgetting to Save LR
```assembly
function:
    BL other_func  ; Overwrites X30!
    RET            ; Returns to wrong place!
```

### ✅ Proper Frame Setup
```assembly
function:
    STP X29, X30, [SP, #-16]!  ; Save LR
    MOV X29, SP

    BL other_func

    LDP X29, X30, [SP], #16    ; Restore LR
    RET                        ; Correct return
```

### ❌ Misaligned Stack
```assembly
SUB SP, SP, #8  ; ❌ Now misaligned!
```

### ✅ Aligned Stack
```assembly
SUB SP, SP, #16  ; ✅ Always multiple of 16
```

### ❌ Using Callee-Saved Without Saving
```assembly
function:
    MOV X19, X0  ; ❌ Corrupts X19 without saving!
    RET
```

### ✅ Save/Restore Callee-Saved
```assembly
function:
    STP X29, X30, [SP, #-32]!
    STP X19, X20, [SP, #16]  ; Save X19-X20

    MOV X19, X0  ; ✅ OK to use now

    LDP X19, X20, [SP, #16]  ; Restore
    LDP X29, X30, [SP], #32
    RET
```

## Function Without Prologue (Leaf Function)

If a function:
- Doesn't call other functions
- Doesn't use callee-saved registers
- Doesn't need stack space

It can skip the prologue:

```assembly
simple_add:
    ADD X0, X0, X1
    RET  ; That's it!
```

But **this only works for trivial functions**. Most real functions need proper setup.

## Debugging Tips

### Check Stack Alignment
```assembly
; Add assertion in prologue
MOV X16, SP
AND X16, X16, #15
CBZ X16, ok  ; Should be zero
BRK #0       ; Crash if misaligned
ok:
```

### Verify Saved Registers
```bash
# In lldb:
(lldb) register read x29 x30
(lldb) x/2gx $sp  # Should show saved FP/LR
```

### Stack Trace
```bash
(lldb) bt  # Backtrace relies on proper frame pointers
```

## Key Takeaways

1. **X0-X7** for arguments, **X0** for return value
2. **X19-X28** must be preserved by callee
3. **Stack must be 16-byte aligned**
4. **STP/LDP** are your friends for save/restore
5. **BL** saves return address in X30
6. **RET** returns to address in X30
7. **main** can use exit syscall instead of RET
8. **Syscalls** use X16 for syscall number
9. **Always save X30** if you call other functions
10. **Frame pointer (X29)** optional but helpful for debugging

## References

- [ARM AAPCS64 Documentation](https://github.com/ARM-software/abi-aa/blob/main/aapcs64/aapcs64.rst)
- [ARM Architecture Reference Manual](https://developer.arm.com/documentation/ddi0487/latest)
- Procedure Call Standard for the ARM 64-bit Architecture (AArch64)
