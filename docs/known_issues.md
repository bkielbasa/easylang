# Known Issues

## Global Array Limit (SIGBUS with 10+ arrays)

**Status**: Open bug, needs investigation

**Description**:
Programs with 10 or more global array variables crash with SIGBUS (exit code 138) during runtime initialization, before any main() code executes.

**Reproduced with**:
```ease
let mut g1 = []int{}
let mut g2 = []int{}
// ... (up to g9 works fine)
let mut g10 = []int{}  // Adding 10th causes SIGBUS

fn main() -> int {
    // Never reaches here
    return 0
}
```

**Symptoms**:
- Compiles successfully
- 1-9 global arrays: works fine
- 10+ global arrays: SIGBUS crash during initialization
- The crash occurs before any main() code runs

**Investigation so far**:
- Mach-O file structure looks correct (segments, sections, offsets all valid)
- Data section size is correct (24 bytes × N arrays)
- Disassembly shows correct ADRP+ADD for addressing
- Each array gets proper mmap call for heap allocation
- Likely related to register allocation or stack frame management during initialization

**Workaround**:
For now, limit programs to 9 or fewer global arrays. For larger state (like bootstrap sema with 20 arrays), use one of:
1. Combine related arrays into struct arrays (when struct globals are implemented)
2. Declare globals in main() and pass around (less efficient)
3. Split across multiple compilation units

**Priority**: High - blocks bootstrap self-hosting progress

## Struct Return Buffer Corruption with Nested Calls

**Status**: Documented workaround exists

**Description**:
When a function returns a large struct (>100 bytes), the sret buffer is allocated at a low offset in the caller's stack frame. If the caller then makes nested function calls with large stack frames, those frames can extend upward and overwrite the sret buffer, corrupting the returned struct data.

**Workaround**: Keep structs small or avoid returning them from functions that will be called recursively.

**See**: CLAUDE.md Known Issues section
