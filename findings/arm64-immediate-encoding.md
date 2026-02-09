# ARM64 Immediate Value Encoding

## The Challenge

ARM64 instructions are fixed-width (32 bits), which means there's limited space to encode immediate values directly in instructions. For a 64-bit architecture working with 64-bit values, this creates an interesting encoding challenge.

## Three Instructions for Loading Immediates

### 1. MOVZ - Move Zero
```assembly
MOVZ X0, #42    ; X0 = 42, all other bits zeroed
```
- **Purpose**: Load a 16-bit immediate, zero the rest
- **Encoding**: `0xD2800000 | (hw << 21) | (imm16 << 5) | rd`
- **Range**: 0-65535 (16-bit unsigned)
- **Shift**: Can specify shift (LSL #0, #16, #32, or #48) via `hw` field

### 2. MOVN - Move NOT
```assembly
MOVN X4, #0     ; X4 = NOT(0) = 0xFFFFFFFFFFFFFFFF = -1
```
- **Purpose**: Load a 16-bit immediate and invert ALL bits
- **Encoding**: `0x92800000 | (hw << 21) | (imm16 << 5) | rd`
- **Use case**: Efficiently encode values with many 1-bits (like -1)
- **Example**: `-1` can be `MOVN X4, #0` instead of multiple MOV instructions

### 3. MOVK - Move Keep
```assembly
MOVZ X16, #0xC5             ; X16 = 0x00000000000000C5
MOVK X16, #0x200, LSL #16   ; X16 = 0x00000000020000C5
```
- **Purpose**: Load 16 bits while keeping other bits unchanged
- **Encoding**: `0xF2800000 | (hw << 21) | (imm16 << 5) | rd`
- **Use case**: Build large immediates with multiple instructions

## Loading Large Immediate Values

### Example: Loading 0x20000C5 (macOS mmap syscall number)

The value 0x20000C5 cannot fit in a single instruction. Here's how to load it:

```assembly
MOVZ X16, #0xC5              ; Load lower 16 bits: 0x00000000000000C5
MOVK X16, #0x200, LSL #16    ; Insert bits [31:16]: 0x00000000020000C5
```

**Why this works**:
- First instruction zeros all bits and sets bits [15:0] to 0xC5
- Second instruction keeps [15:0] unchanged, sets bits [31:16] to 0x200
- Result: 0x200 << 16 | 0xC5 = 0x20000C5

### Full 64-bit Values

For arbitrary 64-bit values, you can use up to 4 instructions:

```assembly
MOVZ X0, #imm0              ; Bits [15:0]
MOVK X0, #imm1, LSL #16     ; Bits [31:16]
MOVK X0, #imm2, LSL #32     ; Bits [47:32]
MOVK X0, #imm3, LSL #48     ; Bits [63:48]
```

**Optimization**: Skip MOVK instructions for zero chunks to reduce code size.

## Common Patterns

### Loading -1 (all ones)
```assembly
MOVN X4, #0     ; More efficient than MOVZ + MOVK sequence
```

### Loading small positive values (0-65535)
```assembly
MOVZ X0, #42    ; Single instruction
```

### Loading values with many set bits
```assembly
MOVN X0, #<inverted>    ; If most bits are 1, invert and use MOVN
```

## Practical Implementation

```c
void load_immediate(int rd, int64_t value) {
    if (value >= 0 && value < 65536) {
        // MOVZ for small positives
        emit_movz(rd, value & 0xFFFF, 0);
    }
    else if (value < 0 && value >= -65536) {
        // MOVN for small negatives
        emit_movn(rd, (~value) & 0xFFFF, 0);
    }
    else {
        // Multi-instruction sequence
        emit_movz(rd, value & 0xFFFF, 0);
        if ((value >> 16) & 0xFFFF)
            emit_movk(rd, (value >> 16) & 0xFFFF, 1);
        if ((value >> 32) & 0xFFFF)
            emit_movk(rd, (value >> 32) & 0xFFFF, 2);
        if ((value >> 48) & 0xFFFF)
            emit_movk(rd, (value >> 48) & 0xFFFF, 3);
    }
}
```

## Common Mistakes

### ❌ Using modulo for large values
```c
// This fails for values > 65535
encode_mov_imm(rd, 0x20000C5 % 65536)  // Only encodes 0xC5!
```

### ✅ Proper multi-instruction sequence
```c
encode_movz(rd, value & 0xFFFF, 0);
if ((value >> 16) != 0)
    encode_movk(rd, (value >> 16) & 0xFFFF, 1);
```

## Key Takeaways

1. **Fixed instruction width** (32 bits) limits immediate encoding
2. **MOVZ** for loading with zero-extension
3. **MOVN** for bit-inverted values (efficient for -1)
4. **MOVK** for building multi-part immediates
5. **Shift field** (`hw`) allows targeting different 16-bit chunks
6. **Optimization matters**: Skip unnecessary MOVK instructions for zero chunks

## References

- ARM Architecture Reference Manual (ARM DDI 0487)
- See: Move wide (immediate) section
- Encoding: sf=1 (64-bit), opc determines MOVZ/MOVN/MOVK, hw=shift
