# Struct Return Buffer Corruption Bug - Detailed Analysis

## Executive Summary

A critical bug in the Ease compiler caused struct return buffer corruption, leading to crashes in the bootstrap compiler tests. The issue stemmed from a **type size mismatch** between the IR (Intermediate Representation) generation phase and the ARM64 code generation phase.

## The Problem

### Symptoms
- Bootstrap tests (`parser.ease`, `sema.ease`) would crash with segfaults (exit code 138/139)
- Simple test programs worked, but complex programs with struct returns failed
- Crashes occurred when functions returned large structs (>100 bytes) and made nested calls

### Root Cause

The bug was caused by a discrepancy in how arrays and slices were sized:

| Component | Array/Slice Size | Rationale |
|-----------|-----------------|-----------|
| **IR Builder** (`pkg/ir/builder.go`) | 24 bytes | Correct - fat pointer with 3 fields |
| **Codegen** (`pkg/codegen/arm64/emit.go`) | 8 bytes | Wrong - treated as simple pointer |

#### Array/Slice Representation in Ease

Arrays and slices in Ease are represented as **fat pointers** containing:
1. `ptr` (8 bytes) - pointer to data
2. `len` (8 bytes) - number of elements
3. `cap` (8 bytes) - capacity

**Total: 24 bytes**

## Detailed Walkthrough

### Example: The Failing Code

```ease
struct Sema {
    type_tags: []int,      // Should be 24 bytes
    type_names: []string,  // Should be 24 bytes
    type_elem: []int,      // Should be 24 bytes
    errors: []string,      // Should be 24 bytes
}
// Actual size: 4 × 24 = 96 bytes

fn new_sema() -> Sema {
    let mut s = Sema {
        type_tags: []int{},
        type_names: []string{},
        type_elem: []int{},
        errors: []string{},
    }
    // ... initialize s ...
    return s  // Returns 96-byte struct
}

fn main() -> int {
    let s = new_sema()  // Expects to receive 96-byte struct
    // ...
}
```

### Step-by-Step Execution (BEFORE FIX)

#### 1. IR Generation Phase
When processing `new_sema()`:
- IR builder calculates struct size using `b.typeSize()`
- For each array field: `typeSize([]int) = 24`
- **Total Sema size = 4 × 24 = 96 bytes**
- Generates `OpAlloc` instruction to allocate 96 bytes on stack

#### 2. Code Generation Phase - Caller (main)
When `main()` calls `new_sema()`:
- Codegen calculates how much buffer space to allocate for the return value
- Uses `getStructSize()` which calls `typeSize()`
- **BUG**: `typeSize()` has no case for `Array`/`Slice`, falls through to `default: return 8`
- Calculates **Sema size = 4 × 8 = 32 bytes** ❌
- Allocates only **32-byte buffer** at `FP_main + 168`
- Sets X8 register to point to this 32-byte buffer
- Calls `new_sema()`

#### 3. Code Generation Phase - Callee (new_sema)
Inside `new_sema()`:
- Allocates 512-byte stack frame (including 96 bytes for the struct at `FP + 416`)
- Saves caller's X8 pointer at `FP + 16`
- Builds the Sema struct (filling 96 bytes at `FP + 416`)
- On return:
  - Loads X8 from `FP + 16` (points to caller's 32-byte buffer)
  - **Copies 96 bytes** from `FP + 416` to `[X8]` ❌

#### 4. Buffer Overflow
```
Caller's stack frame:
  FP + 168: [32-byte buffer]  ← X8 points here
  FP + 200: [other data...]   ← Gets overwritten!
                              ↓ 64 bytes overflow
                              ← Corruption continues
```

The callee writes 96 bytes into a 32-byte buffer, overwriting:
- Adjacent stack variables
- Saved registers
- Return addresses
- Other critical data

**Result**: Segmentation fault or bus error

### The Stack Layout Issue

```
main() stack frame (208 bytes):
  [SP + 0]   saved FP/LR (16 bytes)
  [FP + 16]  spilled registers
  [FP + 168] sret buffer for new_sema (32 bytes allocated, 96 needed!) ❌
  [FP + 200] other variables ← CORRUPTED by overflow

new_sema() stack frame (512 bytes):
  [SP + 0]   saved FP/LR (16 bytes)
  [FP + 16]  saved X8 (caller's sret pointer)
  [FP + 416] local Sema struct (96 bytes) ✓ correctly allocated
```

## The Fix

### Code Changes

**File**: `pkg/codegen/arm64/emit.go`

**Before** (lines 197-225):
```go
func typeSize(t types.Type) int {
    if t == nil {
        return 8
    }
    switch typ := t.Underlying().(type) {
    case *types.Basic:
        // ... handle basic types ...
    case *types.Pointer:
        return 8
    case *types.Struct:
        size := 0
        for _, f := range typ.Fields {
            size += typeSize(f.Type)
        }
        return size
    default:
        return 8  // ❌ Arrays/Slices fall through here!
    }
}
```

**After**:
```go
func typeSize(t types.Type) int {
    if t == nil {
        return 8
    }
    switch typ := t.Underlying().(type) {
    case *types.Basic:
        // ... handle basic types ...
    case *types.Pointer:
        return 8
    case *types.Array, *types.Slice:
        return 24 // ✅ fat pointer: ptr (8) + len (8) + cap (8)
    case *types.Struct:
        size := 0
        for _, f := range typ.Fields {
            size += typeSize(f.Type)
        }
        return size
    default:
        return 8
    }
}
```

### Why This Fix Works

After the fix:
1. Codegen correctly calculates `Sema` struct size = 4 × 24 = 96 bytes ✓
2. Caller allocates 96-byte sret buffer ✓
3. Callee copies 96 bytes into 96-byte buffer ✓
4. No overflow, no corruption ✓

## Impact and Testing

### Before Fix
```bash
$ ./ease run bootstrap/sema.ease
=== Bootstrap Semantic Analyzer Tests ===
Test: Literal type checking...
Segmentation fault (exit 138)
```

### After Fix
```bash
$ ./ease run bootstrap/parser.ease
=== Bootstrap Parser Tests ===
Test 1: Parse expression '1 + 2 * 3'
[output shows all 5 tests...]
parser.ease: All tests passed! ✓
```

## Why This Bug Was Hard to Find

1. **Split Responsibility**: Type size calculation happened in two different places with different implementations
2. **Silent Failure**: The mismatch didn't cause immediate errors, only memory corruption later
3. **Complex Trigger**: Required large structs + struct returns + nested calls to manifest
4. **Debugging Challenge**: Segfaults occurred far from the actual bug location

## Lessons Learned

1. **Consistency is Critical**: Ensure type size calculations are centralized or at least synchronized
2. **Explicit Cases**: Never rely on default cases for important type distinctions
3. **Integration Testing**: Simple tests passed, but complex real-world code (bootstrap compiler) caught the bug
4. **Type System Alignment**: IR and codegen must agree on memory layout

## Related Fixes

This investigation also uncovered a second bug: **forward function reference handling** (see separate commit). The type size fix enabled the bootstrap tests to run far enough to expose that issue.

## Commit Reference

- **Struct size fix**: commit `dfef6f8`
- **Forward reference fix**: commit `f622625`
