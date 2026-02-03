// Package ir defines the intermediate representation for ease.
package ir

import (
	"fmt"
	"strings"

	"ease/pkg/types"
)

// Op represents an IR operation kind.
type Op int

const (
	OpInvalid Op = iota

	// Arithmetic
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpNeg

	// Bitwise
	OpAnd
	OpOr
	OpXor
	OpNot
	OpShl
	OpShr

	// Comparison
	OpEq
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe

	// Data movement
	OpCopy      // dest = src
	OpLoadParam // dest = param[index]
	OpLoadConst // dest = constant

	// Control flow
	OpCall   // dest = call func(args...)
	OpReturn // return [value]
	OpJump   // unconditional jump
	OpBranch // conditional branch

	// Memory
	OpAlloc  // allocate stack space, returns pointer
	OpLoad   // load from memory address
	OpStore  // store to memory address

	// Array operations
	OpArrayPtr  // get pointer from array fat pointer (arg0=array)
	OpArrayLen  // get length from array fat pointer (arg0=array)
	OpArrayCap  // get capacity from array fat pointer (arg0=array)
	OpMakeArray // create fat pointer (arg0=ptr, arg1=len, arg2=cap)
	OpArrayPush // push element to array (arg0=array, arg1=elem, arg2=elemSize)
	OpIndexAddr // compute element address: base + index * elemSize

	// String operations
	OpStrEq         // string equality comparison
	OpStrNe         // string inequality comparison
	OpStrLen        // string length (scans for null)
	OpStrConcat     // string concatenation (arg0 + arg1)
	OpStrSlice      // string slice (arg0=str, arg1=start, arg2=end)
	OpLoadByte      // load single byte from address (for string indexing)
	OpStrContains   // check if haystack contains needle (arg0, arg1) -> bool
	OpStrStartsWith // check if string starts with prefix (arg0, arg1) -> bool
	OpStrEndsWith   // check if string ends with suffix (arg0, arg1) -> bool
	OpStrIndexOf    // find substring index, -1 if not found (arg0, arg1) -> int
	OpStrSubstring  // extract substring (arg0=str, arg1=start, arg2=end) -> string
	OpStrCharAt     // get character code at index (arg0=str, arg1=index) -> int
	OpStrTrim       // trim chars from both ends (arg0=str, arg1=chars) -> string
	OpStrReplace    // replace all occurrences (arg0=str, arg1=old, arg2=new) -> string
	OpStrSplit      // split by separator (arg0=str, arg1=sep) -> []string

	// IO operations
	OpPrint     // print string to stdout (arg0=string)
	OpReadFile  // read file contents (arg0=path) -> string
	OpWriteFile // write string to file (arg0=path, arg1=content) -> int (0=success, -1=error)
	OpArgc      // get argument count
	OpArgv      // get argument at index (arg0=index) -> string

	// Conversion operations
	OpIntToStr // convert int to string (arg0=int) -> string
	OpStrToInt // convert string to int (arg0=string) -> int

	// Heap operations
	OpHeapAlloc // allocate memory from heap (arg0=size) -> pointer

	// File syscalls
	OpSyscallOpen  // open(path, flags, mode) -> fd
	OpSyscallRead  // read(fd, buf, size) -> bytes_read
	OpSyscallWrite // write(fd, buf, size) -> bytes_written
	OpSyscallClose // close(fd) -> int

	// Map operations
	OpMapNew    // (keySize, valSize) -> map ptr
	OpMapGet    // (map, key) -> value
	OpMapSet    // (map, key, value)
	OpMapDelete // (map, key)
	OpMapLen    // (map) -> int
)

var opNames = [...]string{
	OpInvalid:   "invalid",
	OpAdd:       "add",
	OpSub:       "sub",
	OpMul:       "mul",
	OpDiv:       "div",
	OpMod:       "mod",
	OpNeg:       "neg",
	OpAnd:       "and",
	OpOr:        "or",
	OpXor:       "xor",
	OpNot:       "not",
	OpShl:       "shl",
	OpShr:       "shr",
	OpEq:        "eq",
	OpNe:        "ne",
	OpLt:        "lt",
	OpLe:        "le",
	OpGt:        "gt",
	OpGe:        "ge",
	OpCopy:      "copy",
	OpLoadParam: "loadparam",
	OpLoadConst: "loadconst",
	OpCall:      "call",
	OpReturn:    "return",
	OpJump:      "jump",
	OpBranch:    "branch",
	OpAlloc:     "alloc",
	OpLoad:      "load",
	OpStore:     "store",
	OpArrayPtr:  "arrayptr",
	OpArrayLen:  "arraylen",
	OpArrayCap:  "arraycap",
	OpMakeArray: "makearray",
	OpArrayPush: "arraypush",
	OpIndexAddr: "indexaddr",
	OpStrEq:         "streq",
	OpStrNe:         "strne",
	OpStrLen:        "strlen",
	OpStrConcat:     "strconcat",
	OpStrSlice:      "strslice",
	OpLoadByte:      "loadbyte",
	OpStrContains:   "strcontains",
	OpStrStartsWith: "strstartswith",
	OpStrEndsWith:   "strendswith",
	OpStrIndexOf:    "strindexof",
	OpStrSubstring:  "strsubstring",
	OpStrCharAt:     "strcharat",
	OpStrTrim:       "strtrim",
	OpStrReplace:    "strreplace",
	OpStrSplit:      "strsplit",
	OpPrint:     "print",
	OpReadFile:  "readfile",
	OpWriteFile: "writefile",
	OpArgc:      "argc",
	OpArgv:      "argv",
	OpIntToStr:  "inttostr",
	OpStrToInt:  "strtoint",
	OpHeapAlloc:    "heapalloc",
	OpSyscallOpen:  "syscall_open",
	OpSyscallRead:  "syscall_read",
	OpSyscallWrite: "syscall_write",
	OpSyscallClose: "syscall_close",
	OpMapNew:       "mapnew",
	OpMapGet:       "mapget",
	OpMapSet:       "mapset",
	OpMapDelete:    "mapdelete",
	OpMapLen:       "maplen",
}

func (o Op) String() string {
	if int(o) < len(opNames) {
		return opNames[o]
	}
	return fmt.Sprintf("op(%d)", o)
}

// OperandKind represents the kind of an operand.
type OperandKind int

const (
	OpndNone OperandKind = iota
	OpndVReg             // virtual register
	OpndImm              // immediate value
	OpndLabel            // label reference
	OpndFunc             // function reference
	OpndStr              // string constant (index into program's string table)
)

// Operand represents an instruction operand.
type Operand struct {
	Kind    OperandKind
	VReg    int    // for OpndVReg
	Imm     int64  // for OpndImm
	Label   string // for OpndLabel
	Func    string // for OpndFunc
	StrIdx  int    // for OpndStr (index into program's string table)
	Type    types.Type
}

func (o Operand) String() string {
	switch o.Kind {
	case OpndNone:
		return "_"
	case OpndVReg:
		return fmt.Sprintf("v%d", o.VReg)
	case OpndImm:
		return fmt.Sprintf("%d", o.Imm)
	case OpndLabel:
		return o.Label
	case OpndFunc:
		return o.Func
	case OpndStr:
		return fmt.Sprintf("str%d", o.StrIdx)
	default:
		return "?"
	}
}

// VReg creates a virtual register operand.
func VReg(n int, typ types.Type) Operand {
	return Operand{Kind: OpndVReg, VReg: n, Type: typ}
}

// Imm creates an immediate operand.
func Imm(val int64, typ types.Type) Operand {
	return Operand{Kind: OpndImm, Imm: val, Type: typ}
}

// Label creates a label operand.
func Label(name string) Operand {
	return Operand{Kind: OpndLabel, Label: name}
}

// FuncRef creates a function reference operand.
func FuncRef(name string, typ types.Type) Operand {
	return Operand{Kind: OpndFunc, Func: name, Type: typ}
}

// StrConst creates a string constant operand.
func StrConst(idx int) Operand {
	return Operand{Kind: OpndStr, StrIdx: idx, Type: types.Typ[types.String]}
}

// None creates an empty operand.
func None() Operand {
	return Operand{Kind: OpndNone}
}

// Instr represents a single IR instruction.
type Instr struct {
	Op      Op
	Dest    Operand   // destination operand (if any)
	Args    []Operand // source operands
	Comment string    // optional comment for debugging
}

func (i *Instr) String() string {
	var args []string
	for _, a := range i.Args {
		args = append(args, a.String())
	}

	var s string
	if i.Dest.Kind != OpndNone {
		s = fmt.Sprintf("%s = %s %s", i.Dest, i.Op, strings.Join(args, ", "))
	} else if len(args) > 0 {
		s = fmt.Sprintf("%s %s", i.Op, strings.Join(args, ", "))
	} else {
		s = i.Op.String()
	}

	if i.Comment != "" {
		s += "  // " + i.Comment
	}
	return s
}

// Block represents a basic block of instructions.
type Block struct {
	Label  string
	Instrs []*Instr
	Preds  []*Block // predecessor blocks
	Succs  []*Block // successor blocks
}

func (b *Block) String() string {
	var lines []string
	lines = append(lines, b.Label+":")
	for _, instr := range b.Instrs {
		lines = append(lines, "    "+instr.String())
	}
	return strings.Join(lines, "\n")
}

// Function represents an IR function.
type Function struct {
	Name       string
	Params     []*Param
	Result     types.Type
	Blocks     []*Block
	Entry      *Block
	NextVReg   int // next virtual register number
	StackSize  int // total stack space needed
}

// Param represents a function parameter in IR.
type Param struct {
	Name   string
	Type   types.Type
	VReg   int // virtual register holding this parameter
}

func (f *Function) String() string {
	var params []string
	for _, p := range f.Params {
		params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type))
	}

	var ret string
	if f.Result != nil && !f.Result.Equals(types.Typ[types.Unit]) {
		ret = " -> " + f.Result.String()
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("fn %s(%s)%s {", f.Name, strings.Join(params, ", "), ret))
	for _, block := range f.Blocks {
		lines = append(lines, block.String())
	}
	lines = append(lines, "}")
	return strings.Join(lines, "\n")
}

// NewBlock creates a new basic block.
func (f *Function) NewBlock(label string) *Block {
	block := &Block{Label: label}
	f.Blocks = append(f.Blocks, block)
	return block
}

// NewVReg allocates a new virtual register.
func (f *Function) NewVReg(typ types.Type) Operand {
	vreg := f.NextVReg
	f.NextVReg++
	return VReg(vreg, typ)
}

// Program represents an entire IR program.
type Program struct {
	Functions []*Function
	Globals   map[string]Operand // global variables/constants
	Strings   []string           // string constant table
}

// NewProgram creates a new IR program.
func NewProgram() *Program {
	return &Program{
		Globals: make(map[string]Operand),
	}
}

func (p *Program) String() string {
	var lines []string
	for _, f := range p.Functions {
		lines = append(lines, f.String())
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// FunctionByName finds a function by name.
func (p *Program) FunctionByName(name string) *Function {
	for _, f := range p.Functions {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// AddString adds a string constant to the program's string table.
// Returns the index of the string (reuses existing if duplicate).
func (p *Program) AddString(s string) int {
	// Check for existing string
	for i, existing := range p.Strings {
		if existing == s {
			return i
		}
	}
	// Add new string
	idx := len(p.Strings)
	p.Strings = append(p.Strings, s)
	return idx
}
