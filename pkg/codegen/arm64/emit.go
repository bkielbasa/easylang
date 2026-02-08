package arm64

import (
	"ease/pkg/ir"
	"ease/pkg/types"
	"fmt"
)

// Apple ARM64 ABI:
// - x0-x7: arguments and return values
// - x8: indirect result location register
// - x9-x15: caller-saved temporaries
// - x16-x17: intra-procedure call scratch (IP0, IP1)
// - x18: platform register (reserved)
// - x19-x28: callee-saved
// - x29: frame pointer (FP)
// - x30: link register (LR)
// - SP: stack pointer (must be 16-byte aligned)

const (
	numArgRegs   = 8     // x0-x7
	numTempRegs  = 0     // Spill all vregs to stack for correctness across calls
	numSavedRegs = 10    // x19-x28
	debugEmit    = false // Enable debug output for stack allocation
)

// Emitter converts IR to ARM64 machine code.
type Emitter struct {
	asm              *Assembler
	fn               *ir.Function
	prog              *ir.Program    // program being emitted (for string table)
	vregToReg         map[int]Reg    // virtual register to physical register
	vregSpill         map[int]int    // virtual register to stack offset
	allocOffset       map[int]int    // vreg to allocation offset (for OpAlloc results)
	structRetOffset   map[int]int    // vreg to struct return copy offset (for OpCall with struct results)
	structRetSize     map[int]int    // vreg to struct size (for struct return copies)
	structParamOffset map[int]int    // param vreg to struct parameter copy offset
	structParamSize   map[int]int    // param vreg to struct size (for struct parameter copies)
	labels            map[string]int // label to code offset (block labels)
	funcOffsets       map[string]int // function name to code offset
	globalOffsets     map[string]int // global variable name to data section offset
	fixups            []fixup        // branch fixups
	strFixups         []strFixup     // string address fixups
	globalFixups      []globalFixup  // global variable address fixups
	stackSize         int            // total stack size
	usesHeapAlloc     bool           // whether function uses heap allocation (needs to save X25/X26)
}

type strFixup struct {
	offset int // offset of ADR instruction
	strIdx int // index into program's string table
}

type globalFixup struct {
	adrpOffset int    // offset of ADRP instruction
	addOffset  int    // offset of ADD instruction
	name       string // global variable name
}

type fixup struct {
	offset int
	label  string
	kind   fixupKind
}

type fixupKind int

const (
	fixupB fixupKind = iota
	fixupBcond
	fixupBL
)

// NewEmitter creates a new code emitter.
func NewEmitter() *Emitter {
	return &Emitter{
		asm:           NewAssembler(),
		vregToReg:     make(map[int]Reg),
		vregSpill:     make(map[int]int),
		labels:        make(map[string]int),
		funcOffsets:   make(map[string]int),
		globalOffsets: make(map[string]int),
	}
}

// EmitProgram generates ARM64 code for the entire IR program.
// funcOffsets is populated with function name -> code offset mappings.
func (e *Emitter) EmitProgram(prog *ir.Program, funcOffsets map[string]int64) []byte {
	e.funcOffsets = make(map[string]int)
	e.prog = prog

	// Calculate global variable offsets (will be placed after code + strings)
	e.calculateGlobalOffsets()

	// First pass: emit all functions and record their offsets
	for _, fn := range prog.Functions {
		offset := e.asm.Offset()
		e.funcOffsets[fn.Name] = offset
		if funcOffsets != nil {
			funcOffsets[fn.Name] = int64(offset)
		}
		e.emitFunction(fn)
	}

	// Second pass: fix up cross-function calls
	e.fixupCalls()

	return e.asm.Code()
}

// Emit generates ARM64 code for the IR program.
func (e *Emitter) Emit(prog *ir.Program) []byte {
	return e.EmitProgram(prog, nil)
}

// EmitFunction emits code for a single function and returns its offset.
func (e *Emitter) EmitFunction(fn *ir.Function) (offset int, code []byte) {
	offset = e.asm.Offset()
	e.funcOffsets[fn.Name] = offset
	e.emitFunction(fn)
	return offset, e.asm.Code()[offset:]
}

// Code returns the generated machine code.
func (e *Emitter) Code() []byte {
	return e.asm.Code()
}

// CodeSize returns the size of the generated code.
func (e *Emitter) CodeSize() int {
	return e.asm.Offset()
}

// FixupStrings patches ADRP+ADD instructions for string constants.
// stringOffsets[i] is the offset of strings[i] from the start of the code section.
// codeVMAddr is the VM address where the code will be loaded.
func (e *Emitter) FixupStrings(stringOffsets []uint64, codeVMAddr uint64) {
	for _, fix := range e.strFixups {
		// Calculate VM addresses
		instrVMAddr := codeVMAddr + uint64(fix.offset)
		strVMAddr := codeVMAddr + stringOffsets[fix.strIdx]

		// Read ADRP instruction to get Rd
		adrpInstr := uint32(e.asm.code[fix.offset]) |
			uint32(e.asm.code[fix.offset+1])<<8 |
			uint32(e.asm.code[fix.offset+2])<<16 |
			uint32(e.asm.code[fix.offset+3])<<24
		rd := adrpInstr & 0x1F

		// ADRP: Form PC-relative address to 4KB page
		// Result = (PC & ~0xFFF) + SignExtend(immhi:immlo, 21) << 12
		targetPage := strVMAddr & ^uint64(0xFFF)
		adrpPage := instrVMAddr & ^uint64(0xFFF)
		pageOffset := int64(targetPage) - int64(adrpPage)
		pageCount := int32(pageOffset >> 12)

		// Encode ADRP: op=1, immlo[1:0], 10000, immhi[18:0], Rd
		immlo := uint32(pageCount & 0x3)
		immhi := uint32((pageCount >> 2) & 0x7FFFF)
		adrpNew := uint32(0x90000000) | (immlo << 29) | (immhi << 5) | rd
		e.asm.Patch(fix.offset, adrpNew)

		// ADD: Add offset within page
		// ADD Xd, Xd, #pageOffset
		pageOffset12 := uint32(strVMAddr & 0xFFF)

		// Encode ADD: sf=1, 00, S=0, 10001, sh=00, imm12, Rn, Rd
		addInstr := uint32(0x91000000) | (pageOffset12 << 10) | (rd << 5) | rd
		e.asm.Patch(fix.offset+4, addInstr)
	}
}

// FixupGlobals patches ADRP+ADD instructions for global variables.
// globalAddrs is a map from global variable name to its VM address.
// codeVMAddr is the VM address of the start of the code section.
func (e *Emitter) FixupGlobals(globalAddrs map[string]uint64, codeVMAddr uint64) {
	for _, fix := range e.globalFixups {
		targetAddr, ok := globalAddrs[fix.name]
		if !ok {
			continue // Global not found, skip
		}

		// Calculate PC for ADRP instruction
		adrpPC := codeVMAddr + uint64(fix.adrpOffset)

		// ADRP: compute page-aligned offset (4KB pages)
		// Target page = targetAddr & ~0xFFF
		// Source page = adrpPC & ~0xFFF
		// Page offset = (target page - source page) >> 12
		targetPage := targetAddr & ^uint64(0xFFF)
		sourcePage := adrpPC & ^uint64(0xFFF)
		pageOffset := int64(targetPage) - int64(sourcePage)

		// ADRP immediate is page offset / 4096, but encoded specially
		// immhi:immlo represents the offset in pages
		// We need to shift right by 12 to get page count
		pageCount := int32(pageOffset >> 12)

		// Encode ADRP: immlo is bits [1:0] of pageCount, immhi is bits [20:2]
		immlo := uint32(pageCount & 0x3)
		immhi := uint32((pageCount >> 2) & 0x7FFFF)

		// Read existing ADRP instruction to get Rd
		oldAdrp := uint32(e.asm.code[fix.adrpOffset]) |
			uint32(e.asm.code[fix.adrpOffset+1])<<8 |
			uint32(e.asm.code[fix.adrpOffset+2])<<16 |
			uint32(e.asm.code[fix.adrpOffset+3])<<24
		rd := oldAdrp & 0x1F

		// Re-encode ADRP
		adrpInstr := uint32(0x90000000) | (immlo << 29) | (immhi << 5) | rd
		e.asm.Patch(fix.adrpOffset, adrpInstr)

		// ADD: compute offset within page (bits [11:0] of target address)
		pageOffsetLow := uint16(targetAddr & 0xFFF)

		// Read existing ADD instruction to get Rd and Rn
		oldAdd := uint32(e.asm.code[fix.addOffset]) |
			uint32(e.asm.code[fix.addOffset+1])<<8 |
			uint32(e.asm.code[fix.addOffset+2])<<16 |
			uint32(e.asm.code[fix.addOffset+3])<<24
		rdAdd := oldAdd & 0x1F
		rnAdd := (oldAdd >> 5) & 0x1F

		// Re-encode ADD immediate
		// ADDi format: sf=1, op=0, S=0, 100010, shift=00, imm12, Rn, Rd
		addInstr := uint32(0x91000000) | (uint32(pageOffsetLow) << 10) | (rnAdd << 5) | rdAdd
		e.asm.Patch(fix.addOffset, addInstr)
	}
}

func (e *Emitter) emitFunction(fn *ir.Function) {
	e.fn = fn
	e.labels = make(map[string]int)
	e.vregToReg = make(map[int]Reg)
	e.vregSpill = make(map[int]int)
	e.allocOffset = make(map[int]int)
	e.structRetOffset = make(map[int]int)
	e.structRetSize = make(map[int]int)
	e.structParamOffset = make(map[int]int)
	e.structParamSize = make(map[int]int)

	// Calculate stack layout
	e.calculateStackLayout()

	// Emit prologue
	e.emitPrologue()

	// Map parameters to registers/stack
	e.loadParameters()

	// Emit each block
	for _, block := range fn.Blocks {
		e.emitBlock(block)
	}

	// Fix up intra-function branches
	e.fixupBranches()

	e.fn = nil
}

// getStructSize returns the size of a struct type in bytes, or 0 if not a struct.
func getStructSize(t types.Type) int {
	if t == nil {
		return 0
	}
	// Check if it's a struct type
	underlying := t.Underlying()
	st, ok := underlying.(*types.Struct)
	if !ok {
		return 0
	}
	// Sum up field sizes
	size := 0
	for _, f := range st.Fields {
		size += typeSize(f.Type)
	}
	return size
}

// typeSize returns the size of a type in bytes.
func typeSize(t types.Type) int {
	if t == nil {
		return 8
	}
	switch typ := t.Underlying().(type) {
	case *types.Basic:
		switch typ.Kind {
		case types.Bool:
			return 8 // bools are stored as 64-bit for simplicity
		case types.Int:
			return 8
		case types.String:
			return 8 // pointer to null-terminated string
		default:
			return 8
		}
	case *types.Pointer:
		return 8
	case *types.Array, *types.Slice:
		return 24 // fat pointer: ptr (8) + len (8) + cap (8)
	case *types.Struct:
		size := 0
		for _, f := range typ.Fields {
			size += typeSize(f.Type)
		}
		return size
	default:
		return 8
	}
}

// addImm emits code to compute rd = rn + imm, handling large immediates
// that don't fit in ADDi's 12-bit immediate field.
func (e *Emitter) addImm(rd, rn Reg, imm int) {
	if imm < 4096 {
		// Small immediate - use ADDi directly
		e.asm.ADDi(rd, rn, uint16(imm))
	} else {
		// Large immediate - load into scratch register and use ADD
		e.asm.MOVimm(X17, int64(imm))
		e.asm.ADD(rd, rn, X17)
	}
}

func (e *Emitter) calculateStackLayout() {
	debugFn := debugEmit

	// Check if this function uses heap allocation (needs to save/restore X25/X26)
	usesHeapAlloc := false
	for _, block := range e.fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op == ir.OpHeapAlloc {
				usesHeapAlloc = true
				break
			}
		}
		if usesHeapAlloc {
			break
		}
	}
	e.usesHeapAlloc = usesHeapAlloc

	// Check if this function returns a struct (needs to save X8)
	sretSize := 0
	if e.fn.Result != nil {
		if sz := getStructSize(e.fn.Result); sz > 0 {
			sretSize = 8 // Reserve 8 bytes at FP+16 to save the X8 sret pointer
		}
	}

	// NOTE: We no longer save X25/X26 on the stack - they persist as global heap state
	heapRegsSize := 0

	// Count how many vregs we need to spill
	maxVReg := e.fn.NextVReg
	numSpills := 0

	if debugFn {
		fmt.Printf("DEBUG %s: maxVReg=%d, numTempRegs=%d\n", e.fn.Name, maxVReg, numTempRegs)
	}

	// Simple allocation: first 7 vregs go to x9-x15, rest spill
	// Spill offsets start after the sret save slot (if present)
	for vreg := 0; vreg < maxVReg; vreg++ {
		if vreg < numTempRegs {
			e.vregToReg[vreg] = Reg(X9 + Reg(vreg))
		} else {
			e.vregSpill[vreg] = heapRegsSize + sretSize + numSpills*8
			if debugFn {
				fmt.Printf("DEBUG %s: vreg %d spills to offset %d (FP+16+heapRegs+%d = FP+%d)\n",
					e.fn.Name, vreg, heapRegsSize+sretSize+numSpills*8, heapRegsSize+sretSize+numSpills*8, 16+heapRegsSize+sretSize+numSpills*8)
			}
			numSpills++
		}
	}

	// Stack layout:
	// [sp]        saved x29, x30 (16 bytes)
	// [sp+16]     saved X8 (8 bytes, only for sret functions)
	// [sp+16+sretSize]     spilled vregs
	// NOTE: X25/X26 (heap state) are NOT saved on stack - they persist as global state
	spillSize := sretSize + numSpills*8

	if debugFn {
		fmt.Printf("DEBUG %s: spillSize=%d\n", e.fn.Name, spillSize)
	}

	// Scan for OpAlloc instructions and pre-allocate space
	allocSize := 0
	for _, block := range e.fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op == ir.OpAlloc {
				// Track the allocation offset for this vreg
				// Allocations come after spills
				e.allocOffset[instr.Dest.VReg] = 16 + heapRegsSize + spillSize + allocSize

				// Get allocation size from the immediate argument
				size := int(instr.Args[0].Imm)
				// Round up to 8-byte alignment
				alignedSize := (size + 7) & ^7
				if debugFn {
					fmt.Printf("DEBUG %s: alloc vreg %d at offset %d (size %d)\n",
						e.fn.Name, instr.Dest.VReg, 16+heapRegsSize+spillSize+allocSize, alignedSize)
				}
				allocSize += alignedSize
			}
		}
	}

	if debugFn {
		fmt.Printf("DEBUG %s: allocSize=%d\n", e.fn.Name, allocSize)
	}

	// Scan for OpCall instructions with struct return types
	// and pre-allocate space to copy the returned struct
	structRetSize := 0
	for _, block := range e.fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op == ir.OpCall && instr.Dest.Kind == ir.OpndVReg {
				// Check if the destination type is a struct
				size := getStructSize(instr.Dest.Type)
				if size > 0 {
					// Allocate space for struct return copy
					offset := 16 + heapRegsSize + spillSize + allocSize + structRetSize
					e.structRetOffset[instr.Dest.VReg] = offset
					e.structRetSize[instr.Dest.VReg] = size
					alignedSize := (size + 7) & ^7
					if debugFn {
						fmt.Printf("DEBUG %s: struct ret vreg %d at offset %d (size %d, type=%v, func=%v)\n",
							e.fn.Name, instr.Dest.VReg, offset, size, instr.Dest.Type, instr.Args[0])
					}
					structRetSize += alignedSize
				}
			}
		}
	}

	if debugFn {
		fmt.Printf("DEBUG %s: structRetSize=%d\n", e.fn.Name, structRetSize)
	}

	// Scan function parameters for struct types that need to be copied to local stack
	structParamSize := 0
	for _, param := range e.fn.Params {
		size := getStructSize(param.Type)
		if size > 0 {
			// This parameter is a struct - allocate local space for it
			offset := 16 + heapRegsSize + spillSize + allocSize + structRetSize + structParamSize
			e.structParamOffset[param.VReg] = offset
			e.structParamSize[param.VReg] = size
			alignedSize := (size + 7) & ^7
			if debugFn {
				fmt.Printf("DEBUG %s: struct param vreg %d at offset %d (size %d, type=%v)\n",
					e.fn.Name, param.VReg, offset, size, param.Type)
			}
			structParamSize += alignedSize
		}
	}

	if debugFn {
		fmt.Printf("DEBUG %s: structParamSize=%d\n", e.fn.Name, structParamSize)
	}

	e.stackSize = 16 + heapRegsSize + spillSize + allocSize + structRetSize + structParamSize

	// Align to 16 bytes
	e.stackSize = (e.stackSize + 15) & ^15

	if debugFn {
		fmt.Printf("DEBUG %s: final stackSize=%d\n", e.fn.Name, e.stackSize)
	}
}

func (e *Emitter) emitPrologue() {
	// STP pre-index has limited range (-512 to 504 bytes)
	// For larger stacks, we need to adjust SP separately
	if e.stackSize <= 504 {
		// stp x29, x30, [sp, #-stackSize]!  (save FP and LR, allocate stack)
		e.asm.STPpre(X29, X30, SP, int16(-e.stackSize))
	} else {
		// For large stacks: first allocate, then save FP/LR
		// SUBi only handles 12-bit immediates (max 4095), so use scratch register for larger sizes
		if e.stackSize <= 4095 {
			e.asm.SUBi(SP, SP, uint16(e.stackSize))
		} else {
			e.asm.MOVimm(X17, int64(e.stackSize))
			e.asm.SUB(SP, SP, X17)
		}
		e.asm.STP(X29, X30, SP, 0)
	}

	// mov x29, sp  (set up frame pointer)
	// Need to use ADD since MOV(x29, SP) doesn't work correctly with SP
	e.asm.ADDi(X29, SP, 0)

	// NOTE: X25/X26 are used as global heap state (heap_ptr and heap_end) and
	// are NOT saved/restored in prologue/epilogue. They persist across all function
	// calls as part of the bump allocator design.

	// For functions returning structs, save X8 (the sret pointer from caller)
	// X8 may be clobbered by nested function calls, so save it at a known location
	if e.fn.Result != nil {
		if structSize := getStructSize(e.fn.Result); structSize > 0 {
			// Save X8 at FP+16 (after saved FP/LR)
			// This is the sret save slot allocated in calculateStackLayout
			// NOTE: X25/X26 (heap state) are NOT saved on stack - they persist globally
			e.asm.STR(X8, X29, 16)
		}
	}
}

func (e *Emitter) emitEpilogue() {
	// Restore SP from FP in case of dynamic stack allocation
	// FP points to where we saved FP/LR, SP might have been modified
	e.asm.ADDi(SP, X29, 0) // Use ADDi since MOV treats r31 as XZR, not SP

	// NOTE: X25/X26 (heap state) are NOT restored - they persist as global state

	// LDP post-index has limited range (-512 to 504 bytes)
	// For larger stacks, we need to adjust SP separately
	if e.stackSize <= 504 {
		// ldp x29, x30, [sp], #stackSize  (restore FP and LR, deallocate stack)
		e.asm.LDPpost(X29, X30, SP, int16(e.stackSize))
	} else {
		// For large stacks: restore FP/LR, then deallocate
		e.asm.LDP(X29, X30, SP, 0)
		// ADDi only handles 12-bit immediates (max 4095), so use scratch register for larger sizes
		if e.stackSize <= 4095 {
			e.asm.ADDi(SP, SP, uint16(e.stackSize))
		} else {
			e.asm.MOVimm(X17, int64(e.stackSize))
			e.asm.ADD(SP, SP, X17)
		}
	}

	// ret
	e.asm.RET()
}

func (e *Emitter) loadParameters() {
	// For main function, save argc (x0) and argv (x1) to callee-saved registers
	// before they get overwritten by parameter loading
	if e.fn.Name == "main" {
		e.asm.MOV(X27, X0) // Save argc
		e.asm.MOV(X28, X1) // Save argv

		// Initialize heap state registers
		// X25 = heap_ptr (current bump pointer, 0 = uninitialized)
		// X26 = heap_end (end of current mmap'd region)
		e.asm.MOVimm(X25, 0)
		e.asm.MOVimm(X26, 0)
	}

	// Load parameters from argument registers into their allocated locations
	for i, param := range e.fn.Params {
		if i >= numArgRegs {
			// Parameter is on stack - not yet supported
			continue
		}

		argReg := Reg(X0 + Reg(i))

		// Check if this is a struct parameter that needs to be copied
		if offset, ok := e.structParamOffset[param.VReg]; ok {
			size := e.structParamSize[param.VReg]
			// argReg contains pointer to struct in caller's stack
			// Copy struct data to our local space
			// X16 = destination (our local stack)
			// argReg (X0-X7) = source (caller's struct pointer)
			e.addImm(X16, X29, offset)

			// Copy struct data 8 bytes at a time
			for off := 0; off < size; off += 8 {
				e.asm.LDR(X17, argReg, uint16(off))
				e.asm.STR(X17, X16, uint16(off))
			}

			// Store our local pointer to the vreg
			e.storeToVReg(param.VReg, X16)
		} else {
			// Non-struct parameter - just store the register value
			e.storeToVReg(param.VReg, argReg)
		}
	}
}

func (e *Emitter) emitBlock(block *ir.Block) {
	// Record label position
	e.labels[block.Label] = e.asm.Offset()

	for _, instr := range block.Instrs {
		e.emitInstr(instr)
	}
}

func (e *Emitter) emitInstr(instr *ir.Instr) {
	switch instr.Op {
	case ir.OpAdd:
		e.emitBinaryOp(instr, e.asm.ADD)
	case ir.OpSub:
		e.emitBinaryOp(instr, e.asm.SUB)
	case ir.OpMul:
		e.emitBinaryOp(instr, e.asm.MUL)
	case ir.OpDiv:
		e.emitBinaryOp(instr, e.asm.SDIV)
	case ir.OpMod:
		e.emitMod(instr)
	case ir.OpNeg:
		e.emitNeg(instr)
	case ir.OpAnd:
		e.emitBinaryOp(instr, e.asm.AND)
	case ir.OpOr:
		e.emitBinaryOp(instr, e.asm.ORR)
	case ir.OpXor:
		e.emitBinaryOp(instr, e.asm.EOR)
	case ir.OpNot:
		e.emitNot(instr)
	case ir.OpEq:
		e.emitCompare(instr, CondEQ)
	case ir.OpNe:
		e.emitCompare(instr, CondNE)
	case ir.OpLt:
		e.emitCompare(instr, CondLT)
	case ir.OpLe:
		e.emitCompare(instr, CondLE)
	case ir.OpGt:
		e.emitCompare(instr, CondGT)
	case ir.OpGe:
		e.emitCompare(instr, CondGE)
	case ir.OpCopy:
		e.emitCopy(instr)
	case ir.OpLoadParam:
		// Parameters are already loaded in loadParameters
	case ir.OpLoadConst:
		e.emitLoadConst(instr)
	case ir.OpCall:
		e.emitCall(instr)
	case ir.OpReturn:
		e.emitReturn(instr)
	case ir.OpJump:
		e.emitJump(instr)
	case ir.OpBranch:
		e.emitBranch(instr)
	case ir.OpAlloc:
		e.emitAlloc(instr)
	case ir.OpLoad:
		e.emitLoad(instr)
	case ir.OpStore:
		e.emitStore(instr)
	case ir.OpMemCopy:
		e.emitMemCopy(instr)
	case ir.OpIndexAddr:
		e.emitIndexAddr(instr)
	case ir.OpArrayPtr:
		e.emitArrayPtr(instr)
	case ir.OpStrEq:
		e.emitStrCompare(instr, true)
	case ir.OpStrNe:
		e.emitStrCompare(instr, false)
	case ir.OpStrLen:
		e.emitStrLen(instr)
	case ir.OpStrConcat:
		e.emitStrConcat(instr)
	case ir.OpStrSlice:
		e.emitStrSlice(instr)
	case ir.OpLoadByte:
		e.emitLoadByte(instr)
	case ir.OpStrContains:
		e.emitStrContains(instr)
	case ir.OpStrStartsWith:
		e.emitStrStartsWith(instr)
	case ir.OpStrEndsWith:
		e.emitStrEndsWith(instr)
	case ir.OpStrIndexOf:
		e.emitStrIndexOf(instr)
	case ir.OpStrSubstring:
		e.emitStrSubstring(instr)
	case ir.OpStrCharAt:
		e.emitStrCharAt(instr)
	case ir.OpStrTrim:
		e.emitStrTrim(instr)
	case ir.OpStrReplace:
		e.emitStrReplace(instr)
	case ir.OpStrSplit:
		e.emitStrSplit(instr)
	case ir.OpArrayLen:
		e.emitArrayLen(instr)
	case ir.OpArrayCap:
		e.emitArrayCap(instr)
	case ir.OpArrayPush:
		e.emitArrayPush(instr)
	case ir.OpMakeArray:
		e.emitMakeArray(instr)
	case ir.OpPrint:
		e.emitPrint(instr)
	case ir.OpReadFile:
		e.emitReadFile(instr)
	case ir.OpWriteFile:
		e.emitWriteFile(instr)
	case ir.OpArgc:
		e.emitArgc(instr)
	case ir.OpArgv:
		e.emitArgv(instr)
	case ir.OpIntToStr:
		e.emitIntToStr(instr)
	case ir.OpStrToInt:
		e.emitStrToInt(instr)
	case ir.OpHeapAlloc:
		e.emitHeapAlloc(instr)
	case ir.OpPoke:
		e.emitPoke(instr)
	case ir.OpPeek:
		e.emitPeek(instr)
	case ir.OpMemSet:
		e.emitMemSet(instr)
	case ir.OpSyscallOpen:
		e.emitSyscallOpen(instr)
	case ir.OpSyscallRead:
		e.emitSyscallRead(instr)
	case ir.OpSyscallWrite:
		e.emitSyscallWrite(instr)
	case ir.OpSyscallClose:
		e.emitSyscallClose(instr)
	case ir.OpMapNew:
		e.emitMapNew(instr)
	case ir.OpMapGet:
		e.emitMapGet(instr)
	case ir.OpMapSet:
		e.emitMapSet(instr)
	case ir.OpMapDelete:
		e.emitMapDelete(instr)
	case ir.OpMapLen:
		e.emitMapLen(instr)
	}
}

func (e *Emitter) emitBinaryOp(instr *ir.Instr, op func(rd, rn, rm Reg)) {
	left := e.loadOperand(instr.Args[0], X16)
	right := e.loadOperand(instr.Args[1], X17)
	// Use X16 as temp dest, then store to proper location
	op(X16, left, right)
	e.storeToVReg(instr.Dest.VReg, X16)
}

func (e *Emitter) emitMod(instr *ir.Instr) {
	// a % b = a - (a / b) * b
	left := e.loadOperand(instr.Args[0], X16)
	right := e.loadOperand(instr.Args[1], X17)

	// Use X18 as temp to avoid overwriting left if it's in X16
	// X18 = left / right
	e.asm.SDIV(X18, left, right)
	// X18 = left - X18 * right
	e.asm.MSUB(X18, X18, right, left)
	e.storeToVReg(instr.Dest.VReg, X18)
}

func (e *Emitter) emitNeg(instr *ir.Instr) {
	src := e.loadOperand(instr.Args[0], X16)
	e.asm.NEG(X16, src)
	e.storeToVReg(instr.Dest.VReg, X16)
}

func (e *Emitter) emitNot(instr *ir.Instr) {
	src := e.loadOperand(instr.Args[0], X16)

	// For boolean NOT, use EOR with 1
	if instr.Args[0].Type != nil && instr.Args[0].Type.Equals(types.Typ[types.Bool]) {
		e.asm.MOVimm(X17, 1)
		e.asm.EOR(X16, src, X17)
	} else {
		// Bitwise NOT
		e.asm.MVN(X16, src)
	}
	e.storeToVReg(instr.Dest.VReg, X16)
}

func (e *Emitter) emitCompare(instr *ir.Instr, cond Cond) {
	left := e.loadOperand(instr.Args[0], X16)
	right := e.loadOperand(instr.Args[1], X17)

	e.asm.CMP(left, right)
	e.asm.CSET(X16, cond)
	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrCompare compares two null-terminated strings byte by byte.
// If eq is true, returns 1 if equal, 0 otherwise.
// If eq is false, returns 1 if not equal, 0 otherwise.
func (e *Emitter) emitStrCompare(instr *ir.Instr, eq bool) {
	// Load string pointers
	str1 := e.loadOperand(instr.Args[0], X16)
	str2 := e.loadOperand(instr.Args[1], X17)

	// If pointers are equal, strings are equal (optimization)
	e.asm.CMP(str1, str2)
	ptrEqOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder, will patch

	// Copy pointers to X9, X10 for iteration
	e.asm.MOV(X9, str1)
	e.asm.MOV(X10, str2)

	// Loop: compare bytes
	loopStart := e.asm.Offset()

	// Load byte from str1 into X11 (LDRB)
	e.asm.LDRB(X11, X9, 0)
	// Load byte from str2 into X12 (LDRB)
	e.asm.LDRB(X12, X10, 0)

	// Compare bytes
	e.asm.CMP(X11, X12)
	neOffset := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // not equal, jump to notEqual

	// Check if we hit null (end of string)
	e.asm.CMP(X11, XZR)
	nullOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // if null, strings are equal

	// Increment pointers
	e.asm.ADDi(X9, X9, 1)
	e.asm.ADDi(X10, X10, 1)

	// Jump back to loop
	loopEnd := e.asm.Offset()
	backOffset := int32(loopStart - loopEnd)
	e.asm.B(backOffset) // B() divides by 4 internally

	// Strings are equal (either same pointer or byte-by-byte equal)
	equalLabel := e.asm.Offset()
	// Patch the pointer equality branch
	e.asm.Patch(ptrEqOffset, e.asm.Bcond_instr(CondEQ, int32(equalLabel-ptrEqOffset)>>2))
	// Patch the null check branch
	e.asm.Patch(nullOffset, e.asm.Bcond_instr(CondEQ, int32(equalLabel-nullOffset)>>2))

	if eq {
		e.asm.MOVimm(X16, 1) // equal
	} else {
		e.asm.MOVimm(X16, 0) // not not-equal = equal
	}
	doneOffset := e.asm.Offset()
	e.asm.B(0) // placeholder, will patch

	// Strings are not equal
	notEqualLabel := e.asm.Offset()
	// Patch the not-equal branch
	e.asm.Patch(neOffset, e.asm.Bcond_instr(CondNE, int32(notEqualLabel-neOffset)>>2))

	if eq {
		e.asm.MOVimm(X16, 0) // not equal
	} else {
		e.asm.MOVimm(X16, 1) // not-equal = true
	}

	// Done
	doneLabel := e.asm.Offset()
	// Patch the done branch
	e.asm.Patch(doneOffset, uint32(0x14000000)|uint32((int32(doneLabel-doneOffset)>>2)&0x3FFFFFF))

	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrLen computes the length of a null-terminated string.
func (e *Emitter) emitStrLen(instr *ir.Instr) {
	str := e.loadOperand(instr.Args[0], X16)

	// Copy pointer to X9 for iteration
	e.asm.MOV(X9, str)
	// Initialize length counter to 0
	e.asm.MOVimm(X10, 0)

	// Loop: find null
	loopStart := e.asm.Offset()

	// Load byte from str into X11 (LDRB)
	e.asm.LDRB(X11, X9, 0)

	// Check if null
	e.asm.CMP(X11, XZR)
	nullOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // if null, done

	// Increment pointer and counter
	e.asm.ADDi(X9, X9, 1)
	e.asm.ADDi(X10, X10, 1)

	// Jump back to loop
	loopEnd := e.asm.Offset()
	backOffset := int32(loopStart - loopEnd)
	e.asm.B(backOffset) // B() divides by 4 internally

	// Done
	doneLabel := e.asm.Offset()
	// Patch the null check branch
	e.asm.Patch(nullOffset, e.asm.Bcond_instr(CondEQ, int32(doneLabel-nullOffset)>>2))

	// Move length to destination
	e.asm.MOV(X16, X10)
	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrConcat concatenates two strings, allocating space on the heap.
// Uses heap allocation so the string persists after function returns.
func (e *Emitter) emitStrConcat(instr *ir.Instr) {
	// Load string pointers
	str1 := e.loadOperand(instr.Args[0], X9)
	e.asm.MOV(X19, str1) // Save str1 in callee-saved register
	str2 := e.loadOperand(instr.Args[1], X10)
	e.asm.MOV(X20, str2) // Save str2 in callee-saved register

	// Calculate length of str1 -> X21
	e.asm.MOV(X9, X19)
	e.asm.MOVimm(X21, 0)
	len1Loop := e.asm.Offset()
	e.asm.LDRB(X11, X9, 0)
	e.asm.CMP(X11, XZR)
	len1Done := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder
	e.asm.ADDi(X9, X9, 1)
	e.asm.ADDi(X21, X21, 1)
	e.asm.B(int32(len1Loop - e.asm.Offset()))
	len1DoneLabel := e.asm.Offset()
	e.asm.Patch(len1Done, e.asm.Bcond_instr(CondEQ, int32(len1DoneLabel-len1Done)>>2))

	// Calculate length of str2 -> X22
	e.asm.MOV(X9, X20)
	e.asm.MOVimm(X22, 0)
	len2Loop := e.asm.Offset()
	e.asm.LDRB(X11, X9, 0)
	e.asm.CMP(X11, XZR)
	len2Done := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder
	e.asm.ADDi(X9, X9, 1)
	e.asm.ADDi(X22, X22, 1)
	e.asm.B(int32(len2Loop - e.asm.Offset()))
	len2DoneLabel := e.asm.Offset()
	e.asm.Patch(len2Done, e.asm.Bcond_instr(CondEQ, int32(len2DoneLabel-len2Done)>>2))

	// Total size = len1 + len2 + 1 (for null terminator)
	e.asm.ADD(X23, X21, X22)
	e.asm.ADDi(X23, X23, 1)

	// Align to 8 bytes: size = (size + 7) & ~7
	e.asm.ADDi(X23, X23, 7)
	e.asm.MOVimm(X11, -8)
	e.asm.AND(X23, X23, X11)

	// Use bump allocator for heap allocation (instead of stack)
	// This ensures the string persists after the function returns
	e.emitBumpAlloc(X23, X24) // Allocate X23 bytes, result in X24

	// Copy str1 bytes to result
	e.asm.MOV(X9, X19)  // src = str1
	e.asm.MOV(X10, X24) // dst = result
	copy1Loop := e.asm.Offset()
	e.asm.LDRB(X11, X9, 0)
	e.asm.STRB(X11, X10, 0)
	e.asm.CMP(X11, XZR)
	copy1Done := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder
	e.asm.ADDi(X9, X9, 1)
	e.asm.ADDi(X10, X10, 1)
	e.asm.B(int32(copy1Loop - e.asm.Offset()))
	copy1DoneLabel := e.asm.Offset()
	e.asm.Patch(copy1Done, e.asm.Bcond_instr(CondEQ, int32(copy1DoneLabel-copy1Done)>>2))

	// Copy str2 bytes to result (X10 is already at the null position)
	e.asm.MOV(X9, X20) // src = str2
	copy2Loop := e.asm.Offset()
	e.asm.LDRB(X11, X9, 0)
	e.asm.STRB(X11, X10, 0)
	e.asm.CMP(X11, XZR)
	copy2Done := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder
	e.asm.ADDi(X9, X9, 1)
	e.asm.ADDi(X10, X10, 1)
	e.asm.B(int32(copy2Loop - e.asm.Offset()))
	copy2DoneLabel := e.asm.Offset()
	e.asm.Patch(copy2Done, e.asm.Bcond_instr(CondEQ, int32(copy2DoneLabel-copy2Done)>>2))

	// Result pointer is in X24
	e.storeToVReg(instr.Dest.VReg, X24)
}

// emitStrSlice extracts a substring: result = str[start:end]
// Uses heap allocation so the string persists after function returns.
func (e *Emitter) emitStrSlice(instr *ir.Instr) {
	// Load arguments: str, start, end
	str := e.loadOperand(instr.Args[0], X19)
	e.asm.MOV(X19, str) // Save str in callee-saved
	start := e.loadOperand(instr.Args[1], X20)
	e.asm.MOV(X20, start) // Save start
	end := e.loadOperand(instr.Args[2], X21)
	e.asm.MOV(X21, end) // Save end

	// Calculate length = end - start
	e.asm.SUB(X22, X21, X20)

	// Allocate size = length + 1, aligned to 8 bytes
	e.asm.ADDi(X23, X22, 1)
	e.asm.ADDi(X23, X23, 7)
	e.asm.MOVimm(X11, -8)
	e.asm.AND(X23, X23, X11)

	// Use bump allocator for heap allocation (instead of stack)
	// This ensures the string persists after the function returns
	e.emitBumpAlloc(X23, X24) // Allocate X23 bytes, result in X24

	// Calculate source address: str + start
	e.asm.ADD(X9, X19, X20)
	// Destination is X24
	e.asm.MOV(X10, X24)

	// Copy loop: copy X22 bytes
	// Note: Use X27 for counter since X25 is used by bump allocator for heap_ptr
	e.asm.MOV(X27, X22) // Counter

	// If length is 0, skip copy
	e.asm.CMP(X27, XZR)
	skipCopy := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	copyLoop := e.asm.Offset()
	e.asm.LDRB(X11, X9, 0)
	e.asm.STRB(X11, X10, 0)
	e.asm.ADDi(X9, X9, 1)
	e.asm.ADDi(X10, X10, 1)
	e.asm.SUBi(X27, X27, 1)
	e.asm.CMP(X27, XZR)
	e.asm.Bcond(CondNE, int32(copyLoop-e.asm.Offset()))

	// Patch skip branch
	skipCopyLabel := e.asm.Offset()
	e.asm.Patch(skipCopy, e.asm.Bcond_instr(CondEQ, int32(skipCopyLabel-skipCopy)>>2))

	// Add null terminator (X10 is already at the right position)
	e.asm.STRB(XZR, X10, 0)

	// Result pointer is in X24
	e.storeToVReg(instr.Dest.VReg, X24)
}

// emitLoadByte loads a single byte from an address (zero-extended to 64-bit).
func (e *Emitter) emitLoadByte(instr *ir.Instr) {
	// OpLoadByte: dest = *(uint8*)addr
	addr := e.loadOperand(instr.Args[0], X16)

	// Load byte from the address (LDRB zero-extends to 64-bit)
	e.asm.LDRB(X17, addr, 0)
	e.storeToVReg(instr.Dest.VReg, X17)
}

// emitStrCharAt returns the byte at index position in a string
func (e *Emitter) emitStrCharAt(instr *ir.Instr) {
	str := e.loadOperand(instr.Args[0], X9)
	index := e.loadOperand(instr.Args[1], X10)

	// Compute address: str + index
	e.asm.ADD(X11, str, index)
	// Load byte at that address
	e.asm.LDRB(X16, X11, 0)
	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrStartsWith checks if string starts with prefix
func (e *Emitter) emitStrStartsWith(instr *ir.Instr) {
	str := e.loadOperand(instr.Args[0], X9)
	prefix := e.loadOperand(instr.Args[1], X10)

	// Save original pointers
	e.asm.MOV(X19, str)
	e.asm.MOV(X20, prefix)

	// Compare loop
	loopStart := e.asm.Offset()

	// Load byte from prefix
	e.asm.LDRB(X11, X20, 0)
	// If prefix byte is null, we've matched all of prefix - success
	e.asm.CMP(X11, XZR)
	successBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder

	// Load byte from str
	e.asm.LDRB(X12, X19, 0)
	// If str byte is null but prefix isn't - fail
	e.asm.CMP(X12, XZR)
	failBranch1 := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder

	// Compare bytes
	e.asm.CMP(X11, X12)
	failBranch2 := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder

	// Bytes match - advance both pointers
	e.asm.ADDi(X19, X19, 1)
	e.asm.ADDi(X20, X20, 1)
	e.asm.B(int32(loopStart - e.asm.Offset()))

	// Success: return 1
	successLabel := e.asm.Offset()
	e.asm.Patch(successBranch, e.asm.Bcond_instr(CondEQ, int32(successLabel-successBranch)>>2))
	e.asm.MOVimm(X16, 1)
	endBranch := e.asm.Offset()
	e.asm.B(0) // placeholder

	// Fail: return 0
	failLabel := e.asm.Offset()
	e.asm.Patch(failBranch1, e.asm.Bcond_instr(CondEQ, int32(failLabel-failBranch1)>>2))
	e.asm.Patch(failBranch2, e.asm.Bcond_instr(CondNE, int32(failLabel-failBranch2)>>2))
	e.asm.MOVimm(X16, 0)

	// End
	endLabel := e.asm.Offset()
	e.asm.Patch(endBranch, e.asm.B_instr(int32(endLabel-endBranch)>>2))

	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrEndsWith checks if string ends with suffix
func (e *Emitter) emitStrEndsWith(instr *ir.Instr) {
	str := e.loadOperand(instr.Args[0], X9)
	suffix := e.loadOperand(instr.Args[1], X10)

	// Calculate length of str -> X21
	e.asm.MOV(X11, str)
	e.asm.MOVimm(X21, 0)
	len1Loop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	len1Done := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X21, X21, 1)
	e.asm.B(int32(len1Loop - e.asm.Offset()))
	len1DoneLabel := e.asm.Offset()
	e.asm.Patch(len1Done, e.asm.Bcond_instr(CondEQ, int32(len1DoneLabel-len1Done)>>2))

	// Calculate length of suffix -> X22
	e.asm.MOV(X11, suffix)
	e.asm.MOVimm(X22, 0)
	len2Loop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	len2Done := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X22, X22, 1)
	e.asm.B(int32(len2Loop - e.asm.Offset()))
	len2DoneLabel := e.asm.Offset()
	e.asm.Patch(len2Done, e.asm.Bcond_instr(CondEQ, int32(len2DoneLabel-len2Done)>>2))

	// If suffix is longer than str, return false
	e.asm.CMP(X22, X21)
	failBranchLen := e.asm.Offset()
	e.asm.Bcond(CondGT, 0)

	// Position str pointer at (str + len(str) - len(suffix))
	e.asm.SUB(X23, X21, X22)
	e.asm.ADD(X19, str, X23) // X19 = str + (strLen - suffixLen)
	e.asm.MOV(X20, suffix)   // X20 = suffix

	// Compare loop
	cmpLoop := e.asm.Offset()
	e.asm.LDRB(X11, X20, 0)
	e.asm.CMP(X11, XZR)
	successBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.LDRB(X12, X19, 0)
	e.asm.CMP(X11, X12)
	failBranchCmp := e.asm.Offset()
	e.asm.Bcond(CondNE, 0)

	e.asm.ADDi(X19, X19, 1)
	e.asm.ADDi(X20, X20, 1)
	e.asm.B(int32(cmpLoop - e.asm.Offset()))

	// Success
	successLabel := e.asm.Offset()
	e.asm.Patch(successBranch, e.asm.Bcond_instr(CondEQ, int32(successLabel-successBranch)>>2))
	e.asm.MOVimm(X16, 1)
	endBranch := e.asm.Offset()
	e.asm.B(0)

	// Fail
	failLabel := e.asm.Offset()
	e.asm.Patch(failBranchLen, e.asm.Bcond_instr(CondGT, int32(failLabel-failBranchLen)>>2))
	e.asm.Patch(failBranchCmp, e.asm.Bcond_instr(CondNE, int32(failLabel-failBranchCmp)>>2))
	e.asm.MOVimm(X16, 0)

	endLabel := e.asm.Offset()
	e.asm.Patch(endBranch, e.asm.B_instr(int32(endLabel-endBranch)>>2))

	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrIndexOf finds the first occurrence of needle in haystack, returns -1 if not found
func (e *Emitter) emitStrIndexOf(instr *ir.Instr) {
	haystack := e.loadOperand(instr.Args[0], X9)
	needle := e.loadOperand(instr.Args[1], X10)

	// Get length of needle -> X22
	e.asm.MOV(X11, needle)
	e.asm.MOVimm(X22, 0)
	needleLenLoop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	needleLenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X22, X22, 1)
	e.asm.B(int32(needleLenLoop - e.asm.Offset()))
	needleLenDoneLabel := e.asm.Offset()
	e.asm.Patch(needleLenDone, e.asm.Bcond_instr(CondEQ, int32(needleLenDoneLabel-needleLenDone)>>2))

	// If needle is empty, return 0
	e.asm.CMP(X22, XZR)
	emptyNeedleBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// X19 = current position in haystack (index)
	e.asm.MOVimm(X19, 0)
	// X20 = haystack pointer
	e.asm.MOV(X20, haystack)

	// Outer loop: check each position in haystack
	outerLoop := e.asm.Offset()

	// Check if haystack[pos] is null - end of string, not found
	e.asm.LDRB(X11, X20, 0)
	e.asm.CMP(X11, XZR)
	notFoundBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// Inner comparison: compare needle at current position
	e.asm.MOV(X23, X20) // X23 = current haystack position
	e.asm.MOV(X24, needle)
	e.asm.MOVimm(X13, 0) // X13 = needle index (avoid X25 - heap_ptr)

	innerLoop := e.asm.Offset()
	// If we've matched all of needle, we found it
	e.asm.CMP(X13, X22)
	foundBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// Load and compare bytes
	e.asm.LDRB(X11, X23, 0)
	e.asm.LDRB(X12, X24, 0)
	e.asm.CMP(X11, X12)
	mismatchBranch := e.asm.Offset()
	e.asm.Bcond(CondNE, 0)

	// Bytes match, continue
	e.asm.ADDi(X23, X23, 1)
	e.asm.ADDi(X24, X24, 1)
	e.asm.ADDi(X13, X13, 1) // X13 = needle index (avoid X25 - heap_ptr)
	e.asm.B(int32(innerLoop - e.asm.Offset()))

	// Mismatch - try next position
	mismatchLabel := e.asm.Offset()
	e.asm.Patch(mismatchBranch, e.asm.Bcond_instr(CondNE, int32(mismatchLabel-mismatchBranch)>>2))
	e.asm.ADDi(X19, X19, 1)
	e.asm.ADDi(X20, X20, 1)
	e.asm.B(int32(outerLoop - e.asm.Offset()))

	// Found: return index
	foundLabel := e.asm.Offset()
	e.asm.Patch(foundBranch, e.asm.Bcond_instr(CondEQ, int32(foundLabel-foundBranch)>>2))
	e.asm.MOV(X16, X19)
	endBranch1 := e.asm.Offset()
	e.asm.B(0)

	// Empty needle: return 0
	emptyLabel := e.asm.Offset()
	e.asm.Patch(emptyNeedleBranch, e.asm.Bcond_instr(CondEQ, int32(emptyLabel-emptyNeedleBranch)>>2))
	e.asm.MOVimm(X16, 0)
	endBranch2 := e.asm.Offset()
	e.asm.B(0)

	// Not found: return -1
	notFoundLabel := e.asm.Offset()
	e.asm.Patch(notFoundBranch, e.asm.Bcond_instr(CondEQ, int32(notFoundLabel-notFoundBranch)>>2))
	e.asm.MOVimm(X16, -1)

	// End
	endLabel := e.asm.Offset()
	e.asm.Patch(endBranch1, e.asm.B_instr(int32(endLabel-endBranch1)>>2))
	e.asm.Patch(endBranch2, e.asm.B_instr(int32(endLabel-endBranch2)>>2))

	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrContains checks if haystack contains needle
func (e *Emitter) emitStrContains(instr *ir.Instr) {
	// Reuse str_index_of logic: contains = (index_of >= 0)
	haystack := e.loadOperand(instr.Args[0], X9)
	needle := e.loadOperand(instr.Args[1], X10)

	// Get length of needle -> X22
	e.asm.MOV(X11, needle)
	e.asm.MOVimm(X22, 0)
	needleLenLoop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	needleLenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X22, X22, 1)
	e.asm.B(int32(needleLenLoop - e.asm.Offset()))
	needleLenDoneLabel := e.asm.Offset()
	e.asm.Patch(needleLenDone, e.asm.Bcond_instr(CondEQ, int32(needleLenDoneLabel-needleLenDone)>>2))

	// Empty needle always found
	e.asm.CMP(X22, XZR)
	emptyNeedleBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// X20 = haystack pointer
	e.asm.MOV(X20, haystack)

	// Outer loop
	outerLoop := e.asm.Offset()
	e.asm.LDRB(X11, X20, 0)
	e.asm.CMP(X11, XZR)
	notFoundBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// Inner comparison
	e.asm.MOV(X23, X20)
	e.asm.MOV(X24, needle)
	e.asm.MOVimm(X13, 0) // X13 = needle index (avoid X25 - heap_ptr)

	innerLoop := e.asm.Offset()
	e.asm.CMP(X13, X22)
	foundBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.LDRB(X11, X23, 0)
	e.asm.LDRB(X12, X24, 0)
	e.asm.CMP(X11, X12)
	mismatchBranch := e.asm.Offset()
	e.asm.Bcond(CondNE, 0)

	e.asm.ADDi(X23, X23, 1)
	e.asm.ADDi(X24, X24, 1)
	e.asm.ADDi(X13, X13, 1) // X13 = needle index (avoid X25 - heap_ptr)
	e.asm.B(int32(innerLoop - e.asm.Offset()))

	mismatchLabel := e.asm.Offset()
	e.asm.Patch(mismatchBranch, e.asm.Bcond_instr(CondNE, int32(mismatchLabel-mismatchBranch)>>2))
	e.asm.ADDi(X20, X20, 1)
	e.asm.B(int32(outerLoop - e.asm.Offset()))

	// Found or empty needle: return true (1)
	foundLabel := e.asm.Offset()
	e.asm.Patch(foundBranch, e.asm.Bcond_instr(CondEQ, int32(foundLabel-foundBranch)>>2))
	e.asm.Patch(emptyNeedleBranch, e.asm.Bcond_instr(CondEQ, int32(foundLabel-emptyNeedleBranch)>>2))
	e.asm.MOVimm(X16, 1)
	endBranch := e.asm.Offset()
	e.asm.B(0)

	// Not found: return false (0)
	notFoundLabel := e.asm.Offset()
	e.asm.Patch(notFoundBranch, e.asm.Bcond_instr(CondEQ, int32(notFoundLabel-notFoundBranch)>>2))
	e.asm.MOVimm(X16, 0)

	endLabel := e.asm.Offset()
	e.asm.Patch(endBranch, e.asm.B_instr(int32(endLabel-endBranch)>>2))

	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrSubstring extracts a substring from start to end indices
// Allocates memory for the result string
func (e *Emitter) emitStrSubstring(instr *ir.Instr) {
	str := e.loadOperand(instr.Args[0], X9)
	start := e.loadOperand(instr.Args[1], X10)
	end := e.loadOperand(instr.Args[2], X11)

	// Save inputs in callee-saved registers
	e.asm.MOV(X19, str)
	e.asm.MOV(X20, start)
	e.asm.MOV(X21, end)

	// Calculate length = end - start
	e.asm.SUB(X22, X21, X20)

	// Allocate size = length + 1 (for null terminator), aligned to 16
	e.asm.ADDi(X23, X22, 1)
	e.asm.ADDi(X23, X23, 15)
	// Align to 16: mask with ~15 = -16
	e.asm.MOVimm(X0, -16)
	e.asm.AND(X23, X23, X0)

	// Allocate using mmap
	e.asm.MOV(X0, XZR)           // addr = NULL
	e.asm.MOV(X1, X23)           // len = size
	e.asm.MOVimm(X2, 3)          // prot = PROT_READ | PROT_WRITE
	e.asm.MOVimm(X3, 0x1002)     // flags = MAP_PRIVATE | MAP_ANON
	e.asm.MOVimm(X4, -1)         // fd = -1
	e.asm.MOV(X5, XZR)           // offset = 0
	e.asm.MOVimm(X16, 0x20000C5) // mmap syscall
	e.asm.SVC(0x80)

	// X0 = allocated buffer pointer
	e.asm.MOV(X24, X0) // Save destination pointer

	// Copy substring: from str+start to buffer
	e.asm.ADD(X13, X19, X20) // X13 = source pointer (str + start) - avoid X25 (heap_ptr)
	e.asm.MOV(X14, X24)      // X14 = dest pointer - avoid X26 (heap_end)
	e.asm.MOVimm(X27, 0)     // X27 = bytes copied

	// Copy loop
	copyLoop := e.asm.Offset()
	e.asm.CMP(X27, X22)
	copyDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.LDRB(X28, X13, 0)
	e.asm.STRB(X28, X14, 0)
	e.asm.ADDi(X13, X13, 1)
	e.asm.ADDi(X14, X14, 1)
	e.asm.ADDi(X27, X27, 1)
	e.asm.B(int32(copyLoop - e.asm.Offset()))

	copyDoneLabel := e.asm.Offset()
	e.asm.Patch(copyDone, e.asm.Bcond_instr(CondEQ, int32(copyDoneLabel-copyDone)>>2))

	// Null terminate
	e.asm.STRB(XZR, X14, 0)

	// Return buffer pointer
	e.asm.MOV(X16, X24)
	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrTrim trims specified characters from both ends of a string
func (e *Emitter) emitStrTrim(instr *ir.Instr) {
	str := e.loadOperand(instr.Args[0], X9)
	chars := e.loadOperand(instr.Args[1], X10)

	// Save inputs
	e.asm.MOV(X19, str)
	e.asm.MOV(X20, chars)

	// Get length of string -> X21
	e.asm.MOV(X11, X19)
	e.asm.MOVimm(X21, 0)
	strLenLoop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	strLenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X21, X21, 1)
	e.asm.B(int32(strLenLoop - e.asm.Offset()))
	strLenDoneLabel := e.asm.Offset()
	e.asm.Patch(strLenDone, e.asm.Bcond_instr(CondEQ, int32(strLenDoneLabel-strLenDone)>>2))

	// If empty string, jump to empty handler
	e.asm.CMP(X21, XZR)
	emptyBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// Find start index -> X22
	e.asm.MOVimm(X22, 0)
	findStartLoop := e.asm.Offset()
	e.asm.CMP(X22, X21)
	allTrimmedBranch1 := e.asm.Offset()
	e.asm.Bcond(CondGE, 0)

	// Load char at start
	e.asm.ADD(X11, X19, X22)
	e.asm.LDRB(X23, X11, 0)

	// Check if char is in trim set
	e.asm.MOV(X24, X20)
	charCheckLoop := e.asm.Offset()
	e.asm.LDRB(X13, X24, 0) // X13 = trim char (avoid X25 - heap_ptr)
	e.asm.CMP(X13, XZR)
	startFound := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.CMP(X23, X13)
	charMatches := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.ADDi(X24, X24, 1)
	e.asm.B(int32(charCheckLoop - e.asm.Offset()))

	// Char matches, advance start
	charMatchesLabel := e.asm.Offset()
	e.asm.Patch(charMatches, e.asm.Bcond_instr(CondEQ, int32(charMatchesLabel-charMatches)>>2))
	e.asm.ADDi(X22, X22, 1)
	e.asm.B(int32(findStartLoop - e.asm.Offset()))

	// Start found, now find end
	startFoundLabel := e.asm.Offset()
	e.asm.Patch(startFound, e.asm.Bcond_instr(CondEQ, int32(startFoundLabel-startFound)>>2))

	// Find end index -> X14 (exclusive) - avoid X26 (heap_end)
	e.asm.MOV(X14, X21)
	findEndLoop := e.asm.Offset()
	e.asm.CMP(X14, X22)
	allTrimmedBranch2 := e.asm.Offset()
	e.asm.Bcond(CondLE, 0)

	// Load char at end-1
	e.asm.SUBi(X11, X14, 1) // X14 = end index (avoid X26)
	e.asm.ADD(X11, X19, X11)
	e.asm.LDRB(X23, X11, 0)

	// Check if char is in trim set
	e.asm.MOV(X24, X20)
	charCheckLoop2 := e.asm.Offset()
	e.asm.LDRB(X13, X24, 0) // X13 = trim char (avoid X25)
	e.asm.CMP(X13, XZR)
	endFound := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.CMP(X23, X13)
	charMatches2 := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.ADDi(X24, X24, 1)
	e.asm.B(int32(charCheckLoop2 - e.asm.Offset()))

	// Char matches, decrement end
	charMatchesLabel2 := e.asm.Offset()
	e.asm.Patch(charMatches2, e.asm.Bcond_instr(CondEQ, int32(charMatchesLabel2-charMatches2)>>2))
	e.asm.SUBi(X14, X14, 1) // X14 = end index (avoid X26)
	e.asm.B(int32(findEndLoop - e.asm.Offset()))

	// End found, extract substring
	endFoundLabel := e.asm.Offset()
	e.asm.Patch(endFound, e.asm.Bcond_instr(CondEQ, int32(endFoundLabel-endFound)>>2))

	// Calculate length = end - start -> X27
	e.asm.SUB(X27, X14, X22) // X14 = end index (avoid X26)

	// Save X22 (start) in X28 temporarily (X27 should be preserved)
	e.asm.MOV(X28, X22)

	// Allocate size = length + 1 (for null), aligned to 16
	e.asm.ADDi(X23, X27, 1)
	e.asm.ADDi(X23, X23, 15)
	e.asm.MOVimm(X0, -16)
	e.asm.AND(X23, X23, X0)

	// mmap syscall
	e.asm.MOV(X0, XZR)           // addr = NULL
	e.asm.MOV(X1, X23)           // len = size
	e.asm.MOVimm(X2, 3)          // prot = PROT_READ | PROT_WRITE
	e.asm.MOVimm(X3, 0x1002)     // flags = MAP_PRIVATE | MAP_ANON
	e.asm.MOVimm(X4, -1)         // fd = -1
	e.asm.MOV(X5, XZR)           // offset = 0
	e.asm.MOVimm(X16, 0x20000C5) // mmap syscall
	e.asm.SVC(0x80)

	// X0 = allocated buffer
	// Save buffer ptr and recalculate values (may have been clobbered by syscall)
	e.asm.MOV(X23, X0) // X23 = dest buffer

	// Recalculate start and length from X28 (saved start) and by re-scanning
	// Actually, X27 (length) and X28 (start) should still be valid if they're callee-saved
	// But to be safe, let's use them directly: X28 = start index, X27 = length
	// Source = X19 + X28, copy X27 bytes

	e.asm.ADD(X24, X19, X28) // X24 = source ptr (original string + start)
	e.asm.MOV(X13, X23)      // X13 = dest ptr (avoid X25 - heap_ptr)

	// Copy loop
	trimCopyLoop := e.asm.Offset()
	e.asm.CMP(X27, XZR)
	trimCopyDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.LDRB(X11, X24, 0)
	e.asm.STRB(X11, X13, 0)
	e.asm.ADDi(X24, X24, 1)
	e.asm.ADDi(X13, X13, 1)
	e.asm.SUBi(X27, X27, 1)
	e.asm.B(int32(trimCopyLoop - e.asm.Offset()))

	trimCopyDoneLabel := e.asm.Offset()
	e.asm.Patch(trimCopyDone, e.asm.Bcond_instr(CondEQ, int32(trimCopyDoneLabel-trimCopyDone)>>2))

	// Null terminate
	e.asm.STRB(XZR, X13, 0)
	e.asm.MOV(X16, X23) // return buffer ptr
	endBranch := e.asm.Offset()
	e.asm.B(0)

	// Empty/all trimmed handler
	emptyLabel := e.asm.Offset()
	e.asm.Patch(emptyBranch, e.asm.Bcond_instr(CondEQ, int32(emptyLabel-emptyBranch)>>2))
	e.asm.Patch(allTrimmedBranch1, e.asm.Bcond_instr(CondGE, int32(emptyLabel-allTrimmedBranch1)>>2))
	e.asm.Patch(allTrimmedBranch2, e.asm.Bcond_instr(CondLE, int32(emptyLabel-allTrimmedBranch2)>>2))

	// Allocate empty string
	e.asm.MOV(X0, XZR)
	e.asm.MOVimm(X1, 16)
	e.asm.MOVimm(X2, 3)
	e.asm.MOVimm(X3, 0x1002)
	e.asm.MOVimm(X4, -1)
	e.asm.MOV(X5, XZR)
	e.asm.MOVimm(X16, 0x20000C5)
	e.asm.SVC(0x80)
	e.asm.STRB(XZR, X0, 0)
	e.asm.MOV(X16, X0)

	endLabel := e.asm.Offset()
	e.asm.Patch(endBranch, e.asm.B_instr(int32(endLabel-endBranch)>>2))

	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrReplace replaces all occurrences of old with new in a string
func (e *Emitter) emitStrReplace(instr *ir.Instr) {
	str := e.loadOperand(instr.Args[0], X9)
	oldStr := e.loadOperand(instr.Args[1], X10)
	newStr := e.loadOperand(instr.Args[2], X11)

	// Save inputs in callee-saved registers
	e.asm.MOV(X19, str)    // X19 = original string
	e.asm.MOV(X20, oldStr) // X20 = old string
	e.asm.MOV(X21, newStr) // X21 = new string

	// Get length of original string -> X22
	e.asm.MOV(X11, str)
	e.asm.MOVimm(X22, 0)
	strLenLoop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	strLenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X22, X22, 1)
	e.asm.B(int32(strLenLoop - e.asm.Offset()))
	strLenDoneLabel := e.asm.Offset()
	e.asm.Patch(strLenDone, e.asm.Bcond_instr(CondEQ, int32(strLenDoneLabel-strLenDone)>>2))

	// Get length of old string -> X23
	e.asm.MOV(X11, oldStr)
	e.asm.MOVimm(X23, 0)
	oldLenLoop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	oldLenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X23, X23, 1)
	e.asm.B(int32(oldLenLoop - e.asm.Offset()))
	oldLenDoneLabel := e.asm.Offset()
	e.asm.Patch(oldLenDone, e.asm.Bcond_instr(CondEQ, int32(oldLenDoneLabel-oldLenDone)>>2))

	// Get length of new string -> X24
	e.asm.MOV(X11, newStr)
	e.asm.MOVimm(X24, 0)
	newLenLoop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	newLenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X24, X24, 1)
	e.asm.B(int32(newLenLoop - e.asm.Offset()))
	newLenDoneLabel := e.asm.Offset()
	e.asm.Patch(newLenDone, e.asm.Bcond_instr(CondEQ, int32(newLenDoneLabel-newLenDone)>>2))

	// If old string is empty, just return copy of original
	e.asm.CMP(X23, XZR)
	oldEmptyBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// Count occurrences of old in str -> X13
	e.asm.MOVimm(X13, 0) // count - avoid X25 (heap_ptr)
	e.asm.MOV(X14, X19)  // current position - avoid X26 (heap_end)
	countLoop := e.asm.Offset()
	e.asm.LDRB(X11, X14, 0)
	e.asm.CMP(X11, XZR)
	countDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// Check if old starts at current position
	e.asm.MOV(X27, X14) // compare ptr in str - avoid X26 (heap_end)
	e.asm.MOV(X28, X20) // compare ptr in old
	e.asm.MOVimm(X0, 0) // match count

	cmpLoop := e.asm.Offset()
	e.asm.CMP(X0, X23)
	matchFound := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // matched all of old

	e.asm.LDRB(X11, X27, 0)
	e.asm.LDRB(X12, X28, 0)
	e.asm.CMP(X11, X12)
	noMatch := e.asm.Offset()
	e.asm.Bcond(CondNE, 0)

	e.asm.ADDi(X27, X27, 1)
	e.asm.ADDi(X28, X28, 1)
	e.asm.ADDi(X0, X0, 1)
	e.asm.B(int32(cmpLoop - e.asm.Offset()))

	// Match found, increment count and skip old length
	matchFoundLabel := e.asm.Offset()
	e.asm.Patch(matchFound, e.asm.Bcond_instr(CondEQ, int32(matchFoundLabel-matchFound)>>2))
	e.asm.ADDi(X13, X13, 1)
	e.asm.ADD(X14, X14, X23)
	e.asm.B(int32(countLoop - e.asm.Offset()))

	// No match, advance one char
	noMatchLabel := e.asm.Offset()
	e.asm.Patch(noMatch, e.asm.Bcond_instr(CondNE, int32(noMatchLabel-noMatch)>>2))
	e.asm.ADDi(X14, X14, 1)
	e.asm.B(int32(countLoop - e.asm.Offset()))

	countDoneLabel := e.asm.Offset()
	e.asm.Patch(countDone, e.asm.Bcond_instr(CondEQ, int32(countDoneLabel-countDone)>>2))

	// Calculate result size: origLen - count*oldLen + count*newLen + 1
	// X14 = count * oldLen
	e.asm.MUL(X14, X13, X23)
	// X27 = count * newLen
	e.asm.MUL(X27, X13, X24)
	// X28 = origLen - count*oldLen
	e.asm.SUB(X28, X22, X14)
	// X0 = resultLen = X28 + count*newLen + 1
	e.asm.ADD(X0, X28, X27)
	e.asm.ADDi(X0, X0, 1)

	// Align to 16
	e.asm.ADDi(X0, X0, 15)
	e.asm.MOVimm(X11, -16)
	e.asm.AND(X1, X0, X11) // X1 = aligned size (len parameter for mmap)

	// mmap syscall
	e.asm.MOV(X0, XZR)           // addr = NULL
	e.asm.MOVimm(X2, 3)          // prot = PROT_READ | PROT_WRITE
	e.asm.MOVimm(X3, 0x1002)     // flags = MAP_PRIVATE | MAP_ANON
	e.asm.MOVimm(X4, -1)         // fd = -1
	e.asm.MOV(X5, XZR)           // offset = 0
	e.asm.MOVimm(X16, 0x20000C5) // mmap syscall
	e.asm.SVC(0x80)

	// X0 = result buffer, save it
	e.asm.MOV(X28, X0) // save result ptr

	// Recalculate oldLen -> X23 (since X23 may have been clobbered by syscall)
	e.asm.MOV(X11, X20)
	e.asm.MOVimm(X23, 0)
	recalcOldLenLoop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	recalcOldLenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X23, X23, 1)
	e.asm.B(int32(recalcOldLenLoop - e.asm.Offset()))
	recalcOldLenDoneLabel := e.asm.Offset()
	e.asm.Patch(recalcOldLenDone, e.asm.Bcond_instr(CondEQ, int32(recalcOldLenDoneLabel-recalcOldLenDone)>>2))

	// Recalculate newLen -> X24
	e.asm.MOV(X11, X21)
	e.asm.MOVimm(X24, 0)
	recalcNewLenLoop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	recalcNewLenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X24, X24, 1)
	e.asm.B(int32(recalcNewLenLoop - e.asm.Offset()))
	recalcNewLenDoneLabel := e.asm.Offset()
	e.asm.Patch(recalcNewLenDone, e.asm.Bcond_instr(CondEQ, int32(recalcNewLenDoneLabel-recalcNewLenDone)>>2))

	// Now copy with replacements
	e.asm.MOV(X14, X19) // source ptr - avoid X26 (heap_end)
	e.asm.MOV(X27, X28) // dest ptr (from saved result)

	replaceLoop := e.asm.Offset()
	e.asm.LDRB(X11, X14, 0)
	e.asm.CMP(X11, XZR)
	replaceDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// Check if old starts at current position
	e.asm.MOV(X0, X14) // compare ptr in str
	e.asm.MOV(X1, X20) // compare ptr in old
	e.asm.MOVimm(X2, 0) // match count

	cmpLoop2 := e.asm.Offset()
	e.asm.CMP(X2, X23)
	matchFound2 := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // matched all of old

	e.asm.LDRB(X11, X0, 0)
	e.asm.LDRB(X12, X1, 0)
	e.asm.CMP(X11, X12)
	noMatch2 := e.asm.Offset()
	e.asm.Bcond(CondNE, 0)

	e.asm.ADDi(X0, X0, 1)
	e.asm.ADDi(X1, X1, 1)
	e.asm.ADDi(X2, X2, 1)
	e.asm.B(int32(cmpLoop2 - e.asm.Offset()))

	// Match found, copy new string
	matchFoundLabel2 := e.asm.Offset()
	e.asm.Patch(matchFound2, e.asm.Bcond_instr(CondEQ, int32(matchFoundLabel2-matchFound2)>>2))
	e.asm.MOV(X0, X21) // new string ptr
	e.asm.MOVimm(X1, 0) // counter

	copyNewLoop := e.asm.Offset()
	e.asm.CMP(X1, X24)
	copyNewDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.LDRB(X11, X0, 0)
	e.asm.STRB(X11, X27, 0)
	e.asm.ADDi(X0, X0, 1)
	e.asm.ADDi(X27, X27, 1)
	e.asm.ADDi(X1, X1, 1)
	e.asm.B(int32(copyNewLoop - e.asm.Offset()))

	copyNewDoneLabel := e.asm.Offset()
	e.asm.Patch(copyNewDone, e.asm.Bcond_instr(CondEQ, int32(copyNewDoneLabel-copyNewDone)>>2))

	// Skip old string in source
	e.asm.ADD(X14, X14, X23)
	e.asm.B(int32(replaceLoop - e.asm.Offset()))

	// No match, copy single char
	noMatchLabel2 := e.asm.Offset()
	e.asm.Patch(noMatch2, e.asm.Bcond_instr(CondNE, int32(noMatchLabel2-noMatch2)>>2))
	e.asm.LDRB(X11, X14, 0)
	e.asm.STRB(X11, X27, 0)
	e.asm.ADDi(X14, X14, 1)
	e.asm.ADDi(X27, X27, 1)
	e.asm.B(int32(replaceLoop - e.asm.Offset()))

	replaceDoneLabel := e.asm.Offset()
	e.asm.Patch(replaceDone, e.asm.Bcond_instr(CondEQ, int32(replaceDoneLabel-replaceDone)>>2))

	// Null terminate
	e.asm.STRB(XZR, X27, 0)

	// Return result
	e.asm.MOV(X16, X28)
	endBranch := e.asm.Offset()
	e.asm.B(0)

	// Old string empty: just copy original
	oldEmptyLabel := e.asm.Offset()
	e.asm.Patch(oldEmptyBranch, e.asm.Bcond_instr(CondEQ, int32(oldEmptyLabel-oldEmptyBranch)>>2))

	// Allocate for copy
	e.asm.ADDi(X1, X22, 1)    // X1 = origLen + 1 (for null)
	e.asm.ADDi(X1, X1, 15)
	e.asm.MOVimm(X11, -16)
	e.asm.AND(X1, X1, X11)    // X1 = aligned size

	e.asm.MOV(X0, XZR)        // addr = NULL
	e.asm.MOVimm(X2, 3)       // prot
	e.asm.MOVimm(X3, 0x1002)  // flags
	e.asm.MOVimm(X4, -1)      // fd = -1
	e.asm.MOV(X5, XZR)        // offset = 0
	e.asm.MOVimm(X16, 0x20000C5)
	e.asm.SVC(0x80)

	e.asm.MOV(X14, X19) // source - avoid X26 (heap_end)
	e.asm.MOV(X27, X0)  // dest
	e.asm.MOV(X28, X0)  // save result

	copyOrigLoop := e.asm.Offset()
	e.asm.LDRB(X11, X14, 0)
	e.asm.STRB(X11, X27, 0)
	e.asm.CMP(X11, XZR)
	copyOrigDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X14, X14, 1)
	e.asm.ADDi(X27, X27, 1)
	e.asm.B(int32(copyOrigLoop - e.asm.Offset()))

	copyOrigDoneLabel := e.asm.Offset()
	e.asm.Patch(copyOrigDone, e.asm.Bcond_instr(CondEQ, int32(copyOrigDoneLabel-copyOrigDone)>>2))

	e.asm.MOV(X16, X28)

	endLabel := e.asm.Offset()
	e.asm.Patch(endBranch, e.asm.B_instr(int32(endLabel-endBranch)>>2))

	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitStrSplit splits a string by separator and returns an array of strings
func (e *Emitter) emitStrSplit(instr *ir.Instr) {
	str := e.loadOperand(instr.Args[0], X9)
	sep := e.loadOperand(instr.Args[1], X10)

	// Save inputs in callee-saved registers
	e.asm.MOV(X19, str) // X19 = string
	e.asm.MOV(X20, sep) // X20 = separator

	// Get length of string -> X21
	e.asm.MOV(X11, str)
	e.asm.MOVimm(X21, 0)
	strLenLoop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	strLenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X21, X21, 1)
	e.asm.B(int32(strLenLoop - e.asm.Offset()))
	strLenDoneLabel := e.asm.Offset()
	e.asm.Patch(strLenDone, e.asm.Bcond_instr(CondEQ, int32(strLenDoneLabel-strLenDone)>>2))

	// Get length of separator -> X22
	e.asm.MOV(X11, sep)
	e.asm.MOVimm(X22, 0)
	sepLenLoop := e.asm.Offset()
	e.asm.LDRB(X12, X11, 0)
	e.asm.CMP(X12, XZR)
	sepLenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X11, X11, 1)
	e.asm.ADDi(X22, X22, 1)
	e.asm.B(int32(sepLenLoop - e.asm.Offset()))
	sepLenDoneLabel := e.asm.Offset()
	e.asm.Patch(sepLenDone, e.asm.Bcond_instr(CondEQ, int32(sepLenDoneLabel-sepLenDone)>>2))

	// Count parts (= occurrences + 1) -> X23
	e.asm.MOVimm(X23, 1) // at least one part
	e.asm.MOV(X24, X19)  // current position

	// If separator is empty, return array with just the original string
	e.asm.CMP(X22, XZR)
	sepEmptyBranch := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	countPartsLoop := e.asm.Offset()
	e.asm.LDRB(X11, X24, 0)
	e.asm.CMP(X11, XZR)
	countPartsDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// Check if separator starts at current position
	e.asm.MOV(X13, X24) // compare ptr in str - avoid X25 (heap_ptr)
	e.asm.MOV(X14, X20) // compare ptr in sep - avoid X26 (heap_end)
	e.asm.MOVimm(X27, 0) // match count

	cmpSepLoop := e.asm.Offset()
	e.asm.CMP(X27, X22)
	sepMatchFound := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // matched all of sep

	e.asm.LDRB(X11, X13, 0)
	e.asm.LDRB(X12, X14, 0)
	e.asm.CMP(X11, X12)
	sepNoMatch := e.asm.Offset()
	e.asm.Bcond(CondNE, 0)

	e.asm.ADDi(X13, X13, 1)
	e.asm.ADDi(X14, X14, 1)
	e.asm.ADDi(X27, X27, 1)
	e.asm.B(int32(cmpSepLoop - e.asm.Offset()))

	// Sep found, increment count and skip sep length
	sepMatchFoundLabel := e.asm.Offset()
	e.asm.Patch(sepMatchFound, e.asm.Bcond_instr(CondEQ, int32(sepMatchFoundLabel-sepMatchFound)>>2))
	e.asm.ADDi(X23, X23, 1)
	e.asm.ADD(X24, X24, X22)
	e.asm.B(int32(countPartsLoop - e.asm.Offset()))

	// Sep not found, advance one char
	sepNoMatchLabel := e.asm.Offset()
	e.asm.Patch(sepNoMatch, e.asm.Bcond_instr(CondNE, int32(sepNoMatchLabel-sepNoMatch)>>2))
	e.asm.ADDi(X24, X24, 1)
	e.asm.B(int32(countPartsLoop - e.asm.Offset()))

	countPartsDoneLabel := e.asm.Offset()
	e.asm.Patch(countPartsDone, e.asm.Bcond_instr(CondEQ, int32(countPartsDoneLabel-countPartsDone)>>2))

	// Allocate fat pointer (24 bytes) for array
	e.asm.MOV(X0, XZR)       // addr = NULL
	e.asm.MOVimm(X1, 24)     // len = 24
	e.asm.MOVimm(X2, 3)      // PROT_READ | PROT_WRITE
	e.asm.MOVimm(X3, 0x1002) // MAP_PRIVATE | MAP_ANON
	e.asm.MOVimm(X4, -1)     // fd = -1
	e.asm.MOV(X5, XZR)       // offset = 0
	e.asm.MOVimm(X16, 0x20000C5)
	e.asm.SVC(0x80)
	e.asm.MOV(X28, X0) // X28 = fat pointer

	// Allocate array of string pointers (8 bytes each)
	e.asm.LSL(X1, X23, 3) // X1 = count * 8 (len)
	e.asm.MOV(X0, XZR)       // addr = NULL
	e.asm.MOVimm(X2, 3)
	e.asm.MOVimm(X3, 0x1002)
	e.asm.MOVimm(X4, -1)
	e.asm.MOV(X5, XZR)       // offset = 0
	e.asm.MOVimm(X16, 0x20000C5)
	e.asm.SVC(0x80)
	e.asm.MOV(X27, X0) // X27 = data pointer

	// Store in fat pointer: [ptr, len, cap]
	e.asm.STR(X27, X28, 0)     // ptr
	e.asm.STR(X23, X28, 8)     // len
	e.asm.STR(X23, X28, 16)    // cap

	// Now extract each part
	e.asm.MOV(X24, X19)  // current position in string
	e.asm.MOV(X13, X27)  // current position in array - avoid X25 (heap_ptr)
	e.asm.MOV(X14, X24)  // start of current part - avoid X26 (heap_end)

	splitLoop := e.asm.Offset()
	e.asm.LDRB(X11, X24, 0)
	e.asm.CMP(X11, XZR)
	splitDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	// Check if separator starts at current position
	e.asm.MOV(X0, X24) // compare ptr in str
	e.asm.MOV(X1, X20) // compare ptr in sep
	e.asm.MOVimm(X2, 0) // match count

	cmpSepLoop2 := e.asm.Offset()
	e.asm.CMP(X2, X22)
	sepMatchFound2 := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // matched all of sep

	e.asm.LDRB(X11, X0, 0)
	e.asm.LDRB(X12, X1, 0)
	e.asm.CMP(X11, X12)
	sepNoMatch2 := e.asm.Offset()
	e.asm.Bcond(CondNE, 0)

	e.asm.ADDi(X0, X0, 1)
	e.asm.ADDi(X1, X1, 1)
	e.asm.ADDi(X2, X2, 1)
	e.asm.B(int32(cmpSepLoop2 - e.asm.Offset()))

	// Sep found - extract part from X14 to X24
	sepMatchFoundLabel2 := e.asm.Offset()
	e.asm.Patch(sepMatchFound2, e.asm.Bcond_instr(CondEQ, int32(sepMatchFoundLabel2-sepMatchFound2)>>2))

	// Calculate part length
	e.asm.SUB(X3, X24, X14) // part length

	// Allocate memory for part
	e.asm.ADDi(X1, X3, 1) // +1 for null -> X1
	e.asm.ADDi(X1, X1, 15)
	e.asm.MOVimm(X11, -16)
	e.asm.AND(X1, X1, X11) // X1 = aligned size (len for mmap)

	// mmap call
	e.asm.MOV(X0, XZR)       // addr = NULL
	e.asm.MOVimm(X2, 3)      // prot
	e.asm.MOVimm(X3, 0x1002) // flags
	e.asm.MOVimm(X4, -1)     // fd = -1
	e.asm.MOV(X5, XZR)       // offset = 0
	e.asm.MOVimm(X16, 0x20000C5)
	e.asm.SVC(0x80)

	// X0 = part buffer
	// Copy part
	e.asm.MOV(X1, X14) // source
	e.asm.MOV(X2, X0)  // dest
	e.asm.MOV(X4, X0)  // save part ptr

	// Recalculate part length
	e.asm.SUB(X3, X24, X14)

	copyPartLoop := e.asm.Offset()
	e.asm.CMP(X3, XZR)
	copyPartDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.LDRB(X11, X1, 0)
	e.asm.STRB(X11, X2, 0)
	e.asm.ADDi(X1, X1, 1)
	e.asm.ADDi(X2, X2, 1)
	e.asm.SUBi(X3, X3, 1)
	e.asm.B(int32(copyPartLoop - e.asm.Offset()))

	copyPartDoneLabel := e.asm.Offset()
	e.asm.Patch(copyPartDone, e.asm.Bcond_instr(CondEQ, int32(copyPartDoneLabel-copyPartDone)>>2))

	// Null terminate part
	e.asm.STRB(XZR, X2, 0)

	// Store part pointer in array
	e.asm.STR(X4, X13, 0)
	e.asm.ADDi(X13, X13, 8) // advance array pointer

	// Skip separator and continue
	e.asm.ADD(X24, X24, X22)
	e.asm.MOV(X14, X24) // new start
	e.asm.B(int32(splitLoop - e.asm.Offset()))

	// No sep match, advance one char
	sepNoMatchLabel2 := e.asm.Offset()
	e.asm.Patch(sepNoMatch2, e.asm.Bcond_instr(CondNE, int32(sepNoMatchLabel2-sepNoMatch2)>>2))
	e.asm.ADDi(X24, X24, 1)
	e.asm.B(int32(splitLoop - e.asm.Offset()))

	// Done - add final part
	splitDoneLabel := e.asm.Offset()
	e.asm.Patch(splitDone, e.asm.Bcond_instr(CondEQ, int32(splitDoneLabel-splitDone)>>2))

	// Calculate final part length
	e.asm.SUB(X3, X24, X14)

	// Allocate memory for final part
	e.asm.ADDi(X1, X3, 1)    // X1 = len + 1
	e.asm.ADDi(X1, X1, 15)
	e.asm.MOVimm(X11, -16)
	e.asm.AND(X1, X1, X11)   // X1 = aligned size (len for mmap)

	e.asm.MOV(X0, XZR)       // addr = NULL
	e.asm.MOVimm(X2, 3)      // prot
	e.asm.MOVimm(X3, 0x1002) // flags
	e.asm.MOVimm(X4, -1)     // fd = -1
	e.asm.MOV(X5, XZR)       // offset = 0
	e.asm.MOVimm(X16, 0x20000C5)
	e.asm.SVC(0x80)

	// Copy final part
	e.asm.MOV(X1, X14)
	e.asm.MOV(X2, X0)
	e.asm.MOV(X4, X0)
	e.asm.SUB(X3, X24, X14)

	copyFinalLoop := e.asm.Offset()
	e.asm.CMP(X3, XZR)
	copyFinalDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	e.asm.LDRB(X11, X1, 0)
	e.asm.STRB(X11, X2, 0)
	e.asm.ADDi(X1, X1, 1)
	e.asm.ADDi(X2, X2, 1)
	e.asm.SUBi(X3, X3, 1)
	e.asm.B(int32(copyFinalLoop - e.asm.Offset()))

	copyFinalDoneLabel := e.asm.Offset()
	e.asm.Patch(copyFinalDone, e.asm.Bcond_instr(CondEQ, int32(copyFinalDoneLabel-copyFinalDone)>>2))

	e.asm.STRB(XZR, X2, 0) // null terminate

	// Store final part pointer
	e.asm.STR(X4, X13, 0)

	// Return fat pointer
	e.asm.MOV(X16, X28)
	endBranch := e.asm.Offset()
	e.asm.B(0)

	// Empty separator - return array with just the original string
	sepEmptyLabel := e.asm.Offset()
	e.asm.Patch(sepEmptyBranch, e.asm.Bcond_instr(CondEQ, int32(sepEmptyLabel-sepEmptyBranch)>>2))

	// Allocate fat pointer
	e.asm.MOV(X0, XZR)       // addr = NULL
	e.asm.MOVimm(X1, 24)     // len = 24
	e.asm.MOVimm(X2, 3)      // prot
	e.asm.MOVimm(X3, 0x1002) // flags
	e.asm.MOVimm(X4, -1)     // fd = -1
	e.asm.MOV(X5, XZR)       // offset = 0
	e.asm.MOVimm(X16, 0x20000C5)
	e.asm.SVC(0x80)
	e.asm.MOV(X28, X0)

	// Allocate array for 1 string
	e.asm.MOV(X0, XZR)       // addr = NULL
	e.asm.MOVimm(X1, 8)      // len = 8
	e.asm.MOVimm(X2, 3)      // prot
	e.asm.MOVimm(X3, 0x1002) // flags
	e.asm.MOVimm(X4, -1)     // fd = -1
	e.asm.MOV(X5, XZR)       // offset = 0
	e.asm.MOVimm(X16, 0x20000C5)
	e.asm.SVC(0x80)
	e.asm.MOV(X27, X0)

	// Copy original string - allocate buffer
	e.asm.ADDi(X1, X21, 1)   // X1 = strlen + 1
	e.asm.ADDi(X1, X1, 15)
	e.asm.MOVimm(X11, -16)
	e.asm.AND(X1, X1, X11)   // X1 = aligned size (len for mmap)

	e.asm.MOV(X0, XZR)       // addr = NULL
	e.asm.MOVimm(X2, 3)      // prot
	e.asm.MOVimm(X3, 0x1002) // flags
	e.asm.MOVimm(X4, -1)     // fd = -1
	e.asm.MOV(X5, XZR)       // offset = 0
	e.asm.MOVimm(X16, 0x20000C5)
	e.asm.SVC(0x80)

	e.asm.MOV(X24, X19) // source
	e.asm.MOV(X13, X0)  // dest - avoid X25 (heap_ptr)
	e.asm.MOV(X14, X0)  // save - avoid X26 (heap_end)

	copyOrigLoop2 := e.asm.Offset()
	e.asm.LDRB(X11, X24, 0)
	e.asm.STRB(X11, X13, 0)
	e.asm.CMP(X11, XZR)
	copyOrigDone2 := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)
	e.asm.ADDi(X24, X24, 1)
	e.asm.ADDi(X13, X13, 1)
	e.asm.B(int32(copyOrigLoop2 - e.asm.Offset()))

	copyOrigDoneLabel2 := e.asm.Offset()
	e.asm.Patch(copyOrigDone2, e.asm.Bcond_instr(CondEQ, int32(copyOrigDoneLabel2-copyOrigDone2)>>2))

	// Store in array
	e.asm.STR(X14, X27, 0)

	// Store in fat pointer
	e.asm.STR(X27, X28, 0)
	e.asm.MOVimm(X11, 1)
	e.asm.STR(X11, X28, 8)
	e.asm.STR(X11, X28, 16)

	e.asm.MOV(X16, X28)

	endLabel := e.asm.Offset()
	e.asm.Patch(endBranch, e.asm.B_instr(int32(endLabel-endBranch)>>2))

	e.storeToVReg(instr.Dest.VReg, X16)
}

func (e *Emitter) emitCopy(instr *ir.Instr) {
	src := e.loadOperand(instr.Args[0], X16)
	e.storeToVReg(instr.Dest.VReg, src)
}

// storeToVReg stores a value from a register to a virtual register
// If the VReg is spilled, it stores to the stack
func (e *Emitter) storeToVReg(vreg int, src Reg) {
	if reg, ok := e.vregToReg[vreg]; ok {
		// VReg is in a register
		if reg != src {
			e.asm.MOV(reg, src)
		}
	} else if offset, ok := e.vregSpill[vreg]; ok {
		// VReg is spilled - store to stack
		e.asm.STR(src, X29, uint16(16+offset))
	}
}

func (e *Emitter) emitLoadConst(instr *ir.Instr) {
	e.asm.MOVimm(X16, instr.Args[0].Imm)
	e.storeToVReg(instr.Dest.VReg, X16)
}

func (e *Emitter) emitCall(instr *ir.Instr) {
	// Args[0] is the function reference, Args[1:] are arguments
	fnRef := instr.Args[0]

	// For struct returns, set X8 to point to caller's result buffer
	if instr.Dest.Kind == ir.OpndVReg {
		if offset, ok := e.structRetOffset[instr.Dest.VReg]; ok {
			// X8 = FP + offset (our local buffer for the result)
			e.addImm(X8, X29, offset)
		}
	}

	// Move arguments to x0-x7
	for i := 1; i < len(instr.Args) && i <= numArgRegs; i++ {
		arg := instr.Args[i]
		argReg := Reg(X0 + Reg(i-1))
		src := e.loadOperand(arg, X16)
		if src != argReg {
			e.asm.MOV(argReg, src)
		}
	}

	// Call the function
	if fnRef.Kind == ir.OpndFunc {
		// Direct call - add a fixup for the function address
		e.fixups = append(e.fixups, fixup{
			offset: e.asm.Offset(),
			label:  fnRef.Func,
			kind:   fixupBL,
		})
		e.asm.BL(0) // placeholder
	} else {
		// Indirect call through register
		callReg := e.loadOperand(fnRef, X16)
		e.asm.BLR(callReg)
	}

	// Move result from x0 to destination
	if instr.Dest.Kind == ir.OpndVReg {
		// Check if this is a struct return
		if offset, ok := e.structRetOffset[instr.Dest.VReg]; ok {
			// Struct return: callee already wrote to our buffer via X8
			// Just compute and store the pointer to our local buffer
			e.addImm(X16, X29, offset)
			e.storeToVReg(instr.Dest.VReg, X16)
		} else {
			// Not a struct return, just store X0
			e.storeToVReg(instr.Dest.VReg, X0)
		}
	}
}

func (e *Emitter) emitReturn(instr *ir.Instr) {
	debugFn := debugEmit

	// Check if returning a struct (sret semantics)
	if e.fn.Result != nil {
		if structSize := getStructSize(e.fn.Result); structSize > 0 {
			if debugFn {
				fmt.Printf("DEBUG %s: emitReturn struct size=%d, type=%v\n", e.fn.Name, structSize, e.fn.Result)
				if len(instr.Args) > 0 {
					fmt.Printf("DEBUG %s: emitReturn arg[0]=%v\n", e.fn.Name, instr.Args[0])
				}
			}
			// Load saved X8 from FP+16 (saved in prologue)
			// NOTE: X25/X26 (heap state) are NOT saved on stack - they persist globally
			e.asm.LDR(X8, X29, 16)

			// Returning a struct - copy it to [X8] (caller's buffer)
			// src contains pointer to the struct on our local stack
			if len(instr.Args) > 0 {
				src := e.loadOperand(instr.Args[0], X16)
				// Copy struct from [src] to [X8]
				for i := 0; i < structSize; i += 8 {
					e.asm.LDR(X17, src, uint16(i))
					e.asm.STR(X17, X8, uint16(i))
				}
			}
			// Return X8 in X0 (per ABI)
			e.asm.MOV(X0, X8)
			e.emitEpilogue()
			return
		}
	}

	// Non-struct return - move value to x0
	if len(instr.Args) > 0 {
		src := e.loadOperand(instr.Args[0], X16)
		if src != X0 {
			e.asm.MOV(X0, src)
		}
	}

	e.emitEpilogue()
}

func (e *Emitter) emitJump(instr *ir.Instr) {
	label := instr.Args[0].Label
	e.fixups = append(e.fixups, fixup{
		offset: e.asm.Offset(),
		label:  label,
		kind:   fixupB,
	})
	e.asm.B(0) // placeholder
}

func (e *Emitter) emitBranch(instr *ir.Instr) {
	// Args: [cond, trueLabel, falseLabel]
	cond := e.loadOperand(instr.Args[0], X16)
	trueLabel := instr.Args[1].Label
	falseLabel := instr.Args[2].Label

	// Compare condition to zero
	e.asm.CMPi(cond, 0)

	// Branch if not zero (condition is true)
	e.fixups = append(e.fixups, fixup{
		offset: e.asm.Offset(),
		label:  trueLabel,
		kind:   fixupBcond,
	})
	e.asm.Bcond(CondNE, 0) // placeholder

	// Fall through or branch to false
	e.fixups = append(e.fixups, fixup{
		offset: e.asm.Offset(),
		label:  falseLabel,
		kind:   fixupB,
	})
	e.asm.B(0) // placeholder
}

func (e *Emitter) loadOperand(op ir.Operand, scratch Reg) Reg {
	switch op.Kind {
	case ir.OpndVReg:
		if reg, ok := e.vregToReg[op.VReg]; ok {
			return reg
		}
		// Spilled - load from stack
		offset := e.vregSpill[op.VReg]
		e.asm.LDR(scratch, X29, uint16(16+offset))
		return scratch
	case ir.OpndImm:
		e.asm.MOVimm(scratch, op.Imm)
		return scratch
	case ir.OpndStr:
		// String constant - use ADRP+ADD (standard ARM64 approach)
		// ADRP loads page address, ADD adds offset within page
		adrpOffset := e.asm.Offset()
		e.asm.ADRP(scratch, 0)          // placeholder - will be fixed up
		e.asm.ADDi(scratch, scratch, 0) // placeholder - will be fixed up
		e.strFixups = append(e.strFixups, strFixup{
			offset: adrpOffset, // Store ADRP offset for fixup
			strIdx: op.StrIdx,
		})
		return scratch
	case ir.OpndGlobal:
		// Load global variable address using ADRP+ADD (for full 64-bit address)
		// This will be fixed up later with the actual global address
		// Returns the ADDRESS of the global, not its value
		// (OpLoad/OpStore will do the actual memory access)
		adrpOff := e.asm.Offset()
		e.asm.ADRP(scratch, 0) // placeholder for page address
		addOff := e.asm.Offset()
		e.asm.ADDi(scratch, scratch, 0) // placeholder for page offset
		e.globalFixups = append(e.globalFixups, globalFixup{
			adrpOffset: adrpOff,
			addOffset:  addOff,
			name:       op.Global,
		})
		return scratch
	default:
		return scratch
	}
}

func (e *Emitter) getReg(vreg int) Reg {
	if reg, ok := e.vregToReg[vreg]; ok {
		return reg
	}
	// Spilled register - return X16 as scratch, caller must handle storing back
	// This is used for operations where we need a register but the VReg is spilled
	return X16
}

func (e *Emitter) fixupBranches() {
	// Fix up intra-function branches (block labels)
	for _, fix := range e.fixups {
		target, ok := e.labels[fix.label]
		if !ok {
			// Cross-function call - will be fixed up later
			continue
		}

		offset := int32(target - fix.offset)

		switch fix.kind {
		case fixupB:
			imm26 := (offset >> 2) & 0x3FFFFFF
			instr := uint32(0x14000000) | uint32(imm26)
			e.asm.Patch(fix.offset, instr)
		case fixupBcond:
			imm19 := (offset >> 2) & 0x7FFFF
			// Read existing instruction to get condition
			oldInstr := uint32(e.asm.code[fix.offset]) |
				uint32(e.asm.code[fix.offset+1])<<8 |
				uint32(e.asm.code[fix.offset+2])<<16 |
				uint32(e.asm.code[fix.offset+3])<<24
			cond := oldInstr & 0xF
			instr := uint32(0x54000000) | uint32(imm19)<<5 | cond
			e.asm.Patch(fix.offset, instr)
		}
	}
	// Clear fixups that were resolved (intra-function)
	var remaining []fixup
	for _, fix := range e.fixups {
		if _, ok := e.labels[fix.label]; !ok {
			remaining = append(remaining, fix)
		}
	}
	e.fixups = remaining
}

func (e *Emitter) fixupCalls() {
	// Fix up cross-function calls
	for _, fix := range e.fixups {
		if fix.kind != fixupBL {
			continue
		}

		target, ok := e.funcOffsets[fix.label]
		if !ok {
			// External function - skip for now
			continue
		}

		offset := int32(target - fix.offset)
		imm26 := (offset >> 2) & 0x3FFFFFF
		instr := uint32(0x94000000) | uint32(imm26)
		e.asm.Patch(fix.offset, instr)
	}
}

// GetFunctionOffset returns the code offset for a function by name.
func (e *Emitter) GetFunctionOffset(name string) (int, bool) {
	offset, ok := e.funcOffsets[name]
	return offset, ok
}

func (e *Emitter) emitAlloc(instr *ir.Instr) {
	// OpAlloc allocates stack space and returns a pointer to it
	// The allocation offset was pre-computed in calculateStackLayout

	offset, ok := e.allocOffset[instr.Dest.VReg]
	if !ok {
		// Fallback - shouldn't happen
		return
	}

	// Compute the address: FP - offset (stack grows down, FP points to saved FP/LR)
	// But our stack layout has FP at the top, so we use FP - stackSize + offset
	// Actually, with STPpre, FP points to saved registers at [sp]
	// Allocations are at positive offsets from the original SP (now FP)
	// So the address is SP + offset, but SP = FP - stackSize after prologue
	// Let's use: addr = FP - (stackSize - offset)

	// Actually simpler: after prologue, SP points to bottom of stack frame
	// FP = SP, so allocation at offset N from SP is at FP + N - stackSize + stackSize = FP + N - 16
	// Wait, let me think again...
	//
	// Stack layout (high to low addresses):
	// [FP+0]      = saved FP
	// [FP+8]      = saved LR
	// [FP-8]      = first spill
	// [FP-16]     = second spill
	// ...
	// [FP-spillSize] = first allocation
	//
	// But with STPpre, we do: STP FP, LR, [SP, #-stackSize]!
	// After that: SP = old_SP - stackSize, FP = SP
	// So FP points to saved FP at the bottom
	//
	// Our offset is from SP (which equals FP after prologue)
	// Allocation at offset N means address = SP + N = FP + N
	// But FP doesn't change, and we want FP-relative addressing

	// Let's use ADDi: addr = FP + offset (since offset includes the 16 byte header)
	// No wait, FP = SP after prologue, and our offset is from original SP
	// After STPpre with -stackSize: new_SP = old_SP - stackSize
	// FP = new_SP = old_SP - stackSize
	// Allocation at offset N from new_SP is at FP + N

	// But our stack grows down, so higher offsets are higher addresses
	// Our layout: [FP+0]=FP, [FP+8]=LR, [FP+16...]=spills, [FP+16+spillSize...]=allocs

	// So if allocOffset[vreg] = 16 + spillSize + X, we just do FP + offset
	// But wait, STPpre decrements SP first, so after STP:
	// [SP+0] = FP, [SP+8] = LR
	// And we set FP = SP with ADDi X29, SP, 0
	// So [FP+0] = saved FP, [FP+8] = saved LR
	// Spills start at FP+16

	// So the address is simply: FP + offset
	e.addImm(X16, X29, offset)
	e.storeToVReg(instr.Dest.VReg, X16)
}

func (e *Emitter) emitLoad(instr *ir.Instr) {
	// OpLoad: dest = *addr
	// Args[0] is the address to load from
	addr := e.loadOperand(instr.Args[0], X16)

	// Load from the address
	e.asm.LDRx(X17, addr)
	e.storeToVReg(instr.Dest.VReg, X17)
}

func (e *Emitter) emitStore(instr *ir.Instr) {
	// OpStore: *addr = value
	// Args[0] is the value, Args[1] is the address
	value := e.loadOperand(instr.Args[0], X16)
	addr := e.loadOperand(instr.Args[1], X17)

	// Store the value to the address
	e.asm.STRx(value, addr)
}

func (e *Emitter) emitMemCopy(instr *ir.Instr) {
	// OpMemCopy: copy size bytes from src to dst
	// Args[0] is src address, Args[1] is dst address, Args[2] is size (in bytes)
	src := e.loadOperand(instr.Args[0], X16)
	dst := e.loadOperand(instr.Args[1], X17)
	size := instr.Args[2].Imm // size is an immediate

	// Copy 8 bytes at a time
	for i := int64(0); i < size; i += 8 {
		e.asm.LDR(X18, src, uint16(i))
		e.asm.STR(X18, dst, uint16(i))
	}
}

func (e *Emitter) emitIndexAddr(instr *ir.Instr) {
	// OpIndexAddr: dest = base + offset
	// Args[0] is base pointer, Args[1] is byte offset
	base := e.loadOperand(instr.Args[0], X16)
	offset := e.loadOperand(instr.Args[1], X17)

	// Compute address: dest = base + offset
	e.asm.ADD(X16, base, offset)
	e.storeToVReg(instr.Dest.VReg, X16)
}

func (e *Emitter) emitArrayPtr(instr *ir.Instr) {
	// OpArrayPtr: extract pointer from fat pointer (at offset 0)
	// Args[0] is the fat pointer
	fatPtr := e.loadOperand(instr.Args[0], X16)

	// Load pointer from fat pointer (offset 0)
	e.asm.LDRx(X17, fatPtr)
	e.storeToVReg(instr.Dest.VReg, X17)
}

func (e *Emitter) emitArrayLen(instr *ir.Instr) {
	// OpArrayLen: extract length from fat pointer (at offset 8)
	// Args[0] is the fat pointer
	fatPtr := e.loadOperand(instr.Args[0], X16)

	// Load length from fat pointer (offset 8)
	e.asm.LDR(X17, fatPtr, 8)
	e.storeToVReg(instr.Dest.VReg, X17)
}

func (e *Emitter) emitArrayCap(instr *ir.Instr) {
	// OpArrayCap: extract capacity from fat pointer (at offset 16)
	// Args[0] is the fat pointer
	fatPtr := e.loadOperand(instr.Args[0], X16)

	// Load capacity from fat pointer (offset 16)
	e.asm.LDR(X17, fatPtr, 16)
	e.storeToVReg(instr.Dest.VReg, X17)
}

func (e *Emitter) emitArrayPush(instr *ir.Instr) {
	// OpArrayPush: push element to array with capacity checking and growth
	// Args[0] = array fat pointer, Args[1] = element (or pointer to struct), Args[2] = element size
	// Fat pointer layout: [ptr (8 bytes), len (8 bytes), cap (8 bytes)]

	// Get element size from Args[2] (it's an immediate)
	elemSizeVal := int(instr.Args[2].Imm)

	// Load operands and preserve them
	fatPtr := e.loadOperand(instr.Args[0], X19)
	e.asm.MOV(X19, fatPtr) // X19 = fat pointer address
	elem := e.loadOperand(instr.Args[1], X20)
	e.asm.MOV(X20, elem) // X20 = element (value or ptr to struct)

	// Load len and cap from fat pointer
	e.asm.LDR(X21, X19, 8)  // X21 = len
	e.asm.LDR(X22, X19, 16) // X22 = cap

	// Check if we need to grow: len >= cap
	e.asm.CMP(X21, X22)
	noGrowNeeded := e.asm.Offset()
	e.asm.Bcond(CondLT, 0) // placeholder: skip growth if len < cap

	// === GROWTH SECTION ===
	// Calculate new capacity: new_cap = (cap == 0) ? 8 : cap * 2
	e.asm.CMP(X22, XZR)
	capNonZero := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder: jump if cap != 0

	// cap == 0: new_cap = 8
	e.asm.MOVimm(X23, 8)
	capSet := e.asm.Offset()
	e.asm.B(0) // placeholder: skip the doubling

	// cap != 0: new_cap = cap * 2
	e.asm.Patch(capNonZero, e.asm.Bcond_instr(CondNE, int32(e.asm.Offset()-capNonZero)/4))
	e.asm.LSL(X23, X22, 1) // X23 = new_cap = cap * 2

	// Patch the jump from cap==0 case
	e.asm.Patch(capSet, e.asm.B_instr(int32(e.asm.Offset()-capSet)/4))

	// X23 = new_cap
	// Allocate new buffer: new_cap * elemSize bytes
	e.asm.MOVimm(X24, int64(elemSizeVal))
	e.asm.MUL(X0, X23, X24) // X0 = new_cap * elemSize = allocation size

	// Save registers before heap allocation
	// IMPORTANT: X20 (element) is caller-saved and WILL be clobbered by mmap!
	e.asm.STPpre(X20, XZR, SP, -16) // save element (16 bytes, pad with zero)
	e.asm.STPpre(X19, X21, SP, -16) // save fat_ptr and len (16 bytes)
	e.asm.STPpre(X23, XZR, SP, -16) // save new_cap (16 bytes, pad with zero)

	// Call heap allocator inline (similar to emitHeapAlloc but simplified)
	// X0 already has size, just need to do the allocation
	e.asm.MOV(X1, X0) // X1 = size for mmap

	// Align size to 8 bytes
	e.asm.ADDi(X1, X1, 7)
	e.asm.LSR(X1, X1, 3)
	e.asm.LSL(X1, X1, 3)

	// Use mmap for allocation (simpler than bump allocator for now)
	e.asm.MOV(X0, XZR)       // addr = NULL
	e.asm.MOVimm(X2, 3)      // prot = PROT_READ | PROT_WRITE
	e.asm.MOVimm(X3, 0x1002) // flags = MAP_ANON | MAP_PRIVATE
	e.asm.MOVimm(X4, -1)     // fd = -1
	e.asm.MOV(X5, XZR)       // offset = 0
	e.asm.MOVimm(X16, 0x20000C5)
	e.asm.SVC(0x80)
	// X0 = new buffer pointer

	e.asm.MOV(X24, X0) // X24 = new buffer pointer

	// Restore saved registers (in reverse order from saves)
	e.asm.LDPpost(X23, X0, SP, 16)  // restore new_cap (16 bytes, discard padding)
	e.asm.LDPpost(X19, X21, SP, 16) // restore fat_ptr and len (16 bytes)
	e.asm.LDPpost(X20, X0, SP, 16)  // restore element (16 bytes, discard padding)

	// Copy old data to new buffer if len > 0
	e.asm.CMP(X21, XZR)
	skipCopy := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder: skip copy if len == 0

	// Copy len * elemSize bytes from old to new
	e.asm.LDRx(X14, X19) // X14 = old ptr - avoid X26 (heap_end)
	e.asm.MOVimm(X27, int64(elemSizeVal))
	e.asm.MUL(X28, X21, X27) // X28 = len * elemSize = bytes to copy

	// Simple byte-by-byte copy loop (copy in 8-byte chunks)
	e.asm.MOV(X0, XZR) // X0 = offset = 0
	copyLoop := e.asm.Offset()
	e.asm.CMP(X0, X28)
	copyDone := e.asm.Offset()
	e.asm.Bcond(CondGE, 0) // placeholder: exit if offset >= bytes

	// Load 8 bytes from old[offset]
	e.asm.ADD(X1, X14, X0) // X1 = old + offset
	e.asm.LDRx(X2, X1)     // X2 = old[offset]
	// Store to new[offset]
	e.asm.ADD(X3, X24, X0) // X3 = new + offset
	e.asm.STRx(X2, X3)     // new[offset] = old[offset]
	e.asm.ADDi(X0, X0, 8)  // offset += 8
	e.asm.B(int32(copyLoop - e.asm.Offset()))

	// Patch copy done branch
	e.asm.Patch(copyDone, e.asm.Bcond_instr(CondGE, int32(e.asm.Offset()-copyDone)/4))

	// Patch skip copy branch
	e.asm.Patch(skipCopy, e.asm.Bcond_instr(CondEQ, int32(e.asm.Offset()-skipCopy)/4))

	// Update fat pointer with new ptr and cap
	e.asm.STRx(X24, X19)     // store new ptr at offset 0
	e.asm.STR(X23, X19, 16)  // store new_cap at offset 16

	// X24 now has the data pointer for the push
	e.asm.MOV(X14, X24) // X14 = data ptr (new buffer) - avoid X26 (heap_end)
	noGrowDone := e.asm.Offset()
	e.asm.B(0) // placeholder: skip to push section

	// === NO GROWTH NEEDED ===
	e.asm.Patch(noGrowNeeded, e.asm.Bcond_instr(CondLT, int32(e.asm.Offset()-noGrowNeeded)/4))
	e.asm.LDRx(X14, X19) // X14 = existing data ptr - avoid X26 (heap_end)

	// Patch the jump from growth section
	e.asm.Patch(noGrowDone, e.asm.B_instr(int32(e.asm.Offset()-noGrowDone)/4))

	// === PUSH SECTION ===
	// X19 = fat ptr, X20 = elem, X21 = len, X14 = data ptr
	// Compute element address: data_ptr + len * elemSize
	e.asm.MOVimm(X27, int64(elemSizeVal))
	e.asm.MUL(X28, X21, X27) // X28 = len * elemSize
	e.asm.ADD(X28, X14, X28) // X28 = addr = data_ptr + offset

	// Copy element data based on element size
	if elemSizeVal > 8 {
		// Struct: elem (X20) is a pointer to struct data, copy all fields
		for offset := 0; offset < elemSizeVal; offset += 8 {
			e.asm.LDR(X0, X20, uint16(offset))  // load from source
			e.asm.STR(X0, X28, uint16(offset))  // store to dest
		}
	} else {
		// Primitive: elem (X20) is the value, store directly
		e.asm.STRx(X20, X28)
	}

	// Increment length and store it back
	e.asm.ADDi(X21, X21, 1)
	e.asm.STR(X21, X19, 8) // store new len at offset 8
}

func (e *Emitter) emitMakeArray(instr *ir.Instr) {
	// OpMakeArray: create fat pointer from ptr, len, cap
	// Args[0] is ptr, Args[1] is len, Args[2] is cap (optional, defaults to len)
	// Dest is where to store the fat pointer
	ptr := e.loadOperand(instr.Args[0], X16)
	length := e.loadOperand(instr.Args[1], X17)

	var capacity Reg
	if len(instr.Args) > 2 {
		capacity = e.loadOperand(instr.Args[2], X27)
	} else {
		capacity = X17 // cap = len
	}

	// Get the destination address (should be a stack allocation)
	destAddr := e.loadOperand(instr.Dest, X18)
	e.asm.MOV(X18, destAddr) // preserve in X18

	// Store ptr at offset 0
	e.asm.STRx(ptr, X18)

	// Store len at offset 8
	e.asm.ADDi(X19, X18, 8)
	e.asm.STRx(length, X19)

	// Store cap at offset 16
	e.asm.ADDi(X19, X18, 16)
	e.asm.STRx(capacity, X19)
}

// emitPrint prints a string to stdout using write syscall
func (e *Emitter) emitPrint(instr *ir.Instr) {
	// Load string pointer
	str := e.loadOperand(instr.Args[0], X19)
	e.asm.MOV(X19, str)

	// First, compute string length by scanning for null terminator
	// Save string pointer
	e.asm.MOV(X20, X19) // X20 = original string pointer

	// Count loop
	lenLoop := e.asm.Offset()
	e.asm.LDRB(X21, X19, 0)        // load byte
	e.asm.CMPi(X21, 0)             // check for null
	e.asm.ADDi(X19, X19, 1)        // advance pointer
	e.asm.Bcond(CondNE, int32(lenLoop-e.asm.Offset()))

	// X19 now points past the null, so length = X19 - X20 - 1
	e.asm.SUB(X2, X19, X20)        // X2 = count including null
	e.asm.SUBi(X2, X2, 1)          // X2 = count excluding null

	// write(1, str, len)
	e.asm.MOVimm(X0, 1)            // fd = stdout
	e.asm.MOV(X1, X20)             // buf = string pointer
	// X2 already has length
	e.asm.MOVimm(X16, 0x2000004)   // syscall write
	e.asm.SVC(0x80)
}

// emitReadFile reads a file and returns its contents as a string.
// Uses heap allocation so the string persists after function returns.
func (e *Emitter) emitReadFile(instr *ir.Instr) {
	// Load path string
	path := e.loadOperand(instr.Args[0], X19)
	e.asm.MOV(X19, path) // Save path

	// open(path, O_RDONLY, 0)
	e.asm.MOV(X0, X19)             // path
	e.asm.MOVimm(X1, 0)            // O_RDONLY
	e.asm.MOVimm(X2, 0)            // mode (ignored for read)
	e.asm.MOVimm(X16, 0x2000005)   // syscall open
	e.asm.SVC(0x80)
	e.asm.MOV(X20, X0)             // Save fd in X20

	// Allocate buffer on heap (1MB, 8-byte aligned)
	// Uses bump allocator so the string persists after function returns
	// Use X23 for size (not X17) because emitBumpAlloc corrupts X17 internally
	bufSize := int64(1048576) // 1MB - enough for large source files
	e.asm.MOVimm(X23, bufSize)
	e.emitBumpAlloc(X23, X21)      // Allocate X23 bytes, result in X21

	// Read file in loop until EOF or buffer full
	// X21 = buffer start
	// X22 = total bytes read so far
	// X24 = buffer size - 1 (leave room for null)
	e.asm.MOVimm(X22, 0)            // Total bytes read = 0
	e.asm.MOVimm(X24, bufSize-1)    // Max bytes to read

	// Read loop
	readLoop := e.asm.Offset()

	// Check if buffer is full
	e.asm.CMP(X22, X24)
	readDone := e.asm.Offset()
	e.asm.Bcond(CondGE, 0)          // If bytes_read >= max, done (placeholder)

	// Calculate: remaining = bufSize - 1 - total_read
	e.asm.SUB(X2, X24, X22)         // X2 = remaining space in buffer

	// read(fd, buf + offset, remaining)
	e.asm.MOV(X0, X20)              // fd
	e.asm.ADD(X1, X21, X22)         // buf + offset
	// X2 already set to remaining
	e.asm.MOVimm(X16, 0x2000003)    // syscall read
	e.asm.SVC(0x80)

	// Check result (X0 = bytes read, or -1 on error)
	e.asm.CMP(X0, XZR)
	e.asm.Bcond(CondLE, int32(e.asm.Offset()-readDone)) // If <= 0, done (reuse readDone)

	// Add bytes read to total
	e.asm.ADD(X22, X22, X0)

	// Loop back to read more
	e.asm.B(int32(readLoop - e.asm.Offset()))

	// Patch readDone branch target
	readDoneLabel := e.asm.Offset()
	e.asm.Patch(readDone, e.asm.Bcond_instr(CondGE, int32(readDoneLabel-readDone)>>2))

	// Null-terminate the string
	e.asm.ADD(X23, X21, X22)       // end of data
	e.asm.STRB(XZR, X23, 0)        // write null byte

	// close(fd)
	e.asm.MOV(X0, X20)             // fd
	e.asm.MOVimm(X16, 0x2000006)   // syscall close
	e.asm.SVC(0x80)

	// Result pointer is in X21 (heap allocated, persists after return)
	e.storeToVReg(instr.Dest.VReg, X21)
}

func (e *Emitter) emitWriteFile(instr *ir.Instr) {
	// Load arguments
	path := e.loadOperand(instr.Args[0], X19)
	content := e.loadOperand(instr.Args[1], X20)
	e.asm.MOV(X19, path)    // Save path
	e.asm.MOV(X20, content) // Save content

	// Get string length (same pattern as emitStrLen)
	e.asm.MOV(X21, X20)     // X21 = scanning pointer
	e.asm.MOVimm(X22, 0)    // X22 = length counter

	strlenLoop := e.asm.Offset()
	e.asm.LDRB(X23, X21, 0) // Load byte
	e.asm.CMP(X23, XZR)     // Check if null
	strlenDone := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)  // if null, done (placeholder)
	e.asm.ADDi(X21, X21, 1) // Increment pointer
	e.asm.ADDi(X22, X22, 1) // Increment counter
	strlenEnd := e.asm.Offset()
	backOffset := int32(strlenLoop - strlenEnd)
	e.asm.B(backOffset)     // Jump back to loop

	// Patch the done branch
	strlenDoneLabel := e.asm.Offset()
	e.asm.Patch(strlenDone, e.asm.Bcond_instr(CondEQ, int32(strlenDoneLabel-strlenDone)>>2))
	// X22 now contains the string length

	// open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644)
	// O_WRONLY=1, O_CREAT=0x200, O_TRUNC=0x400 -> 0x601 = 1537
	e.asm.MOV(X0, X19)              // path
	e.asm.MOVimm(X1, 1537)          // flags: O_WRONLY | O_CREAT | O_TRUNC
	e.asm.MOVimm(X2, 420)           // mode: 0644 octal = 420 decimal
	e.asm.MOVimm(X16, 0x2000005)    // syscall open
	e.asm.SVC(0x80)
	e.asm.MOV(X21, X0)              // Save fd in X21 (X22 has length)

	// Check for open error
	e.asm.CMP(X21, XZR)
	openOk := e.asm.Offset()
	e.asm.Bcond(CondGE, 0)          // Branch if fd >= 0

	// Open failed, return -1
	e.asm.MOVimm(X16, -1)
	e.storeToVReg(instr.Dest.VReg, X16)
	openFailed := e.asm.Offset()
	e.asm.B(0)                      // Jump to end (placeholder)

	// Patch openOk branch
	openOkLabel := e.asm.Offset()
	e.asm.Patch(openOk, e.asm.Bcond_instr(CondGE, int32(openOkLabel-openOk)>>2))

	// write(fd, content, length)
	e.asm.MOV(X0, X21)              // fd
	e.asm.MOV(X1, X20)              // content
	e.asm.MOV(X2, X22)              // length
	e.asm.MOVimm(X16, 0x2000004)    // syscall write
	e.asm.SVC(0x80)
	e.asm.MOV(X23, X0)              // Save bytes written

	// close(fd)
	e.asm.MOV(X0, X21)              // fd
	e.asm.MOVimm(X16, 0x2000006)    // syscall close
	e.asm.SVC(0x80)

	// Check if all bytes were written
	e.asm.CMP(X23, X22)             // Compare written with length
	writeOk := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)          // Branch if equal

	// Write failed (partial write), return -1
	e.asm.MOVimm(X16, -1)
	e.storeToVReg(instr.Dest.VReg, X16)
	writeFailed := e.asm.Offset()
	e.asm.B(0)                      // Jump to end (placeholder)

	// Patch writeOk branch
	writeOkLabel := e.asm.Offset()
	e.asm.Patch(writeOk, e.asm.Bcond_instr(CondEQ, int32(writeOkLabel-writeOk)>>2))

	// Success, return 0
	e.asm.MOVimm(X16, 0)
	e.storeToVReg(instr.Dest.VReg, X16)

	// Patch failure jumps
	endLabel := e.asm.Offset()
	e.asm.Patch(openFailed, e.asm.B_instr(int32(endLabel-openFailed)>>2))
	e.asm.Patch(writeFailed, e.asm.B_instr(int32(endLabel-writeFailed)>>2))
}

// emitArgc returns the number of command line arguments
// argc is saved in X27 at program start
func (e *Emitter) emitArgc(instr *ir.Instr) {
	e.storeToVReg(instr.Dest.VReg, X27)
}

// emitArgv returns the command line argument at the given index
// argv is saved in X28 at program start
func (e *Emitter) emitArgv(instr *ir.Instr) {
	// Load index
	idx := e.loadOperand(instr.Args[0], X16)

	// Compute address: argv + index * 8 (pointers are 8 bytes)
	e.asm.MOVimm(X17, 8)
	e.asm.MUL(X16, idx, X17)       // X16 = index * 8
	e.asm.ADD(X16, X28, X16)       // X16 = argv + index * 8

	// Load the string pointer from argv[index]
	e.asm.LDRx(X16, X16)
	e.storeToVReg(instr.Dest.VReg, X16)
}

// emitIntToStr converts an integer to a decimal string
// Algorithm: extract digits by repeatedly dividing by 10, build string backwards
// Uses heap allocation so the string persists after function returns.
func (e *Emitter) emitIntToStr(instr *ir.Instr) {
	// Load the integer value
	num := e.loadOperand(instr.Args[0], X19)
	e.asm.MOV(X19, num) // Save in callee-saved register

	// Allocate 32 bytes on heap (enough for "-9223372036854775808\0" = 21 chars, aligned to 8)
	// We'll build the string from the end backwards
	// Use X23 for size (not X17) because emitBumpAlloc corrupts X17 internally
	e.asm.MOVimm(X23, 32)
	e.emitBumpAlloc(X23, X20) // X20 = buffer start

	// X21 = write pointer (starts at end of buffer - 1 for null terminator)
	e.asm.ADDi(X21, X20, 23)

	// Write null terminator
	e.asm.STRB(XZR, X21, 0)

	// Check if number is negative
	e.asm.CMP(X19, XZR)
	notNegOffset := e.asm.Offset()
	e.asm.Bcond(CondGE, 0) // placeholder: branch if >= 0

	// Number is negative: negate it, remember we need a sign
	e.asm.NEG(X19, X19)
	e.asm.MOVimm(X22, 1) // X22 = 1 means negative
	skipSetPos := e.asm.Offset()
	e.asm.B(0) // placeholder: skip setting positive flag

	// Number is non-negative
	notNegLabel := e.asm.Offset()
	e.asm.Patch(notNegOffset, e.asm.Bcond_instr(CondGE, int32(notNegLabel-notNegOffset)>>2))
	e.asm.MOVimm(X22, 0) // X22 = 0 means non-negative

	// Patch the skip branch
	afterSignCheck := e.asm.Offset()
	e.asm.Patch(skipSetPos, uint32(0x14000000)|uint32((int32(afterSignCheck-skipSetPos)>>2)&0x3FFFFFF))

	// Handle special case: n == 0
	e.asm.CMP(X19, XZR)
	notZeroOffset := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder

	// n == 0: just write '0'
	e.asm.SUBi(X21, X21, 1)
	e.asm.MOVimm(X23, '0')
	e.asm.STRB(X23, X21, 0)
	skipDigitLoop := e.asm.Offset()
	e.asm.B(0) // placeholder: skip to sign handling

	// Patch notZero branch
	notZeroLabel := e.asm.Offset()
	e.asm.Patch(notZeroOffset, e.asm.Bcond_instr(CondNE, int32(notZeroLabel-notZeroOffset)>>2))

	// Digit extraction loop: while n > 0
	digitLoop := e.asm.Offset()

	// digit = n % 10
	e.asm.MOVimm(X23, 10)
	e.asm.UDIV(X24, X19, X23)  // X24 = n / 10
	e.asm.MSUB(X27, X24, X23, X19) // X27 = n - (n/10)*10 = n % 10

	// Convert digit to ASCII: '0' + digit
	e.asm.ADDi(X27, X27, '0')

	// Move write pointer back and store digit
	e.asm.SUBi(X21, X21, 1)
	e.asm.STRB(X27, X21, 0)

	// n = n / 10
	e.asm.MOV(X19, X24)

	// Loop while n > 0
	e.asm.CMP(X19, XZR)
	e.asm.Bcond(CondNE, int32(digitLoop-e.asm.Offset()))

	// Patch skip digit loop branch
	afterDigitLoop := e.asm.Offset()
	e.asm.Patch(skipDigitLoop, uint32(0x14000000)|uint32((int32(afterDigitLoop-skipDigitLoop)>>2)&0x3FFFFFF))

	// Add negative sign if needed
	e.asm.CMP(X22, XZR)
	skipSignOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder: skip if not negative

	// Add '-' sign
	e.asm.SUBi(X21, X21, 1)
	e.asm.MOVimm(X23, '-')
	e.asm.STRB(X23, X21, 0)

	// Patch skip sign branch
	afterSign := e.asm.Offset()
	e.asm.Patch(skipSignOffset, e.asm.Bcond_instr(CondEQ, int32(afterSign-skipSignOffset)>>2))

	// X21 now points to the start of the string
	// Note: Stack is not deallocated; epilogue will clean it up
	e.storeToVReg(instr.Dest.VReg, X21)
}

// emitStrToInt converts a decimal string to an integer
// Algorithm: parse digits, handle optional leading '-'
func (e *Emitter) emitStrToInt(instr *ir.Instr) {
	// Load string pointer
	str := e.loadOperand(instr.Args[0], X19)
	e.asm.MOV(X19, str) // X19 = string pointer

	// Initialize result to 0
	e.asm.MOVimm(X20, 0) // X20 = result

	// Initialize sign flag to 0 (positive)
	e.asm.MOVimm(X21, 0) // X21 = negative flag

	// Load first character
	e.asm.LDRB(X22, X19, 0)

	// Check for '-' sign
	e.asm.MOVimm(X23, '-')
	e.asm.CMP(X22, X23)
	notNegOffset := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder

	// It's negative: set flag and advance pointer
	e.asm.MOVimm(X21, 1)
	e.asm.ADDi(X19, X19, 1)

	// Patch not-negative branch
	notNegLabel := e.asm.Offset()
	e.asm.Patch(notNegOffset, e.asm.Bcond_instr(CondNE, int32(notNegLabel-notNegOffset)>>2))

	// Check for '+' sign (skip it if present)
	e.asm.LDRB(X22, X19, 0)
	e.asm.MOVimm(X23, '+')
	e.asm.CMP(X22, X23)
	notPlusOffset := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder

	// It's '+': advance pointer
	e.asm.ADDi(X19, X19, 1)

	// Patch not-plus branch
	notPlusLabel := e.asm.Offset()
	e.asm.Patch(notPlusOffset, e.asm.Bcond_instr(CondNE, int32(notPlusLabel-notPlusOffset)>>2))

	// Digit parsing loop
	digitLoop := e.asm.Offset()

	// Load current character
	e.asm.LDRB(X22, X19, 0)

	// Check if it's a digit (>= '0' and <= '9')
	// First check >= '0'
	e.asm.MOVimm(X23, '0')
	e.asm.CMP(X22, X23)
	notDigitOffset1 := e.asm.Offset()
	e.asm.Bcond(CondLT, 0) // placeholder: not a digit if < '0'

	// Check <= '9'
	e.asm.MOVimm(X23, '9')
	e.asm.CMP(X22, X23)
	notDigitOffset2 := e.asm.Offset()
	e.asm.Bcond(CondGT, 0) // placeholder: not a digit if > '9'

	// It's a digit: result = result * 10 + (char - '0')
	e.asm.MOVimm(X23, 10)
	e.asm.MUL(X20, X20, X23)      // result *= 10

	e.asm.MOVimm(X23, '0')
	e.asm.SUB(X22, X22, X23)      // digit = char - '0'
	e.asm.ADD(X20, X20, X22)      // result += digit

	// Advance pointer and continue loop
	e.asm.ADDi(X19, X19, 1)
	e.asm.B(int32(digitLoop - e.asm.Offset()))

	// End of digits (not a digit or end of string)
	endDigits := e.asm.Offset()
	e.asm.Patch(notDigitOffset1, e.asm.Bcond_instr(CondLT, int32(endDigits-notDigitOffset1)>>2))
	e.asm.Patch(notDigitOffset2, e.asm.Bcond_instr(CondGT, int32(endDigits-notDigitOffset2)>>2))

	// Apply sign if negative
	e.asm.CMP(X21, XZR)
	skipNegOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder: skip if not negative

	// Negate result
	e.asm.NEG(X20, X20)

	// Patch skip-negate branch
	afterNeg := e.asm.Offset()
	e.asm.Patch(skipNegOffset, e.asm.Bcond_instr(CondEQ, int32(afterNeg-skipNegOffset)>>2))

	// Store result
	e.storeToVReg(instr.Dest.VReg, X20)
}

// emitHeapAlloc allocates memory from the heap using a bump allocator.
// Heap state is stored in callee-saved registers:
//   X25 = heap_ptr (current bump pointer, 0 = uninitialized)
//   X26 = heap_end (end of current mmap'd region)
// On macOS ARM64, mmap syscall is 0x20000C5 (197 + 0x2000000)
func (e *Emitter) emitHeapAlloc(instr *ir.Instr) {
	// Load requested size
	size := e.loadOperand(instr.Args[0], X16)
	e.asm.MOV(X19, size) // X19 = requested size

	// Align size to 8 bytes: size = (size + 7) & ~7
	// Use shifts to clear the low 3 bits: (x + 7) >> 3 << 3
	e.asm.ADDi(X19, X19, 7)
	e.asm.LSR(X19, X19, 3)
	e.asm.LSL(X19, X19, 3)

	// Check if heap is initialized (X25 != 0)
	e.asm.CMP(X25, XZR)
	heapInitialized := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder: jump if initialized

	// Heap not initialized: call mmap to get initial region
	e.emitMmapCall(1024 * 1024) // 1MB initial heap

	// X0 now has the mmap result
	// Check for error (mmap returns -1 on error)
	e.asm.CMN(X0, 1) // compare with -1
	mmapOK := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder: jump if OK

	// mmap failed - for now, just return 0 (null pointer)
	e.asm.MOVimm(X16, 0)
	e.storeToVReg(instr.Dest.VReg, X16)
	mmapFailedRet := e.asm.Offset()
	e.asm.B(0) // placeholder: jump to end

	// Patch mmapOK branch
	mmapOKLabel := e.asm.Offset()
	e.asm.Patch(mmapOK, e.asm.Bcond_instr(CondNE, int32(mmapOKLabel-mmapOK)>>2))

	// Initialize heap state
	e.asm.MOV(X25, X0)              // heap_ptr = mmap result
	e.asm.MOVimm(X17, 1024*1024)    // heap size
	e.asm.ADD(X26, X0, X17)         // heap_end = heap_ptr + size

	// Patch heapInitialized branch
	initDone := e.asm.Offset()
	e.asm.Patch(heapInitialized, e.asm.Bcond_instr(CondNE, int32(initDone-heapInitialized)>>2))

	// Now allocate from the bump allocator
	// Check if we have enough space: heap_ptr + size <= heap_end
	e.asm.ADD(X17, X25, X19)   // X17 = heap_ptr + size
	e.asm.CMP(X17, X26)        // compare with heap_end
	haveSpace := e.asm.Offset()
	e.asm.Bcond(CondLE, 0)     // placeholder: jump if we have space

	// Not enough space: need to mmap more
	// For simplicity, mmap another 1MB region
	// Note: This creates a new region, not extending the old one
	// A more sophisticated allocator would handle this better
	e.emitMmapCall(1024 * 1024)

	// Check for mmap error
	e.asm.CMN(X0, 1)
	mmap2OK := e.asm.Offset()
	e.asm.Bcond(CondNE, 0)

	// mmap failed
	e.asm.MOVimm(X16, 0)
	e.storeToVReg(instr.Dest.VReg, X16)
	mmap2FailedRet := e.asm.Offset()
	e.asm.B(0) // placeholder: jump to end

	// Patch mmap2OK
	mmap2OKLabel := e.asm.Offset()
	e.asm.Patch(mmap2OK, e.asm.Bcond_instr(CondNE, int32(mmap2OKLabel-mmap2OK)>>2))

	// Update heap state to new region
	e.asm.MOV(X25, X0)
	e.asm.MOVimm(X17, 1024*1024)
	e.asm.ADD(X26, X0, X17)

	// Patch haveSpace branch
	allocLabel := e.asm.Offset()
	e.asm.Patch(haveSpace, e.asm.Bcond_instr(CondLE, int32(allocLabel-haveSpace)>>2))

	// Bump allocate: result = heap_ptr, heap_ptr += size
	e.asm.MOV(X16, X25)        // X16 = current heap_ptr (result)
	e.asm.ADD(X25, X25, X19)   // heap_ptr += size

	// Store result
	e.storeToVReg(instr.Dest.VReg, X16)

	// Patch failure return jumps to here
	endLabel := e.asm.Offset()
	e.asm.Patch(mmapFailedRet, e.asm.B_instr(int32(endLabel-mmapFailedRet)>>2))
	e.asm.Patch(mmap2FailedRet, e.asm.B_instr(int32(endLabel-mmap2FailedRet)>>2))
}

// emitBumpAlloc allocates memory from the heap using the bump allocator.
// sizeReg contains the size to allocate (should be already aligned to 8 bytes).
// dstReg is where the result pointer will be stored.
// Uses callee-saved X25 (heap_ptr) and X26 (heap_end) for heap state.
// Clobbers X0-X5, X16, X17.
func (e *Emitter) emitBumpAlloc(sizeReg, dstReg Reg) {
	// Check if heap is initialized (X25 != 0)
	e.asm.CMP(X25, XZR)
	heapInitialized := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder: jump if initialized

	// Heap not initialized: call mmap to get initial region
	e.emitMmapCall(1024 * 1024) // 1MB initial heap

	// Check for error (mmap returns -1 on error)
	e.asm.CMN(X0, 1) // compare with -1
	mmapOK := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder: jump if OK

	// mmap failed - return 0 (null pointer)
	e.asm.MOV(dstReg, XZR)
	mmapFailedRet := e.asm.Offset()
	e.asm.B(0) // placeholder: jump to end

	// Patch mmapOK branch
	mmapOKLabel := e.asm.Offset()
	e.asm.Patch(mmapOK, e.asm.Bcond_instr(CondNE, int32(mmapOKLabel-mmapOK)>>2))

	// Initialize heap state
	e.asm.MOV(X25, X0)           // heap_ptr = mmap result
	e.asm.MOVimm(X17, 1024*1024) // heap size
	e.asm.ADD(X26, X0, X17)      // heap_end = heap_ptr + size

	// Patch heapInitialized branch
	initDone := e.asm.Offset()
	e.asm.Patch(heapInitialized, e.asm.Bcond_instr(CondNE, int32(initDone-heapInitialized)>>2))

	// Now allocate from the bump allocator
	// Check if we have enough space: heap_ptr + size <= heap_end
	e.asm.ADD(X17, X25, sizeReg) // X17 = heap_ptr + size
	e.asm.CMP(X17, X26)          // compare with heap_end
	haveSpace := e.asm.Offset()
	e.asm.Bcond(CondLE, 0) // placeholder: jump if we have space

	// Not enough space: need to mmap more
	e.emitMmapCall(1024 * 1024)

	// Check for mmap error
	e.asm.CMN(X0, 1)
	mmap2OK := e.asm.Offset()
	e.asm.Bcond(CondNE, 0)

	// mmap failed
	e.asm.MOV(dstReg, XZR)
	mmap2FailedRet := e.asm.Offset()
	e.asm.B(0) // placeholder: jump to end

	// Patch mmap2OK
	mmap2OKLabel := e.asm.Offset()
	e.asm.Patch(mmap2OK, e.asm.Bcond_instr(CondNE, int32(mmap2OKLabel-mmap2OK)>>2))

	// Update heap state to new region
	e.asm.MOV(X25, X0)
	e.asm.MOVimm(X17, 1024*1024)
	e.asm.ADD(X26, X0, X17)

	// Patch haveSpace branch
	allocLabel := e.asm.Offset()
	e.asm.Patch(haveSpace, e.asm.Bcond_instr(CondLE, int32(allocLabel-haveSpace)>>2))

	// Bump allocate: result = heap_ptr, heap_ptr += size
	e.asm.MOV(dstReg, X25)       // result = current heap_ptr
	e.asm.ADD(X25, X25, sizeReg) // heap_ptr += size

	// Patch failure return jumps to here
	endLabel := e.asm.Offset()
	e.asm.Patch(mmapFailedRet, e.asm.B_instr(int32(endLabel-mmapFailedRet)>>2))
	e.asm.Patch(mmap2FailedRet, e.asm.B_instr(int32(endLabel-mmap2FailedRet)>>2))
}

// emitMmapCall emits a mmap syscall to allocate anonymous memory.
// Result is returned in X0.
// mmap(addr=0, len=size, prot=RW, flags=PRIVATE|ANON, fd=-1, offset=0)
func (e *Emitter) emitMmapCall(size int64) {
	e.asm.MOVimm(X0, 0)                // addr = NULL (let kernel choose)
	e.asm.MOVimm(X1, size)             // length
	e.asm.MOVimm(X2, 0x3)              // prot = PROT_READ | PROT_WRITE
	e.asm.MOVimm(X3, 0x1002)           // flags = MAP_PRIVATE | MAP_ANONYMOUS
	e.asm.MOVimm(X4, -1)               // fd = -1 (anonymous)
	e.asm.MOVimm(X5, 0)                // offset = 0
	e.asm.MOVimm(X16, 0x20000C5)       // syscall mmap (197 + 0x2000000)
	e.asm.SVC(0x80)
}

// emitPoke writes a byte to memory
// Args[0] is address, Args[1] is value (byte)
func (e *Emitter) emitPoke(instr *ir.Instr) {
	addr := e.loadOperand(instr.Args[0], X16)
	value := e.loadOperand(instr.Args[1], X17)

	if addr != X16 {
		e.asm.MOV(X16, addr)
	}
	if value != X17 {
		e.asm.MOV(X17, value)
	}

	// Write byte: STRB W17, [X16]
	e.asm.STRB(X17, X16, 0)
}

// emitPeek reads a byte from memory
// Args[0] is address, Dest is result (byte value as int)
func (e *Emitter) emitPeek(instr *ir.Instr) {
	addr := e.loadOperand(instr.Args[0], X16)

	if addr != X16 {
		e.asm.MOV(X16, addr)
	}

	// Read byte: LDRB W17, [X16]
	e.asm.LDRB(X17, X16, 0)
	e.storeToVReg(instr.Dest.VReg, X17)
}

// emitMemSet sets memory to a byte value
// Args[0] is address, Args[1] is value (byte), Args[2] is count
func (e *Emitter) emitMemSet(instr *ir.Instr) {
	addr := e.loadOperand(instr.Args[0], X16)
	value := e.loadOperand(instr.Args[1], X17)
	count := e.loadOperand(instr.Args[2], X18)

	if addr != X16 {
		e.asm.MOV(X16, addr)
	}
	if value != X17 {
		e.asm.MOV(X17, value)
	}
	if count != X18 {
		e.asm.MOV(X18, count)
	}

	// Load constant 1 for increment/decrement
	e.asm.MOVimm(X19, 1)

	// Loop: check count, write byte, repeat
	loopStart := e.asm.Offset()

	// Check if count is zero
	e.asm.CMP(X18, XZR)
	doneOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // if zero, jump to done (placeholder)

	// Write byte
	e.asm.STRB(X17, X16, 0)

	// Increment address and decrement count
	e.asm.ADD(X16, X16, X19)  // addr++ (register add)
	e.asm.SUB(X18, X18, X19)  // count-- (register sub)

	// Unconditional jump back to loop start
	loopEnd := e.asm.Offset()
	backOffset := int32(loopStart - loopEnd)
	e.asm.B(backOffset) // B() handles division by 4

	// Patch done branch
	doneLabel := e.asm.Offset()
	e.asm.Patch(doneOffset, e.asm.Bcond_instr(CondEQ, int32(doneLabel-doneOffset)>>2))
}

// emitSyscallOpen emits the open syscall.
// open(path, flags, mode) -> fd
// macOS ARM64 syscall: 5 (0x2000005)
func (e *Emitter) emitSyscallOpen(instr *ir.Instr) {
	// Load arguments
	path := e.loadOperand(instr.Args[0], X0)   // path
	flags := e.loadOperand(instr.Args[1], X1)  // flags
	mode := e.loadOperand(instr.Args[2], X2)   // mode

	if path != X0 {
		e.asm.MOV(X0, path)
	}
	if flags != X1 {
		e.asm.MOV(X1, flags)
	}
	if mode != X2 {
		e.asm.MOV(X2, mode)
	}

	// Syscall number for open
	e.asm.MOVimm(X16, 0x2000005)
	e.asm.SVC(0x80)

	// Result is in X0
	e.storeToVReg(instr.Dest.VReg, X0)
}

// emitSyscallRead emits the read syscall.
// read(fd, buf, size) -> bytes_read
// macOS ARM64 syscall: 3 (0x2000003)
func (e *Emitter) emitSyscallRead(instr *ir.Instr) {
	// Load arguments
	fd := e.loadOperand(instr.Args[0], X0)    // fd
	buf := e.loadOperand(instr.Args[1], X1)   // buf
	size := e.loadOperand(instr.Args[2], X2)  // size

	if fd != X0 {
		e.asm.MOV(X0, fd)
	}
	if buf != X1 {
		e.asm.MOV(X1, buf)
	}
	if size != X2 {
		e.asm.MOV(X2, size)
	}

	// Syscall number for read
	e.asm.MOVimm(X16, 0x2000003)
	e.asm.SVC(0x80)

	// Result is in X0
	e.storeToVReg(instr.Dest.VReg, X0)
}

// emitSyscallWrite emits the write syscall.
// write(fd, buf, size) -> bytes_written
// macOS ARM64 syscall: 4 (0x2000004)
func (e *Emitter) emitSyscallWrite(instr *ir.Instr) {
	// Load arguments
	fd := e.loadOperand(instr.Args[0], X0)    // fd
	buf := e.loadOperand(instr.Args[1], X1)   // buf
	size := e.loadOperand(instr.Args[2], X2)  // size

	if fd != X0 {
		e.asm.MOV(X0, fd)
	}
	if buf != X1 {
		e.asm.MOV(X1, buf)
	}
	if size != X2 {
		e.asm.MOV(X2, size)
	}

	// Syscall number for write
	e.asm.MOVimm(X16, 0x2000004)
	e.asm.SVC(0x80)

	// Result is in X0
	e.storeToVReg(instr.Dest.VReg, X0)
}

// emitSyscallClose emits the close syscall.
// close(fd) -> int
// macOS ARM64 syscall: 6 (0x2000006)
func (e *Emitter) emitSyscallClose(instr *ir.Instr) {
	// Load argument
	fd := e.loadOperand(instr.Args[0], X0)

	if fd != X0 {
		e.asm.MOV(X0, fd)
	}

	// Syscall number for close
	e.asm.MOVimm(X16, 0x2000006)
	e.asm.SVC(0x80)

	// Result is in X0
	e.storeToVReg(instr.Dest.VReg, X0)
}

// Map implementation constants
const (
	mapHeaderSize   = 40 // 5 * 8 bytes
	mapBucketsOff   = 0  // offset to buckets pointer
	mapCountOff     = 8  // offset to count
	mapCapacityOff  = 16 // offset to capacity
	mapKeySizeOff   = 24 // offset to key size
	mapValSizeOff   = 32 // offset to value size
	bucketStatusOff = 0  // status byte offset in bucket
	bucketHashOff   = 8  // hash offset in bucket
	bucketKeyOff    = 16 // key offset in bucket
	bucketEmpty     = 0
	bucketOccupied  = 1
	bucketDeleted   = 2
	initialMapCap   = 8 // initial number of buckets
)

// emitHeapAllocHelper allocates memory from the heap using mmap.
// size: number of bytes to allocate
// dstReg: register to store the result pointer
func (e *Emitter) emitHeapAllocHelper(size int, dstReg Reg) {
	// Use mmap syscall to allocate memory
	// mmap(addr, len, prot, flags, fd, offset)
	// addr = 0 (let kernel choose)
	// len = size (round up to page size for efficiency)
	// prot = PROT_READ | PROT_WRITE = 3
	// flags = MAP_ANON | MAP_PRIVATE = 0x1002
	// fd = -1
	// offset = 0

	e.asm.MOV(X0, XZR)           // addr = NULL
	e.asm.MOVimm(X1, int64(size)) // len = size
	e.asm.MOVimm(X2, 3)          // prot = PROT_READ | PROT_WRITE
	e.asm.MOVimm(X3, 0x1002)     // flags = MAP_ANON | MAP_PRIVATE
	e.asm.MOVimm(X4, -1)         // fd = -1
	e.asm.MOV(X5, XZR)           // offset = 0
	e.asm.MOVimm(X16, 0x20000C5) // mmap syscall number for macOS
	e.asm.SVC(0x80)

	if dstReg != X0 {
		e.asm.MOV(dstReg, X0)
	}
}

// emitMapNew creates a new empty map.
// Args: keySize (imm), valSize (imm)
// Returns: pointer to map header
func (e *Emitter) emitMapNew(instr *ir.Instr) {
	keySize := instr.Args[0].Imm
	valSize := instr.Args[1].Imm
	bucketSize := 16 + keySize + valSize // status(1) + padding(7) + hash(8) + key + value

	// Allocate header (40 bytes)
	e.emitHeapAllocHelper(mapHeaderSize, X19) // Use callee-saved to preserve across second mmap
	// X19 = header ptr

	// Allocate buckets array
	totalBucketSize := bucketSize * initialMapCap
	e.emitHeapAllocHelper(int(totalBucketSize), X1)
	// X1 = buckets ptr

	// Zero out the buckets (all status bytes = 0 means empty)
	// Simple loop to zero memory
	e.asm.MOV(X2, X1)                                     // current ptr
	e.asm.MOVimm(X3, int64(totalBucketSize))              // size to zero
	e.asm.ADD(X3, X1, X3)                                 // end ptr = buckets + size

	// Loop: while (X2 < X3) { *X2 = 0; X2 += 8; }
	zeroLoopOffset := e.asm.Offset()
	e.asm.CMP(X2, X3)
	endZeroOffset := e.asm.Offset()
	e.asm.Bcond(CondGE, 0) // placeholder - branch if done
	e.asm.STRx(XZR, X2)    // store 0
	e.asm.ADDi(X2, X2, 8)  // advance by 8
	loopBackOffset := e.asm.Offset() - zeroLoopOffset
	e.asm.B(-int32(loopBackOffset)) // jump back to loop start

	// Patch the exit branch
	endZeroLabel := e.asm.Offset()
	e.asm.Patch(endZeroOffset, e.asm.Bcond_instr(CondGE, int32(endZeroLabel-endZeroOffset)>>2))

	// Initialize header (X19 = header, X1 = buckets)
	e.asm.STR(X1, X19, mapBucketsOff)  // buckets_ptr
	e.asm.STR(XZR, X19, mapCountOff)   // count = 0
	e.asm.MOVimm(X4, initialMapCap)
	e.asm.STR(X4, X19, mapCapacityOff) // capacity
	e.asm.MOVimm(X4, keySize)
	e.asm.STR(X4, X19, mapKeySizeOff)  // key_size
	e.asm.MOVimm(X4, valSize)
	e.asm.STR(X4, X19, mapValSizeOff)  // val_size

	// Return header pointer
	e.storeToVReg(instr.Dest.VReg, X19)
}

// emitHashInt computes hash of an integer value.
// Input: value in srcReg
// Output: hash in dstReg
// Uses simple multiplication for hash distribution
func (e *Emitter) emitHashInt(srcReg, dstReg Reg) {
	// hash = value * 2654435769 (golden ratio * 2^32)
	// Using a smaller constant that fits in 32 bits
	e.asm.MOVimm(X9, 2654435769)
	e.asm.MUL(dstReg, srcReg, X9)
}

// emitHashString computes hash of a null-terminated string using DJB2.
// Input: string pointer in srcReg
// Output: hash in dstReg
func (e *Emitter) emitHashString(srcReg, dstReg Reg) {
	// DJB2: hash = 5381; for each char: hash = hash * 33 + char
	e.asm.MOVimm(dstReg, 5381)
	e.asm.MOV(X10, srcReg) // current char ptr

	loopStart := e.asm.Offset()
	e.asm.LDRB(X11, X10, 0) // load byte
	cbzOffset := e.asm.Offset()
	e.asm.CBZ(X11, 0) // placeholder - if null, done

	// hash = hash * 33 + char = hash * 32 + hash + char
	e.asm.LSL(X12, dstReg, 5)      // hash * 32
	e.asm.ADD(dstReg, X12, dstReg) // hash * 33
	e.asm.ADD(dstReg, dstReg, X11) // + char
	e.asm.ADDi(X10, X10, 1)        // advance ptr
	loopBackDist := e.asm.Offset() - loopStart
	e.asm.B(-int32(loopBackDist)) // jump back to loop start

	// Patch CBZ to jump here (end of loop)
	endLoop := e.asm.Offset()
	e.asm.Patch(cbzOffset, e.asm.CBZ_instr(X11, int32(endLoop-cbzOffset)>>2))
}

// emitMapGet looks up a key in the map.
// Args: map, key
// Returns: value (or zero if not found)
func (e *Emitter) emitMapGet(instr *ir.Instr) {
	mapReg := e.loadOperand(instr.Args[0], X0)
	keyReg := e.loadOperand(instr.Args[1], X1)

	// Load map metadata
	e.asm.LDR(X2, mapReg, mapBucketsOff)  // buckets_ptr
	e.asm.LDR(X3, mapReg, mapCapacityOff) // capacity
	e.asm.LDR(X4, mapReg, mapKeySizeOff)  // key_size
	e.asm.LDR(X5, mapReg, mapValSizeOff)  // val_size

	// Compute bucket size: 16 + key_size + val_size
	e.asm.ADDi(X6, X4, 16)
	e.asm.ADD(X6, X6, X5) // X6 = bucket_size

	// Compute hash
	keyType := instr.Args[1].Type
	if keyType != nil && keyType.String() == "string" {
		e.emitHashString(keyReg, X7)
	} else {
		e.emitHashInt(keyReg, X7)
	}

	// index = hash % capacity
	e.asm.UDIV(X8, X7, X3)
	e.asm.MSUB(X8, X8, X3, X7) // X8 = hash - (hash/cap)*cap = hash % cap

	// Linear probe loop
	e.asm.MOV(X9, X8) // X9 = current index, X8 = start index

	probeLoopStart := e.asm.Offset()

	// bucket_ptr = buckets + index * bucket_size
	e.asm.MUL(X10, X9, X6)
	e.asm.ADD(X10, X2, X10) // X10 = bucket_ptr

	// Check status
	e.asm.LDRB(X11, X10, bucketStatusOff)
	e.asm.CMPi(X11, bucketEmpty)
	notFoundBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder - empty = not found

	// If deleted, continue probing
	e.asm.CMPi(X11, bucketDeleted)
	continueProbeBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder - jump to continue probe

	// Compare key (integer keys only for now)
	e.asm.LDR(X12, X10, bucketKeyOff)
	e.asm.CMP(X12, keyReg)
	keyNotEqualBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder - key not equal, continue probing

	// Found - load value
	e.asm.ADDi(X0, X10, bucketKeyOff)
	e.asm.ADD(X0, X0, X4) // value_ptr = bucket + 16 + key_size
	e.asm.LDRx(X0, X0)    // load value
	doneFromFoundOffset := e.asm.Offset()
	e.asm.B(0) // placeholder - jump to done

	// Not found - return 0
	notFoundLabel := e.asm.Offset()
	e.asm.MOV(X0, XZR)
	doneFromNotFoundOffset := e.asm.Offset()
	e.asm.B(0) // placeholder - jump to done

	// Continue probing
	continueProbeLabel := e.asm.Offset()
	e.asm.ADDi(X9, X9, 1) // index++
	// Wrap around: if index >= capacity, index = 0
	e.asm.CMP(X9, X3)
	e.asm.CSEL(X9, XZR, X9, CondGE)
	// Check if we've wrapped around completely
	e.asm.CMP(X9, X8)
	wrapBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder - not wrapped, continue loop
	// Wrapped around = not found
	e.asm.MOV(X0, XZR)

	doneLabel := e.asm.Offset()

	// Patch all branches
	e.asm.Patch(notFoundBranchOffset, e.asm.Bcond_instr(CondEQ, int32(notFoundLabel-notFoundBranchOffset)>>2))
	e.asm.Patch(continueProbeBranchOffset, e.asm.Bcond_instr(CondEQ, int32(continueProbeLabel-continueProbeBranchOffset)>>2))
	e.asm.Patch(keyNotEqualBranchOffset, e.asm.Bcond_instr(CondNE, int32(continueProbeLabel-keyNotEqualBranchOffset)>>2))
	e.asm.Patch(doneFromFoundOffset, e.asm.B_instr(int32(doneLabel-doneFromFoundOffset)>>2))
	e.asm.Patch(doneFromNotFoundOffset, e.asm.B_instr(int32(doneLabel-doneFromNotFoundOffset)>>2))
	e.asm.Patch(wrapBranchOffset, e.asm.Bcond_instr(CondNE, int32(probeLoopStart-wrapBranchOffset)>>2))

	e.storeToVReg(instr.Dest.VReg, X0)
}

// emitMapSet inserts or updates a key-value pair in the map.
// Args: map, key, value
func (e *Emitter) emitMapSet(instr *ir.Instr) {
	mapReg := e.loadOperand(instr.Args[0], X0)
	keyReg := e.loadOperand(instr.Args[1], X1)
	e.loadOperand(instr.Args[2], X19) // Load value into X19 (callee-saved)

	// Save map ptr and key in callee-saved registers
	e.asm.MOV(X20, mapReg)
	e.asm.MOV(X21, keyReg)

	// Load map metadata
	e.asm.LDR(X2, mapReg, mapBucketsOff)  // buckets_ptr
	e.asm.LDR(X3, mapReg, mapCapacityOff) // capacity
	e.asm.LDR(X4, mapReg, mapKeySizeOff)  // key_size
	e.asm.LDR(X5, mapReg, mapValSizeOff)  // val_size

	// Compute bucket size
	e.asm.ADDi(X6, X4, 16)
	e.asm.ADD(X6, X6, X5)

	// Compute hash
	keyType := instr.Args[1].Type
	if keyType != nil && keyType.String() == "string" {
		e.emitHashString(keyReg, X7)
	} else {
		e.emitHashInt(keyReg, X7)
	}
	e.asm.MOV(X22, X7) // save hash in callee-saved

	// index = hash % capacity
	e.asm.UDIV(X8, X7, X3)
	e.asm.MSUB(X8, X8, X3, X7)

	// Linear probe to find slot
	e.asm.MOV(X9, X8) // current index

	probeLoopStart := e.asm.Offset()

	// bucket_ptr = buckets + index * bucket_size
	e.asm.MUL(X10, X9, X6)
	e.asm.ADD(X10, X2, X10)

	// Check status
	e.asm.LDRB(X11, X10, bucketStatusOff)

	// If empty, insert here
	e.asm.CMPi(X11, bucketEmpty)
	insertBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder

	// If deleted, insert here
	e.asm.CMPi(X11, bucketDeleted)
	insertDeletedBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder

	// Check if key matches (integer keys only)
	e.asm.LDR(X12, X10, bucketKeyOff)
	e.asm.CMP(X12, X21)
	updateBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder - key matches, update

	// Continue probing
	e.asm.ADDi(X9, X9, 1)
	e.asm.CMP(X9, X3)
	e.asm.CSEL(X9, XZR, X9, CondGE)
	e.asm.CMP(X9, X8)
	loopBackBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder - not wrapped, continue loop
	// Table full - just exit (should resize but we don't)
	doneFromFullOffset := e.asm.Offset()
	e.asm.B(0) // placeholder

	// Insert new entry
	insertLabel := e.asm.Offset()
	e.asm.MOVimm(X11, bucketOccupied)
	e.asm.STRB(X11, X10, bucketStatusOff)
	e.asm.STR(X22, X10, bucketHashOff)  // Store hash
	e.asm.STR(X21, X10, bucketKeyOff)   // Store key
	e.asm.ADDi(X12, X10, bucketKeyOff)
	e.asm.ADD(X12, X12, X4)
	e.asm.STRx(X19, X12) // Store value

	// Increment count
	e.asm.LDR(X11, X20, mapCountOff)
	e.asm.ADDi(X11, X11, 1)
	e.asm.STR(X11, X20, mapCountOff)
	doneFromInsertOffset := e.asm.Offset()
	e.asm.B(0) // placeholder

	// Update existing entry
	updateLabel := e.asm.Offset()
	e.asm.ADDi(X12, X10, bucketKeyOff)
	e.asm.ADD(X12, X12, X4)
	e.asm.STRx(X19, X12) // Store value

	doneLabel := e.asm.Offset()

	// Patch all branches
	e.asm.Patch(insertBranchOffset, e.asm.Bcond_instr(CondEQ, int32(insertLabel-insertBranchOffset)>>2))
	e.asm.Patch(insertDeletedBranchOffset, e.asm.Bcond_instr(CondEQ, int32(insertLabel-insertDeletedBranchOffset)>>2))
	e.asm.Patch(updateBranchOffset, e.asm.Bcond_instr(CondEQ, int32(updateLabel-updateBranchOffset)>>2))
	e.asm.Patch(loopBackBranchOffset, e.asm.Bcond_instr(CondNE, int32(probeLoopStart-loopBackBranchOffset)>>2))
	e.asm.Patch(doneFromFullOffset, e.asm.B_instr(int32(doneLabel-doneFromFullOffset)>>2))
	e.asm.Patch(doneFromInsertOffset, e.asm.B_instr(int32(doneLabel-doneFromInsertOffset)>>2))
}

// emitMapDelete marks a key as deleted in the map.
// Args: map, key
func (e *Emitter) emitMapDelete(instr *ir.Instr) {
	mapReg := e.loadOperand(instr.Args[0], X0)
	keyReg := e.loadOperand(instr.Args[1], X1)

	// Save map ptr
	e.asm.MOV(X20, mapReg)

	// Load map metadata
	e.asm.LDR(X2, mapReg, mapBucketsOff)
	e.asm.LDR(X3, mapReg, mapCapacityOff)
	e.asm.LDR(X4, mapReg, mapKeySizeOff)
	e.asm.LDR(X5, mapReg, mapValSizeOff)

	// Bucket size
	e.asm.ADDi(X6, X4, 16)
	e.asm.ADD(X6, X6, X5)

	// Hash
	keyType := instr.Args[1].Type
	if keyType != nil && keyType.String() == "string" {
		e.emitHashString(keyReg, X7)
	} else {
		e.emitHashInt(keyReg, X7)
	}

	// index = hash % capacity
	e.asm.UDIV(X8, X7, X3)
	e.asm.MSUB(X8, X8, X3, X7)

	e.asm.MOV(X9, X8)

	probeLoopStart := e.asm.Offset()

	e.asm.MUL(X10, X9, X6)
	e.asm.ADD(X10, X2, X10)

	e.asm.LDRB(X11, X10, bucketStatusOff)
	e.asm.CMPi(X11, bucketEmpty)
	notFoundBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder - not found

	e.asm.CMPi(X11, bucketDeleted)
	continueProbeBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder - continue probing

	// Compare key (integer keys only)
	e.asm.LDR(X12, X10, bucketKeyOff)
	e.asm.CMP(X12, keyReg)
	foundBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0) // placeholder - found

	// Continue probing
	continueProbeLabel := e.asm.Offset()
	e.asm.ADDi(X9, X9, 1)
	e.asm.CMP(X9, X3)
	e.asm.CSEL(X9, XZR, X9, CondGE)
	e.asm.CMP(X9, X8)
	loopBackBranchOffset := e.asm.Offset()
	e.asm.Bcond(CondNE, 0) // placeholder
	doneFromWrapOffset := e.asm.Offset()
	e.asm.B(0) // placeholder - wrapped, done

	// Found - mark as deleted
	foundLabel := e.asm.Offset()
	e.asm.MOVimm(X11, bucketDeleted)
	e.asm.STRB(X11, X10, bucketStatusOff)
	// Decrement count
	e.asm.LDR(X11, X20, mapCountOff)
	e.asm.SUBi(X11, X11, 1)
	e.asm.STR(X11, X20, mapCountOff)

	doneLabel := e.asm.Offset()

	// Patch all branches
	e.asm.Patch(notFoundBranchOffset, e.asm.Bcond_instr(CondEQ, int32(doneLabel-notFoundBranchOffset)>>2))
	e.asm.Patch(continueProbeBranchOffset, e.asm.Bcond_instr(CondEQ, int32(continueProbeLabel-continueProbeBranchOffset)>>2))
	e.asm.Patch(foundBranchOffset, e.asm.Bcond_instr(CondEQ, int32(foundLabel-foundBranchOffset)>>2))
	e.asm.Patch(loopBackBranchOffset, e.asm.Bcond_instr(CondNE, int32(probeLoopStart-loopBackBranchOffset)>>2))
	e.asm.Patch(doneFromWrapOffset, e.asm.B_instr(int32(doneLabel-doneFromWrapOffset)>>2))
}

// emitMapLen returns the number of entries in the map.
// Args: map
// Returns: count
func (e *Emitter) emitMapLen(instr *ir.Instr) {
	mapReg := e.loadOperand(instr.Args[0], X0)
	e.asm.LDR(X0, mapReg, mapCountOff)
	e.storeToVReg(instr.Dest.VReg, X0)
}


// calculateGlobalOffsets computes the offset for each global variable
// Globals are placed after code and strings in the binary
func (e *Emitter) calculateGlobalOffsets() {
	// We don't know the final code size yet, but we'll calculate relative offsets
	// The actual addresses will be fixed up later
	offset := 0
	for _, gv := range e.prog.GlobalVars {
		// Align to 8-byte boundary
		if offset%8 != 0 {
			offset += 8 - (offset % 8)
		}
		e.globalOffsets[gv.Name] = offset
		offset += gv.Size
	}
}

// GlobalsSize returns the total size of the global variables section
func (e *Emitter) GlobalsSize() int {
	size := 0
	for _, gv := range e.prog.GlobalVars {
		// Align to 8-byte boundary
		if size%8 != 0 {
			size += 8 - (size % 8)
		}
		size += gv.Size
	}
	return size
}
