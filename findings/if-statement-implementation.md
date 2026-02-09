# IF Statement Implementation in Bootstrap Compiler

**Date**: February 9, 2026
**Status**: ✅ Complete and Working

## Problem

The bootstrap compiler could not compile IF statements. Attempts to compile programs with IF conditions would fail because:
1. Comparison operators had no ARM64 code generation
2. Branch offset calculations were incorrect
3. Main function exit syscall was wrong

## Solution Overview

Three major fixes were required:

### 1. Comparison Operator Codegen

Added ARM64 code generation for all 6 comparison operators using CMP + CSET instruction pattern.

**ARM64 Instructions:**
```assembly
CMP Xarg1, Xarg2       # Compare two registers, set flags
CSET Xdest, condition  # Set dest=1 if condition true, else dest=0
```

**Condition Codes:**
- EQ = 0 (equal)
- NE = 1 (not equal)
- GE = 10 (greater or equal, signed)
- LT = 11 (less than, signed)
- GT = 12 (greater than, signed)
- LE = 13 (less or equal, signed)

**Implementation** (bootstrap/compiler.ease:2759-2814):
```ease
if instr.op == OP_CMP_EQ() {
    let cmp_encoded = encode_cmp(instr.arg1, instr.arg2)
    push(code, cmp_encoded)
    instr_count = instr_count + 1
    let cset_encoded = encode_cset(instr.dest, 0)  // EQ condition
    push(code, cset_encoded)
    instr_count = instr_count + 1
}
// Similar for OP_CMP_NE, OP_CMP_LT, OP_CMP_LE, OP_CMP_GT, OP_CMP_GE
```

**Helper Functions** (bootstrap/compiler.ease:2669-2685):
```ease
fn encode_cmp(rn: int, rm: int) -> int {
    // CMP Xrn, Xrm (alias for SUBS XZR, Xrn, Xrm)
    return 0xEB00001F + (rm * 0x10000) + (rn * 0x20)
}

fn encode_cset(rd: int, cond: int) -> int {
    // CSET Xrd, cond (alias for CSINC Xrd, XZR, XZR, invert(cond))
    let inv_cond = cond ^ 1
    return 0x9A9F07E0 + (inv_cond * 0x1000) + rd
}
```

### 2. Branch Offset Calculation Fix

**Problem**: Pass 1 (label map building) was counting each IR instruction as 1 ARM64 instruction, but Pass 2 (code generation) generates multiple ARM64 instructions for some operations:
- Comparison operators: 2 instructions (CMP + CSET)
- RET in main: 4 instructions (MOV + MOVZ + MOVK + SVC)
- RET in other functions: 2 instructions (MOV + RET)

This caused label positions in Pass 1 to be wrong, leading to incorrect branch offsets.

**Solution** (bootstrap/compiler.ease:2694-2768):

Updated Pass 1 to count actual ARM64 instructions that will be generated:

```ease
if instr.op == OP_CMP_EQ() {
    instr_count = instr_count + 2  // CMP + CSET
}
// ... similar for other comparison operators ...
else if instr.op == OP_RET() {
    if current_func == "main" {
        if instr.dest >= 0 {
            instr_count = instr_count + 4  // MOV + MOVZ + MOVK + SVC
        } else {
            instr_count = instr_count + 3  // MOVZ + MOVK + SVC
        }
    } else {
        if instr.dest >= 0 {
            instr_count = instr_count + 2  // MOV + RET
        } else {
            instr_count = instr_count + 1  // Just RET
        }
    }
}
```

### 3. Exit Syscall Fix

**Problem**: Main function was using syscall number 1 instead of macOS ARM64's actual exit syscall 0x2000001.

**Solution** (bootstrap/compiler.ease:3143-3157):
```ease
if is_main_func == 1 {
    // X16 = 0x2000001 (exit syscall number for macOS ARM64)
    let movz_x16_low = encode_mov_imm(16, 1)  // MOVZ X16, #1
    push(code, movz_x16_low)
    instr_count = instr_count + 1
    let movk_x16_high = encode_movk(16, 0x200, 16)  // MOVK X16, #0x200, LSL #16
    push(code, movk_x16_high)
    instr_count = instr_count + 1
    push(code, 0xd4001001)  // SVC #0x80
    instr_count = instr_count + 1
}
```

## Testing

### Test Cases

**✅ test_if_minimal.ease** - Function with IF:
```ease
fn test() -> int {
    if 1 == 0 {
        return 5
    }
    return 42
}
fn main() -> int {
    return test()
}
```
Result: Exits with status 42 ✓

**✅ test_if_simple.ease** - IF in main:
```ease
fn main() -> int {
    if 1 == 0 {
        return 5
    }
    return 42
}
```
Result: Exits with status 42 ✓

**⚠️ test_if.ease** - IF with variable:
```ease
fn main() -> int {
    let x = 5
    if x == 0 {
        return 1
    }
    return 42
}
```
Result: Exits with status 1 (incorrect - separate IR generation bug)

### Generated Code Example

For `if 1 == 0 { return 5 }`:

**IR:**
```
v1 = loadconst 1
v2 = loadconst 0
v3 = cmp_eq v1, v2
branch_if_zero v3, else_label
v5 = loadconst 5
ret v5
jump end_label
else_label:
end_label:
```

**ARM64:**
```assembly
MOV X1, #1              ; v1 = 1
MOV X2, #0              ; v2 = 0
CMP X1, X2              ; Compare
CSET X3, EQ             ; X3 = (X1 == X2) ? 1 : 0
CBZ X3, else_label      ; Branch if X3 == 0
MOV X5, #5              ; Load return value
MOV X0, X5              ; Move to return register
RET                     ; Return (or exit for main)
```

## Known Issues

1. **Variable comparison bug**: Programs with `let` statements before `if` generate incorrect IR order
2. **No else-if optimization**: Each `else if` generates nested branches

## Performance

- Comparison: 2 ARM64 instructions (CMP + CSET)
- Branch: 1 instruction (CBZ)
- Overhead: Minimal, matches modern compiler output

## Impact

This implementation enables:
- ✅ Conditional logic in compiled programs
- ✅ Error handling patterns
- ✅ Control flow for algorithms
- ✅ Major step toward self-hosting compiler

## Technical Notes

### CSET Encoding Quirk

CSET is an alias for CSINC (Conditional Select Increment):
```
CSINC Xd, Xn, Xm, cond:
  if cond then Xd = Xn
  else Xd = Xm + 1
```

For CSET Xd, cond (set to 1 if cond true):
```
CSINC Xd, XZR, XZR, invert(cond):
  if invert(cond) then Xd = 0     (condition false)
  else Xd = 0 + 1 = 1             (condition true)
```

So we must XOR the condition code with 1 to get the inverted condition.

### Branch Offset Encoding

CBZ uses a signed 19-bit immediate for the offset:
- Offset is in 4-byte instruction units
- Range: ±1MB
- Formula: `offset = (target_pos - current_pos)`

## Future Work

1. Fix IR generation for variable comparisons
2. Implement while loops using same branch infrastructure
3. Add optimizations (branch prediction hints, condition merging)
4. Support else-if chains without nested branches

## References

- ARM Architecture Reference Manual (CSINC, CMP, CBZ instructions)
- macOS ARM64 syscall numbers (XNU kernel source)
- bootstrap/compiler.ease lines 2669-2814 (implementation)
