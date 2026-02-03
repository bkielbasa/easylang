# Ease Programming Language

A compiled language focusing on developer experience, self-hosting, fast execution, and web development.

## Design Decisions

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
ease test                    # Run tests (not yet implemented)
ease version                 # Print version
```

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

### Next Steps
- [ ] Test runner (discovery, filtering, execution)
- [ ] Structs (codegen)
- [ ] Enums and pattern matching (codegen)
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
