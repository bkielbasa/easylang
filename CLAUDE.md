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

### Core Design
- **Type System**: Static with inference
- **Memory**: Garbage collected
- **Targets**: Native (macOS ARM64/x86_64 via LLVM) + WebAssembly (future)
- **Syntax**: Go-like (braces, no semicolons required)

### Error Handling
- **Result types**: `Result<T, Error>` for fallible operations
- **No null**: Use `Option<T>` instead (Some/None)
- **Try operator**: `?` for error propagation
- **Return inference**: `return value` infers Ok, `return error.New("msg")` infers Err
- **Implicit success**: Functions returning `Result<(), Error>` succeed implicitly at end

### Visibility (Go-style)
- **Uppercase** first letter = public (exported)
- **Lowercase** first letter = private (package-internal)

### Package Declarations
Every `.ease` file starts with a `package` declaration, exactly like Go:
```
package main           // executable programs
package token          // library package (matches directory name)
```
- One package per directory
- Package name must match the directory name
- `package main` for executable entry points
- The parser skips the declaration (no semantic enforcement yet)

### Imports
```
import (
    "io"                          // stdlib - bare name
    "./config"                    // local file - starts with ./
    "./mylib"                     // local directory package
    "github.com/user/pkg" as p    // external - URL style (TODO)
)
```
- Always use `()` syntax
- Reference by last path segment (or alias)
- Visibility: Uppercase names are exported, lowercase are private
- Imported functions compiled into the binary
- Unused imports = compile error (TODO)

**Status**: Local file imports, directory package imports, and stdlib imports all working! Directory imports enforce visibility (uppercase = public). External imports coming soon.

### Loops (Go-style, only `for`)
```
for { }                    // infinite loop
for condition { }          // condition-based (like while)
for x in collection { }    // range iteration
```

### Enums with Pattern Matching (implemented)
```
enum Color { Red, Green, Blue }
enum Option { Some { value: int }, None }

fn color_code(c: int) -> int {
    result := match c {
        Color::Red => 1,
        Color::Green => 2,
        Color::Blue => 3,
    }
    return result
}

fn unwrap_or(opt: int, def: int) -> int {
    result := match opt {
        Option::Some { value } => value,
        Option::None => def,
    }
    return result
}
```
- Enum values are heap-allocated tagged unions: `[tag: i64 | field1: i64 | ...]`
- Variant construction: `Color::Red`, `Option::Some { value: 42 }`
- Pattern matching with `match` expression and field destructuring
- `match` works as expression: `result := match expr { ... }`

### Testing (Go-style, implemented)
Tests live in `*_test.ease` files alongside source code. Test functions start with `Test` (uppercase T) and accept a `t: T` parameter (Go-style).

```ease
// math_test.ease
package main

fn TestAdd(t: T) {
    result := add(2, 3)
    if result != 5 {
        testing.Fatal("expected 5")
    }
}

fn TestMultiply(t: T) {
    result := multiply(6, 7)
    if result != 42 {
        testing.Fatal("expected 42")
    }
}
```

- **Convention**: `fn TestXxx(t: T)` in `*_test.ease` files — Go-style naming
- **T struct**: `testing.T` with `name: string` field (test name)
- **Failure**: `testing.Fatal(msg)` prints the message and aborts the test (setjmp/longjmp)
- **Discovery**: Compiler discovers `*_test.ease` files, identifies `TestXxx` functions
- **Runner**: Synthetic main() wraps each test with setjmp for failure recovery
- **Output**: Go-style `=== RUN` / `--- PASS` / `--- FAIL` / `ok` or `FAIL`
- **Exit code**: 0 if all pass, 1 if any fail
- **stdlib auto-loaded**: `testing`, `io`, `strings`, `os`, `strconv`, `time`, `result` available without import

```bash
make test                           # run all tests in tests/
make test DIR=path/to/dir           # run tests in a specific directory
make bench                          # run tests + benchmarks
make bench DIR=path/to/dir          # benchmarks in a specific directory
ease test dir/                      # compile tests (then clang + run)
ease test dir/ --bench              # compile tests + benchmarks
```

**Example output:**
```
=== RUN   TestAdd
--- PASS: TestAdd
=== RUN   TestSubtract
    expected 5, got 3
--- FAIL: TestSubtract
FAIL
```

### Benchmarks (Go-style, implemented)
Benchmark functions live in `*_test.ease` files alongside tests. They start with `Benchmark` and accept a `b: B` parameter.

```ease
// math_test.ease
package main

fn BenchmarkAdd(b: B) {
    i := 0
    for i < b.N {
        add(2, 3)
        i = i + 1
    }
}
```

- **Convention**: `fn BenchmarkXxx(b: B)` in `*_test.ease` files
- **B struct**: `testing.B` with `name: string` and `N: int` fields
- **Auto-calibration**: Framework doubles `b.N` until benchmark runs >= 1 second
- **Output**: `BenchmarkXxx\t<iterations>\t<ns/op> ns/op`
- **Skipped on failure**: Benchmarks only run if all tests pass
- **Opt-in**: Benchmarks only run with `--bench` flag (`make bench` or `ease test dir/ --bench`)
- **C runtime**: `ease_time_nanos()` via `clock_gettime(CLOCK_MONOTONIC)`

**Example output:**
```
BenchmarkAdd	536870912	2 ns/op
BenchmarkFactorial	33554432	35 ns/op
```

### Concurrency
- **Goroutines**: `go expression`
- **Channels**: `chan<T>()`, `ch <- value`, `<-ch`
- **Select**: for multiple channel operations

## Project Structure

```
ease/
├── Makefile              # Build system (seed-based, no external dependencies)
├── grammar.ebnf          # Language specification (EBNF)
├── CLAUDE.md             # This file
├── runtime/
│   └── ease_runtime.c    # C runtime (memory, syscalls, string ops)
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
│       └── result/result.ease     # Result/Option types (Option, Result, StringOption)
├── tests/                # Go-style tests (30 passing)
├── examples/             # Example programs
│   └── testdemo/         # Go-style test demo (math.ease + math_test.ease)
└── findings/             # Compiler engineering notes
```

## Building and Usage

The compiler is fully self-hosting. No Go or other compilers needed — just `clang` (for the C runtime and LLVM IR).

```bash
make                    # Build compiler from seed LLVM IR
make test               # Run tests (36 passing)
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

# Step 2: Link with C runtime to produce executable
clang -O1 runtime/ease_runtime.c tmp/output.ll -o myprogram

# Step 3: Run
./myprogram
```

## Rules

 - Never add information to commits that I used Claude
 - never use `cat` to create file
 - alwasy save temporary files into `./tmp/` folder (create if not exists)

## Implementation Status

### Compiler — 100% Self-Hosting

The Ease compiler is written in Ease and compiles itself with byte-identical convergence via the LLVM IR backend.

**Pipeline**: `seed.ll` → (clang) → `ease` binary → compiles `compiler.ease` → LLVM IR → (clang) → new `ease` binary (identical output)

**Capabilities**:
- Full lexer, parser, IR generation, LLVM IR code generation
- Control flow (if/else, for loops, range loops), arrays, strings, structs
- Module/import system (local files, directory packages, stdlib imports)
- Directory package imports with Go-style visibility (uppercase = public)
- Package declarations (`package main`, `package token`, etc.)
- Standard library (strings, strconv, io, os, testing, time, result — all pure Ease implementations)
- Go-style testing framework (`fn TestXxx(t: T)` in `*_test.ease`, `testing.T` struct, `testing.Fatal(msg)`, setjmp/longjmp recovery)
- Go-style benchmark framework (`fn BenchmarkXxx(b: B)` with auto-calibration and ns/op reporting)
- Global variables (mutable and immutable)
- File I/O and command-line arguments
- **Vreg-based type system** — tracks types via `g_vreg_types`/`g_param_types` arrays, replaces `is_string_expr` heuristic
- String `==`/`!=`/`+` auto-dispatch via vreg type lookups (no heuristic needed)
- Dynamic struct field registry for user-defined structs
- Go-style test suite (36 tests passing, 2 benchmarks)

**Compiler Components** (all in `bootstrap/ease/`):
- [x] Lexer with comment handling (// comments)
- [x] Parser for expressions, statements, declarations
- [x] Symbol table with variable tracking
- [x] IR generation (3-address code)
- [x] LLVM IR code generation
- [x] Control flow (if/else, for loops, range for loops)
- [x] Function calls with multiple arguments
- [x] Array indexing, literals, push/len
- [x] Top-level struct declarations and globals
- [x] File I/O (reading source from disk)
- [x] Function return type registry
- [x] Pure Ease stdlib (io, strings, os, strconv)

## Known Issues

**Critical:**
- None currently!

**Minor:**
- Struct literals disabled as postfix operators (causes ambiguity with blocks)

## Future Work

### Standard Library Expansion
- [ ] os - process, environment, command execution (partial: Argc, Argv, ReadFile done)
- [ ] path - file path manipulation
- [ ] net - networking support
- [ ] json - JSON parsing and serialization
- [ ] http - HTTP client and server

### Additional Backends
- [ ] WebAssembly backend for browser execution
- [ ] x86_64 backend for Intel Macs and Linux
- [x] LLVM backend for optimization and portability

### Compiler Improvements
- [x] **Vreg-based type system** — Every vreg gets a type recorded in `g_vreg_types` (regular vregs) / `g_param_types` (params, scoped per function via `g_param_types_start`). Replaces the old `is_string_expr`/`is_string_array_expr` heuristics and `g_string_vars`/`g_string_array_vars` tracking. Type lookups drive opcode dispatch for `+`/`==`/`!=`/`len()`.
- [x] **Function return type registry** — Built from parsed `-> type` annotations, used by type system for EXPR_CALL type propagation.
- [x] **Struct field type registry** — Built from parsed DECL_FIELD type annotations, used by type system for EXPR_FIELD type propagation.
- [x] **Pure Ease stdlib** — `print`, `str_substring`, `os.ReadFile` replaced with pure Ease implementations. Added `syscall.read`, `strconv.Atoi`, string utility functions.
- [x] **Go-style directory packages** — Modules organized as `bootstrap/ease/token/token.ease` with `package token` declarations.
- [x] **NOT prefix operator** — `!expr` support in parser and IRgen.
- [x] **Range-based for loops** — `for i in start..end` parsed and desugared to init/compare/body/increment.
- [x] **Modulo operator** — `%` → `OP_MOD` → LLVM `srem`.
- [x] **Dynamic struct field registry** — `RegisterStruct` + `RegisterFieldOffset` for user-defined structs.
- [x] **Go-style testing framework** — `fn TestXxx(t: T)` in `*_test.ease`, `testing.T` struct with name field, `testing.Fatal(msg)`, setjmp/longjmp for test recovery, synthetic runner with `=== RUN` / `--- PASS/FAIL` output.
- [x] **Go-style benchmark framework** — `fn BenchmarkXxx(b: B)` with `testing.B` struct (`name: string`, `N: int`), auto-calibration (doubles N until >= 1s elapsed), `ease_time_nanos()` C runtime + `OP_TIME_NANOS` IR opcode, reports `iterations\tns/op`.

### Language Features
- [x] **Go-style `:=` declarations** — Replaced `let`/`let mut` with `:=`. All variables are mutable.
- [ ] **`const` keyword** — Compile-time constants (e.g. `const MAX_SIZE = 1024`)
- [x] Enums with pattern matching (heap-allocated tagged unions, `match` expressions)
- [x] Result and Option types (stdlib enums: `Option`, `Result`, `StringOption` with helpers)
- [ ] Traits (parser done, codegen TODO)
- [ ] Generics (design TODO)
- [ ] Closures and lambdas
- [ ] Error propagation operator (`?`)
- [x] Result and Option types

### Tooling
- [ ] Language server protocol (LSP) for IDE support
- [ ] Formatter (`ease fmt`)
- [ ] Linter (`ease lint`)
- [ ] Package manager (`ease get`)
- [ ] Documentation generator (`ease doc`)

## Running Tests

```bash
make                                    # Build from seed (no Go required)
make test                               # Run tests (36 passing)
make test DIR=path                      # Run tests in specific directory
make bench                              # Run tests + benchmarks
make verify                             # Verify self-hosting convergence
make update-seed                        # Update seed after source changes
```

**Test Suite (36 tests, Go-style):**

Tests live in `tests/` as `*_test.ease` files with `fn TestXxx(t: T)` functions:

| File | Tests | Features Covered |
|------|-------|-----------------|
| `math_test.ease` | 4 | `+`, `-`, `*`, `/`, conditionals |
| `functions_test.ease` | 2 | Parameters, return values, recursion |
| `arrays_test.ease` | 3 | Literal, index, push, len |
| `strings_test.ease` | 6 | Concat, Contains, StartsWith, EndsWith, IndexOf, strconv |
| `structs_test.ease` | 3 | Struct literals, field access, pass to functions |
| `loops_test.ease` | 3 | Range `for i in start..end`, condition loops, modulo |
| `enum_test.ease` | 3 | Enum variants, match expressions, field destructuring |
| `result_test.ease` | 6 | Option, Result, StringOption types, match arm string bindings |
| `time_test.ease` | 6 | time.Now, Unix, UnixNano, Add, Before, After, Since |
| `bench_test.ease` | 2 | Benchmark: add, factorial (auto-calibrating ns/op) |
| `helpers.ease` | — | Shared helper functions, structs, enums |

## Example Programs

See `examples/` directory for working example programs:
- `calculator.ease` - Arithmetic, recursion (factorial, fibonacci)
- `string_demo.ease` - String operations, stdlib usage
- `data_structures.ease` - Structs, arrays, algorithms
- `file_io.ease` - File I/O operations

All examples tested and working. See `examples/README.md` for feature matrix.
