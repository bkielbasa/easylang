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

## ⚠️ Limitations

### Mutable Globals
While reads work, **writes to mutable globals are not yet implemented**:
```ease
let mut count = 0
count = count + 1         // Won't actually update the global
```

**Reason**: Immutables are compile-time constants (no storage). Mutables need runtime storage in a data section.

### Complex Initializers
Arrays, structs, and expressions don't work as globals yet:
```ease
let mut arr = []int{}     // Crashes - needs storage
let config = Config {...} // Crashes - needs storage
let computed = f()        // Crashes - needs init code
```

**Reason**: These need either:
1. Data section allocation (compile-time)
2. Runtime initialization code (at program start)

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

✅ **Phase 1 Complete**: Parser, Sema, IR, simple literal globals
⚠️ **Phase 2 Needed**: Data section + complex globals for full functionality
🎯 **Goal**: Enable `let mut g_semas = []Sema{}` pattern
