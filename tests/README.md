# Ease Compiler Integration Tests

This directory contains integration tests that verify end-to-end compiler functionality.

## Running Tests

```bash
make test
```

Or run individual tests manually:
```bash
./tmp/ease tests/01_basic_math.ease
clang -O1 runtime/ease_runtime.c tmp/output.ll -o tmp/test_bin
./tmp/test_bin
```

## Test Coverage

### 01_basic_math.ease
- Basic arithmetic operations (+, -, *, /)
- Variable declarations
- Integer comparisons
- Conditional logic (if statements)

### 02_functions.ease
- Multi-parameter function calls
- Function return values
- Recursive functions (factorial)
- Function composition

### 03_arrays.ease
- Array literals: `[]int{1, 2, 3}`
- Array indexing: `arr[i]`
- Array length: `len(arr)`
- Array mutation: `push(arr, value)`

### 04_strings.ease
- String module operations
- String concatenation, contains, starts/ends with
- String index search
- strconv module (Itoa, Atoi)
- Module imports

### 05_structs.ease
- Struct definitions
- Struct literals
- Field access
- Struct parameters and returns
- Multiple struct types

### 06_loops.ease
- Range loops: `for i in start..end`
- Condition loops: `for condition`
- Loop variable mutation
- Array iteration
- Modulo operator (%)

## Test Exit Codes

- `0`: Test passed
- `1-N`: Test failed at assertion N (see test source for which assertion)

## Adding New Tests

1. Create a new `.ease` file in this directory
2. Name it with a number prefix (e.g., `07_new_feature.ease`)
3. Return 0 for success, non-zero for failure
4. Test will be automatically included when running `make test`

Example:
```ease
package main

fn main() -> int {
    let x = some_operation()
    if x != expected_value {
        return 1  // fail
    }
    return 0  // success
}
```
