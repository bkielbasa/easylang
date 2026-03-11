# Generics Design for Ease

## Overview

Add compile-time generics to Ease using type erasure. Since all Ease values are i64 at runtime (uniform representation), generics are purely a compile-time type-checking layer with zero runtime cost.

## Syntax

Go-style square brackets for type parameters.

### Generic Functions
```ease
fn Identity[T](x: T) -> T { return x }
fn Map[T, U](items: []T, f: fn(T) -> U) -> []U { ... }
```

### Generic Structs
```ease
struct Box[T] { value: T }
struct Pair[A, B] { first: A, second: B }
```

### Generic Enums
```ease
enum Option[T] { Some { value: T }, None }
enum Result[T, E] { Ok { value: T }, Err { error: E } }
```

### Generic Methods
```ease
fn (b: Box[T]) Get() -> T { return b.value }
fn (b: Box[T]) Set(v: T) -> Box[T] { return Box[T]{ value: v } }
```

### Generic Interfaces
```ease
interface Container[T] { Get() -> T, Size() -> int }
```

### Constrained Type Parameters
```ease
fn Print[T: Stringer](x: T) { print(x.String()) }
fn Max[T: Comparable](a: T, b: T) -> T { ... }
```

### Call-Site Usage
```ease
// Inferred
b := Box{ value: 42 }
x := Identity(42)

// Explicit
b := Box[string]{ value: "hi" }
x := Identity[int](42)
```

## Implementation Strategy

### Type Erasure

All values are i64 at runtime. Generic and non-generic code produce identical LLVM IR. `Box[int].value` and `Box[string].value` both load an i64 from offset 0. The only work is in the parser (syntax) and sema (type checking). The backend is untouched.

### Parser Changes

- Parse `[T]`, `[T, U]`, `[T: Iface]` after struct/enum/fn/interface names
- Store type parameters as list of (name, constraint) pairs
- Parse `T` as a valid type in parameter/return/field positions (placeholder type)
- Parse `Box[int]`, `Option[string]` as concrete instantiations at use sites

### Sema Changes

- New type parameter scope: bind T, U as type variables when entering generic definition
- Constraint checking: verify operations on T are valid per its interface bound
- Instantiation: at call sites, resolve type arguments (inferred or explicit), verify constraints
- Unification: infer T from argument types by matching against function parameters
- Field registry: generic struct fields store type parameter name, resolve at use site

### IRgen / LLVM Changes

None. Everything is i64. Type parameters are erased after sema validation.

### Unified TYPE_ARRAY

Add `TYPE_ARRAY = 14` constant. `[]T` resolves to `TYPE_ARRAY` with element type info stored in the vreg struct name field (e.g., `"[]Person"`). Replaces combinatorial explosion of `TYPE_X_ARRAY` constants over time.

## Error Messages

Errors point at the call site, not inside generic bodies.

### Constraint violation
```
error: type 'int' does not satisfy interface 'Stringer'
  --> main.ease:5:1
  | Print(42)
  |       ^^ 'int' is missing method 'String() -> string'
  note: required by constraint T: Stringer in Print[T: Stringer]
```

### Type mismatch
```
error: type mismatch in struct literal
  --> main.ease:3:25
  | Box[int]{ value: "hello" }
  |                  ^^^^^^^ expected 'int', found 'string'
```

### Inference conflict
```
error: conflicting types for type parameter T
  --> main.ease:4:1
  | Pick(42, "hello")
  |      ^^ inferred T = int
  |          ^^^^^^^ inferred T = string
```

## Stdlib Impact

### Replace current non-generic types
```ease
enum Option[T] { Some { value: T }, None }
enum Result[T, E] { Ok { value: T }, Err { error: E } }

fn Unwrap[T](opt: Option[T], def: T) -> T
fn UnwrapResult[T, E](res: Result[T, E], def: T) -> T
```

### New collection functions
```ease
fn Map[T, U](items: []T, f: fn(T) -> U) -> []U
fn Filter[T](items: []T, f: fn(T) -> bool) -> []T
fn Reduce[T, U](items: []T, init: U, f: fn(U, T) -> U) -> U
fn Contains[T: Comparable](items: []T, target: T) -> bool
```

### Try operator
`?` works with `Result[T, E]` and `Option[T]` — same tag-based mechanism, just recognizing generic enum names.

### Non-generic stdlib unchanged
`strings`, `strconv`, `io`, `os`, `json`, `map[K]V` — no changes needed.

## Migration Plan

1. Implement generics in compiler using old non-generic types
2. Update stdlib to use generic types
3. Update compiler source to use new generic stdlib
4. Verify byte-identical self-compilation convergence

## Testing

### Unit tests (tests/generics_test.ease)
- Generic functions (Identity with int, string)
- Generic structs (Box, Pair with various types)
- Generic enums (Option, Result)
- Generic methods (Box.Get, Box.Set)
- Constrained generics (T: Stringer)
- Type inference (from arguments, struct literals)
- Collection functions (Map, Filter, Reduce)
- Try operator with generic Result/Option
- Compiler error tests (constraint violations, mismatches, inference conflicts)

### Self-hosting convergence
Existing bootstrap test must keep passing after stdlib migration.
