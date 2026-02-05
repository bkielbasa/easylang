// Package macho provides Mach-O binary file generation for ease.
package macho

import (
	"bytes"
	"encoding/binary"
)

// Mach-O constants
const (
	MH_MAGIC_64           = 0xfeedfacf
	MH_EXECUTE            = 0x2
	MH_NOUNDEFS           = 0x1
	MH_DYLDLINK           = 0x4
	MH_TWOLEVEL           = 0x80
	MH_PIE                = 0x200000
	CPU_TYPE_ARM64        = 0x100000C
	CPU_SUBTYPE_ARM64_ALL = 0x0

	LC_SEGMENT_64     = 0x19
	LC_SYMTAB         = 0x2
	LC_DYSYMTAB       = 0xb
	LC_LOAD_DYLINKER  = 0xe
	LC_UUID           = 0x1b
	LC_BUILD_VERSION  = 0x32
	LC_SOURCE_VERSION = 0x2A
	LC_MAIN           = 0x80000028
	LC_LOAD_DYLIB     = 0xc
	LC_CODE_SIGNATURE = 0x1d

	VM_PROT_NONE    = 0x0
	VM_PROT_READ    = 0x1
	VM_PROT_WRITE   = 0x2
	VM_PROT_EXECUTE = 0x4

	S_REGULAR                = 0x0
	S_ATTR_PURE_INSTRUCTIONS = 0x80000000
	S_ATTR_SOME_INSTRUCTIONS = 0x00000400

	N_EXT  = 0x01
	N_SECT = 0x0e

	PLATFORM_MACOS = 1

	// Page size on Apple Silicon
	PageSize = 0x4000 // 16KB
)

// Writer writes Mach-O binary files.
type Writer struct {
	buf     bytes.Buffer
	code    []byte
	strings []string       // string constants (will be placed after code)
	globals map[string]int64 // global variables (name -> initial value)
	symbols []Symbol
	mainOff int64
}

// Symbol represents a symbol in the binary.
type Symbol struct {
	Name    string
	Offset  int64
	Section int
	Extern  bool
}

// NewWriter creates a new Mach-O writer.
func NewWriter() *Writer {
	return &Writer{
		globals: make(map[string]int64),
	}
}

// SetCode sets the executable code.
func (w *Writer) SetCode(code []byte) {
	w.code = code
}

// SetStrings sets the string constants that will be placed after the code.
func (w *Writer) SetStrings(strings []string) {
	w.strings = strings
}

// StringOffset returns the file offset of a string constant (relative to code start).
// This should be called after SetCode.
func (w *Writer) StringOffset(idx int) uint64 {
	offset := uint64(len(w.code))
	for i := 0; i < idx; i++ {
		offset += uint64(len(w.strings[i]) + 1) // +1 for null terminator
	}
	return offset
}

// StringsSize returns the total size of all string constants.
func (w *Writer) StringsSize() uint64 {
	var size uint64
	for _, s := range w.strings {
		size += uint64(len(s) + 1) // +1 for null terminator
	}
	return size
}

// SetGlobal sets a global variable with its initial value.
func (w *Writer) SetGlobal(name string, value int64) {
	w.globals[name] = value
}

// GlobalOffset returns the file offset of a global variable (relative to code start).
// Globals are placed after strings in memory.
func (w *Writer) GlobalOffset(name string) uint64 {
	// Start after code and strings
	offset := uint64(len(w.code)) + w.StringsSize()

	// Add padding to align to 8-byte boundary
	if offset%8 != 0 {
		offset += 8 - (offset % 8)
	}

	// For now, allocate globals sequentially (8 bytes each for int64)
	// In a real implementation, we'd track each global's offset properly
	// For simplicity, just return a placeholder offset
	// TODO: Implement proper global allocation
	return offset
}

// GlobalsSize returns the total size of all global variables.
func (w *Writer) GlobalsSize() uint64 {
	// Each global is 8 bytes (int64)
	return uint64(len(w.globals) * 8)
}

// SetMainOffset sets the offset of the main function within the code.
func (w *Writer) SetMainOffset(off int64) {
	w.mainOff = off
}

// AddSymbol adds a symbol to the binary.
func (w *Writer) AddSymbol(name string, offset int64, section int, extern bool) {
	w.symbols = append(w.symbols, Symbol{
		Name:    name,
		Offset:  offset,
		Section: section,
		Extern:  extern,
	})
}

// Write generates the Mach-O binary and returns the bytes.
func (w *Writer) Write() []byte {
	w.buf.Reset()

	// Section size includes code and string constants
	codeSize := uint64(len(w.code)) + w.StringsSize()

	// Build string table first
	strTable := []byte{0}
	strOffsets := make(map[string]uint32)

	var hasMain bool
	for _, sym := range w.symbols {
		if sym.Name == "_main" {
			hasMain = true
			break
		}
	}

	for _, sym := range w.symbols {
		strOffsets[sym.Name] = uint32(len(strTable))
		strTable = append(strTable, sym.Name...)
		strTable = append(strTable, 0)
	}

	if !hasMain {
		strOffsets["_main"] = uint32(len(strTable))
		strTable = append(strTable, "_main"...)
		strTable = append(strTable, 0)
	}

	numSyms := len(w.symbols)
	if numSyms == 0 {
		numSyms = 1
	}

	// Calculate load commands size
	// Header: 32 bytes
	// __PAGEZERO: 72 bytes
	// __TEXT + 1 section: 72 + 80 = 152 bytes
	// __LINKEDIT: 72 bytes
	// LC_SYMTAB: 24 bytes
	// LC_DYSYMTAB: 80 bytes
	// LC_LOAD_DYLINKER: 32 bytes (aligned)
	// LC_MAIN: 24 bytes
	// LC_BUILD_VERSION: 24 bytes
	// LC_SOURCE_VERSION: 16 bytes
	// Reserve 16 bytes for LC_CODE_SIGNATURE (added by codesign)
	headerSize := uint64(32)
	loadCmdsSize := uint64(72 + 152 + 72 + 24 + 80 + 32 + 24 + 24 + 16)
	numCmds := uint32(9)

	// Code starts well after load commands to leave room for codesign to add LC_CODE_SIGNATURE
	// Align to 256 bytes to be safe (C binaries typically have code at ~800+ offset)
	codeFileOff := alignUp(headerSize+loadCmdsSize+64, 256) // +64 for LC_CODE_SIGNATURE and padding
	codeVMAddr := uint64(0x100000000) + codeFileOff // VM address matches file offset within __TEXT

	// __TEXT segment must be large enough to contain all code and strings
	// Round up to page boundary for proper memory mapping
	textSegFileSize := alignUp(codeFileOff+codeSize, uint64(PageSize))
	textSegVMSize := textSegFileSize

	// __LINKEDIT comes after __TEXT
	linkeditFileOff := textSegFileSize
	linkeditVMAddr := uint64(0x100000000) + linkeditFileOff

	symtabOff := linkeditFileOff
	strtabOff := symtabOff + uint64(numSyms*16)
	linkeditSize := uint64(numSyms*16) + uint64(len(strTable))

	// Write Mach-O header
	w.writeU32(MH_MAGIC_64)
	w.writeU32(CPU_TYPE_ARM64)
	w.writeU32(CPU_SUBTYPE_ARM64_ALL)
	w.writeU32(MH_EXECUTE)
	w.writeU32(numCmds)
	w.writeU32(uint32(loadCmdsSize))
	w.writeU32(MH_NOUNDEFS | MH_DYLDLINK | MH_TWOLEVEL | MH_PIE)
	w.writeU32(0) // reserved

	// LC_SEGMENT_64 __PAGEZERO
	w.writeU32(LC_SEGMENT_64)
	w.writeU32(72)
	w.writeSegName("__PAGEZERO")
	w.writeU64(0)           // vmaddr
	w.writeU64(0x100000000) // vmsize
	w.writeU64(0)           // fileoff
	w.writeU64(0)           // filesize
	w.writeU32(VM_PROT_NONE)
	w.writeU32(VM_PROT_NONE)
	w.writeU32(0) // nsects
	w.writeU32(0) // flags

	// LC_SEGMENT_64 __TEXT
	w.writeU32(LC_SEGMENT_64)
	w.writeU32(152) // 72 + 80 for one section
	w.writeSegName("__TEXT")
	w.writeU64(0x100000000) // vmaddr
	w.writeU64(textSegVMSize)
	w.writeU64(0) // fileoff
	w.writeU64(textSegFileSize)
	w.writeU32(VM_PROT_READ | VM_PROT_EXECUTE)
	w.writeU32(VM_PROT_READ | VM_PROT_EXECUTE)
	w.writeU32(1) // nsects
	w.writeU32(0) // flags

	// Section __text
	w.writeSectName("__text")
	w.writeSegName("__TEXT")
	w.writeU64(codeVMAddr)          // addr
	w.writeU64(codeSize)            // size
	w.writeU32(uint32(codeFileOff)) // offset
	w.writeU32(2)                   // align (2^2 = 4)
	w.writeU32(0)                   // reloff
	w.writeU32(0)                   // nreloc
	w.writeU32(S_REGULAR | S_ATTR_PURE_INSTRUCTIONS | S_ATTR_SOME_INSTRUCTIONS)
	w.writeU32(0) // reserved1
	w.writeU32(0) // reserved2
	w.writeU32(0) // reserved3

	// LC_SEGMENT_64 __LINKEDIT
	w.writeU32(LC_SEGMENT_64)
	w.writeU32(72)
	w.writeSegName("__LINKEDIT")
	w.writeU64(linkeditVMAddr)
	w.writeU64(alignUp(linkeditSize, uint64(PageSize)))
	w.writeU64(linkeditFileOff)
	w.writeU64(linkeditSize)
	w.writeU32(VM_PROT_READ)
	w.writeU32(VM_PROT_READ)
	w.writeU32(0) // nsects
	w.writeU32(0) // flags

	// LC_SYMTAB
	w.writeU32(LC_SYMTAB)
	w.writeU32(24)
	w.writeU32(uint32(symtabOff))
	w.writeU32(uint32(numSyms))
	w.writeU32(uint32(strtabOff))
	w.writeU32(uint32(len(strTable)))

	// LC_DYSYMTAB
	w.writeU32(LC_DYSYMTAB)
	w.writeU32(80)
	w.writeU32(0)                   // ilocalsym
	w.writeU32(0)                   // nlocalsym
	w.writeU32(0)                   // iextdefsym
	w.writeU32(uint32(numSyms))     // nextdefsym
	w.writeU32(uint32(numSyms))     // iundefsym
	w.writeU32(0)                   // nundefsym
	w.writeU32(0)                   // tocoff
	w.writeU32(0)                   // ntoc
	w.writeU32(0)                   // modtaboff
	w.writeU32(0)                   // nmodtab
	w.writeU32(0)                   // extrefsymoff
	w.writeU32(0)                   // nextrefsyms
	w.writeU32(0)                   // indirectsymoff
	w.writeU32(0)                   // nindirectsyms
	w.writeU32(0)                   // extreloff
	w.writeU32(0)                   // nextrel
	w.writeU32(0)                   // locreloff
	w.writeU32(0)                   // nlocrel

	// LC_LOAD_DYLINKER
	dylinker := "/usr/lib/dyld"
	dylinkerCmdSize := (12 + len(dylinker) + 1 + 7) & ^7
	w.writeU32(LC_LOAD_DYLINKER)
	w.writeU32(uint32(dylinkerCmdSize))
	w.writeU32(12) // offset to string
	w.buf.WriteString(dylinker)
	w.buf.WriteByte(0)
	// Pad to dylinkerCmdSize
	for w.buf.Len() < int(headerSize)+72+152+72+24+80+dylinkerCmdSize {
		w.buf.WriteByte(0)
	}

	// LC_MAIN
	w.writeU32(LC_MAIN)
	w.writeU32(24)
	w.writeU64(codeFileOff + uint64(w.mainOff)) // entryoff
	w.writeU64(0)                               // stacksize

	// LC_BUILD_VERSION
	w.writeU32(LC_BUILD_VERSION)
	w.writeU32(24)
	w.writeU32(PLATFORM_MACOS)
	w.writeU32(0x000E0000) // macOS 14.0
	w.writeU32(0x000E0000) // SDK 14.0
	w.writeU32(0)          // ntools

	// LC_SOURCE_VERSION
	w.writeU32(LC_SOURCE_VERSION)
	w.writeU32(16)
	w.writeU64(0) // version 0.0.0.0.0

	// Verify we're at the right offset
	if uint64(w.buf.Len()) != headerSize+loadCmdsSize {
		// Pad if needed
		for uint64(w.buf.Len()) < headerSize+loadCmdsSize {
			w.buf.WriteByte(0)
		}
	}

	// Pad to code start
	for uint64(w.buf.Len()) < codeFileOff {
		w.buf.WriteByte(0)
	}

	// Write code
	w.buf.Write(w.code)

	// Write string constants (null-terminated)
	for _, s := range w.strings {
		w.buf.WriteString(s)
		w.buf.WriteByte(0)
	}

	// Pad to linkedit
	for uint64(w.buf.Len()) < linkeditFileOff {
		w.buf.WriteByte(0)
	}

	// Write symbol table
	if len(w.symbols) > 0 {
		for _, sym := range w.symbols {
			w.writeU32(strOffsets[sym.Name])
			w.buf.WriteByte(N_SECT | N_EXT)
			w.buf.WriteByte(1) // nsect (section 1 = __text)
			w.writeU16(0)      // ndesc
			w.writeU64(codeVMAddr + uint64(sym.Offset))
		}
	} else {
		w.writeU32(strOffsets["_main"])
		w.buf.WriteByte(N_SECT | N_EXT)
		w.buf.WriteByte(1)
		w.writeU16(0)
		w.writeU64(codeVMAddr + uint64(w.mainOff))
	}

	// Write string table
	w.buf.Write(strTable)

	return w.buf.Bytes()
}

func (w *Writer) writeU16(v uint16) {
	binary.Write(&w.buf, binary.LittleEndian, v)
}

func (w *Writer) writeU32(v uint32) {
	binary.Write(&w.buf, binary.LittleEndian, v)
}

func (w *Writer) writeU64(v uint64) {
	binary.Write(&w.buf, binary.LittleEndian, v)
}

func (w *Writer) writeSegName(name string) {
	var buf [16]byte
	copy(buf[:], name)
	w.buf.Write(buf[:])
}

func (w *Writer) writeSectName(name string) {
	var buf [16]byte
	copy(buf[:], name)
	w.buf.Write(buf[:])
}

func alignUp(v, align uint64) uint64 {
	return (v + align - 1) & ^(align - 1)
}
