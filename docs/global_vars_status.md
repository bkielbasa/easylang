# Global Variable Support - Current Status

## ✅ Working

### Simple Literal Globals
All simple literal types work correctly:
```ease
let x = 42                // int
let mut y = 100           // mutable int
let name = "Hello"        // string
let flag = true           // bool
```

**How it works**: Simple literals are stored as immediate values (Imm operands) in the IR Globals map. When referenced, they're loaded as immediates - no storage needed.

**Test**: `tmp/test_globals_comprehensive.ease` - all tests pass ✓

### Usage in Expressions
Globals can be used in expressions:
```ease
let sum = x + y           // Works!
if flag { ... }           // Works!
print(name)               // Works!
```

## ✅ Mutable Simple Globals (NEW)
Mutable simple types now work correctly:
```ease
let mut count = 0
count = count + 1         // Works! Updates the global
```

**How it works**:
- Mutable globals are stored in __DATA segment with zero-initialization
- ADRP+ADD instructions compute the address (fixed up at compile time)
- OpLoad emits LDRx to read from global address
- OpStore emits STRx to write to global address

**Test**: `tmp/test_mutable_int.ease` - all tests pass ✓

## ⚠️ Limitations

### Complex Initializers (Still TODO)
Arrays, structs, and expressions don't work as globals yet:
```ease
let mut arr = []int{}     // Not implemented yet
let config = Config {...} // Not implemented yet
let computed = f()        // Not implemented yet
```

**Reason**: These need:
1. Data section allocation (✓ implemented)
2. Runtime initialization code (at program start) - NOT YET IMPLEMENTED

## Implementation Details

### IR Layer
- **Simple literals** → `Imm(value)` or `StrConst(idx)` in Globals map
- **Complex values** → `GlobalRef(name)` placeholder

### Code Generation
- **Imm/StrConst**: loadOperand returns scratch register with value
- **GlobalRef**: Attempts ADR+LDR (currently broken, needs data section)

## Next Steps to Complete Feature

### Option A: Data Section (Proper Solution)
1. Add .data segment to Mach-O writer
2. Allocate space for each global
3. Initialize with values at compile-time
4. Fix up ADR instructions to point to data section
5. Support LDR/STR for reads/writes

### Option B: Runtime Initialization (Simpler)
1. Generate __init function or inject code at start of main
2. For each complex global, emit initialization code:
   ```
   make_array(...) → store at global address
   ```
3. Still need data section for storage, but values filled at runtime

### Option C: Hybrid (Recommended)
1. **Simple literals**: Keep as immediates (current approach) ✓
2. **Mutable simple**: Allocate 8-byte slots in data section
3. **Complex globals**: Runtime initialization + data section

## For Bootstrap Sema Refactoring

The critical pattern for fixing the semantic analyzer is:
```ease
let mut g_semas = []Sema{}
```

This needs **Option B or C** to work. Current simple literal support is not sufficient.

**Workaround for now**: Could manually initialize in main:
```ease
let mut g_semas_storage = []Sema{}  // Can't be global yet

fn main() -> int {
    init_sema()  // Initialize locally
    analyze_program(...)
}
```

But this defeats the purpose of avoiding pass-by-value.

## Summary

✅ **Phase 1 Complete**: Parser, Sema, IR for global variables
✅ **Phase 2 Complete**: Code generation for simple globals (immutable and mutable)
✅ **Phase 3 Complete**: Data section, ADRP+ADD addressing, OpLoad/OpStore for mutable globals
⚠️ **Phase 4 Needed**: Runtime initialization for complex globals (arrays, structs)
🎯 **Goal**: Enable `let mut g_semas = []Sema{}` pattern (needs Phase 4)
