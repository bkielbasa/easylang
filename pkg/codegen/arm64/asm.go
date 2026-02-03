// Package arm64 provides ARM64 code generation for ease.
package arm64

import (
	"encoding/binary"
)

// Register constants for ARM64
type Reg uint8

const (
	X0 Reg = iota
	X1
	X2
	X3
	X4
	X5
	X6
	X7
	X8
	X9
	X10
	X11
	X12
	X13
	X14
	X15
	X16 // IP0 - intra-procedure scratch register
	X17 // IP1 - intra-procedure scratch register
	X18 // Platform register (reserved on Apple)
	X19 // Callee-saved
	X20
	X21
	X22
	X23
	X24
	X25
	X26
	X27
	X28
	X29 // FP - Frame pointer
	X30 // LR - Link register
	XZR // Zero register / SP when used in certain contexts
	SP  = XZR
)

// Condition codes for ARM64
type Cond uint8

const (
	CondEQ Cond = 0b0000 // Equal
	CondNE Cond = 0b0001 // Not equal
	CondCS Cond = 0b0010 // Carry set / unsigned higher or same
	CondCC Cond = 0b0011 // Carry clear / unsigned lower
	CondMI Cond = 0b0100 // Minus / negative
	CondPL Cond = 0b0101 // Plus / positive or zero
	CondVS Cond = 0b0110 // Overflow
	CondVC Cond = 0b0111 // No overflow
	CondHI Cond = 0b1000 // Unsigned higher
	CondLS Cond = 0b1001 // Unsigned lower or same
	CondGE Cond = 0b1010 // Signed greater or equal
	CondLT Cond = 0b1011 // Signed less than
	CondGT Cond = 0b1100 // Signed greater than
	CondLE Cond = 0b1101 // Signed less or equal
	CondAL Cond = 0b1110 // Always
)

// Assembler generates ARM64 machine code.
type Assembler struct {
	code []byte
}

// NewAssembler creates a new ARM64 assembler.
func NewAssembler() *Assembler {
	return &Assembler{
		code: make([]byte, 0, 1024),
	}
}

// Code returns the generated machine code.
func (a *Assembler) Code() []byte {
	return a.code
}

// Offset returns the current code offset.
func (a *Assembler) Offset() int {
	return len(a.code)
}

// emit appends a 32-bit instruction.
func (a *Assembler) emit(instr uint32) {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, instr)
	a.code = append(a.code, buf...)
}

// Patch patches a 32-bit instruction at the given offset.
func (a *Assembler) Patch(offset int, instr uint32) {
	binary.LittleEndian.PutUint32(a.code[offset:], instr)
}

// ============================================
// Data Processing -- Register
// ============================================

// ADD Xd, Xn, Xm
// Add two registers
func (a *Assembler) ADD(rd, rn, rm Reg) {
	// sf=1, op=0, S=0, shift=00, Rm, imm6=000000, Rn, Rd
	instr := uint32(0x8B000000) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// SUB Xd, Xn, Xm
func (a *Assembler) SUB(rd, rn, rm Reg) {
	// For SP (register 31), use extended register form which treats r31 as SP
	if rd == SP || rn == SP {
		// SUB (extended register): sf=1, op=1, S=0, 01011, opt=00, 1, Rm, option=011 (UXTX), imm3=000, Rn, Rd
		instr := uint32(0xCB206000) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
		a.emit(instr)
		return
	}
	// sf=1, op=1, S=0, shift=00, Rm, imm6=000000, Rn, Rd
	instr := uint32(0xCB000000) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// MUL Xd, Xn, Xm
func (a *Assembler) MUL(rd, rn, rm Reg) {
	// MADD Xd, Xn, Xm, XZR
	// sf=1, op54=00, op31=011, Rm, o0=0, Ra=11111, Rn, Rd
	instr := uint32(0x9B007C00) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// SDIV Xd, Xn, Xm (signed divide)
func (a *Assembler) SDIV(rd, rn, rm Reg) {
	// sf=1, op=0, S=0, opcode2=000110, Rm, opcode=000011, Rn, Rd
	instr := uint32(0x9AC00C00) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// UDIV Xd, Xn, Xm (unsigned divide)
func (a *Assembler) UDIV(rd, rn, rm Reg) {
	// sf=1, op=0, S=0, opcode2=000110, Rm, opcode=000010, Rn, Rd
	instr := uint32(0x9AC00800) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// MSUB Xd, Xn, Xm, Xa: Xd = Xa - (Xn * Xm) -- used for modulo
func (a *Assembler) MSUB(rd, rn, rm, ra Reg) {
	// sf=1, op54=00, op31=011, Rm, o0=1, Ra, Rn, Rd
	instr := uint32(0x9B008000) | uint32(rm)<<16 | uint32(ra)<<10 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// NEG Xd, Xm (negate: Xd = 0 - Xm)
func (a *Assembler) NEG(rd, rm Reg) {
	// SUB Xd, XZR, Xm (shifted register form where r31 = XZR)
	// sf=1, op=1, S=0, 01011, shift=00, 0, Rm, imm6=000000, Rn=11111, Rd
	// Note: We directly encode here instead of calling SUB because
	// SUB's SP detection would incorrectly use extended register form
	instr := uint32(0xCB000000) | uint32(rm)<<16 | uint32(XZR)<<5 | uint32(rd)
	a.emit(instr)
}

// AND Xd, Xn, Xm
func (a *Assembler) AND(rd, rn, rm Reg) {
	// sf=1, opc=00, shift=00, N=0, Rm, imm6=000000, Rn, Rd
	instr := uint32(0x8A000000) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// ORR Xd, Xn, Xm
func (a *Assembler) ORR(rd, rn, rm Reg) {
	// sf=1, opc=01, shift=00, N=0, Rm, imm6=000000, Rn, Rd
	instr := uint32(0xAA000000) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// EOR Xd, Xn, Xm (exclusive or)
func (a *Assembler) EOR(rd, rn, rm Reg) {
	// sf=1, opc=10, shift=00, N=0, Rm, imm6=000000, Rn, Rd
	instr := uint32(0xCA000000) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// MVN Xd, Xm (bitwise NOT: Xd = ~Xm)
func (a *Assembler) MVN(rd, rm Reg) {
	// ORN Xd, XZR, Xm
	// sf=1, opc=01, shift=00, N=1, Rm, imm6=000000, Rn=11111, Rd
	instr := uint32(0xAA200000) | uint32(rm)<<16 | uint32(XZR)<<5 | uint32(rd)
	a.emit(instr)
}

// LSL Xd, Xn, #shift (logical shift left by immediate)
// Encoded as UBFM Xd, Xn, #(-shift MOD 64), #(63-shift)
func (a *Assembler) LSL(rd, rn Reg, shift uint8) {
	// sf=1, opc=10, 100110, N=1, immr, imms, Rn, Rd
	// For LSL: immr = -shift MOD 64, imms = 63 - shift
	immr := uint32((64 - shift) & 0x3F)
	imms := uint32(63 - shift)
	instr := uint32(0xD3400000) | immr<<16 | imms<<10 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// LSR Xd, Xn, #shift (logical shift right by immediate)
// Encoded as UBFM Xd, Xn, #shift, #63
func (a *Assembler) LSR(rd, rn Reg, shift uint8) {
	// sf=1, opc=10, 100110, N=1, immr, imms=63, Rn, Rd
	immr := uint32(shift & 0x3F)
	imms := uint32(63)
	instr := uint32(0xD3400000) | immr<<16 | imms<<10 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// ============================================
// Data Processing -- Immediate
// ============================================

// ADDi Xd, Xn, imm12
func (a *Assembler) ADDi(rd, rn Reg, imm12 uint16) {
	// sf=1, op=0, S=0, 100010, sh=0, imm12, Rn, Rd
	instr := uint32(0x91000000) | uint32(imm12&0xFFF)<<10 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// SUBi Xd, Xn, imm12
func (a *Assembler) SUBi(rd, rn Reg, imm12 uint16) {
	// sf=1, op=1, S=0, 100010, sh=0, imm12, Rn, Rd
	instr := uint32(0xD1000000) | uint32(imm12&0xFFF)<<10 | uint32(rn)<<5 | uint32(rd)
	a.emit(instr)
}

// MOVimm Xd, imm16 (move immediate, 16-bit)
// For small values, uses MOVZ; for larger values may need MOVK sequences
func (a *Assembler) MOVimm(rd Reg, imm int64) {
	if imm >= 0 && imm < 65536 {
		// MOVZ Xd, imm16, LSL #0
		a.MOVZ(rd, uint16(imm), 0)
	} else if imm < 0 && imm >= -65536 {
		// MOVN Xd, ~imm16, LSL #0
		a.MOVN(rd, uint16(^imm), 0)
	} else {
		// Full 64-bit immediate using MOVZ + MOVK sequence
		a.MOVZ(rd, uint16(imm&0xFFFF), 0)
		if (imm>>16)&0xFFFF != 0 {
			a.MOVK(rd, uint16((imm>>16)&0xFFFF), 1)
		}
		if (imm>>32)&0xFFFF != 0 {
			a.MOVK(rd, uint16((imm>>32)&0xFFFF), 2)
		}
		if (imm>>48)&0xFFFF != 0 {
			a.MOVK(rd, uint16((imm>>48)&0xFFFF), 3)
		}
	}
}

// MOVZ Xd, imm16, LSL #(hw*16)
func (a *Assembler) MOVZ(rd Reg, imm16 uint16, hw uint8) {
	// sf=1, opc=10, 100101, hw, imm16, Rd
	instr := uint32(0xD2800000) | uint32(hw&3)<<21 | uint32(imm16)<<5 | uint32(rd)
	a.emit(instr)
}

// MOVN Xd, imm16, LSL #(hw*16)
func (a *Assembler) MOVN(rd Reg, imm16 uint16, hw uint8) {
	// sf=1, opc=00, 100101, hw, imm16, Rd
	instr := uint32(0x92800000) | uint32(hw&3)<<21 | uint32(imm16)<<5 | uint32(rd)
	a.emit(instr)
}

// MOVK Xd, imm16, LSL #(hw*16)
func (a *Assembler) MOVK(rd Reg, imm16 uint16, hw uint8) {
	// sf=1, opc=11, 100101, hw, imm16, Rd
	instr := uint32(0xF2800000) | uint32(hw&3)<<21 | uint32(imm16)<<5 | uint32(rd)
	a.emit(instr)
}

// MOV Xd, Xm (alias for ORR Xd, XZR, Xm)
// For SP, uses ADD Xd, SP, #0 since ORR treats r31 as XZR
func (a *Assembler) MOV(rd, rm Reg) {
	if rm == SP || rd == SP {
		// Use ADD with immediate 0, which treats r31 as SP
		a.ADDi(rd, rm, 0)
		return
	}
	a.ORR(rd, XZR, rm)
}

// ============================================
// Comparison
// ============================================

// CMP Xn, Xm (compare registers, sets flags)
func (a *Assembler) CMP(rn, rm Reg) {
	// SUBS XZR, Xn, Xm
	// sf=1, op=1, S=1, shift=00, Rm, imm6=000000, Rn, Rd=11111
	instr := uint32(0xEB000000) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(XZR)
	a.emit(instr)
}

// CMPi Xn, imm12 (compare with immediate)
func (a *Assembler) CMPi(rn Reg, imm12 uint16) {
	// SUBS XZR, Xn, imm12
	// sf=1, op=1, S=1, 100010, sh=0, imm12, Rn, Rd=11111
	instr := uint32(0xF1000000) | uint32(imm12&0xFFF)<<10 | uint32(rn)<<5 | uint32(XZR)
	a.emit(instr)
}

// CMN Xn, Xm (compare negative: Xn + Xm, sets flags)
func (a *Assembler) CMN(rn, rm Reg) {
	// ADDS XZR, Xn, Xm
	// sf=1, op=0, S=1, shift=00, Rm, imm6=000000, Rn, Rd=11111
	instr := uint32(0xAB000000) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(XZR)
	a.emit(instr)
}

// CMNi Xn, imm12 (compare negative with immediate)
func (a *Assembler) CMNi(rn Reg, imm12 uint16) {
	// ADDS XZR, Xn, imm12
	// sf=1, op=0, S=1, 100010, sh=0, imm12, Rn, Rd=11111
	instr := uint32(0xB1000000) | uint32(imm12&0xFFF)<<10 | uint32(rn)<<5 | uint32(XZR)
	a.emit(instr)
}

// CSET Xd, cond (conditional set: Xd = cond ? 1 : 0)
func (a *Assembler) CSET(rd Reg, cond Cond) {
	// CSINC Xd, XZR, XZR, invert(cond)
	// sf=1, op=0, S=0, 11010100, Rm=11111, cond, 01, Rn=11111, Rd
	invCond := cond ^ 1 // invert condition
	// 0x9A9F07E0: Rm=XZR(31), o2=01, Rn=XZR(31), Rd=0
	instr := uint32(0x9A9F07E0) | uint32(invCond)<<12 | uint32(rd)
	a.emit(instr)
}

// ============================================
// Branches
// ============================================

// B offset (unconditional branch, PC-relative)
// offset is in bytes, will be converted to instruction count
func (a *Assembler) B(offset int32) {
	// 000101, imm26
	imm26 := (offset >> 2) & 0x3FFFFFF
	instr := uint32(0x14000000) | uint32(imm26)
	a.emit(instr)
}

// Bcond offset (conditional branch)
func (a *Assembler) Bcond(cond Cond, offset int32) {
	// 01010100, imm19, 0, cond
	imm19 := (offset >> 2) & 0x7FFFF
	instr := uint32(0x54000000) | uint32(imm19)<<5 | uint32(cond)
	a.emit(instr)
}

// Bcond_instr returns the encoding for B.cond without emitting it
func (a *Assembler) Bcond_instr(cond Cond, imm19 int32) uint32 {
	// 01010100, imm19, 0, cond
	return uint32(0x54000000) | uint32(imm19&0x7FFFF)<<5 | uint32(cond)
}

// B_instr returns the encoding for B without emitting it
func (a *Assembler) B_instr(imm26 int32) uint32 {
	// 000101, imm26
	return uint32(0x14000000) | uint32(imm26&0x3FFFFFF)
}

// BL offset (branch with link, for function calls)
func (a *Assembler) BL(offset int32) {
	// 100101, imm26
	imm26 := (offset >> 2) & 0x3FFFFFF
	instr := uint32(0x94000000) | uint32(imm26)
	a.emit(instr)
}

// BLR Xn (branch with link to register)
func (a *Assembler) BLR(rn Reg) {
	// 1101011 0001 11111 000000 Rn 00000
	instr := uint32(0xD63F0000) | uint32(rn)<<5
	a.emit(instr)
}

// BR Xn (branch to register)
func (a *Assembler) BR(rn Reg) {
	// 1101011 0000 11111 000000 Rn 00000
	instr := uint32(0xD61F0000) | uint32(rn)<<5
	a.emit(instr)
}

// RET (return, branch to LR)
func (a *Assembler) RET() {
	// RET X30
	// 1101011 0010 11111 000000 11110 00000
	instr := uint32(0xD65F03C0)
	a.emit(instr)
}

// CBZ Xn, offset (compare and branch if zero)
func (a *Assembler) CBZ(rn Reg, offset int32) {
	// sf=1, 011010 0, imm19, Rt
	imm19 := (offset >> 2) & 0x7FFFF
	instr := uint32(0xB4000000) | uint32(imm19)<<5 | uint32(rn)
	a.emit(instr)
}

// CBNZ Xn, offset (compare and branch if not zero)
func (a *Assembler) CBNZ(rn Reg, offset int32) {
	// sf=1, 011010 1, imm19, Rt
	imm19 := (offset >> 2) & 0x7FFFF
	instr := uint32(0xB5000000) | uint32(imm19)<<5 | uint32(rn)
	a.emit(instr)
}

// ============================================
// PC-Relative Addressing
// ============================================

// ADR Xd, #imm21 (load PC-relative address)
// The immediate is a signed 21-bit offset from PC
func (a *Assembler) ADR(rd Reg, imm21 int32) {
	// op=0, immlo[1:0], 10000, immhi[18:0], Rd[4:0]
	// Final offset = (immhi << 2) | immlo
	immlo := uint32(imm21 & 0x3)
	immhi := uint32((imm21 >> 2) & 0x7FFFF)
	instr := uint32(0x10000000) | (immlo << 29) | (immhi << 5) | uint32(rd)
	a.emit(instr)
}

// ============================================
// Load/Store
// ============================================

// LDR Xt, [Xn, #imm] (load register, unsigned offset)
func (a *Assembler) LDR(rt, rn Reg, imm12 uint16) {
	// size=11, 111 0 01 01, imm12, Rn, Rt
	// imm12 is scaled by 8 for 64-bit loads
	instr := uint32(0xF9400000) | uint32(imm12>>3)<<10 | uint32(rn)<<5 | uint32(rt)
	a.emit(instr)
}

// LDRr Xt, [Xn, Xm] (load register, register offset)
func (a *Assembler) LDRr(rt, rn, rm Reg) {
	// size=11, 111 0 00 01 1, Rm, option=011, S=1, 10, Rn, Rt
	// This is LDR (register) with LSL #3 (S=1, size=11)
	instr := uint32(0xF8606800) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rt)
	a.emit(instr)
}

// LDRx Xt, [Xn] (load register, zero offset - unscaled)
func (a *Assembler) LDRx(rt, rn Reg) {
	// LDUR Xt, [Xn, #0] - unscaled immediate
	// size=11, 111 0 00 01 0, imm9=0, 00, Rn, Rt
	instr := uint32(0xF8400000) | uint32(rn)<<5 | uint32(rt)
	a.emit(instr)
}

// LDRB Wt, [Xn, #imm] (load byte, unsigned offset, zero-extend)
func (a *Assembler) LDRB(rt, rn Reg, imm12 uint16) {
	// size=00, 111 0 01 01, imm12, Rn, Rt
	// LDRB (unsigned offset) - loads byte, zero extends to 64-bit
	instr := uint32(0x39400000) | uint32(imm12)<<10 | uint32(rn)<<5 | uint32(rt)
	a.emit(instr)
}

// STRB Wt, [Xn, #imm] (store byte, unsigned offset)
func (a *Assembler) STRB(rt, rn Reg, imm12 uint16) {
	// size=00, 111 0 01 00, imm12, Rn, Rt
	// STRB (unsigned offset) - stores low 8 bits
	instr := uint32(0x39000000) | uint32(imm12)<<10 | uint32(rn)<<5 | uint32(rt)
	a.emit(instr)
}

// STR Xt, [Xn, #imm] (store register, unsigned offset)
func (a *Assembler) STR(rt, rn Reg, imm12 uint16) {
	// size=11, 111 0 01 00, imm12, Rn, Rt
	instr := uint32(0xF9000000) | uint32(imm12>>3)<<10 | uint32(rn)<<5 | uint32(rt)
	a.emit(instr)
}

// STRr Xt, [Xn, Xm] (store register, register offset)
func (a *Assembler) STRr(rt, rn, rm Reg) {
	// size=11, 111 0 00 00 1, Rm, option=011, S=1, 10, Rn, Rt
	instr := uint32(0xF8206800) | uint32(rm)<<16 | uint32(rn)<<5 | uint32(rt)
	a.emit(instr)
}

// STRx Xt, [Xn] (store register, zero offset - unscaled)
func (a *Assembler) STRx(rt, rn Reg) {
	// STUR Xt, [Xn, #0] - unscaled immediate
	// size=11, 111 0 00 00 0, imm9=0, 00, Rn, Rt
	instr := uint32(0xF8000000) | uint32(rn)<<5 | uint32(rt)
	a.emit(instr)
}

// LDP Xt1, Xt2, [Xn, #imm] (load pair)
func (a *Assembler) LDP(rt1, rt2, rn Reg, imm7 int16) {
	// opc=10, 101 0 001 1, imm7, Rt2, Rn, Rt1
	// imm7 is scaled by 8, signed
	simm7 := (imm7 >> 3) & 0x7F
	instr := uint32(0xA9400000) | uint32(simm7)<<15 | uint32(rt2)<<10 | uint32(rn)<<5 | uint32(rt1)
	a.emit(instr)
}

// STP Xt1, Xt2, [Xn, #imm] (store pair)
func (a *Assembler) STP(rt1, rt2, rn Reg, imm7 int16) {
	// opc=10, 101 0 001 0, imm7, Rt2, Rn, Rt1
	simm7 := (imm7 >> 3) & 0x7F
	instr := uint32(0xA9000000) | uint32(simm7)<<15 | uint32(rt2)<<10 | uint32(rn)<<5 | uint32(rt1)
	a.emit(instr)
}

// LDPpre Xt1, Xt2, [Xn, #imm]! (load pair, pre-index)
func (a *Assembler) LDPpre(rt1, rt2, rn Reg, imm7 int16) {
	// opc=10, 101 0 011 1, imm7, Rt2, Rn, Rt1
	simm7 := (imm7 >> 3) & 0x7F
	instr := uint32(0xA9C00000) | uint32(simm7)<<15 | uint32(rt2)<<10 | uint32(rn)<<5 | uint32(rt1)
	a.emit(instr)
}

// STPpre Xt1, Xt2, [Xn, #imm]! (store pair, pre-index)
func (a *Assembler) STPpre(rt1, rt2, rn Reg, imm7 int16) {
	// opc=10, 101 0 011 0, imm7, Rt2, Rn, Rt1
	simm7 := (imm7 >> 3) & 0x7F
	instr := uint32(0xA9800000) | uint32(simm7)<<15 | uint32(rt2)<<10 | uint32(rn)<<5 | uint32(rt1)
	a.emit(instr)
}

// LDPpost Xt1, Xt2, [Xn], #imm (load pair, post-index)
func (a *Assembler) LDPpost(rt1, rt2, rn Reg, imm7 int16) {
	// opc=10, 101 0 001 1, imm7, Rt2, Rn, Rt1  (with post-index encoding)
	simm7 := (imm7 >> 3) & 0x7F
	instr := uint32(0xA8C00000) | uint32(simm7)<<15 | uint32(rt2)<<10 | uint32(rn)<<5 | uint32(rt1)
	a.emit(instr)
}

// STPpost Xt1, Xt2, [Xn], #imm (store pair, post-index)
func (a *Assembler) STPpost(rt1, rt2, rn Reg, imm7 int16) {
	// opc=10, 101 0 000 0, imm7, Rt2, Rn, Rt1  (with post-index encoding)
	simm7 := (imm7 >> 3) & 0x7F
	instr := uint32(0xA8800000) | uint32(simm7)<<15 | uint32(rt2)<<10 | uint32(rn)<<5 | uint32(rt1)
	a.emit(instr)
}

// ============================================
// System
// ============================================

// NOP
func (a *Assembler) NOP() {
	// 11010101000000110010000000011111
	a.emit(0xD503201F)
}

// BRK #imm16 (breakpoint)
func (a *Assembler) BRK(imm16 uint16) {
	// 11010100 001 imm16 00000
	instr := uint32(0xD4200000) | uint32(imm16)<<5
	a.emit(instr)
}

// SVC #imm16 (supervisor call / syscall)
func (a *Assembler) SVC(imm16 uint16) {
	// 11010100 000 imm16 00001
	instr := uint32(0xD4000001) | uint32(imm16)<<5
	a.emit(instr)
}
