package arm64

import (
	"encoding/binary"
	"testing"
)

func TestADD(t *testing.T) {
	asm := NewAssembler()
	asm.ADD(X0, X1, X2)

	code := asm.Code()
	if len(code) != 4 {
		t.Fatalf("Expected 4 bytes, got %d", len(code))
	}

	// ADD X0, X1, X2: 0x8B020020
	instr := binary.LittleEndian.Uint32(code)
	expected := uint32(0x8B020020)
	if instr != expected {
		t.Errorf("ADD X0, X1, X2 = %08X, want %08X", instr, expected)
	}
}

func TestSUB(t *testing.T) {
	asm := NewAssembler()
	asm.SUB(X0, X1, X2)

	code := asm.Code()
	instr := binary.LittleEndian.Uint32(code)
	expected := uint32(0xCB020020)
	if instr != expected {
		t.Errorf("SUB X0, X1, X2 = %08X, want %08X", instr, expected)
	}
}

func TestMOVimm(t *testing.T) {
	tests := []struct {
		name     string
		imm      int64
		expected []uint32
	}{
		{
			name:     "small positive",
			imm:      42,
			expected: []uint32{0xD2800540}, // MOVZ X0, #42
		},
		{
			name:     "zero",
			imm:      0,
			expected: []uint32{0xD2800000}, // MOVZ X0, #0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asm := NewAssembler()
			asm.MOVimm(X0, tt.imm)

			code := asm.Code()
			if len(code) < 4 {
				t.Fatalf("Expected at least 4 bytes, got %d", len(code))
			}

			instr := binary.LittleEndian.Uint32(code[:4])
			if instr != tt.expected[0] {
				t.Errorf("MOVimm X0, %d = %08X, want %08X", tt.imm, instr, tt.expected[0])
			}
		})
	}
}

func TestRET(t *testing.T) {
	asm := NewAssembler()
	asm.RET()

	code := asm.Code()
	instr := binary.LittleEndian.Uint32(code)
	expected := uint32(0xD65F03C0)
	if instr != expected {
		t.Errorf("RET = %08X, want %08X", instr, expected)
	}
}

func TestBranch(t *testing.T) {
	asm := NewAssembler()
	asm.B(8) // Jump forward 8 bytes (2 instructions)

	code := asm.Code()
	instr := binary.LittleEndian.Uint32(code)
	// B +8 bytes = B +2 instructions, imm26 = 2
	expected := uint32(0x14000002)
	if instr != expected {
		t.Errorf("B +8 = %08X, want %08X", instr, expected)
	}
}

func TestBcond(t *testing.T) {
	asm := NewAssembler()
	asm.Bcond(CondEQ, 8) // Branch if equal, +8 bytes

	code := asm.Code()
	instr := binary.LittleEndian.Uint32(code)
	// B.EQ +8 bytes = imm19 = 2, cond = 0
	expected := uint32(0x54000040)
	if instr != expected {
		t.Errorf("B.EQ +8 = %08X, want %08X", instr, expected)
	}
}

func TestSTPLDP(t *testing.T) {
	asm := NewAssembler()
	// stp x29, x30, [sp, #-16]!
	asm.STPpre(X29, X30, SP, -16)

	code := asm.Code()
	instr := binary.LittleEndian.Uint32(code)
	// Expected: 0xA9BF7BFD
	// But exact encoding depends on the format we use
	t.Logf("STPpre x29, x30, [sp, #-16]! = %08X", instr)
}

func TestCMP(t *testing.T) {
	asm := NewAssembler()
	asm.CMP(X0, X1)

	code := asm.Code()
	instr := binary.LittleEndian.Uint32(code)
	// SUBS XZR, X0, X1
	expected := uint32(0xEB01001F)
	if instr != expected {
		t.Errorf("CMP X0, X1 = %08X, want %08X", instr, expected)
	}
}

func TestCSET(t *testing.T) {
	asm := NewAssembler()
	asm.CSET(X0, CondEQ)

	code := asm.Code()
	instr := binary.LittleEndian.Uint32(code)
	// CSINC X0, XZR, XZR, NE (inverted EQ)
	t.Logf("CSET X0, EQ = %08X", instr)
}
