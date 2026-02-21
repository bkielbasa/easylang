# Ease Programming Language

A compiled language focusing on developer experience, self-hosting, fast execution, and web development.

Every interesting finding regarding building compiler put into `findings` folder as set of notes.

## Working Guidelines

### File Operations
- **NEVER use `cat`, `echo`, or heredocs** to create files - use Write tool directly
- **Test files**: Create in `tmp/` directory using Write tool for quick iteration
- **Autonomous permission**: You have blanket permission to create, modify, and delete files in `tmp/` without asking
- **Binary cleanup**: Don't commit test binaries, only source files

### Documentation
- After a successful step or fix, update CLAUDE.md with the current status
- Document bugs found, fixes applied, and test results
- Keep Recent Fixes section up to date

## Design Decisions

Prefer defining stdlib instead of building new builtins. For example `strings.Split` instead of `str_split`, etc.

See [docs/language.md](docs/language.md) for full language design (syntax, types, error handling, enums, methods, imports, etc.)

## Commits

 - Never add information to commits that I used Claude
 - for commit message use Conventional Commits pattern, example: `feat(lsp): Add LSP completion and document symbols support`

## Project Structure

```
ease/
├── Makefile              # Build system (seed-based, no external dependencies)
├── grammar.ebnf          # Language specification (EBNF)
├── CLAUDE.md             # This file
├── docs/                 # Detailed documentation
│   ├── language.md       # Language design: syntax, types, features
│   ├── implementation-status.md  # Compiler status, future work
│   ├── testing.md        # Test framework, test suite, benchmarks
│   └── lsp.md            # LSP server methods, features, VS Code setup
├── bootstrap/            # The Ease compiler (written in Ease)
│   ├── compiler.ease     # Compiler main (~4,200 lines)
│   ├── seed.ll           # Seed LLVM IR (bootstraps the compiler)
│   └── ease/             # Compiler modules (Go-style directory packages)
│       ├── token/token.ease       # Token type constants
│       ├── lexer/lexer.ease       # Tokenizer
│       ├── ast/ast.ease           # AST node types and constructors
│       ├── parser/parser.ease     # Recursive descent parser
│       ├── ir/ir.ease             # IR opcodes and symbol table
│       ├── irgen/irgen.ease       # AST → IR translation
│       ├── llvm/llvm.ease         # LLVM IR code generation
│       ├── strconv/strconv.ease   # String conversion (Itoa, Atoi)
│       ├── io/io.ease             # I/O (print via syscall)
│       ├── strings/strings.ease   # String functions
│       ├── os/os.ease             # OS functions (ReadFile via syscall)
│       ├── testing/testing.ease   # Testing framework (Fatal)
│       ├── time/time.ease         # Time package (Now, Unix, Add, Before, After)
│       ├── result/result.ease     # Result/Option types (Option, Result, StringOption)
│       ├── json/json.ease         # JSON document API (build, serialize, parse)
│       ├── maps/maps.ease         # Hash map (map[string]int)
│       └── lsp/lsp.ease           # LSP server (diagnostics)
├── editors/              # Editor integrations
│   └── vscode/           # VS Code extension (LSP client)
├── tests/                # Go-style tests (111 passing)
├── examples/             # Example programs
│   └── testdemo/         # Go-style test demo (math.ease + math_test.ease)
└── findings/             # Compiler engineering notes
```

## Building and Usage

The compiler is fully self-hosting. No Go or C runtime needed — just `clang` (to assemble LLVM IR and link libc).

```bash
make                    # Build compiler from seed LLVM IR
make test               # Run tests (111 passing)
make test DIR=path      # Run tests in specific directory
make bench              # Run tests + benchmarks
make verify             # Verify self-hosting convergence (gen1 == gen2)
make update-seed        # Update seed after modifying compiler source
make clean              # Remove build artifacts
```

### Compiling a program

```bash
# Step 1: Compile .ease source to LLVM IR
./tmp/ease myprogram.ease               # Produces tmp/output.ll

# Step 2: Link to produce executable (no C runtime needed)
clang -O1 tmp/output.ll -o myprogram

# Step 3: Run
./myprogram
```

### LSP Server

```bash
./tmp/ease lsp    # Start LSP server (stdin/stdout)
```

See [docs/lsp.md](docs/lsp.md) for supported methods, features, and VS Code extension setup.

## Reference Documentation

- **[Language Design](docs/language.md)** — syntax, types, error handling, enums, methods, imports, pointers, concurrency
- **[Implementation Status](docs/implementation-status.md)** — compiler capabilities, known issues, future work
- **[Testing](docs/testing.md)** — test framework, benchmark framework, test suite (58 tests), example programs
- **[LSP Server](docs/lsp.md)** — supported methods, features, VS Code extension
