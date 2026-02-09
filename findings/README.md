# Bootstrap Compiler Findings

This directory contains detailed technical documentation about low-level programming concepts discovered while building the Ease language bootstrap compiler.

## Documents

### [ARM64 Immediate Encoding](arm64-immediate-encoding.md)
Deep dive into how ARM64 handles immediate values in instructions:
- MOVZ (Move Zero) for 16-bit immediates
- MOVN (Move NOT) for inverted patterns (efficient -1 encoding)
- MOVK (Move Keep) for building multi-part immediates
- Common pitfalls and solutions
- Practical implementation guide

**Key Insight**: You can't encode 0x20000C5 in a single instruction - you need `MOVZ` + `MOVK`.

### [Heap Allocation Strategies](heap-allocation-strategies.md)
Comparison of memory allocation approaches:
- Direct mmap per allocation (naive approach)
- Bump allocator (Go compiler's approach)
- Performance analysis (500x speedup)
- Register usage (X25/X26 for heap state)
- Why our first attempt failed

**Key Insight**: Production compilers use bump allocators with 1MB regions, not mmap per allocation.

### [macOS Code Signing](macos-code-signing.md)
Complete guide to executable security on macOS:
- Code signature structure (SuperBlob, CodeDirectory)
- SHA-256 hash computation for code pages
- Why binaries get SIGKILL'd
- Minimal working signature implementation
- Common mistakes and fixes

**Key Insight**: Even "unsigned" executables need embedded signature structures on modern macOS.

### [ARM64 Calling Convention](arm64-calling-convention.md)
AAPCS64 explained with practical examples:
- Register roles (X0-X7 for args, X19-X28 callee-saved)
- Stack frame layout
- Function prologue/epilogue patterns
- Syscall convention (X16 for syscall number)
- Common mistakes to avoid

**Key Insight**: Stack must be 16-byte aligned at ALL times, or everything breaks.

### [Debugging Generated Binaries](debugging-generated-binaries.md)
Real-world debugging story from our mmap bug hunt:
- Tools: otool, lldb, xxd, codesign
- Case study: The 0x16 (EINVAL) mystery
- How we tracked down the immediate encoding bug
- Verification checklist
- When to ask for help

**Key Insight**: Always check syscall return values - we spent hours because we assumed mmap succeeded.

## Why These Documents Matter

When building a compiler that generates native code, you quickly encounter concepts that are:
1. **Poorly documented** - ARM manuals are dense
2. **Counterintuitive** - Why can't I just encode any number?
3. **Platform-specific** - macOS does things differently than Linux
4. **Hard to debug** - Your code generated the code that's crashing!

These documents capture the hard-won knowledge from actually building a working compiler.

## Target Audience

- **Compiler developers** building for ARM64
- **Systems programmers** working close to the metal
- **Students** learning about code generation
- **Curious developers** who wonder how it all works

## Usage

Each document is self-contained and can be read independently, but they build on each other:

```
Start here for basics:
├─ ARM64 Calling Convention (registers, stack)
├─ ARM64 Immediate Encoding (instruction basics)
│
Apply to real problems:
├─ Heap Allocation Strategies (memory management)
├─ macOS Code Signing (running your binaries)
│
Debug when things break:
└─ Debugging Generated Binaries (tools and techniques)
```

## Related Code

The concepts in these documents are implemented in:
- `bootstrap/compiler.ease` - Self-hosting compiler
- `pkg/codegen/arm64/` - Go reference implementation
- `tmp/test_*.ease` - Example test programs

## Contributing

If you find errors or have suggestions:
1. These are learning documents, not authoritative references
2. Check the ARM Architecture Reference Manual for canonical info
3. Feel free to extract and use these for blog posts, tutorials, etc.

## License

These documents are part of the Ease language project and follow the same license as the main codebase.

---

*"In theory, theory and practice are the same. In practice, they are not."*

These documents are practice, not theory.
