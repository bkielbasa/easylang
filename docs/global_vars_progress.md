# Global Variable Support - Progress Report

## Completed

### 1. AST (pkg/ast/ast.go)
✓ Added VarDecl struct for global variable declarations
✓ Includes Mutable bool field to support `let` and `let mut`

### 2. Parser (pkg/parser/parser.go)
✓ Added case for token.Let in parseDeclaration()  
✓ Implemented parseVarDecl() function
✓ Parses both `let name = expr` and `let mut name = expr`
✓ Requires initializer for global variables

### 3. Semantic Analysis (pkg/sema/analyzer.go)
✓ Added case for *ast.VarDecl in collectDeclarations()
✓ Implemented collectVarDecl() function
✓ Type checks initializer expression
✓ Distinguishes between mutable (VarSymbol) and immutable (ConstSymbol)
✓ Registers globals in symbol table

### 4. IR Generation (pkg/ir/ir.go, pkg/ir/builder.go)
✓ Added OpndGlobal operand kind
✓ Added Global string field to Operand struct
✓ Implemented GlobalRef() constructor
✓ Updated Operand.String() to display globals as "@name"
✓ Implemented buildConstDecl() and buildVarDecl()
✓ buildIdent() already supports looking up globals

## Testing

Created test file `/tmp/test_global_var.ease`:
```ease
let x = 42
let mut y = 100

fn main() -> int {
    print("x = ")
    print(strconv.Itoa(x))
    print("\n")
    
    print("y = ")
    print(strconv.Itoa(y))
    print("\n")
    
    return 0
}
```

**Current Status**: 
- Parser ✓ Works
- Semantic Analysis ✓ Works  
- IR Generation ✓ Works
- Code Generation ⚠️  Needs implementation
- Runtime: Prints garbage addresses instead of values

## Remaining Work

### Code Generation (pkg/codegen/arm64/emit.go)
Need to implement:
1. Allocate globals in .data section (initialized) or .bss (uninitialized)
2. Generate OpndGlobal load/store operations
3. Use ADRP + ADD for PC-relative addressing to access globals
4. Handle global initialization (may need __init function)

### Mach-O Generation (pkg/macho/writer.go)
Need to implement:
1. Create .data and .bss segments for globals
2. Write initialized global data
3. Set up proper segment alignment and permissions

## Next Steps

1. Implement loadOperand() support for OpndGlobal in emit.go
2. Add global data section to Mach-O output
3. Generate initialization code for complex global expressions
4. Test with various global variable types (int, string, arrays, structs)
5. Add mutation support for `let mut` globals

## Impact on Bootstrap Semantic Analyzer

Once complete, this enables the refactoring of bootstrap/sema.ease to use:
```ease
let mut g_semas = []Sema{}

fn analyze_binary(s_idx: int, ...) -> int {
    let mut s = g_semas[s_idx]
    // ... work with s ...
    g_semas[s_idx] = s
}
```

This avoids the 480-byte struct-by-value passing that causes stack corruption.
