package arm64

import (
	"ease/pkg/ir"
	"ease/pkg/types"
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
	numArgRegs   = 8 // x0-x7
	numTempRegs  = 0 // Spill all vregs to stack for correctness across calls
	numSavedRegs = 10 // x19-x28
)

// Emitter converts IR to ARM64 machine code.
type Emitter struct {
	asm         *Assembler
	fn          *ir.Function
	prog        *ir.Program    // program being emitted (for string table)
	vregToReg   map[int]Reg    // virtual register to physical register
	vregSpill   map[int]int    // virtual register to stack offset
	allocOffset map[int]int    // vreg to allocation offset (for OpAlloc results)
	labels      map[string]int // label to code offset (block labels)
	funcOffsets map[string]int // function name to code offset
	fixups      []fixup        // branch fixups
	strFixups   []strFixup     // string address fixups
	stackSize   int            // total stack size
}

type strFixup struct {
	offset int // offset of ADR instruction
	strIdx int // index into program's string table
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
		asm:         NewAssembler(),
		vregToReg:   make(map[int]Reg),
		vregSpill:   make(map[int]int),
		labels:      make(map[string]int),
		funcOffsets: make(map[string]int),
	}
}

// EmitProgram generates ARM64 code for the entire IR program.
// funcOffsets is populated with function name -> code offset mappings.
func (e *Emitter) EmitProgram(prog *ir.Program, funcOffsets map[string]int64) []byte {
	e.funcOffsets = make(map[string]int)
	e.prog = prog

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

// FixupStrings patches ADR instructions for string constants.
// codeSize is the total size of the code section.
// stringOffsets[i] is the offset of strings[i] from the start of the code section.
func (e *Emitter) FixupStrings(stringOffsets []uint64) {
	for _, fix := range e.strFixups {
		// String is at stringOffsets[fix.strIdx] from code start
		// ADR instruction is at fix.offset from code start
		// Offset = stringOffset - instrOffset
		strOffset := int32(stringOffsets[fix.strIdx])
		instrOffset := int32(fix.offset)
		offset := strOffset - instrOffset

		// Re-encode ADR instruction with correct offset
		immlo := uint32(offset & 0x3)
		immhi := uint32((offset >> 2) & 0x7FFFF)
		// Read existing instruction to get Rd
		oldInstr := uint32(e.asm.code[fix.offset]) |
			uint32(e.asm.code[fix.offset+1])<<8 |
			uint32(e.asm.code[fix.offset+2])<<16 |
			uint32(e.asm.code[fix.offset+3])<<24
		rd := oldInstr & 0x1F
		instr := uint32(0x10000000) | (immlo << 29) | (immhi << 5) | rd
		e.asm.Patch(fix.offset, instr)
	}
}

func (e *Emitter) emitFunction(fn *ir.Function) {
	e.fn = fn
	e.labels = make(map[string]int)
	e.vregToReg = make(map[int]Reg)
	e.vregSpill = make(map[int]int)
	e.allocOffset = make(map[int]int)

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

func (e *Emitter) calculateStackLayout() {
	// Count how many vregs we need to spill
	maxVReg := e.fn.NextVReg
	numSpills := 0

	// Simple allocation: first 7 vregs go to x9-x15, rest spill
	for vreg := 0; vreg < maxVReg; vreg++ {
		if vreg < numTempRegs {
			e.vregToReg[vreg] = Reg(X9 + Reg(vreg))
		} else {
			e.vregSpill[vreg] = numSpills * 8
			numSpills++
		}
	}

	// Stack layout:
	// [sp]        saved x29, x30 (16 bytes)
	// [sp+16]     spilled vregs
	spillSize := numSpills * 8

	// Scan for OpAlloc instructions and pre-allocate space
	allocSize := 0
	for _, block := range e.fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op == ir.OpAlloc {
				// Track the allocation offset for this vreg
				// Allocations come after spills
				e.allocOffset[instr.Dest.VReg] = 16 + spillSize + allocSize

				// Get allocation size from the immediate argument
				size := int(instr.Args[0].Imm)
				// Round up to 8-byte alignment
				alignedSize := (size + 7) & ^7
				allocSize += alignedSize
			}
		}
	}

	e.stackSize = 16 + spillSize + allocSize

	// Align to 16 bytes
	e.stackSize = (e.stackSize + 15) & ^15
}

func (e *Emitter) emitPrologue() {
	// STP pre-index has limited range (-512 to 504 bytes)
	// For larger stacks, we need to adjust SP separately
	if e.stackSize <= 504 {
		// stp x29, x30, [sp, #-stackSize]!  (save FP and LR, allocate stack)
		e.asm.STPpre(X29, X30, SP, int16(-e.stackSize))
	} else {
		// For large stacks: first allocate, then save FP/LR
		e.asm.SUBi(SP, SP, uint16(e.stackSize))
		e.asm.STP(X29, X30, SP, 0)
	}

	// mov x29, sp  (set up frame pointer)
	// Need to use ADD since MOV(x29, SP) doesn't work correctly with SP
	e.asm.ADDi(X29, SP, 0)
}

func (e *Emitter) emitEpilogue() {
	// Restore SP from FP in case of dynamic stack allocation
	// FP points to where we saved FP/LR, SP might have been modified
	e.asm.MOV(SP, X29)

	// LDP post-index has limited range (-512 to 504 bytes)
	// For larger stacks, we need to adjust SP separately
	if e.stackSize <= 504 {
		// ldp x29, x30, [sp], #stackSize  (restore FP and LR, deallocate stack)
		e.asm.LDPpost(X29, X30, SP, int16(e.stackSize))
	} else {
		// For large stacks: restore FP/LR, then deallocate
		e.asm.LDP(X29, X30, SP, 0)
		e.asm.ADDi(SP, SP, uint16(e.stackSize))
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
		e.storeToVReg(param.VReg, argReg)
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
	case ir.OpSyscallOpen:
		e.emitSyscallOpen(instr)
	case ir.OpSyscallRead:
		e.emitSyscallRead(instr)
	case ir.OpSyscallWrite:
		e.emitSyscallWrite(instr)
	case ir.OpSyscallClose:
		e.emitSyscallClose(instr)
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

	// X16 = left / right
	e.asm.SDIV(X16, left, right)
	// X16 = left - X16 * right
	e.asm.MSUB(X16, X16, right, left)
	e.storeToVReg(instr.Dest.VReg, X16)
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

// emitStrConcat concatenates two strings, allocating space on the stack.
// Result pointer is valid until function returns.
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

	// Align to 16 bytes: size = (size + 15) & ~15
	e.asm.ADDi(X23, X23, 15)
	e.asm.MOVimm(X11, -16) // ~15 in two's complement
	e.asm.AND(X23, X23, X11)

	// Allocate stack space: SP = SP - size
	e.asm.SUB(SP, SP, X23)
	// Save result pointer (current SP)
	e.asm.MOV(X24, SP)

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

	// Allocate size = length + 1, aligned to 16
	e.asm.ADDi(X23, X22, 1)
	e.asm.ADDi(X23, X23, 15)
	e.asm.MOVimm(X11, -16)
	e.asm.AND(X23, X23, X11)

	// Grow stack
	e.asm.SUB(SP, SP, X23)
	e.asm.MOV(X24, SP) // Save result pointer

	// Calculate source address: str + start
	e.asm.ADD(X9, X19, X20)
	// Destination is X24
	e.asm.MOV(X10, X24)

	// Copy loop: copy X22 bytes
	e.asm.MOV(X25, X22) // Counter

	// If length is 0, skip copy
	e.asm.CMP(X25, XZR)
	skipCopy := e.asm.Offset()
	e.asm.Bcond(CondEQ, 0)

	copyLoop := e.asm.Offset()
	e.asm.LDRB(X11, X9, 0)
	e.asm.STRB(X11, X10, 0)
	e.asm.ADDi(X9, X9, 1)
	e.asm.ADDi(X10, X10, 1)
	e.asm.SUBi(X25, X25, 1)
	e.asm.CMP(X25, XZR)
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
		e.storeToVReg(instr.Dest.VReg, X0)
	}
}

func (e *Emitter) emitReturn(instr *ir.Instr) {
	// Move return value to x0
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
		// String constant - emit ADR with placeholder, fixup later
		e.strFixups = append(e.strFixups, strFixup{
			offset: e.asm.Offset(),
			strIdx: op.StrIdx,
		})
		e.asm.ADR(scratch, 0) // placeholder
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
	e.asm.ADDi(X16, X29, uint16(offset))
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
	// OpArrayPush: push element to array
	// Args[0] = array fat pointer, Args[1] = element, Args[2] = element size
	fatPtr := e.loadOperand(instr.Args[0], X19)
	e.asm.MOV(X19, fatPtr)
	elem := e.loadOperand(instr.Args[1], X20)
	e.asm.MOV(X20, elem)
	elemSize := e.loadOperand(instr.Args[2], X21)
	e.asm.MOV(X21, elemSize)

	// Load current length
	e.asm.LDR(X22, X19, 8) // len at offset 8

	// Compute element address: ptr + len * elemSize
	e.asm.LDRx(X23, X19) // data ptr at offset 0
	e.asm.MUL(X24, X22, X21) // offset = len * elemSize
	e.asm.ADD(X25, X23, X24) // addr = ptr + offset

	// Store element at computed address
	e.asm.STRx(X20, X25)

	// Increment length
	e.asm.ADDi(X22, X22, 1)

	// Store new length back
	e.asm.ADDi(X26, X19, 8) // len address
	e.asm.STRx(X22, X26)
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

// emitReadFile reads a file and returns its contents as a string
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

	// Allocate buffer on stack (64KB, 16-byte aligned)
	// SUBi can only handle 12-bit immediate, so use register
	bufSize := int64(65536)
	e.asm.MOVimm(X17, bufSize)
	e.asm.SUB(SP, SP, X17)
	e.asm.MOV(X21, SP)             // X21 = buffer address

	// read(fd, buf, bufSize)
	e.asm.MOV(X0, X20)             // fd
	e.asm.MOV(X1, X21)             // buf
	e.asm.MOVimm(X2, bufSize-1)    // count (leave room for null)
	e.asm.MOVimm(X16, 0x2000003)   // syscall read
	e.asm.SVC(0x80)
	e.asm.MOV(X22, X0)             // Save bytes read in X22

	// Null-terminate the string
	e.asm.ADD(X23, X21, X22)       // end of data
	e.asm.STRB(XZR, X23, 0)        // write null byte

	// close(fd)
	e.asm.MOV(X0, X20)             // fd
	e.asm.MOVimm(X16, 0x2000006)   // syscall close
	e.asm.SVC(0x80)

	// Note: Stack buffer is NOT deallocated here.
	// The function epilogue will restore SP from FP, cleaning it up.
	// This means the string is valid until the function returns.
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
func (e *Emitter) emitIntToStr(instr *ir.Instr) {
	// Load the integer value
	num := e.loadOperand(instr.Args[0], X19)
	e.asm.MOV(X19, num) // Save in callee-saved register

	// Allocate 24 bytes on stack (enough for "-9223372036854775808\0" = 21 chars, aligned to 16)
	// We'll build the string from the end backwards
	e.asm.SUBi(SP, SP, 32)
	e.asm.MOV(X20, SP) // X20 = buffer start

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
	e.asm.MSUB(X25, X24, X23, X19) // X25 = n - (n/10)*10 = n % 10

	// Convert digit to ASCII: '0' + digit
	e.asm.ADDi(X25, X25, '0')

	// Move write pointer back and store digit
	e.asm.SUBi(X21, X21, 1)
	e.asm.STRB(X25, X21, 0)

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

