# Ease Programming Language

A compiled language focusing on developer experience, self-hosting, fast execution, and web development.

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
    "io"                          // stdlib - bare name ✅ IMPLEMENTED
    "./config"                    // local - starts with ./ ✅ IMPLEMENTED
    "github.com/user/pkg" as p    // external - URL style (TODO)
)
```
- Always use `()` syntax
- Reference by last path segment (or alias) ✅
- Visibility: Uppercase names are exported, lowercase are private ✅
- Imported functions compiled into the binary ✅
- Unused imports = compile error (TODO)

**Status**: Local imports and stdlib imports fully working! External imports coming soon.

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
├── pkg/
│   ├── token/            # Token types and keywords
│   ├── lexer/            # Tokenizer
│   ├── ast/              # AST node definitions
│   ├── parser/           # Recursive descent parser
│   ├── types/            # Type system
│   ├── symbols/          # Symbol table
│   ├── sema/             # Semantic analysis
│   ├── ir/               # Intermediate representation
│   ├── codegen/arm64/    # ARM64 code generation
│   └── macho/            # Mach-O binary writer
└── bootstrap/            # Self-hosting compiler in Ease
    └── compiler.ease     # Bootstrap compiler (3,543 lines)
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

## Rules

 - Never add information to commits that I used Claude
 - never use `cat` to create file
 - alwasy save temporary files into `./tmp/` folder (create if not exists)

## Implementation Status

### Go Compiler (Production Compiler) - ✅ Complete

The Go implementation is the production compiler with all core features working:
- ✅ Full lexer, parser, semantic analysis, IR, ARM64 codegen
- ✅ Control flow (if/else, for loops), arrays, strings, structs
- ✅ Module/import system (local and stdlib imports)
- ✅ Standard library (strings, strconv, io, os, syscall modules)
- ✅ Global variables (mutable and immutable)
- ✅ File I/O and command-line arguments
- ✅ Integration test suite (6/6 passing)
- ✅ Example programs demonstrating features

See `tests/README.md` for test coverage and `examples/README.md` for example programs.

### Bootstrap Compiler (Self-Hosting) - 🚧 In Progress

**Goal**: Ease compiler that can compile itself (written in Ease, compiles Ease)

**Current Status**: ~70% complete

The Go compiler successfully compiles `bootstrap/compiler.ease` (3,543 lines) into a working 151KB ARM64 binary. The bootstrap compiler can:
- ✅ Read source files from disk (os.ReadFile, os.Argc, os.Argv)
- ✅ Lex files (10,646+ tokens including comments)
- ✅ Parse functions, structs, imports
- ✅ Generate IR for expressions and statements
- ✅ Generate ARM64 machine code
- ✅ Output Mach-O binary structure

**Completed Components:**
- [x] Lexer with comment handling (// comments)
- [x] Parser for expressions, statements, declarations
- [x] Symbol table with variable tracking
- [x] IR generation (3-address code)
- [x] ARM64 code generation (MOV, ADD, SUB, MUL, LDR, BL, B, CBZ, RET)
- [x] Control flow (for loops work, if statements have parse issue - see below)
- [x] Function calls with single argument
- [x] Array indexing and field access
- [x] Top-level struct declarations
- [x] File I/O (reading source from disk)
- [x] Mach-O binary generation framework

**Progress Update: If Statements Now Working! ✅**

The critical if statement parsing bug has been fixed! The bootstrap compiler now successfully parses if statements. See "Recent Fixes" section for details.

**Current Parsing Status**:
- Initial: 45 functions (stopped at if statements)
- After if-statement fix: 50 functions
- After break statement support: 47 functions
- After string literal support: 50 functions
- After converting range-based for loops: 79 functions
- After global variables + void functions + array types: 83 functions
- Total functions in bootstrap/compiler.ease: 182
- **Current progress: 46% of bootstrap compiler parsed (83/182)**

**Remaining Parsing Challenges**:
The bootstrap compiler currently parses 79 out of 182 functions (43%). The next blocker:
- **Top-level global variables**: Parser stops at `let mut g_call_args = []int{}` (line 346)
- Main parsing loop only handles: `import`, `struct`, `fn` declarations
- Need to add `TK_LET` handling for module-level global variable declarations
- Attempted skip-based workaround but complex expressions in initializers make this difficult
- Remaining blockers after globals: More range-based for loops, additional language constructs

### Remaining Work for Self-Hosting

**High Priority (Blockers for Self-Compilation):**

1. **Fix If Statement Parsing Bug** ⚠️ CRITICAL
   - Root cause unknown, under active investigation
   - Prevents bootstrap compiler from parsing most real-world code
   - Go compiler works fine; issue is in bootstrap compiler runtime behavior

2. **Multi-Argument Function Calls**
   - Parser and IR currently support only single argument
   - Need to extend to handle multiple arguments
   - Requires: argument list parsing, stack frame setup, register allocation

3. **Struct Literals with Memory Allocation**
   - Parser can parse struct declarations
   - Need: struct literal syntax parsing, heap allocation for struct data
   - Required for AST node creation and data structures

4. **Complete Memory Model**
   - Heap allocation for arrays and structs in generated code
   - Stack frame management for local variables
   - Proper calling convention with register save/restore

5. **Additional IR Operations**
   - STORE (memory writes)
   - ALLOCA (stack allocation)
   - More complete array access operations
   - String operations

6. **Semantic Analysis Integration**
   - bootstrap/sema.ease exists and works (8/8 tests passing)
   - Need to integrate into main compilation pipeline
   - Type checking during compilation, not just IR generation

**Medium Priority:**

7. **Standard Library Integration**
   - Bootstrap compiler needs to import and use stdlib modules
   - strings, strconv, io modules for string manipulation
   - Module resolution and symbol lookup

8. **More ARM64 Instructions**
   - Division (SDIV, UDIV)
   - Store instructions (STR, STRB)
   - More addressing modes
   - Floating point operations

9. **Error Reporting**
   - Line/column information in parse errors
   - Better error messages for compilation failures
   - Stack traces for runtime errors

10. **Code Signing**
    - Generate valid LC_CODE_SIGNATURE load command
    - Compute proper code directory hashes
    - Enable binaries to execute without external codesign tool

**Low Priority (Nice to Have):**

11. **Optimization**
    - Dead code elimination
    - Constant folding
    - Register allocation improvements
    - Peephole optimization

12. **Debugging Support**
    - DWARF debug information
    - Line number tables
    - Symbol information for debuggers

13. **More Language Features**
    - Range-based for loops (`for x in collection`)
    - Enums with pattern matching
    - Traits and implementations
    - Generics

**Estimated Progress**: 70% complete
- Core infrastructure: ✅ Done
- Basic code generation: ✅ Done
- Critical bug fix: ⚠️ In progress
- Multi-arg functions: 📋 TODO
- Memory model: 📋 TODO
- Full self-hosting: 📋 TODO (estimated 2-3 weeks of work remaining)

## Recent Fixes

**Bootstrap Compiler File Size Limit - FIXED (Feb 8, 2026):**
- **Problem**: Bootstrap compiler stopped parsing at position 65,535 bytes
  - Could only parse 146/207 functions in compiler.ease (71%)
  - Appeared to be 16-bit integer overflow in position tracking
- **Root Causes Identified**:
  1. `os.ReadFile` had hardcoded 64KB buffer limit (65,536 bytes) - files were truncated!
  2. Bootstrap compiler lexer didn't support hexadecimal literals (`0x...`)
  3. Bootstrap compiler lexer/parser didn't support modulo operator (`%`)
- **Solutions**:
  - Increased ReadFile buffer from 64KB to 1MB with read loop (pkg/codegen/arm64/emit.go)
  - Added hex literal support to bootstrap lexer:
    - `is_hex_digit()` helper function
    - Updated `lex_end()` to detect `0x` prefix and consume hex digits
    - Updated `lex_int_value()` to parse hex with base-16 conversion
  - Added modulo operator support:
    - Added `TK_PERCENT` token (value 34) and updated all subsequent token values
    - Added lexing support for '%' character (ASCII 37)
    - Added parsing support in `parse_multiplicative` function
- **Results**:
  - ✅ Can now read files up to 1MB (was 64KB limit)
  - ✅ Bootstrap compiler parses 182/207 functions (88%, was 71%)
  - ✅ Successfully handles ARM64 instruction hex encodings and modulo operations
  - ✅ File reading no longer truncates at 65,535 bytes
  - ✅ Codegen functions now parse correctly (15/15 with hex and modulo)
- **Testing**: Created comprehensive tests for large files (148KB+), hex parsing, and modulo expressions
- **Status**: ✅ RESOLVED - Major progress toward self-hosting!

**Continued Progress Session (Feb 7, 2026 - Part 2):**
- **Global Variable Parsing**: Added `TK_LET` handling in main parsing loop using parse_let_stmt
- **Optional Return Types**: Made return types optional for void functions
- **Array Parameter Support**: Extended parse_param to handle `[]Type` syntax
- **Array Return Types**: Extended parse_func_decl to handle `-> []Type` syntax
- **Result**: Jumped from 79 to 83 functions (46% complete)

**Major Progress Session (Feb 7, 2026 - Part 1):**
- **Break Statement Support**: Added `STMT_BREAK` handling in parse_stmt
- **String Literal Parsing**: Added `EXPR_STRING` support in parse_atom
- **Range-Based For Loop Workaround**: Converted `for i in 0..X` to traditional for loops
  - Issue: Bootstrap compiler's ARM64 code has bug with `..` operator recognition
  - Solution: Manually converted range-based loops to `let mut i = 0; for i < limit`
  - Converted: lex_int_value, store_call_args, get_call_args, store_struct_fields, get_struct_fields
- **Result**: Jumped from 50 to 79 functions parsing (43% complete)

**If Statement Parsing Bug - FIXED (Feb 7, 2026):**
- **Problem**: Bootstrap compiler failed to parse if statements at runtime
- **Root Cause**: parse_primary_expr tried to parse `condition { block }` as struct literal
  - After parsing condition expression, parser checked for postfix operators
  - Saw LBRACE and attempted to parse as `StructName { field: value }`
  - Struct literal parsing failed (block contents aren't valid fields)
  - Returned -1, causing entire function parse to fail
- **Solution**:
  - Disabled struct literal parsing as postfix operator (causes ambiguity)
  - Struct literals must be parsed differently to avoid conflict with blocks
- **Result**: If statements now parse correctly!
  - Before: 0 functions parsed when if statements present
  - After: All if statement tests passing
  - Bootstrap compiler now parses 46/182 functions (was stuck at 45)
- **Files**: bootstrap/compiler.ease line 1617 (added check to disable struct literal postfix)
- **Status**: ✅ RESOLVED

**Global Struct Slice Field Initialization (Feb 7, 2026):**
- Fixed global structs with slice fields not initializing properly
- Root cause: buildArrayFieldInit only handled `*types.Array`, not `*types.Slice`
- Solution: Extended to handle both array and slice types
- File: pkg/ir/builder.go lines 3255-3276
- Result: All global struct slice fields now initialize correctly
- Bonus: Also fixed array operations on returned structs

**Earlier Fixes (Condensed):**
- Array push corruption (X15 register clobbering during mmap)
- Struct assignment bug (OpMemCopy vs OpStore)
- ARM64 stack corruption for large frames
- String constant loading (ADRP+ADD)
- Heap allocator state preservation (X25/X26)
- Modulo operator (%) correctness
- See git history for detailed fix information

## Known Issues

**Critical:**
- None currently! If statement parsing bug resolved.

**Minor:**
- Bootstrap compiler parses 182/207 functions (88%) - 25 functions still need investigation
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
- [ ] LLVM backend for optimization and portability

### Language Features
- [ ] **Variable declaration syntax change**: Switch from `let`/`let mut` to Go-style `:=` operator
  - `x := 5` declares new mutable variable
  - `x = 10` reassigns existing variable
  - Compile error if `:=` used on existing variable (prevents shadowing)
  - Breaking change: requires migration of all existing code
  - TODO: Design transition plan, update lexer/parser/semantic analyzer
- [ ] Enums (parser done, codegen TODO)
- [ ] Traits (parser done, codegen TODO)
- [ ] Generics (design TODO)
- [ ] Pattern matching
- [ ] Closures and lambdas
- [ ] Error propagation operator (`?`)
- [ ] Result and Option types

### Tooling
- [ ] Language server protocol (LSP) for IDE support
- [ ] Formatter (`ease fmt`)
- [ ] Linter (`ease lint`)
- [ ] Package manager (`ease get`)
- [ ] Documentation generator (`ease doc`)

## Running Tests

**Go Compiler Tests:**
```bash
go test ./pkg/... -v                    # Unit tests
./tests/run_tests.sh                    # Integration tests (6/6 passing)
```

**Bootstrap Compiler:**
```bash
go run cmd/ease/main.go build bootstrap/compiler.ease -o bootstrap_compiler_new
./bootstrap_compiler_new <file.ease>    # Compile with bootstrap compiler
```

**Integration Tests:**
End-to-end compiler tests that verify features work correctly:
- Basic arithmetic and variables
- Function calls and recursion
- Array operations (literal, index, push, len)
- String operations (strings and strconv modules)
- Struct definitions and operations
- Loop variants (range, condition)

All tests return exit code 0 on success. See `tests/README.md` for details.

## Example Programs

See `examples/` directory for working example programs:
- `calculator.ease` - Arithmetic, recursion (factorial, fibonacci)
- `string_demo.ease` - String operations, stdlib usage
- `data_structures.ease` - Structs, arrays, algorithms
- `file_io.ease` - File I/O operations

All examples tested and working. See `examples/README.md` for feature matrix.
