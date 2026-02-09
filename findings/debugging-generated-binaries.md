# Debugging Generated Binaries: A War Story

## The Problem

You've written a compiler. It generates what looks like correct ARM64 code. The binary is structurally valid (Mach-O header, load commands, etc.). But when you run it:

```bash
$ ./my_program
(exits with code 137 - SIGKILL)
```

Or worse:

```bash
$ lldb ./my_program
Process stopped: EXC_BAD_ACCESS (code=1, address=0x16)
```

How do you debug code that **you generated**?

## Essential Tools

### 1. otool - Disassemble and Inspect

```bash
# Disassemble code section
otool -tv ./my_program

# View load commands
otool -l ./my_program

# Check entry point
otool -l ./my_program | grep -A 4 "cmd LC_MAIN"
```

### 2. lldb - Step Through Generated Code

```bash
# Run with lldb
lldb ./my_program

# Set breakpoint at entry point
(lldb) b 0x100000210

# Run and examine registers
(lldb) run
(lldb) register read

# Step single instruction
(lldb) stepi

# Examine memory
(lldb) x/10gx $sp  # View stack
(lldb) x/s $x0     # View string at X0
```

### 3. hexdump / xxd - View Raw Bytes

```bash
# View specific offset in binary
xxd -s 528 -l 64 ./my_program

# Check signature offset
xxd -s $SIG_OFFSET -l 256 ./my_program
```

### 4. codesign - Verify Signatures

```bash
# Check if signed
codesign -dvv ./my_program

# Ad-hoc sign for testing
codesign -s - ./my_program
```

## Real Bug Case Study: The Immediate Value Bug

### Symptom
```bash
$ lldb ./test_output
Process stopped: EXC_BAD_ACCESS (address=0x16)
* frame #0: 0x100000238 test_output
-> 0x100000238: strb w3, [x2]
```

Program crashes trying to write to address 0x16.

### Initial Investigation

```bash
(lldb) register read x2
x2 = 0x0000000000000016
```

X2 contains 0x16 (22 decimal) - **not a valid pointer**!

### Backtracking: Where did X2 come from?

```assembly
0x100000230: svc   #0x80       ; mmap syscall
0x100000234: mov   x2, x0      ; X2 = mmap result
0x100000238: strb  w3, [x2]    ; CRASH HERE
```

X2 comes from X0 (mmap return value). The value 0x16 (22) is actually **errno EINVAL** - the mmap syscall **failed**!

### Why Did mmap Fail?

Examine syscall parameters:

```bash
(lldb) b 0x100000228  # Before SVC
(lldb) run
(lldb) register read x0 x1 x2 x3 x4 x5 x16
x0 = 0x0000000000000000  ; addr (NULL) ✅
x1 = 0x0000000000000040  ; size (64 bytes) ✅
x2 = 0x0000000000000003  ; prot (R+W) ✅
x3 = 0x0000000000001002  ; flags ✅
x4 = 0x000000000000ffff  ; fd ❌ Should be -1!
x5 = 0x0000000000000000  ; offset ✅
x16 = 0x00000000000000c5  ; syscall ❌ Should be 0x20000C5!
```

**Two bugs found**:
1. X4 = 0xFFFF instead of 0xFFFFFFFFFFFFFFFF (-1)
2. X16 = 0xC5 instead of 0x20000C5

### Root Cause: Immediate Encoding

Looking at disassembly:

```assembly
0x100000220: mov x4, #0xffff     ; Only lower 16 bits!
0x100000228: mov x16, #0xc5      ; Only lower 16 bits!
```

The compiler's `encode_mov_imm` function only handled 16-bit immediates:

```c
// BUG:
fn encode_mov_imm(rd, imm) {
    return 0xd2800000 + ((imm % 65536) * 0x20) + rd
}
```

For 0x20000C5, this gave:
```
0x20000C5 % 65536 = 0xC5  ; Lost upper bits!
```

### The Fix: Multi-Instruction Sequences

```assembly
; For -1: Use MOVN (Move NOT)
MOVN X4, #0              ; NOT(0) = -1

; For 0x20000C5: Use MOVZ + MOVK
MOVZ X16, #0xC5          ; Lower 16 bits
MOVK X16, #0x200, LSL #16 ; Upper bits, keeping lower
```

Implementation:

```c
fn encode_movn(rd, imm) {
    return 0x92800000 + ((imm % 65536) * 0x20) + rd
}

fn encode_movk(rd, imm, shift) {
    let hw = shift / 16
    return 0xf2800000 + (hw * 0x200000) + ((imm % 65536) * 0x20) + rd
}

// For mmap syscall:
emit(encode_movn(4, 0))           // MOVN X4, #0 → -1
emit(encode_mov_imm(16, 0xC5))    // MOVZ X16, #0xC5
emit(encode_movk(16, 0x200, 16))  // MOVK X16, #0x200, LSL #16
```

### Verification

After fix:

```bash
$ otool -tv ./test_output
0x100000220: mov  x4, #-0x1              ; ✅ Correct!
0x100000228: mov  x16, #0xc5
0x10000022c: movk x16, #0x200, lsl #16  ; ✅ Now complete!
0x100000230: svc  #0x80
```

Run again:

```bash
$ lldb ./test_output
(lldb) b 0x100000234  # After mmap
(lldb) run
(lldb) register read x0
x0 = 0x0000000104000000  ; ✅ Valid heap pointer!
```

Success! mmap now works.

## Debugging Strategy

### 1. Start with Known-Good Code

Compile a simple test with a working compiler:

```bash
$ go run cmd/ease/main.go build test.ease -o test_good
$ otool -tv ./test_good > good.asm
```

Compare with your generated code:

```bash
$ ./my_compiler test.ease -o test_mine
$ otool -tv ./test_mine > mine.asm
$ diff good.asm mine.asm
```

### 2. Isolate the Failing Operation

Create minimal test cases:

```c
// test1.ease - Just return
fn main() -> int { return 42 }

// test2.ease - Simple arithmetic
fn main() -> int { return 2 + 3 }

// test3.ease - Function call
fn add(a: int, b: int) -> int { return a + b }
fn main() -> int { return add(2, 3) }
```

Find **exactly where** it breaks.

### 3. Verify Each Component

For complex operations (like syscalls), check **every parameter**:

```bash
(lldb) b *0x<before_syscall>
(lldb) run
# Check each register matches expected value
(lldb) p/x $x0  # Expected: 0x0
(lldb) p/x $x1  # Expected: size
(lldb) p/x $x16 # Expected: syscall number
```

### 4. Use Assertions in Generated Code

Add debug instructions:

```assembly
; Check stack alignment
MOV X16, SP
AND X16, X16, #0xF
CBZ X16, aligned
BRK #0                ; Crash with debug break
aligned:
    ; Continue...
```

### 5. Print Intermediate Results

Add debug output to your compiler:

```c
print("Generated IR:")
for instr in instructions:
    print(instr)

print("Generated machine code:")
for i, word in enumerate(code):
    print(f"[{i}] 0x{word:08x}")
```

Compare IR with disassembly to find mismatches.

## Common Bug Patterns

### 1. Off-by-One in Offsets

```c
// BUG: Writing hash at wrong offset
offset = 0
sha256(code, size, signature + offset)  // Should be: cd_start + hashOffset
```

**Symptom**: Signature validation fails, structures corrupted.

**Fix**: Always add base offset before seeking to field.

### 2. Endianness Confusion

```c
// BUG: Writing little-endian in big-endian structure
write_u32(buf, 0xFADE0CC0)  // Wrong for SuperBlob magic!
```

**Symptom**: `codesign -dvv` reports "not signed", magic numbers don't match.

**Fix**: Use `write_u32_be` for Mach-O signatures, `write_u32_le` for load commands.

### 3. Register Clobbering

```c
// BUG: X15 gets overwritten by syscall
MOV X15, X2        ; Save value
BL mmap_syscall    ; Overwrites X15!
ADD X16, X15, #8   ; Wrong value!
```

**Symptom**: Mysterious data corruption after function calls.

**Fix**: Use callee-saved registers (X19-X28) or save to stack.

### 4. Missing Stack Alignment

```c
// BUG: Stack not 16-byte aligned
SUB SP, SP, #8
BL function  ; ARM64 ABI violation!
```

**Symptom**: Bus errors, crashes in library calls, undefined behavior.

**Fix**: Always use multiples of 16 for stack adjustments.

### 5. Wrong Entry Point

```c
// BUG: Entry point calculation wrong
entry_offset = header_size  // Points to first function, not main!
```

**Symptom**: Program starts executing wrong function.

**Fix**: Calculate: `entry_offset = header_size + (main_instruction_offset * 4)`

## Verification Checklist

Before declaring victory:

- [ ] Binary structure valid: `otool -l` shows all segments
- [ ] Code disassembles correctly: `otool -tv` makes sense
- [ ] Entry point correct: Points to main function
- [ ] Registers initialized: Frame pointer, stack pointer
- [ ] Stack aligned: SP % 16 == 0 at all times
- [ ] Syscall numbers correct: `man 2 syscall` for reference
- [ ] Error codes checked: Don't assume syscalls succeed
- [ ] Signatures valid: `codesign -v` passes (if applicable)
- [ ] Actually runs: Exit code matches expected value

## When All Else Fails

### Generate and Compare Assembly

```bash
# Good compiler
cc -S test.c -o good.s

# Your compiler
./my_compiler test.ease
otool -tv test > mine.s

# Side-by-side comparison
code good.s mine.s
```

### Ask for Help with Specifics

When posting questions:
1. Show the disassembly (not just source)
2. Show register values at crash
3. Show exact error message
4. Show what you expected vs. what happened
5. Show minimal test case

### Take a Break

Sometimes the bug is obvious after you:
- Sleep on it
- Take a walk
- Explain it to a rubber duck
- Start writing the bug report

## Key Lessons

1. **Debuggers are essential** - lldb saved us countless times
2. **Compare with known-good code** - don't guess, verify
3. **Check return values** - syscalls fail more often than you think
4. **Immediates are tricky** - ARM64 encoding has sharp edges
5. **Endianness matters** - especially in file formats
6. **Offsets are hard** - always double-check pointer arithmetic
7. **Simplify, simplify, simplify** - minimal test cases find bugs fast

## Tools Summary

| Tool | Purpose | Example |
|------|---------|---------|
| `otool -tv` | Disassemble code | See what you generated |
| `otool -l` | View load commands | Check Mach-O structure |
| `lldb` | Debug execution | Step through, examine registers |
| `xxd` | View raw bytes | Check binary layout |
| `codesign` | Verify signatures | Test code signing |
| `diff` | Compare outputs | Find discrepancies |
| `man 2` | System call docs | Syscall parameters |

---

*"The debugger is mightier than the println."* - Ancient programmer proverb
