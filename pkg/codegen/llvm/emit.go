// Package llvm generates LLVM IR text from Ease IR.
// The generated .ll file is compiled to native code by clang.
//
// Design: all Ease values are represented as i64 in LLVM IR.
// Pointers are integers (ptrtoint/inttoptr as needed).
// Each virtual register gets an alloca i64 slot; clang's mem2reg
// pass at -O1 promotes these to SSA registers automatically.
// OpAlloc uses malloc (not alloca) so returned pointers remain valid.
package llvm

import (
	"ease/pkg/ir"
	"ease/pkg/types"
	"fmt"
	"strings"
)

// Emitter converts Ease IR to LLVM IR text.
type Emitter struct {
	buf        strings.Builder
	prog       *ir.Program
	fn         *ir.Function
	tmpIdx     int  // counter for unique temp SSA names
	terminated bool // whether current block has been terminated (br/ret)
}

// NewEmitter creates a new LLVM IR emitter.
func NewEmitter() *Emitter {
	return &Emitter{}
}

// EmitProgram generates LLVM IR text for the entire program.
func (e *Emitter) EmitProgram(prog *ir.Program) string {
	e.prog = prog
	e.buf.Reset()

	// String constants
	e.emitStringConstants()

	// Global variables
	e.emitGlobalVars()

	// Runtime and intrinsic declarations
	e.emitRuntimeDecls()

	// User functions
	for _, fn := range prog.Functions {
		e.emitFunction(fn)
	}

	return e.buf.String()
}

// ---------- helpers ----------

func (e *Emitter) printf(format string, args ...interface{}) {
	fmt.Fprintf(&e.buf, format, args...)
}

// temp returns a unique SSA temp name like %t.0, %t.1, ...
func (e *Emitter) temp() string {
	name := fmt.Sprintf("%%t.%d", e.tmpIdx)
	e.tmpIdx++
	return name
}

// loadVal loads an operand as an i64 value. Returns the LLVM IR value string.
// For globals, returns the ADDRESS of the global (matching ARM64 ADRP+ADD semantics).
func (e *Emitter) loadVal(op ir.Operand) string {
	switch op.Kind {
	case ir.OpndVReg:
		t := e.temp()
		e.printf("  %s = load i64, ptr %%v.%d\n", t, op.VReg)
		return t
	case ir.OpndImm:
		return fmt.Sprintf("%d", op.Imm)
	case ir.OpndStr:
		t := e.temp()
		e.printf("  %s = ptrtoint ptr @.str.%d to i64\n", t, op.StrIdx)
		return t
	case ir.OpndGlobal:
		// Globals in Ease IR represent their ADDRESS, not their value.
		// This matches ARM64 where global operands are resolved via ADRP+ADD.
		t := e.temp()
		e.printf("  %s = ptrtoint ptr @g.%s to i64\n", t, mangleName(op.Global))
		return t
	default:
		return "0"
	}
}

// loadPtr loads an operand as a ptr value (for memory operations).
func (e *Emitter) loadPtr(op ir.Operand) string {
	switch op.Kind {
	case ir.OpndStr:
		// String constants are already ptrs
		return fmt.Sprintf("@.str.%d", op.StrIdx)
	case ir.OpndGlobal:
		// Globals are already ptrs in LLVM IR
		return fmt.Sprintf("@g.%s", mangleName(op.Global))
	default:
		v := e.loadVal(op)
		t := e.temp()
		e.printf("  %s = inttoptr i64 %s to ptr\n", t, v)
		return t
	}
}

// storeToVReg stores an i64 value to a virtual register's alloca slot.
func (e *Emitter) storeToVReg(vreg int, val string) {
	e.printf("  store i64 %s, ptr %%v.%d\n", val, vreg)
}

// funcName returns the LLVM function name (renaming main -> ease_main).
func funcName(name string) string {
	if name == "main" {
		return "ease_main"
	}
	return mangleName(name)
}

// mangleName makes a name safe for LLVM IR (replaces problematic chars).
func mangleName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}

// llvmStringLiteral converts a Go string to LLVM IR c"..." constant syntax.
func llvmStringLiteral(s string) string {
	var buf strings.Builder
	buf.WriteString("c\"")
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch >= 32 && ch < 127 && ch != '"' && ch != '\\' {
			buf.WriteByte(ch)
		} else {
			fmt.Fprintf(&buf, "\\%02X", ch)
		}
	}
	buf.WriteString("\\00\"") // null terminator
	return buf.String()
}

// isVoid returns true if the function returns void (Unit type).
func isVoid(fn *ir.Function) bool {
	return fn.Result == nil || fn.Result.Equals(types.Typ[types.Unit])
}

// ---------- top-level emitters ----------

func (e *Emitter) emitStringConstants() {
	for i, s := range e.prog.Strings {
		e.printf("@.str.%d = private unnamed_addr constant [%d x i8] %s\n",
			i, len(s)+1, llvmStringLiteral(s))
	}
	if len(e.prog.Strings) > 0 {
		e.printf("\n")
	}
}

func (e *Emitter) emitGlobalVars() {
	for _, gv := range e.prog.GlobalVars {
		if gv.Size <= 8 {
			e.printf("@g.%s = global i64 0\n", mangleName(gv.Name))
		} else {
			e.printf("@g.%s = global [%d x i8] zeroinitializer\n", mangleName(gv.Name), gv.Size)
		}
	}
	if len(e.prog.GlobalVars) > 0 {
		e.printf("\n")
	}
}

func (e *Emitter) emitRuntimeDecls() {
	decls := []string{
		// Memory
		"declare ptr @malloc(i64)",
		"declare ptr @memcpy(ptr, ptr, i64)",
		"declare ptr @memset(ptr, i32, i64)",

		// String operations
		"declare i64 @ease_str_eq(ptr, ptr)",
		"declare i64 @ease_str_ne(ptr, ptr)",
		"declare i64 @ease_str_len(ptr)",
		"declare ptr @ease_str_concat(ptr, ptr)",
		"declare ptr @ease_str_slice(ptr, i64, i64)",
		"declare i64 @ease_load_byte(ptr, i64)",
		"declare i64 @ease_str_contains(ptr, ptr)",
		"declare i64 @ease_str_starts_with(ptr, ptr)",
		"declare i64 @ease_str_ends_with(ptr, ptr)",
		"declare i64 @ease_str_index_of(ptr, ptr)",
		"declare ptr @ease_str_substring(ptr, i64, i64)",
		"declare i64 @ease_str_char_at(ptr, i64)",
		"declare ptr @ease_str_trim(ptr, ptr)",
		"declare ptr @ease_str_replace(ptr, ptr, ptr)",
		"declare void @ease_str_split(ptr, ptr, ptr)",

		// Array
		"declare void @ease_array_push(ptr, ptr, i64)",

		// IO
		"declare void @ease_print(ptr)",
		"declare void @ease_println(ptr)",
		"declare ptr @ease_read_file(ptr)",
		"declare i64 @ease_write_file(ptr, ptr)",

		// Conversion
		"declare ptr @ease_int_to_str(i64)",
		"declare i64 @ease_str_to_int(ptr)",

		// Memory ops
		"declare void @ease_poke(ptr, i64)",
		"declare i64 @ease_peek(ptr)",
		"declare void @ease_memset(ptr, i64, i64)",

		// Syscalls
		"declare i64 @ease_syscall_open(ptr, i64, i64)",
		"declare i64 @ease_syscall_read(i64, ptr, i64)",
		"declare i64 @ease_syscall_write(i64, ptr, i64)",
		"declare i64 @ease_syscall_close(i64)",

		// Map
		"declare ptr @ease_map_new(i64, i64)",
		"declare i64 @ease_map_get(ptr, i64)",
		"declare void @ease_map_set(ptr, i64, i64)",
		"declare void @ease_map_delete(ptr, i64)",
		"declare i64 @ease_map_len(ptr)",

		// Argc/Argv
		"declare i64 @ease_argc()",
		"declare ptr @ease_argv(i64)",
	}
	for _, d := range decls {
		e.printf("%s\n", d)
	}
	e.printf("\n")
}

// ---------- function emitter ----------

func (e *Emitter) emitFunction(fn *ir.Function) {
	e.fn = fn
	e.tmpIdx = 0

	name := funcName(fn.Name)

	// Build parameter list
	params := make([]string, len(fn.Params))
	for i := range fn.Params {
		params[i] = fmt.Sprintf("i64 %%param.%d", i)
	}

	retType := "i64"
	if isVoid(fn) {
		retType = "void"
	}

	e.printf("define %s @%s(%s) {\n", retType, name, strings.Join(params, ", "))

	// Entry block: allocas for all vregs, then branch to first IR block
	// All allocas must be in the LLVM entry block for mem2reg to work.
	for i := 0; i < fn.NextVReg; i++ {
		e.printf("  %%v.%d = alloca i64\n", i)
	}

	// Store parameters into their vreg slots
	for i, param := range fn.Params {
		e.printf("  store i64 %%param.%d, ptr %%v.%d\n", i, param.VReg)
	}

	// Branch to first IR block
	if len(fn.Blocks) > 0 {
		e.printf("  br label %%B.%s\n", fn.Blocks[0].Label)
	} else {
		if isVoid(fn) {
			e.printf("  ret void\n")
		} else {
			e.printf("  ret i64 0\n")
		}
	}

	// Emit IR blocks
	for i, block := range fn.Blocks {
		var nextBlock *ir.Block
		if i+1 < len(fn.Blocks) {
			nextBlock = fn.Blocks[i+1]
		}
		e.emitBlock(block, nextBlock)
	}

	e.printf("}\n\n")
	e.fn = nil
}

func (e *Emitter) emitBlock(block *ir.Block, nextBlock *ir.Block) {
	e.printf("B.%s:\n", block.Label)
	e.terminated = false

	for _, instr := range block.Instrs {
		e.emitInstr(instr)
	}

	// If block doesn't end with a terminator, add fallthrough
	if !e.terminated {
		if nextBlock != nil {
			e.printf("  br label %%B.%s\n", nextBlock.Label)
		} else {
			// Last block without terminator - add default return
			if isVoid(e.fn) {
				e.printf("  ret void\n")
			} else {
				e.printf("  ret i64 0\n")
			}
		}
	}
}

// ---------- instruction dispatch ----------

func (e *Emitter) emitInstr(instr *ir.Instr) {
	switch instr.Op {
	// Arithmetic
	case ir.OpAdd:
		e.emitBinOp(instr, "add")
	case ir.OpSub:
		e.emitBinOp(instr, "sub")
	case ir.OpMul:
		e.emitBinOp(instr, "mul")
	case ir.OpDiv:
		e.emitBinOp(instr, "sdiv")
	case ir.OpMod:
		e.emitBinOp(instr, "srem")

	// Bitwise
	case ir.OpAnd:
		e.emitBinOp(instr, "and")
	case ir.OpOr:
		e.emitBinOp(instr, "or")
	case ir.OpXor:
		e.emitBinOp(instr, "xor")
	case ir.OpShl:
		e.emitBinOp(instr, "shl")
	case ir.OpShr:
		e.emitBinOp(instr, "ashr")

	// Unary
	case ir.OpNeg:
		e.emitNeg(instr)
	case ir.OpNot:
		e.emitNot(instr)

	// Comparison
	case ir.OpEq:
		e.emitCmp(instr, "eq")
	case ir.OpNe:
		e.emitCmp(instr, "ne")
	case ir.OpLt:
		e.emitCmp(instr, "slt")
	case ir.OpLe:
		e.emitCmp(instr, "sle")
	case ir.OpGt:
		e.emitCmp(instr, "sgt")
	case ir.OpGe:
		e.emitCmp(instr, "sge")

	// Data movement
	case ir.OpCopy:
		e.emitCopy(instr)
	case ir.OpLoadParam:
		e.emitLoadParam(instr)
	case ir.OpLoadConst:
		e.emitLoadConst(instr)

	// Control flow
	case ir.OpCall:
		e.emitCall(instr)
	case ir.OpReturn:
		e.emitReturn(instr)
	case ir.OpJump:
		e.emitJump(instr)
	case ir.OpBranch:
		e.emitBranch(instr)

	// Memory
	case ir.OpAlloc:
		e.emitAlloc(instr)
	case ir.OpLoad:
		e.emitLoad(instr)
	case ir.OpStore:
		e.emitStore(instr)
	case ir.OpMemCopy:
		e.emitMemCopy(instr)

	// Array
	case ir.OpArrayPtr:
		e.emitArrayPtr(instr)
	case ir.OpArrayLen:
		e.emitArrayLen(instr)
	case ir.OpArrayCap:
		e.emitArrayCap(instr)
	case ir.OpMakeArray:
		e.emitMakeArray(instr)
	case ir.OpArrayPush:
		e.emitArrayPush(instr)
	case ir.OpIndexAddr:
		e.emitIndexAddr(instr)

	// String operations
	case ir.OpStrEq:
		e.emitStrCall2Ret(instr, "ease_str_eq")
	case ir.OpStrNe:
		e.emitStrCall2Ret(instr, "ease_str_ne")
	case ir.OpStrLen:
		e.emitStrCall1Ret(instr, "ease_str_len")
	case ir.OpStrConcat:
		e.emitStrCall2PtrRet(instr, "ease_str_concat")
	case ir.OpStrSlice:
		e.emitStrSlice(instr)
	case ir.OpLoadByte:
		e.emitLoadByte(instr)
	case ir.OpStrContains:
		e.emitStrCall2Ret(instr, "ease_str_contains")
	case ir.OpStrStartsWith:
		e.emitStrCall2Ret(instr, "ease_str_starts_with")
	case ir.OpStrEndsWith:
		e.emitStrCall2Ret(instr, "ease_str_ends_with")
	case ir.OpStrIndexOf:
		e.emitStrCall2Ret(instr, "ease_str_index_of")
	case ir.OpStrSubstring:
		e.emitStrSubstring(instr)
	case ir.OpStrCharAt:
		e.emitStrCharAt(instr)
	case ir.OpStrTrim:
		e.emitStrCall2PtrRet(instr, "ease_str_trim")
	case ir.OpStrReplace:
		e.emitStrReplace(instr)
	case ir.OpStrSplit:
		e.emitStrSplit(instr)

	// IO
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

	// Conversion
	case ir.OpIntToStr:
		e.emitIntToStr(instr)
	case ir.OpStrToInt:
		e.emitStrToInt(instr)

	// Heap
	case ir.OpHeapAlloc:
		e.emitHeapAlloc(instr)

	// Memory ops
	case ir.OpPoke:
		e.emitPoke(instr)
	case ir.OpPeek:
		e.emitPeek(instr)
	case ir.OpMemSet:
		e.emitMemSet(instr)

	// Syscalls
	case ir.OpSyscallOpen:
		e.emitSyscallOpen(instr)
	case ir.OpSyscallRead:
		e.emitSyscallRead(instr)
	case ir.OpSyscallWrite:
		e.emitSyscallWrite(instr)
	case ir.OpSyscallClose:
		e.emitSyscallClose(instr)

	// Map
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

	default:
		e.printf("  ; UNIMPLEMENTED: %s\n", instr.Op)
	}
}

// ---------- arithmetic ----------

func (e *Emitter) emitBinOp(instr *ir.Instr, op string) {
	lhs := e.loadVal(instr.Args[0])
	rhs := e.loadVal(instr.Args[1])
	t := e.temp()
	e.printf("  %s = %s i64 %s, %s\n", t, op, lhs, rhs)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitNeg(instr *ir.Instr) {
	val := e.loadVal(instr.Args[0])
	t := e.temp()
	e.printf("  %s = sub i64 0, %s\n", t, val)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitNot(instr *ir.Instr) {
	val := e.loadVal(instr.Args[0])
	t := e.temp()
	e.printf("  %s = xor i64 %s, 1\n", t, val)
	e.storeToVReg(instr.Dest.VReg, t)
}

// ---------- comparison ----------

func (e *Emitter) emitCmp(instr *ir.Instr, pred string) {
	lhs := e.loadVal(instr.Args[0])
	rhs := e.loadVal(instr.Args[1])
	cmp := e.temp()
	e.printf("  %s = icmp %s i64 %s, %s\n", cmp, pred, lhs, rhs)
	ext := e.temp()
	e.printf("  %s = zext i1 %s to i64\n", ext, cmp)
	e.storeToVReg(instr.Dest.VReg, ext)
}

// ---------- data movement ----------

func (e *Emitter) emitCopy(instr *ir.Instr) {
	val := e.loadVal(instr.Args[0])
	e.storeToVReg(instr.Dest.VReg, val)
}

func (e *Emitter) emitLoadParam(instr *ir.Instr) {
	// LoadParam loads from the parameter slot. Parameters were stored to their
	// vreg slots in the function prologue, so this is redundant if the param
	// vreg matches the dest vreg. But for safety, we load from the param's vreg.
	paramIdx := int(instr.Args[0].Imm)
	if paramIdx < len(e.fn.Params) {
		paramVReg := e.fn.Params[paramIdx].VReg
		t := e.temp()
		e.printf("  %s = load i64, ptr %%v.%d\n", t, paramVReg)
		e.storeToVReg(instr.Dest.VReg, t)
	}
}

func (e *Emitter) emitLoadConst(instr *ir.Instr) {
	val := e.loadVal(instr.Args[0])
	e.storeToVReg(instr.Dest.VReg, val)
}

// ---------- control flow ----------

func (e *Emitter) emitCall(instr *ir.Instr) {
	fnRef := instr.Args[0]
	fnName := funcName(fnRef.Func)

	// Collect arguments
	args := make([]string, 0, len(instr.Args)-1)
	for _, arg := range instr.Args[1:] {
		args = append(args, "i64 "+e.loadVal(arg))
	}

	// Determine return type
	retType := "i64"
	if fnRef.Type != nil {
		if ft, ok := fnRef.Type.(*types.Function); ok {
			if ft.Result == nil || ft.Result.Equals(types.Typ[types.Unit]) {
				retType = "void"
			}
		}
	}

	argStr := strings.Join(args, ", ")

	if retType == "void" {
		e.printf("  call void @%s(%s)\n", fnName, argStr)
	} else {
		t := e.temp()
		e.printf("  %s = call i64 @%s(%s)\n", t, fnName, argStr)
		if instr.Dest.Kind == ir.OpndVReg {
			e.storeToVReg(instr.Dest.VReg, t)
		}
	}
}

func (e *Emitter) emitReturn(instr *ir.Instr) {
	if isVoid(e.fn) {
		e.printf("  ret void\n")
	} else if len(instr.Args) > 0 {
		val := e.loadVal(instr.Args[0])
		e.printf("  ret i64 %s\n", val)
	} else {
		e.printf("  ret i64 0\n")
	}
	e.terminated = true
}

func (e *Emitter) emitJump(instr *ir.Instr) {
	label := instr.Args[0].Label
	e.printf("  br label %%B.%s\n", label)
	e.terminated = true
}

func (e *Emitter) emitBranch(instr *ir.Instr) {
	cond := e.loadVal(instr.Args[0])
	trueLabel := instr.Args[1].Label
	falseLabel := instr.Args[2].Label
	cmp := e.temp()
	e.printf("  %s = icmp ne i64 %s, 0\n", cmp, cond)
	e.printf("  br i1 %s, label %%B.%s, label %%B.%s\n", cmp, trueLabel, falseLabel)
	e.terminated = true
}

// ---------- memory ----------

func (e *Emitter) emitAlloc(instr *ir.Instr) {
	// OpAlloc allocates memory. Use malloc so pointers survive function returns.
	size := instr.Args[0].Imm
	p := e.temp()
	e.printf("  %s = call ptr @malloc(i64 %d)\n", p, size)
	// Zero-initialize the allocated memory
	z := e.temp()
	e.printf("  %s = call ptr @memset(ptr %s, i32 0, i64 %d)\n", z, p, size)
	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, p)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitLoad(instr *ir.Instr) {
	// OpLoad: dest = *addr (load i64 from memory address)
	addr := e.loadPtr(instr.Args[0])
	t := e.temp()
	e.printf("  %s = load i64, ptr %s\n", t, addr)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitStore(instr *ir.Instr) {
	// OpStore: *addr = value. Args[0]=value, Args[1]=address
	val := e.loadVal(instr.Args[0])
	addr := e.loadPtr(instr.Args[1])
	e.printf("  store i64 %s, ptr %s\n", val, addr)
}

func (e *Emitter) emitMemCopy(instr *ir.Instr) {
	// OpMemCopy: memcpy(dst, src, size). Args[0]=src, Args[1]=dst, Args[2]=size
	src := e.loadPtr(instr.Args[0])
	dst := e.loadPtr(instr.Args[1])
	size := e.loadVal(instr.Args[2])
	e.printf("  call ptr @memcpy(ptr %s, ptr %s, i64 %s)\n", dst, src, size)
}

// ---------- array operations ----------

func (e *Emitter) emitArrayPtr(instr *ir.Instr) {
	// Load pointer from fat pointer (offset 0)
	fatPtr := e.loadPtr(instr.Args[0])
	t := e.temp()
	e.printf("  %s = load i64, ptr %s\n", t, fatPtr)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitArrayLen(instr *ir.Instr) {
	// Load length from fat pointer (offset 8)
	base := e.loadVal(instr.Args[0])
	addr := e.temp()
	e.printf("  %s = add i64 %s, 8\n", addr, base)
	ptr := e.temp()
	e.printf("  %s = inttoptr i64 %s to ptr\n", ptr, addr)
	t := e.temp()
	e.printf("  %s = load i64, ptr %s\n", t, ptr)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitArrayCap(instr *ir.Instr) {
	// Load capacity from fat pointer (offset 16)
	base := e.loadVal(instr.Args[0])
	addr := e.temp()
	e.printf("  %s = add i64 %s, 16\n", addr, base)
	ptr := e.temp()
	e.printf("  %s = inttoptr i64 %s to ptr\n", ptr, addr)
	t := e.temp()
	e.printf("  %s = load i64, ptr %s\n", t, ptr)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitMakeArray(instr *ir.Instr) {
	// Create fat pointer: (ptr, len, cap) packed into 24 bytes
	// Args[0]=ptr, Args[1]=len, Args[2]=cap
	ptrVal := e.loadVal(instr.Args[0])
	lenVal := e.loadVal(instr.Args[1])
	capVal := e.loadVal(instr.Args[2])

	// Allocate 24 bytes for the fat pointer
	fp := e.temp()
	e.printf("  %s = call ptr @malloc(i64 24)\n", fp)

	// Store ptr at offset 0
	e.printf("  store i64 %s, ptr %s\n", ptrVal, fp)

	// Store len at offset 8
	lenAddr := e.temp()
	e.printf("  %s = getelementptr i8, ptr %s, i64 8\n", lenAddr, fp)
	e.printf("  store i64 %s, ptr %s\n", lenVal, lenAddr)

	// Store cap at offset 16
	capAddr := e.temp()
	e.printf("  %s = getelementptr i8, ptr %s, i64 16\n", capAddr, fp)
	e.printf("  store i64 %s, ptr %s\n", capVal, capAddr)

	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, fp)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitArrayPush(instr *ir.Instr) {
	// OpArrayPush: push element to array
	// Args[0]=fat ptr addr, Args[1]=element value, Args[2]=element size
	fatPtr := e.loadPtr(instr.Args[0])
	elemVal := e.loadVal(instr.Args[1])
	elemSize := e.loadVal(instr.Args[2])

	// Store element value to a temp alloca so we can pass its address
	tmpAlloca := e.temp()
	e.printf("  %s = alloca i64\n", tmpAlloca)
	e.printf("  store i64 %s, ptr %s\n", elemVal, tmpAlloca)

	e.printf("  call void @ease_array_push(ptr %s, ptr %s, i64 %s)\n",
		fatPtr, tmpAlloca, elemSize)
}

func (e *Emitter) emitIndexAddr(instr *ir.Instr) {
	// OpIndexAddr: dest = base + offset (byte addressing)
	base := e.loadVal(instr.Args[0])
	offset := e.loadVal(instr.Args[1])
	t := e.temp()
	e.printf("  %s = add i64 %s, %s\n", t, base, offset)
	e.storeToVReg(instr.Dest.VReg, t)
}

// ---------- string operations ----------

// emitStrCall2Ret: call runtime with 2 ptr args, returns i64
func (e *Emitter) emitStrCall2Ret(instr *ir.Instr, fn string) {
	a := e.loadPtr(instr.Args[0])
	b := e.loadPtr(instr.Args[1])
	t := e.temp()
	e.printf("  %s = call i64 @%s(ptr %s, ptr %s)\n", t, fn, a, b)
	e.storeToVReg(instr.Dest.VReg, t)
}

// emitStrCall1Ret: call runtime with 1 ptr arg, returns i64
func (e *Emitter) emitStrCall1Ret(instr *ir.Instr, fn string) {
	a := e.loadPtr(instr.Args[0])
	t := e.temp()
	e.printf("  %s = call i64 @%s(ptr %s)\n", t, fn, a)
	e.storeToVReg(instr.Dest.VReg, t)
}

// emitStrCall2PtrRet: call runtime with 2 ptr args, returns ptr (converted to i64)
func (e *Emitter) emitStrCall2PtrRet(instr *ir.Instr, fn string) {
	a := e.loadPtr(instr.Args[0])
	b := e.loadPtr(instr.Args[1])
	p := e.temp()
	e.printf("  %s = call ptr @%s(ptr %s, ptr %s)\n", p, fn, a, b)
	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, p)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitStrSlice(instr *ir.Instr) {
	s := e.loadPtr(instr.Args[0])
	start := e.loadVal(instr.Args[1])
	end := e.loadVal(instr.Args[2])
	p := e.temp()
	e.printf("  %s = call ptr @ease_str_slice(ptr %s, i64 %s, i64 %s)\n", p, s, start, end)
	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, p)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitLoadByte(instr *ir.Instr) {
	addr := e.loadPtr(instr.Args[0])
	idx := e.loadVal(instr.Args[1])
	t := e.temp()
	e.printf("  %s = call i64 @ease_load_byte(ptr %s, i64 %s)\n", t, addr, idx)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitStrSubstring(instr *ir.Instr) {
	s := e.loadPtr(instr.Args[0])
	start := e.loadVal(instr.Args[1])
	end := e.loadVal(instr.Args[2])
	p := e.temp()
	e.printf("  %s = call ptr @ease_str_substring(ptr %s, i64 %s, i64 %s)\n", p, s, start, end)
	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, p)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitStrCharAt(instr *ir.Instr) {
	s := e.loadPtr(instr.Args[0])
	idx := e.loadVal(instr.Args[1])
	t := e.temp()
	e.printf("  %s = call i64 @ease_str_char_at(ptr %s, i64 %s)\n", t, s, idx)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitStrReplace(instr *ir.Instr) {
	s := e.loadPtr(instr.Args[0])
	old := e.loadPtr(instr.Args[1])
	newStr := e.loadPtr(instr.Args[2])
	p := e.temp()
	e.printf("  %s = call ptr @ease_str_replace(ptr %s, ptr %s, ptr %s)\n", p, s, old, newStr)
	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, p)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitStrSplit(instr *ir.Instr) {
	// ease_str_split writes to an output EaseArray (24 bytes)
	s := e.loadPtr(instr.Args[0])
	sep := e.loadPtr(instr.Args[1])

	// Allocate 24 bytes for the result fat pointer
	outArr := e.temp()
	e.printf("  %s = call ptr @malloc(i64 24)\n", outArr)
	e.printf("  call void @ease_str_split(ptr %s, ptr %s, ptr %s)\n", outArr, s, sep)

	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, outArr)
	e.storeToVReg(instr.Dest.VReg, t)
}

// ---------- IO operations ----------

func (e *Emitter) emitPrint(instr *ir.Instr) {
	s := e.loadPtr(instr.Args[0])
	e.printf("  call void @ease_print(ptr %s)\n", s)
}

func (e *Emitter) emitReadFile(instr *ir.Instr) {
	path := e.loadPtr(instr.Args[0])
	p := e.temp()
	e.printf("  %s = call ptr @ease_read_file(ptr %s)\n", p, path)
	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, p)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitWriteFile(instr *ir.Instr) {
	path := e.loadPtr(instr.Args[0])
	content := e.loadPtr(instr.Args[1])
	t := e.temp()
	e.printf("  %s = call i64 @ease_write_file(ptr %s, ptr %s)\n", t, path, content)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitArgc(instr *ir.Instr) {
	t := e.temp()
	e.printf("  %s = call i64 @ease_argc()\n", t)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitArgv(instr *ir.Instr) {
	idx := e.loadVal(instr.Args[0])
	p := e.temp()
	e.printf("  %s = call ptr @ease_argv(i64 %s)\n", p, idx)
	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, p)
	e.storeToVReg(instr.Dest.VReg, t)
}

// ---------- conversion operations ----------

func (e *Emitter) emitIntToStr(instr *ir.Instr) {
	val := e.loadVal(instr.Args[0])
	p := e.temp()
	e.printf("  %s = call ptr @ease_int_to_str(i64 %s)\n", p, val)
	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, p)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitStrToInt(instr *ir.Instr) {
	s := e.loadPtr(instr.Args[0])
	t := e.temp()
	e.printf("  %s = call i64 @ease_str_to_int(ptr %s)\n", t, s)
	e.storeToVReg(instr.Dest.VReg, t)
}

// ---------- heap operations ----------

func (e *Emitter) emitHeapAlloc(instr *ir.Instr) {
	size := e.loadVal(instr.Args[0])
	p := e.temp()
	e.printf("  %s = call ptr @malloc(i64 %s)\n", p, size)
	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, p)
	e.storeToVReg(instr.Dest.VReg, t)
}

// ---------- memory operations ----------

func (e *Emitter) emitPoke(instr *ir.Instr) {
	addr := e.loadPtr(instr.Args[0])
	val := e.loadVal(instr.Args[1])
	e.printf("  call void @ease_poke(ptr %s, i64 %s)\n", addr, val)
}

func (e *Emitter) emitPeek(instr *ir.Instr) {
	addr := e.loadPtr(instr.Args[0])
	t := e.temp()
	e.printf("  %s = call i64 @ease_peek(ptr %s)\n", t, addr)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitMemSet(instr *ir.Instr) {
	addr := e.loadPtr(instr.Args[0])
	val := e.loadVal(instr.Args[1])
	count := e.loadVal(instr.Args[2])
	e.printf("  call void @ease_memset(ptr %s, i64 %s, i64 %s)\n", addr, val, count)
}

// ---------- syscall operations ----------

func (e *Emitter) emitSyscallOpen(instr *ir.Instr) {
	path := e.loadPtr(instr.Args[0])
	flags := e.loadVal(instr.Args[1])
	mode := e.loadVal(instr.Args[2])
	t := e.temp()
	e.printf("  %s = call i64 @ease_syscall_open(ptr %s, i64 %s, i64 %s)\n", t, path, flags, mode)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitSyscallRead(instr *ir.Instr) {
	fd := e.loadVal(instr.Args[0])
	buf := e.loadPtr(instr.Args[1])
	size := e.loadVal(instr.Args[2])
	t := e.temp()
	e.printf("  %s = call i64 @ease_syscall_read(i64 %s, ptr %s, i64 %s)\n", t, fd, buf, size)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitSyscallWrite(instr *ir.Instr) {
	fd := e.loadVal(instr.Args[0])
	buf := e.loadPtr(instr.Args[1])
	size := e.loadVal(instr.Args[2])
	t := e.temp()
	e.printf("  %s = call i64 @ease_syscall_write(i64 %s, ptr %s, i64 %s)\n", t, fd, buf, size)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitSyscallClose(instr *ir.Instr) {
	fd := e.loadVal(instr.Args[0])
	t := e.temp()
	e.printf("  %s = call i64 @ease_syscall_close(i64 %s)\n", t, fd)
	e.storeToVReg(instr.Dest.VReg, t)
}

// ---------- map operations ----------

func (e *Emitter) emitMapNew(instr *ir.Instr) {
	keySize := e.loadVal(instr.Args[0])
	valSize := e.loadVal(instr.Args[1])
	p := e.temp()
	e.printf("  %s = call ptr @ease_map_new(i64 %s, i64 %s)\n", p, keySize, valSize)
	t := e.temp()
	e.printf("  %s = ptrtoint ptr %s to i64\n", t, p)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitMapGet(instr *ir.Instr) {
	m := e.loadPtr(instr.Args[0])
	key := e.loadVal(instr.Args[1])
	t := e.temp()
	e.printf("  %s = call i64 @ease_map_get(ptr %s, i64 %s)\n", t, m, key)
	e.storeToVReg(instr.Dest.VReg, t)
}

func (e *Emitter) emitMapSet(instr *ir.Instr) {
	m := e.loadPtr(instr.Args[0])
	key := e.loadVal(instr.Args[1])
	val := e.loadVal(instr.Args[2])
	e.printf("  call void @ease_map_set(ptr %s, i64 %s, i64 %s)\n", m, key, val)
}

func (e *Emitter) emitMapDelete(instr *ir.Instr) {
	m := e.loadPtr(instr.Args[0])
	key := e.loadVal(instr.Args[1])
	e.printf("  call void @ease_map_delete(ptr %s, i64 %s)\n", m, key)
}

func (e *Emitter) emitMapLen(instr *ir.Instr) {
	m := e.loadPtr(instr.Args[0])
	t := e.temp()
	e.printf("  %s = call i64 @ease_map_len(ptr %s)\n", t, m)
	e.storeToVReg(instr.Dest.VReg, t)
}
