# Bootstrap Compiler Components

This directory contains the Ease compiler implemented in Ease itself, as part of the self-hosting effort.

## Components

### Individual Modules (Proof-of-Concept)

Each module implements a phase of compilation and includes its own tests:

- **token.ease** - Token type definitions (constants for token kinds)
- **lexer.ease** - Tokenization (converts source text to tokens)
- **ast.ease** - AST node definitions (tree structure for representing code)
- **parser.ease** - Syntax analysis (converts tokens to AST)
  - ✅ All 5 tests passing
- **sema.ease** - Semantic analysis (type checking, name resolution)
  - ✅ 6/8 tests passing (2 logic issues, no crashes)
- **ir.ease** - Intermediate representation (3-address code generation)
- **codegen.ease** - ARM64 machine code generation (instruction encoding)

### Integrated Compiler

- **compiler.ease** - Full compilation pipeline demonstration
  - Chains all phases: Lexer → Parser → IR → Codegen
  - Successfully compiles simple expressions like `1 + 2`
  - Generates correct ARM64 machine code
  - ✅ All phases working together

## Running the Components

### Individual Module Tests

```bash
# Test individual components
./ease run bootstrap/token.ease      # Token definitions (all pass)
./ease run bootstrap/lexer.ease      # Lexer tests (all pass)
./ease run bootstrap/parser.ease     # Parser tests (5/5 pass)
./ease run bootstrap/sema.ease       # Semantic analysis (6/8 pass)
./ease run bootstrap/ir.ease         # IR generation (passes)
./ease run bootstrap/codegen.ease    # Code generation (passes)
```

### Integrated Compiler Demo

```bash
# Run the full compilation pipeline
./ease run bootstrap/compiler.ease

# Output shows all phases:
# Phase 1: Lexing - converts "1 + 2" to tokens
# Phase 2: Parsing - builds AST from tokens
# Phase 3: IR Generation - generates intermediate instructions
# Phase 4: Code Generation - emits ARM64 machine code (0x8b010002)
```

## Architecture

The bootstrap compiler uses a traditional multi-phase architecture:

```
Source Code
    ↓
┌─────────────┐
│   Lexer     │ → Stream of tokens (INT, PLUS, etc.)
└─────────────┘
    ↓
┌─────────────┐
│   Parser    │ → Abstract Syntax Tree (AST)
└─────────────┘
    ↓
┌─────────────┐
│   Sema      │ → Type-checked AST + Symbol Table
└─────────────┘
    ↓
┌─────────────┐
│   IR Gen    │ → Intermediate Representation (3-address code)
└─────────────┘
    ↓
┌─────────────┐
│   Codegen   │ → ARM64 Machine Code
└─────────────┘
    ↓
Executable Binary
```

## Design Decisions

### No Import System Yet

Since Ease doesn't have a module/import system yet, each component is self-contained:
- Token constants duplicated across lexer/parser
- Shared definitions copied into each file
- `compiler.ease` includes simplified versions of all components

This will be refactored once the import system is implemented.

### Simplified Data Structures

The bootstrap compiler uses simplified representations:
- **Tokens**: No token struct, just position + kind + value functions
- **AST**: Single `AstNode` struct with `tag` field to distinguish node types
- **IR**: Simple `IRInstr` struct with op/dest/args
- **No pointers**: Uses array indices instead of pointers

### Integer Constants Instead of Enums

Token kinds and node tags use integer constants (functions returning int):
```ease
fn TK_PLUS() -> int { return 30 }
fn EXPR_BINARY() -> int { return 6 }
```

This avoids needing enum support in the bootstrap phase.

## Current Status

**Milestone Achieved**: All compilation phases working and integrated! ✅

The bootstrap compiler successfully:
1. ✅ Lexes source code into tokens
2. ✅ Parses tokens into AST
3. ✅ Performs type checking (mostly working)
4. ✅ Generates IR from AST
5. ✅ Emits ARM64 machine code from IR

**Example**: `1 + 2` compiles to ARM64 instruction `0x8b010002` (ADD x2, x0, x1)

## Next Steps

### Short Term
1. Fix remaining sema test failures (type inference edge cases)
2. Add more IR operations (control flow, function calls)
3. Implement register allocation in codegen
4. Add more complex parsing (function declarations, structs)

### Long Term
1. Implement module/import system
2. Refactor components to use imports
3. Complete Mach-O binary generation
4. Full self-hosting: compile the bootstrap compiler with itself

## Testing

Each component includes comprehensive tests:
- **Lexer**: Tests tokenization of various language constructs
- **Parser**: Tests parsing of expressions, statements, declarations
- **Sema**: Tests type checking, error detection
- **IR**: Tests instruction generation
- **Codegen**: Tests ARM64 instruction encoding

All tests can be run with `./ease run bootstrap/<component>.ease`

## Performance Notes

The bootstrap compiler is intentionally simple and not optimized:
- No optimization passes
- Naive code generation
- Simple data structures

Performance will be improved in later iterations once correctness is established.
