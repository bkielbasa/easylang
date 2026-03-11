# Ease Language Design

## Core Design
- **Type System**: Static with inference
- **Memory**: Garbage collected
- **Targets**: Native (macOS ARM64/x86_64 via LLVM) + WebAssembly (future)
- **Syntax**: Go-like (braces, no semicolons required)

## Error Handling
- **Result types**: `Result<T, Error>` for fallible operations
- **No null**: Use `Option<T>` instead (Some/None)
- **Try operator**: `?` for error propagation (implemented)
- **Return inference**: `return value` infers Ok, `return error.New("msg")` infers Err
- **Implicit success**: Functions returning `Result<(), Error>` succeed implicitly at end

### Try Operator `?` (implemented)
```ease
// Before: verbose match
fn read_config() -> Result {
    res := parse_file("config.txt")
    val := match res {
        Result::Ok { value } => value,
        Result::Err { message } => { return Result::Err { message: message } },
    }
    return Result::Ok { value: val + 1 }
}

// After: concise with ?
fn read_config() -> Result {
    val := parse_file("config.txt")?
    return Result::Ok { value: val + 1 }
}
```
- **Postfix operator**: `expr?` extracts the success value or early-returns the error/none
- **Supported types**: `Result` (Ok/Err), `Option` (Some/None), `StringOption` (Some/None)
- **Success variant**: `Ok` for Result-like enums, `Some` for Option-like enums
- **Error path**: early `return` with the original enum value (Err or None variant)
- **Implementation**: Compiles to tag check + branch + field extract (same IR ops as match)

## Visibility (Go-style)
- **Uppercase** first letter = public (exported)
- **Lowercase** first letter = private (package-internal)

## Package Declarations
Every `.ease` file starts with a `package` declaration, exactly like Go:
```
package main           // executable programs
package token          // library package (matches directory name)
```
- One package per directory
- Package name must match the directory name
- `package main` for executable entry points
- The parser skips the declaration (no semantic enforcement yet)

## Imports
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

**Status**: Local file imports, directory package imports, bare stdlib imports, and `import "testing"` all working! Directory imports enforce visibility (uppercase = public). External imports coming soon.

## FFI: extern fn (implemented)
```ease
// Declare a C function with explicit types
extern fn system(cmd: ptr) -> i32

// Ease wrapper with idiomatic types
fn System(cmd: string) -> int {
    return system(cmd)
}
```
- Supported extern types: `ptr` (C pointer), `i32` (C int), `i64` (C long), `void` (no return)
- The compiler generates LLVM IR wrappers that bridge Ease's all-i64 calling convention to C types
- Use in stdlib modules to call libc functions without adding new opcodes

## Constants (implemented)
```ease
const MAX_SIZE = 1024
const APP_NAME = "myapp"
const DEBUG = false
const NEGATIVE = -1
```
- Top-level only, initializer must be a literal (int, string, bool, or negative int)
- Zero overhead: values are inlined directly at use sites (no global variable allocation)
- Assignment to constants is a compile error
- Constants can be used in any expression context

## Loops (Go-style, only `for`)
```
for { }                    // infinite loop
for condition { }          // condition-based (like while)
for x in collection { }    // range iteration
```

## Enums with Pattern Matching (implemented)
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

## Method Receivers (Go-style, implemented)
Methods are functions with a receiver parameter, declared with Go-style syntax:

```ease
struct Counter {
    count: int,
}

fn NewCounter(initial: int) -> Counter {
    return Counter { count: initial }
}

fn (c: Counter) Value() -> int {
    return c.count
}

fn (c: Counter) Add(n: int) -> int {
    return c.count + n
}

fn (c: *Counter) Double() -> int {
    return c.count * 2
}

fn main() {
    c := NewCounter(42)
    print(strconv.Itoa(c.Value()) + "\n")    // 42
    print(strconv.Itoa(c.Add(8)) + "\n")     // 50
    print(strconv.Itoa(c.Double()) + "\n")   // 84
}
```
- **Value receivers**: `fn (c: Counter) Method()` — receiver passed by value (since structs are heap-allocated, effectively a pointer)
- **Pointer receivers**: `fn (c: *Counter) Method()` — explicit pointer receiver syntax (same semantics currently, since all structs are already pointers)
- **Name mangling**: `fn (c: Counter) Value()` compiles to internal function `Counter_Value(c)`
- **Dispatch**: `c.Value()` checks if `c` is a local variable, looks up its struct type, finds the method, and calls `Counter_Value(c)` with receiver as first argument
- **Struct type tracking**: Vreg-based struct name tracking (`g_vreg_struct_names`) propagates struct type info through assignments, function returns, and field accesses

## Pointer Syntax (implemented)
```ease
*T          // pointer-to-T type (in type positions)
&x          // address-of operator (identity op — structs are already heap pointers)
*x          // dereference operator (identity op — structs are already heap pointers)
```
- Since all struct values are heap-allocated pointers internally (i64 at LLVM level), `&` and `*` are currently identity operations
- Pointer types parsed in parameters, return types, and struct fields
- `TYPE_PTR` (8) added to type system constants

## Interfaces (Go-style, implemented)
Implicit interfaces — a struct satisfies an interface by implementing all required methods (no `implements` keyword).

```ease
interface Greeter {
    Greet() -> string,
}

struct English { name: string }
struct Spanish { name: string }

fn (e: English) Greet() -> string { return "Hello, " + e.name }
fn (s: Spanish) Greet() -> string { return "Hola, " + s.name }

fn greet_with(g: Greeter) -> string {
    return g.Greet()
}

fn main() {
    e := English { name: "World" }
    s := Spanish { name: "Mundo" }
    print(greet_with(e) + "\n")    // Hello, World
    print(greet_with(s) + "\n")    // Hola, Mundo
}
```

- **Implicit satisfaction**: Any struct with matching methods satisfies the interface — no declaration needed
- **Interface values**: Heap-allocated 16-byte pair `[concrete_ptr, vtable_ptr]`
- **Vtable dispatch**: Method calls on interface values go through a vtable for indirect dispatch
- **Auto-wrapping**: Concrete structs are automatically wrapped when passed to interface-typed parameters
- **Multiple interfaces**: A struct can satisfy multiple interfaces simultaneously
- **Method signatures**: Interface methods declare name, parameters, and return type

```ease
interface Sizer {
    Size() -> int,
}

interface Stringer {
    String() -> string,
}

struct Box { width: int, height: int }

// Box satisfies both Sizer and Stringer
fn (b: Box) Size() -> int { return b.width * b.height }
fn (b: Box) String() -> string {
    return strconv.Itoa(b.width) + "x" + strconv.Itoa(b.height)
}
```

## Generics (implemented)

Go-style bracket syntax with type erasure. All Ease values are i64 at runtime (ints, pointers, booleans), so generic type parameters are purely compile-time annotations that get erased during compilation. No monomorphization or boxing needed.

```ease
// Generic struct
struct Box[T] {
    value: T,
}

struct Pair[A, B] {
    first: A,
    second: B,
}

// Generic enum
enum Option[T] { Some { value: T }, None }

// Generic function
fn identity[T](x: T) -> T {
    return x
}

// Usage with explicit type args
b := Box[int] { value: 42 }
p := Pair[string, int] { first: "age", second: 30 }
result := identity[int](99)

// Type args are optional (erased at compile time)
b2 := Box { value: 42 }
```

### Constraints (syntax supported, enforcement planned)
```ease
// Type parameter with interface constraint
struct Sortable[T: Comparable] {
    items: []T,
}
```

### Scope
- Structs, enums, functions, methods, interfaces all support type parameters
- Type parameters are declared with `[T]`, `[T, U]`, or `[T: Constraint]`

## Closures and Lambdas

Closures are anonymous functions that can capture variables from their enclosing scope.

```ease
// Basic closure with block body
add := |a: int, b: int| -> int { return a + b }
add(3, 5)  // 8

// Expression body (implicit return)
double := |x: int| -> int x * 2
double(7)  // 14

// No parameters
get := || -> int { return 42 }
get()  // 42

// Capturing variables from enclosing scope
x := 10
add_x := |y: int| -> int { return x + y }
add_x(5)  // 15

// Factory function returning a closure
fn make_adder(n: int) -> int {
    return |x: int| -> int { return n + x }
}
add5 := make_adder(5)
add5(3)  // 8

// Move semantics (currently same as default capture)
val := 42
get_val := move |x: int| -> int { return val + x }
```

Closures are represented as heap-allocated `[func_ptr, env_ptr]` pairs. Captured variables are copied by value into the environment at closure creation time.

## Concurrency
- **Goroutines**: `go expression`
- **Channels**: `chan<T>()`, `ch <- value`, `<-ch`
- **Select**: for multiple channel operations
