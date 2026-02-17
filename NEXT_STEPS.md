# Ease Language - Next Steps

Current state: Self-hosting bootstrap compiler with LLVM IR backend (convergence achieved Feb 17, 2026). Go compiler is production-ready with 6/6 integration tests passing. 234 functions, 13858 IR instructions.

---

## Phase 1: Solidify Foundation (Weeks 1-4) ✅ COMPLETE

### 1.1 Fix Global Array Limit Bug ✅
- Fixed by LLVM backend migration (no longer uses fragile register-based init)
- Regression test: `testdata/global_arrays.ease`

### 1.2 Implement Test Framework ✅
- `test "name" { }` syntax with `#[tag]` attributes
- `ease test` command with discovery, `-name`, `-tag`, `-skip` filtering
- Example tests: `examples/basic_test.ease`, `examples/math_test.ease`

### 1.3 Unused Imports Error ✅
- Semantic analysis tracks module usage via `usedModules` map
- Compile error emitted for imports that are never referenced
- Error message: `'module' imported and not used`

### 1.4 Add Enum/Match Examples ✅
- `examples/enums.ease` - simple enums, data variants, pattern matching

---

## Phase 2: Error Handling (Weeks 3-6)

### 2.1 Result<T, E> and Option<T> Types
- Implement as builtin enums
- `Result<T, E>` with Ok/Err variants
- `Option<T>` with Some/None variants
- Return inference: `return value` wraps in Ok, `return error.New(msg)` wraps in Err

### 2.2 Try Operator (`?`)
- Error propagation: `let val = risky_call()?`
- Unwraps Ok or early-returns Err
- Only valid inside functions returning Result

### 2.3 Update Stdlib to Use Result Types
- `io.ReadFile` -> `Result<string, Error>`
- `strconv.Atoi` -> `Result<int, Error>`
- Fix remaining TODOs in io.ease (FileExists, ReadDir, CreateDir, Remove)

---

## Phase 3: Type System Completion (Weeks 5-10)

### 3.1 Complete Trait System
- Parser and semantic analysis already done
- Need codegen: vtables or monomorphization for trait method dispatch
- `impl Trait for Type` blocks
- Test with patterns: Display, Iterator, Eq

### 3.2 Full Generics
- Monomorphization infrastructure exists (sema/monomorph.go)
- Complete generic function/struct instantiation
- Add trait bounds: `fn max<T: Ord>(a: T, b: T) -> T`

### 3.3 Tuple Types
- Grammar defined (line 156, 316)
- Destructuring: `let (x, y) = get_coords()`
- Useful for multiple return values

---

## Phase 4: Ecosystem (Weeks 8-14)

### 4.1 External Imports
- URL-style: `"github.com/user/pkg" as p`
- Package fetching (git clone + cache)
- Version resolution

### 4.2 Stdlib Expansion
**Core (high priority):**
- `os` - process, environment, command execution
- `path` - file path manipulation
- `fmt` - formatted printing
- `math` - basic math operations

**Advanced (lower priority):**
- `json` - JSON parsing/serialization
- `http` - HTTP client/server
- `net` - networking

### 4.3 Package Manager
- `ease get` for downloading packages
- `ease.toml` for dependency specification
- Lock files for reproducible builds

---

## Phase 5: Multi-Platform (Weeks 12-20)

### 5.1 x86_64 Backend
- Support Intel Macs and Linux
- System V AMD64 ABI calling convention
- ELF binary format for Linux

### 5.2 WebAssembly Backend
- Browser execution (design goal)
- WASM instruction generation
- JavaScript interop layer

---

## Phase 6: Developer Experience (Ongoing)

### 6.1 Language Server Protocol (LSP)
- Autocomplete, go-to-definition, diagnostics
- VSCode extension

### 6.2 Formatter (`ease fmt`)
- Use existing parser, pretty-print AST
- Define standard style

### 6.3 Documentation Generator (`ease doc`)
- Extract `///` doc comments
- Generate HTML docs

---

## Phase 7: Bootstrap Compiler Growth

### 7.1 Add Type System to Bootstrap
- Currently uses `is_string_expr` heuristic (no types)
- Would eliminate need to manually track string-returning functions
- Major reliability improvement

### 7.2 Remove Go Dependency
- Once bootstrap compiler has enough features, check in a pre-built binary
- Use self-compiled binary as seed compiler (like Rust's stage0)
- Eventually remove Go compiler entirely

---

## Quick Wins (can start anytime)

| Task | Effort | Impact |
|------|--------|--------|
| Unused imports error | 1 day | Code quality |
| Enum/match examples | 1 day | Showcase |
| Fix strconv ASCII hardcoding | 2 days | Correctness |
| More integration tests | ongoing | Confidence |
| Float type support | 1 week | Language completeness |
| Hex/octal/binary literals | 3 days | Developer convenience |
| Constants (`const`) | 2 days | Language completeness |

---

## Priority Summary

**Immediate**: Fix global array bug, test framework, unused imports error
**Short term**: Error handling (Result/Option), trait codegen, generics
**Medium term**: External imports, stdlib expansion, x86_64 backend
**Long term**: WebAssembly, package manager, LSP, optimizations
