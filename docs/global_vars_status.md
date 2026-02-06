# Global Variable Support - Current Status

## 🎉 FULLY IMPLEMENTED

Global variables are now fully functional in Ease!

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

## ✅ Mutable Simple Globals
Mutable simple types work with non-zero initialization:
```ease
let mut count = 42        // Initializes to 42
count = count + 1         // Updates the global
```

**How it works**:
- Mutable globals stored in __DATA segment
- Initial values written to data section (1-byte bool, 8-byte int)
- ADRP+ADD compute the address, LDRx/STRx for reads/writes

## ✅ Array Globals (NEW!)
Array literals as globals are fully functional:
```ease
let mut nums = []int{10, 20, 30}
print(strconv.Itoa(nums[0]))  // 10
nums[1] = 99                  // Modify element
push(nums, 40)                // Push works!
```

**How it works**:
- Runtime initialization at start of main()
- Heap allocation for array elements
- Fat pointer (24 bytes) built directly in global storage
- Mutable globals return address (not copy) for in-place modifications

**Test**: Bootstrap pattern works:
```ease
let mut g_semas = []Sema{}
fn add_sema(s: Sema) { push(g_semas, s) }
```

## ⚠️ Limitations

### Struct Literals as Globals
Struct literals not yet implemented:
```ease
let config = Config { ... } // Not implemented
```

**Reason**: Need struct initialization code generation (similar to arrays)

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
✅ **Phase 4 Complete**: Runtime initialization for array globals
🎯 **Goal Achieved**: `let mut g_semas = []Sema{}` pattern works!

**What's Working:**
- Simple globals: int, bool, string (with non-zero initialization)
- Array globals: `[]int{1,2,3}` with full read/write/push support
- Mutable semantics: modifications persist (no unwanted copies)

**What's Not:**
- Struct literals as globals (easy to add, same pattern as arrays)
- Function calls as initializers: `let x = f()` (needs different approach)
