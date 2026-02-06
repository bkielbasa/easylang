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

## Rules

 - Never add information to commits that I used Claude
 - never use `cat` to create file
 - alwasy save temporary files into `./tmp/` folder (create if not exists)

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
- [x] File I/O (syscalls: `syscall.open`, `syscall.read`, `syscall.write`, `syscall.close`)
  - Low-level syscalls for direct file operations
  - Proper ARM64 syscall implementation with error handling
  - See `examples/file_io.ease` for usage examples
- [x] Global variables (simple and complex types)
  - Parser: `let x = 42`, `let mut y = 100`, `let mut arr = []int{1,2,3}`
  - Semantic analysis: type checking, mutability, symbol registration
  - IR: OpLoad/OpStore for mutable globals, runtime initialization for arrays
  - Codegen: __DATA segment with ADRP+ADD addressing, heap allocation for array data
  - Mutable globals: return address directly (not copy) to allow in-place modifications
  - Working: int, bool, string, arrays with push/read/write operations
  - Limitation: struct literals as globals not yet implemented
- [x] Bootstrap compiler components (in Ease)
  - Lexer, parser, sema, IR, codegen all working independently
  - Integrated compiler demo chains all phases successfully
  - See `bootstrap/README.md` for details ✅
- [x] Module/Import system
  - Local imports: `import ("./math", "./geometry" as geo)`
  - Stdlib imports: `import ("strings", "io")` - bare names resolve to `stdlib/`
  - Visibility rules: Uppercase = exported, lowercase = private
  - Qualified function calls: `math.Add(5, 3)`, `geo.Area(5, 8)`
  - Automatic parsing and analysis of imported modules
  - Cross-module symbol resolution and type checking
  - Imported functions compiled into binary
  - TODO: external imports, unused import detection
- [x] Standard library foundation
  - `strings` module: Split, Join, Contains, StartsWith, EndsWith, IndexOf, Substring, CharAt, Trim, Replace, Concat
  - `strconv` module: Itoa, Atoi, ParseInt (with base 2-36), FormatInt (with base 2-36)
  - `io` module: ReadFile, WriteFile
  - `syscall` module: open, read, write, close (low-level file operations)
  - `os` module: ReadFile, WriteFile, Argc, Argv (high-level OS operations)
  - Architecture: Low-level builtins (`str_*`, `os.*`) as implementation primitives
  - User-facing: Stdlib modules provide clean API (e.g., `strings.Split` instead of `str_split`)
  - All string/file operations now go through stdlib modules

### Bootstrap Compiler (Self-Hosting)

Progress on implementing the Ease compiler in Ease itself:

**Completed Components:**
- [x] **Lexer** - Tokenization with full token support (bootstrap/lexer.ease)
  - ✅ All tests passing
- [x] **Parser** - All language constructs (bootstrap/parser.ease)
  - ✅ All 5 tests passing
- [x] **Semantic Analysis** - Type checking and name resolution (bootstrap/sema.ease)
  - ✅ All 8 tests passing!
  - Fixed by correcting OpMemCopy usage for struct assignments and array fields (see Recent Fixes #4)
- [x] **IR Generation** - 3-address code with simplified instruction format (bootstrap/ir.ease)
  - IRInstr struct with op, dest, arg1, arg2 fields
  - Operations: ADD, SUB, MUL, DIV, EQ, NE, LT, GT, LOADCONST, CALL, RETURN
  - ✅ Tests passing
- [x] **Code Generation** - ARM64 instruction encoding (bootstrap/codegen.ease)
  - Instruction encoders: ADD, SUB, MUL, RET
  - Register constants and hex display utilities
  - ✅ All encodings verified correct
- [x] **Integration** - Full compilation pipeline (bootstrap/compiler.ease)
  - ✅ All phases connected: Lexer → Parser → IR → Codegen
  - ✅ Successfully compiles expressions like `1 + 2` to ARM64 machine code
  - ✅ Example: generates `0x8b010002` (ADD x2, x0, x1) from `1 + 2`
  - See `bootstrap/README.md` for details

**Not Started:**
- [ ] Mach-O generation - Binary output writer (needs file I/O)
- [ ] Full self-hosting - Bootstrap compiler compiling itself

### Recent Fixes

**Memory Operations for Binary Writing (Feb 6, 2026):**
- Added low-level memory operations for Mach-O binary generation
  - `poke(addr, value)` - write byte to memory address
  - `peek(addr) -> int` - read byte from memory address
  - `str_len(s) -> int` - get string length
  - `mem_set(addr, value, count)` - set memory bytes (has loop issues, use with caution)
- Implemented in IR (OpPoke, OpPeek, OpMemSet, OpStrLen)
- ARM64 codegen with LDRB/STRB byte operations
- Enables bootstrap compiler to write binary files byte-by-byte
- Files: pkg/ir/ir.go, pkg/ir/builder.go, pkg/sema/analyzer.go, pkg/codegen/arm64/emit.go
- Note: mem_set has intermittent loop issues; poke/peek/str_len work reliably

**File I/O Implementation (Feb 6, 2026):**
- Implemented complete file I/O syscall support for macOS ARM64
  - `syscall.open(path, flags, mode)` - open file with proper flag/mode handling
  - `syscall.read(fd, buf, count)` - read bytes from file descriptor
  - `syscall.write(fd, buf, count)` - write bytes to file descriptor
  - `syscall.close(fd)` - close file descriptor
- Added semantic analysis for syscall package with type checking
- Added IR builder support for syscall method expressions
- Codegen already had full ARM64 syscall implementations
- Buffer parameters accept both string and int (pointer) types
- Example: `examples/file_io.ease` demonstrates usage
- Files: pkg/sema/analyzer.go, pkg/ir/builder.go, examples/file_io.ease

**Array Push Corruption & Bootstrap Sema Fix (Feb 6, 2026):**
- **CRITICAL FIX #1**: Fixed array push corrupting element values during growth
  - Root cause: emitArrayPush backed up element in X15 (caller-saved register)
  - mmap syscall during array growth would clobber X15, corrupting element
  - ARM64 calling convention: X0-X18 are caller-saved, X19-X28 are callee-saved
  - Solution: Save X20 (element) on stack before mmap, restore after
  - File: pkg/codegen/arm64/emit.go lines 2697-2726
- **CRITICAL FIX #2**: Fixed bootstrap sema corruption from convoluted workaround
  - Root cause: types_equal_safe did push→load→call→push→load (double indirection)
  - The workaround ITSELF was causing corruption in large functions
  - Solution: Remove workaround, call types_equal directly
  - Result: Bootstrap sema now 6/8 tests passing
  - File: bootstrap/sema.ease - removed types_equal_safe function
  - Remaining: Tests 7-8 fail due to struct assignment bug (see Known Issues)
- **CRITICAL FIX #3**: Fixed X8/vreg stack collision in sret functions (Feb 6, 2026)
  - Root cause: X8 (struct return pointer) saved at FP+32 when usesHeapAlloc=true, but vreg 8 also at FP+32
  - Prologue used conditional heapRegsSize=16, but spill offset calculation used heapRegsSize=0
  - Solution: Always save X8 at FP+16 to match spill offset calculation
  - Simplified logic in emitPrologue and emitReturn
  - File: pkg/codegen/arm64/emit.go lines 502-514, 2323-2328
  - This fixed one source of corruption, but tests 7-8 still fail due to struct assignment bug
- **CRITICAL FIX #4**: Fixed global struct assignment and array field storage (Feb 6, 2026)
  - Root cause #1: Array/slice fields in structs used OpStore (8 bytes) instead of OpMemCopy (24 bytes)
    - Arrays are 24-byte fat pointers [ptr, len, cap], not 8-byte values
    - OpStore only copied first 8 bytes (pointer), leaving len/cap uninitialized
    - Solution: Use OpMemCopy for array/slice fields like we do for struct fields
  - Root cause #2: Global struct assignments used OpStore instead of OpMemCopy
    - OpStore copies 8 bytes (pointer to struct), not entire struct data
    - When reassigning `g_s = S { a: []int{} }`, only pointer was copied, not struct content
    - Solution: Use OpMemCopy for global struct assignments, same as local variables
  - File: pkg/ir/builder.go lines 995-1010 (global assignment), 2093-2111 (struct field storage)
  - Result: Bootstrap sema now 8/8 tests passing! All array operations in structs work correctly

**Heap Allocator (Jan 2026):**
- Fixed heap state corruption by removing X25/X26 save/restore
  - X25/X26 hold global heap state (heap_ptr, heap_end) across all functions
  - Previously saved in prologue and restored in epilogue, causing corruption
  - Solution: Treat X25/X26 as truly global, no save/restore needed
  - Heap state now persists correctly across function calls

**ARM64 Code Generation:**
- Fixed modulo operator (%) returning incorrect values
  - SDIV was overwriting left operand when it was in X16
  - Now uses X18 as temporary to preserve original value
  - Correctly computes: result = left - (left / right) * right
- Fixed ARM64 stack corruption for large stack frames (>4095 bytes)
  - 12-bit immediate truncation in SUBi/ADDi caused incorrect stack sizes
  - Added `addImm` helper using MOVimm + ADD/SUB for large values

**Struct and Memory Handling:**
- Fixed struct return buffer corruption from type size mismatch
  - IR builder: arrays/slices = 24 bytes (ptr + len + cap)
  - Codegen was using 8 bytes, causing sret buffer underallocation
  - Fixed emit.go typeSize to return 24 bytes for Array/Slice types
- Implemented proper sret (struct return) calling convention
  - Caller sets X8 to result buffer, callee writes to [X8], saves X8 at FP+16
- Fixed struct parameter passing to copy data to callee's stack frame
- Fixed string size to 8 bytes (pointer to null-terminated data)
  - Changed from 16-byte fat pointer to match runtime implementation
  - All string operations use null-terminated C strings

**String Constant Loading (Feb 2026):**
- Implemented ADRP+ADD for string constants (ARM64 production standard)
  - Replaces single ADR with ADRP (page address) + ADD (page offset)
  - More reliable than ADR for position-independent code
  - Standard approach used by LLVM and GCC
- Fixed codeVMAddr mismatch between compiler and Mach-O writer
  - main.go calculated codeFileOff=1024, but writer used codeFileOff=768
  - Added CodeVMAddr() method to get actual address from writer
  - All fixups now use consistent VM addresses
- Fixed string size inconsistency causing array push crashes
  - pkg/types/types.go still returned 16 bytes while emit.go/builder.go used 8 bytes
  - Caused push to copy 16 bytes from 8-byte pointer, corrupting memory
  - Changed Basic.Size() for String to return 8 bytes consistently
  - All string array operations now work correctly

**Compilation and Symbol Resolution:**
- Fixed forward function references in IR generation
  - buildIdent now checks TypeInfo.Uses to resolve function symbols
  - Enables two-pass compilation: functions callable before definition
  - Bootstrap parser tests now all pass (5/5)

### Known Issues
- **Global structs with array fields crash on array indexing**
  - Symptom: `let mut s = S { a: []int{1,2,3} }; s.a[0]` crashes with SIGSEGV
  - Works: Local structs with arrays, global arrays (not in structs), array length access, manual initialization in function body
  - Fails: Only when array fields are initialized during global initialization (injected at start of main)
  - Root cause: Fat pointer data pointer (offset+0) corrupted during `buildStructGlobalInit`
    - `OpStore(elemPtr, fieldAddr)` where `fieldAddr` from `OpIndexAddr(GlobalRef, offset)` fails
    - Same operation works in regular function body, only fails during global init
    - Length field (offset+8) correct, suggesting partial write or addressing issue
    - Likely vreg spilling/loading bug or GlobalRef+offset materialization issue during initialization
  - **Workaround**: Initialize array fields to empty `[]int{}`, then populate in init function
    ```ease
    let mut g_s = S { a: []int{} }
    fn init() { g_s.a = []int{1, 2, 3} }
    fn main() { init(); ... }
    ```
  - Needs deep debugging: IR dump, assembly inspection, or debugger to trace actual addresses
- **Array operations on returned structs**: When a struct containing an array is returned from a function, then passed to another function that reads from AND pushes to that array, it crashes
  - Pattern: `struct S { arr: []int }; fn make() -> S { ... }; fn use(s: S) { let x = s.arr[0]; push(s.arr, x+1); }`
  - Workaround: Avoid combining struct returns with complex array operations in the same function

### Future
- [ ] Standard library expansion (strings, strconv, and io complete)
  - [x] strconv - string/number conversions (Itoa, Atoi, ParseInt, FormatInt)
  - [ ] os - process, environment, command execution
  - [ ] path - file path manipulation
  - [ ] More as needed for self-hosting
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

