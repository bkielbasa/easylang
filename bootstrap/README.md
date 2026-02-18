# Ease Compiler

This directory contains the Ease compiler, written in Ease. The compiler is fully self-hosting — it compiles itself with byte-identical convergence via the LLVM IR backend.

## Building

```bash
make                # Build from seed LLVM IR (no external compilers needed besides clang)
make verify         # Verify self-hosting convergence (gen1 == gen2)
make update-seed    # Update seed.ll after modifying compiler source
```

## Components

### Compiler Main
- **compiler.ease** — Full compilation pipeline (~4,200 lines)
  - Chains all phases: Lexer → Parser → IR → LLVM IR
  - Compiles itself and all Ease programs
  - Outputs LLVM IR to `tmp/output.ll`

### Modules (Go-style directory packages in `ease/`)

Each module is a directory package with a `package` declaration:

- **token/token.ease** — Token type definitions (constants for token kinds)
- **lexer/lexer.ease** — Tokenization (converts source text to tokens)
- **ast/ast.ease** — AST node definitions (tree structure for representing code)
- **parser/parser.ease** — Syntax analysis (converts tokens to AST)
- **ir/ir.ease** — IR opcodes and symbol table
- **irgen/irgen.ease** — AST → IR translation
- **llvm/llvm.ease** — LLVM IR code generation
- **strconv/strconv.ease** — String conversion (Itoa, Atoi)
- **io/io.ease** — I/O (print via syscall)
- **strings/strings.ease** — String functions (Contains, HasPrefix, HasSuffix, Index, etc.)
- **os/os.ease** — OS functions (ReadFile, IsDir, ListDir via syscall)

### Seed
- **seed.ll** — Pre-compiled LLVM IR of the compiler, used to bootstrap without needing an existing Ease binary

## Architecture

The compiler uses a traditional multi-phase architecture:

```
Source Code (.ease)
    ↓
┌─────────────┐
│   Lexer     │ → Stream of tokens (INT, PLUS, IDENT, etc.)
└─────────────┘
    ↓
┌─────────────┐
│   Parser    │ → Abstract Syntax Tree (AST)
└─────────────┘
    ↓
┌─────────────┐
│   IR Gen    │ → Intermediate Representation (3-address code)
└─────────────┘
    ↓
┌─────────────┐
│  LLVM Gen   │ → LLVM IR (.ll file)
└─────────────┘
    ↓
  clang → Executable Binary
```

## Design Decisions

### No Type System (Heuristic-Based)
The compiler uses heuristics (`is_string_expr`) to determine whether `+`/`==`/`!=` should use string or integer operations. A function return type registry and per-function variable tracking make this reliable.

### Array-Index Data Structures
Uses array indices instead of pointers throughout. AST nodes, IR instructions, and symbol table entries are all stored in flat arrays and referenced by index.

### Integer Constants Instead of Enums
Token kinds and node tags use integer constants (functions returning int):
```ease
fn TK_PLUS() -> int { return 30 }
fn EXPR_BINARY() -> int { return 6 }
```

### Pure Ease Standard Library
The stdlib (io, strings, os, strconv) is implemented in pure Ease using syscalls — no C runtime wrappers needed for print, ReadFile, or string operations.

## Self-Hosting

The compiler achieves full self-hosting convergence:

1. `seed.ll` is compiled by `clang` → `ease` binary
2. `ease` compiles `compiler.ease` → `gen1.ll`
3. `clang` compiles `gen1.ll` → `ease_gen1` binary
4. `ease_gen1` compiles `compiler.ease` → `gen2.ll`
5. `gen1.ll` == `gen2.ll` (byte-identical) — convergence!

This means the compiler's output is a fixed point: compiling itself always produces the same result.
