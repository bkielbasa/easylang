# Ease Programming Language

A compiled language focusing on developer experience, self-hosting, fast execution, and web development.

## Design Decisions

Prefer defining stdlib instead of building new builtins. For example `strings.Split` instead of `str_split`, etc.
After a successful step, update CLAUDE.md with the current status.

### Core Design
- **Type System**: Static with inference
- **Memory**: Garbage collected
- **Targets**: Native (macOS ARM64/x86_64) + WebAssembly (future)
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

### Imports
```
import (
    "io"                          // stdlib - bare name
    "./config"                    // local - starts with ./
    "github.com/user/pkg" as p    // external - URL style, optional alias
)
```
- Always use `()` syntax
- Reference by last path segment (or alias)
- Unused imports = compile error

### Loops (Go-style, only `for`)
```
for { }                    // infinite loop
for condition { }          // condition-based (like while)
for x in collection { }    // range iteration
```

### Enums (Rust-style with named fields)
```
enum Option<T> {
    Some { value: T },
    None,
}

enum Message {
    Quit,
    Move { x: int, y: int },
}
```

### Testing
Tests live in `*_test.ease` files alongside source code.

```
#[slow]
#[parallel]
test "user login succeeds" {
    let result = login("user", "pass")
    if result.is_err() {
        return error.New("login should succeed")
    }
}

test "validates email format" {
    if !validate_email("bad") {
        return error.New("should reject invalid email")
    }
}
```

- **Syntax**: `test "description" { body }` - contextual keyword (can use `test` as identifier elsewhere)
- **Attributes**: `#[slow]`, `#[parallel]`, `#[integration]` for categorization
- **Assertions**: Use `error.New()` to fail tests (Result-based)
- **Execution**: Sequential by default, `#[parallel]` for concurrent tests
- **Filtering**: By name substring or tags via CLI

```bash
ease test                           # run all tests
ease test -name "login"             # filter by description
ease test -tag slow                 # run only #[slow] tests
ease test -skip integration         # skip #[integration] tests
```

### Concurrency
- **Goroutines**: `go expression`
- **Channels**: `chan<T>()`, `ch <- value`, `<-ch`
- **Select**: for multiple channel operations

## Project Structure

```
ease/
├── go.mod
├── grammar.ebnf          # Language specification (EBNF)
├── CLAUDE.md             # This file
├── cmd/
│   └── ease/             # CLI tool
└── pkg/
    ├── token/            # Token types and keywords
    ├── lexer/            # Tokenizer
    ├── ast/              # AST node definitions
    ├── parser/           # Recursive descent parser
    ├── types/            # Type system
    ├── symbols/          # Symbol table
    ├── sema/             # Semantic analysis
    ├── ir/               # Intermediate representation
    ├── codegen/arm64/    # ARM64 code generation
    └── macho/            # Mach-O binary writer
```

## CLI Usage

```bash
ease build <file.ease>       # Compile to executable
ease build -o out file.ease  # Compile with custom output name
ease run <file.ease>         # Compile and run
ease test                    # Run tests in current directory
ease test -name "login"      # Filter by description
ease test -tag slow          # Run tagged tests
ease test -skip integration  # Skip tagged tests
ease test -v                 # Verbose output
ease version                 # Print version
```

## Commits

Never add information to commits that I used Claude

## Implementation Status

### Completed
- [x] Grammar specification (grammar.ebnf)
- [x] Lexer with full token support
- [x] Parser (functions, structs, enums, traits, loops, etc.)
- [x] Semantic analysis (type checking, name resolution)
- [x] IR generation (3-address code)
- [x] ARM64 code generation (Apple Silicon)
- [x] Mach-O binary output
- [x] If/else statements and expressions
- [x] For loops (condition, infinite, range)
- [x] Arrays with Go-style syntax: `[]int{1, 2, 3}`
- [x] `len()` builtin for arrays
- [x] Strings (basic support)
- [x] Test runner (discovery, filtering, execution)
- [x] String builtins: `str_concat`, `str_substring`, `str_index_of`, `str_contains`, `str_starts_with`, `str_ends_with`, `str_char_at`, `str_trim`, `str_replace`, `str_split`
- [x] Short-circuit logical operators (`&&`, `||`)
- [x] Struct returns from functions (proper sret calling convention)
- [x] Bootstrap lexer and token modules (in Ease)
- [x] Bootstrap parser (in Ease) - all 5 tests pass ✅

### In Progress (Self-Hosting)
- [ ] Bootstrap semantic analysis (in Ease) - tests run but crash in types_equal
- [x] Bootstrap IR generation (in Ease) - basic structure implemented ✅
- [ ] Bootstrap code generation (in Ease)

### Recent Fixes
- Fixed ARM64 stack corruption for large stack frames (>4095 bytes)
  - 12-bit immediate truncation in SUBi/ADDi was causing stack sizes like 6496 to become 2400
  - Added `addImm` helper that uses MOVimm + ADD/SUB for large immediates
- Implemented proper sret (struct return) calling convention
  - Caller sets X8 to result buffer, callee writes to [X8], saves X8 at FP+16
- Fixed struct parameter passing to copy data to callee's stack frame
- Fixed string size to 16 bytes (pointer + length) in type size calculations
- Fixed forward function references with two-pass compilation
- Fixed struct return buffer corruption caused by type size mismatch
  - IR builder correctly calculated arrays/slices as 24 bytes (ptr + len + cap)
  - Codegen was incorrectly treating them as 8 bytes, causing sret buffer underallocation
  - Fixed emit.go typeSize to return 24 bytes for Array/Slice types
- Fixed forward function references in IR generation
  - buildIdent now checks TypeInfo.Uses to resolve function symbols
  - Enables proper two-pass compilation where functions can be called before definition
  - Fixed bootstrap/parser.ease tests to all pass

### Known Issues
- Bootstrap semantic analyzer (sema.ease) crashes during types_equal check in binary operator analysis
  - Tests start running but segfault partway through test 2
  - Likely another struct-related or recursion issue
- **Array operations on returned structs**: When a struct containing an array is returned from a function, then passed to another function that reads from AND pushes to that array, it crashes
  - Pattern: `struct S { arr: []int }; fn make() -> S { ... }; fn use(s: S) { let x = s.arr[0]; push(s.arr, x+1); }`
  - Workaround: Avoid combining struct returns with complex array operations in the same function

### Future
- [ ] Standard library
- [ ] WebAssembly backend
- [ ] x86_64 backend

## Running Go Tests

```bash
go test ./pkg/... -v
```

## Example Program

```
import (
    "io"
    "http"
    "./config"
)

enum Result<T, E> {
    Ok { value: T },
    Err { error: E },
}

struct Config {
    Name: string,
    Port: int,
}

fn loadConfig(path: string) -> Result<Config, Error> {
    let content = io.ReadFile(path)?
    return Config { Name: "app", Port: 8080 }
}

fn main() -> Result<(), Error> {
    let cfg = loadConfig("config.json")?

    for i in 0..10 {
        println(i)
    }

    for cfg.Port > 0 {
        // condition loop
    }

    for {
        // infinite loop
        break
    }
}

// In config_test.ease:
test "loadConfig returns valid config" {
    let cfg = loadConfig("test.json")?
    if cfg.Port != 8080 {
        return error.New("expected port 8080")
    }
}

#[integration]
test "config file not found returns error" {
    let result = loadConfig("nonexistent.json")
    if result.is_ok() {
        return error.New("should fail for missing file")
    }
}
```

