# Testing and Benchmarks

## Testing (Go-style, implemented)
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
make test                           # run all tests in tests/ (157 passing)
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

## Benchmarks (Go-style, implemented)
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
- **Timing**: Inline `clock_gettime(CLOCK_MONOTONIC)` via LLVM IR

**Example output:**
```
BenchmarkAdd	536870912	2 ns/op
BenchmarkFactorial	33554432	35 ns/op
```

## Running Tests

```bash
make                                    # Build from seed (no Go required)
make test                               # Run tests (90 passing)
make test DIR=path                      # Run tests in specific directory
make bench                              # Run tests + benchmarks
make verify                             # Verify self-hosting convergence
make update-seed                        # Update seed after source changes
```

## Test Suite (90 tests, Go-style)

Tests live in `tests/` as `*_test.ease` files with `fn TestXxx(t: T)` functions:

| File | Tests | Features Covered |
|------|-------|-----------------|
| `math_test.ease` | 4 | `+`, `-`, `*`, `/`, conditionals |
| `functions_test.ease` | 2 | Parameters, return values, recursion |
| `arrays_test.ease` | 3 | Literal, index, append, len |
| `strings_test.ease` | 6 | Concat, Contains, StartsWith, EndsWith, IndexOf, strconv |
| `structs_test.ease` | 3 | Struct literals, field access, pass to functions |
| `loops_test.ease` | 3 | Range `for i in start..end`, condition loops, modulo |
| `enum_test.ease` | 3 | Enum variants, match expressions, field destructuring |
| `result_test.ease` | 10 | Option, Result, StringOption types, match arm string bindings, `?` try operator |
| `time_test.ease` | 6 | time.Now, Unix, UnixNano, Add, Before, After, Since |
| `methods_test.ease` | 6 | Method receivers, value/pointer receivers, dispatch |
| `interface_test.ease` | 4 | Implicit interfaces, vtable dispatch, polymorphism, multi-interface |
| `json_test.ease` | 12 | JSON build, marshal, parse, nested objects, arrays, escaping |
| `bench_test.ease` | 2 | Benchmark: add, factorial (auto-calibrating ns/op) |
| `helpers.ease` | — | Shared helper functions, structs, enums, methods |

## Example Programs

See `examples/` directory for working example programs:
- `calculator.ease` - Arithmetic, recursion (factorial, fibonacci)
- `string_demo.ease` - String operations, stdlib usage
- `data_structures.ease` - Structs, arrays, algorithms
- `file_io.ease` - File I/O operations

All examples tested and working. See `examples/README.md` for feature matrix.
