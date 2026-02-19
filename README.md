# Ease

A self-hosted compiled language with Go-like syntax, targeting native code via LLVM.

```ease
package main

struct Point {
    x: int,
    y: int,
}

fn (p: Point) Sum() -> int {
    return p.x + p.y
}

fn main() {
    p := Point { x: 3, y: 7 }
    print("Sum: " + strconv.Itoa(p.Sum()) + "\n")
}
```

## Features

- **Self-hosted** -- the compiler is written in Ease and compiles itself
- **Go-like syntax** -- `:=` inference, `for` loops, packages, uppercase visibility
- **Static types** with inference, structs, enums, arrays, strings
- **Method receivers** -- `fn (r: Type) Method()` with value and pointer syntax
- **Pattern matching** -- `match` expressions with enum destructuring
- **Go-style testing** -- `fn TestXxx(t: T)` in `*_test.ease` files with benchmarks
- **LLVM backend** -- compiles to LLVM IR, linked with a small C runtime

## Building

Requires only `clang` (or any C compiler). No Go, Rust, or other toolchain needed.

```bash
make              # Build compiler from seed
make test         # Run tests (42 passing)
make verify       # Verify self-hosting convergence
```

## Usage

```bash
# Compile and run a program
tmp/ease program.ease
clang -O1 runtime/ease_runtime.c tmp/output.ll -o program
./program
```

## Language Overview

### Variables and Functions

```ease
x := 42
name := "ease"

fn add(a: int, b: int) -> int {
    return a + b
}
```

### Structs and Methods

```ease
struct Counter {
    count: int,
}

fn (c: Counter) Value() -> int {
    return c.count
}

fn (c: *Counter) Double() -> int {
    return c.count * 2
}
```

### Enums and Pattern Matching

```ease
enum Option { Some { value: int }, None }

result := match opt {
    Option::Some { value } => value,
    Option::None => 0,
}
```

### Control Flow

```ease
for i in 0..10 { }       // range loop
for x > 0 { }            // condition loop
for { }                   // infinite loop
if x > 0 { } else { }
```

### Imports and Packages

```ease
package main

import (
    "./mylib"             // local package
)
```

Standard library: `strings`, `strconv`, `io`, `os`, `time`, `testing`, `result`.

### Testing

```ease
// math_test.ease
package main

fn TestAdd(t: T) {
    if add(2, 3) != 5 {
        testing.Fatal("expected 5")
    }
}

fn BenchmarkAdd(b: B) {
    i := 0
    for i < b.N {
        add(2, 3)
        i = i + 1
    }
}
```

```bash
make test                 # run all tests
make bench                # run tests + benchmarks
make test DIR=path/       # run tests in specific directory
```

## How It Works

The compiler bootstraps from a seed LLVM IR file:

```
seed.ll -> (clang) -> ease binary -> compiles itself -> LLVM IR -> (clang) -> identical ease binary
```

After modifying the compiler source, run `make update-seed` to regenerate the seed.

## Project Structure

```
bootstrap/
  compiler.ease           # compiler main
  seed.ll                 # seed LLVM IR for bootstrapping
  ease/                   # compiler modules (lexer, parser, irgen, llvm, ...)
runtime/
  ease_runtime.c          # C runtime (memory, syscalls)
tests/                    # test suite (*_test.ease)
examples/                 # example programs
```

## License

MIT
