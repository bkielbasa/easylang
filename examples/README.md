# Ease Language Examples

This directory contains example programs demonstrating various features of the Ease programming language.

## Running Examples

```bash
./ease run examples/<filename>.ease
```

## Examples

### calculator.ease
**Features**: Functions, recursion, arithmetic, control flow

Demonstrates basic arithmetic operations, recursive algorithms (factorial, fibonacci), and function definitions.

```bash
./ease run examples/calculator.ease
```

**Output**: Calculates arithmetic expressions, 5!, and fib(10)

### string_demo.ease
**Features**: String operations, module imports, stdlib usage

Shows string manipulation using the `strings` and `strconv` stdlib modules including concatenation, searching, replacement, and conversions.

```bash
./ease run examples/string_demo.ease
```

**Output**: Various string operation results

### data_structures.ease
**Features**: Structs, nested types, arrays, iteration

Demonstrates struct definitions, nested structs, array operations, and algorithms like sum and find_max.

```bash
./ease run examples/data_structures.ease
```

**Output**: Point/rectangle calculations, array operations

### file_io.ease
**Features**: File I/O, syscalls, error handling

Shows low-level syscall usage and high-level file operations for reading and writing files.

```bash
./ease run examples/file_io.ease
```

**Output**: Creates and reads test files

## Language Features Demonstrated

### Core Features
- [x] Function definitions and calls
- [x] Multiple parameters and return values
- [x] Local variables with let
- [x] Arithmetic operations (+, -, *, /)
- [x] Comparison operators (<, >, ==, !=, etc.)
- [x] Logical operators (&&, ||)

### Control Flow
- [x] If/else statements
- [x] For loops (range and condition)
- [x] Recursion

### Data Types
- [x] Integers (int)
- [x] Booleans (bool)
- [x] Strings (string)
- [x] Arrays ([]T)
- [x] Structs

### Advanced Features
- [x] Module imports
- [x] Standard library usage
- [x] Struct field access
- [x] Array indexing and length
- [x] Nested data structures
- [x] File I/O operations

## Creating New Examples

1. Create a new `.ease` file in this directory
2. Add documentation comment at the top
3. Implement your example
4. Test it: `./ease run examples/your_example.ease`
5. Add entry to this README

Example template:
```ease
// Brief description of what this example demonstrates

fn main() -> int {
    // Your code here
    return 0
}
```
