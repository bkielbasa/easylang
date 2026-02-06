package ir

import (
	"fmt"

	"ease/pkg/ast"
	"ease/pkg/sema"
	"ease/pkg/symbols"
	"ease/pkg/token"
	"ease/pkg/types"
)

// Builder converts an AST to IR.
type Builder struct {
	info       *sema.TypeInfo
	prog       *Program
	fn         *Function
	block      *Block
	loopEnd    *Block // for break
	loopStart  *Block // for continue
	labelCount int
}

// NewBuilder creates a new IR builder.
func NewBuilder(info *sema.TypeInfo) *Builder {
	return &Builder{
		info: info,
		prog: NewProgram(),
	}
}

// Build converts a program AST to IR.
func (b *Builder) Build(prog *ast.Program) *Program {
	// First pass: collect global variables and constants
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.ConstDecl:
			b.buildConstDecl(d)
		case *ast.VarDecl:
			b.buildVarDecl(d)
		}
	}

	// Second pass: build functions
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			b.buildFnDecl(d)
		case *ast.ImplDecl:
			b.buildImplDecl(d)
		}
	}
	return b.prog
}

func (b *Builder) buildConstDecl(c *ast.ConstDecl) {
	// Get the symbol to determine the type
	sym := b.info.Defs[c.Name]
	if sym == nil {
		return
	}

	// For now, we only support simple constant expressions (literals)
	// Complex initialization will be handled later
	var op Operand
	switch v := c.Value.(type) {
	case *ast.IntLit:
		op = Imm(v.Value, sym.Type)
	case *ast.StringLit:
		// Add string to string table (with deduplication)
		idx := b.prog.AddString(v.Value)
		op = StrConst(idx)
	default:
		// For complex expressions, we'll need to generate initialization code
		// For now, just create a placeholder
		op = GlobalRef(c.Name.Name, sym.Type)
	}

	b.prog.Globals[c.Name.Name] = op
}

func (b *Builder) buildVarDecl(v *ast.VarDecl) {
	// Get the symbol to determine the type
	sym := b.info.Defs[v.Name]
	if sym == nil {
		return
	}

	// Determine if this global needs storage allocation
	needsStorage := false
	var op Operand

	switch val := v.Value.(type) {
	case *ast.IntLit:
		if v.Mutable {
			// Mutable globals need storage
			needsStorage = true
		} else {
			// Immutable literals can be immediate values
			op = Imm(val.Value, sym.Type)
		}
	case *ast.StringLit:
		// Strings use the string table (with deduplication)
		idx := b.prog.AddString(val.Value)
		op = StrConst(idx)
	case *ast.BoolLit:
		if v.Mutable {
			needsStorage = true
		} else {
			intVal := int64(0)
			if val.Value {
				intVal = 1
			}
			op = Imm(intVal, sym.Type)
		}
	default:
		// Complex expressions (arrays, structs, etc.) need storage
		needsStorage = true
	}

	if needsStorage {
		// Calculate size of the global
		size := b.calculateTypeSize(sym.Type)

		// Add to globals that need storage
		b.prog.GlobalVars = append(b.prog.GlobalVars, &GlobalVar{
			Name:    v.Name.Name,
			Type:    sym.Type,
			InitVal: v.Value,
			Mutable: v.Mutable,
			Size:    size,
		})

		// Create a GlobalRef operand
		op = GlobalRef(v.Name.Name, sym.Type)
	}

	b.prog.Globals[v.Name.Name] = op
}

func (b *Builder) buildFnDecl(fn *ast.FnDecl) {
	// Skip generic function declarations - they will be instantiated when called
	if len(fn.TypeParams) > 0 {
		return
	}

	// Get the function type from type info
	sym := b.info.Defs[fn.Name]
	if sym == nil {
		// This function may be a monomorphized instantiation without a symbol
		return
	}
	fnType := sym.Type.(*types.Function)

	// Create IR function
	irFn := &Function{
		Name:   fn.Name.Name,
		Result: fnType.Result,
	}
	b.fn = irFn
	b.prog.Functions = append(b.prog.Functions, irFn)

	// Create entry block
	entry := irFn.NewBlock("entry")
	irFn.Entry = entry
	b.block = entry

	// Handle parameters
	for i, p := range fn.Params {
		vreg := irFn.NewVReg(fnType.Params[i].Type)
		param := &Param{
			Name: p.Name.Name,
			Type: fnType.Params[i].Type,
			VReg: vreg.VReg,
		}
		irFn.Params = append(irFn.Params, param)

		// Load parameter into vreg
		b.emit(&Instr{
			Op:   OpLoadParam,
			Dest: vreg,
			Args: []Operand{Imm(int64(i), types.Typ[types.Int])},
		})

		// Record in globals for lookup (temporary - should use scope)
		b.prog.Globals[p.Name.Name] = vreg
	}

	// For main function, inject global initialization code
	if fn.Name.Name == "main" {
		b.buildGlobalInitializers()
	}

	// Build function body
	if fn.Body != nil {
		b.buildBlock(fn.Body)
	}

	// If no explicit return, add implicit return
	if b.block != nil && (len(b.block.Instrs) == 0 || b.block.Instrs[len(b.block.Instrs)-1].Op != OpReturn) {
		if fnType.Result.Equals(types.Typ[types.Unit]) {
			b.emit(&Instr{Op: OpReturn})
		}
	}

	// Clean up globals (parameters were temporary)
	for _, p := range fn.Params {
		delete(b.prog.Globals, p.Name.Name)
	}

	b.fn = nil
}

func (b *Builder) buildImplDecl(impl *ast.ImplDecl) {
	// Get the type name
	var typeName string
	switch t := impl.Type.(type) {
	case *ast.NamedType:
		typeName = t.Name.Name
	default:
		return
	}

	// Build each method
	for i := range impl.Methods {
		method := &impl.Methods[i]
		b.buildMethod(typeName, method)
	}
}

func (b *Builder) buildMethod(typeName string, method *ast.FnDecl) {
	// Get the method type from TypeInfo (we stored it during sema)
	// Method name in IR is TypeName.MethodName
	methodName := typeName + "." + method.Name.Name

	// Build function type from parameters
	var params []*types.Param
	for _, p := range method.Params {
		paramType := b.info.Types[p.Name]
		if paramType == nil {
			// Fall back to resolving type
			if sym, ok := b.info.Defs[p.Name]; ok {
				paramType = sym.Type
			}
		}
		if paramType == nil {
			// Last resort
			paramType = types.Typ[types.Int]
		}
		params = append(params, &types.Param{Name: p.Name.Name, Type: paramType})
	}

	// Get return type from type info's analysis of the method body's return statements
	var resultType types.Type = types.Typ[types.Unit]
	if method.Body != nil && len(method.Body.Stmts) > 0 {
		// Check the last statement for return type
		for _, stmt := range method.Body.Stmts {
			if ret, ok := stmt.(*ast.ReturnStmt); ok && ret.Value != nil {
				if retType := b.info.Types[ret.Value]; retType != nil {
					resultType = retType
					break
				}
			}
		}
	}

	// Create IR function for the method
	irFn := &Function{
		Name:   methodName,
		Result: resultType,
	}
	b.fn = irFn
	b.prog.Functions = append(b.prog.Functions, irFn)

	// Create entry block
	entry := irFn.NewBlock("entry")
	irFn.Entry = entry
	b.block = entry

	// Handle parameters
	for i, p := range method.Params {
		paramType := params[i].Type
		vreg := irFn.NewVReg(paramType)
		param := &Param{
			Name: p.Name.Name,
			Type: paramType,
			VReg: vreg.VReg,
		}
		irFn.Params = append(irFn.Params, param)

		// Load parameter into vreg
		b.emit(&Instr{
			Op:   OpLoadParam,
			Dest: vreg,
			Args: []Operand{Imm(int64(i), types.Typ[types.Int])},
		})

		// Record in globals for lookup
		b.prog.Globals[p.Name.Name] = vreg
	}

	// Build method body
	if method.Body != nil {
		b.buildBlock(method.Body)
	}

	// If no explicit return, add implicit return
	if b.block != nil && (len(b.block.Instrs) == 0 || b.block.Instrs[len(b.block.Instrs)-1].Op != OpReturn) {
		if resultType.Equals(types.Typ[types.Unit]) {
			b.emit(&Instr{Op: OpReturn})
		}
	}

	// Clean up globals (parameters were temporary)
	for _, p := range method.Params {
		delete(b.prog.Globals, p.Name.Name)
	}

	b.fn = nil
}

func (b *Builder) buildBlock(block *ast.BlockStmt) Operand {
	// Save existing variable bindings
	savedVars := make(map[string]Operand)

	var lastExprResult Operand
	for i, stmt := range block.Stmts {
		// If this is the last statement and it's an ExprStmt, use its value as the block result
		if i == len(block.Stmts)-1 {
			if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
				lastExprResult = b.buildExpr(exprStmt.Expr)
				continue
			}
		}
		b.buildStmt(stmt, savedVars)
	}

	var result Operand
	if block.Expr != nil {
		result = b.buildExpr(block.Expr)
	} else if lastExprResult.Kind != OpndNone {
		result = lastExprResult
	}

	// Restore variable bindings
	for name, op := range savedVars {
		if op.Kind == OpndNone {
			delete(b.prog.Globals, name)
		} else {
			b.prog.Globals[name] = op
		}
	}

	return result
}

func (b *Builder) buildStmt(stmt ast.Stmt, savedVars map[string]Operand) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		b.buildLetStmt(s, savedVars)
	case *ast.ExprStmt:
		b.buildExpr(s.Expr)
	case *ast.ReturnStmt:
		b.buildReturnStmt(s)
	case *ast.IfStmt:
		b.buildIfStmt(s)
	case *ast.ForStmt:
		b.buildForStmt(s)
	case *ast.MatchStmt:
		b.buildMatchStmt(s)
	case *ast.BreakStmt:
		if b.loopEnd != nil {
			b.emit(&Instr{Op: OpJump, Args: []Operand{Label(b.loopEnd.Label)}})
		}
	case *ast.ContinueStmt:
		if b.loopStart != nil {
			b.emit(&Instr{Op: OpJump, Args: []Operand{Label(b.loopStart.Label)}})
		}
	case *ast.BlockStmt:
		b.buildBlock(s)
	}
}

func (b *Builder) buildLetStmt(s *ast.LetStmt, savedVars map[string]Operand) {
	// Get the type from type info
	var varType types.Type
	if s.Type != nil {
		// Type annotation present - look up via the value's type
		if s.Value != nil {
			varType = b.info.Types[s.Value]
		}
	} else if s.Value != nil {
		varType = b.info.Types[s.Value]
	}

	if varType == nil {
		varType = types.Typ[types.Invalid]
	}

	// Build the value expression
	var val Operand
	if s.Value != nil {
		val = b.buildExpr(s.Value)
	} else {
		// Zero initialization
		val = Imm(0, varType)
	}

	// Handle identifier pattern
	if ident, ok := s.Pattern.(*ast.IdentPattern); ok {
		name := ident.Name.Name

		// Save old binding if exists
		if old, exists := b.prog.Globals[name]; exists {
			savedVars[name] = old
		} else {
			savedVars[name] = None()
		}

		// For struct and array types, allocate space and copy the data
		// Arrays need special handling because they're 24-byte fat pointers
		underlying := varType.Underlying()
		_, isStruct := underlying.(*types.Struct)
		_, isArray := underlying.(*types.Array)
		_, isSlice := underlying.(*types.Slice)

		if isStruct || isArray || isSlice {
			size := b.typeSize(varType)
			dest := b.fn.NewVReg(types.NewPointer(varType, false))
			b.emit(&Instr{
				Op:   OpAlloc,
				Dest: dest,
				Args: []Operand{Imm(int64(size), types.Typ[types.Int])},
			})
			// Copy data from source to destination (struct or fat pointer)
			b.emit(&Instr{
				Op:   OpMemCopy,
				Args: []Operand{val, dest, Imm(int64(size), types.Typ[types.Int])},
			})
			b.prog.Globals[name] = dest
		} else {
			dest := b.fn.NewVReg(varType)
			b.emit(&Instr{
				Op:   OpCopy,
				Dest: dest,
				Args: []Operand{val},
			})
			b.prog.Globals[name] = dest
		}
	}
}

func (b *Builder) buildReturnStmt(s *ast.ReturnStmt) {
	if s.Value != nil {
		val := b.buildExpr(s.Value)
		b.emit(&Instr{Op: OpReturn, Args: []Operand{val}})
	} else {
		b.emit(&Instr{Op: OpReturn})
	}
}

func (b *Builder) buildIfStmt(s *ast.IfStmt) {
	cond := b.buildExpr(s.Cond)

	thenBlock := b.fn.NewBlock(b.newLabel("if.then"))
	elseBlock := b.fn.NewBlock(b.newLabel("if.else"))
	endBlock := b.fn.NewBlock(b.newLabel("if.end"))

	// Branch
	b.emit(&Instr{
		Op:   OpBranch,
		Args: []Operand{cond, Label(thenBlock.Label), Label(elseBlock.Label)},
	})

	// Then block
	b.block = thenBlock
	b.buildBlock(s.Then)
	if b.block != nil && (len(b.block.Instrs) == 0 || b.block.Instrs[len(b.block.Instrs)-1].Op != OpReturn) {
		b.emit(&Instr{Op: OpJump, Args: []Operand{Label(endBlock.Label)}})
	}

	// Else block
	b.block = elseBlock
	if s.Else != nil {
		switch e := s.Else.(type) {
		case *ast.BlockStmt:
			b.buildBlock(e)
		case *ast.IfStmt:
			b.buildIfStmt(e)
		}
	}
	if b.block != nil && (len(b.block.Instrs) == 0 || b.block.Instrs[len(b.block.Instrs)-1].Op != OpReturn) {
		b.emit(&Instr{Op: OpJump, Args: []Operand{Label(endBlock.Label)}})
	}

	b.block = endBlock
}

func (b *Builder) buildMatchStmt(s *ast.MatchStmt) {
	// Build the scrutinee
	scrutinee := b.buildExpr(s.Expr)
	scrutineeType := b.info.Types[s.Expr]

	// Create end block
	endBlock := b.fn.NewBlock(b.newLabel("match.end"))

	// Build each arm
	for i, arm := range s.Arms {
		armBlock := b.fn.NewBlock(b.newLabel(fmt.Sprintf("match.arm%d", i)))
		var nextBlock *Block
		if i < len(s.Arms)-1 {
			nextBlock = b.fn.NewBlock(b.newLabel(fmt.Sprintf("match.next%d", i)))
		} else {
			nextBlock = endBlock
		}

		// Build pattern match check
		b.buildPatternCheck(arm.Pattern, scrutinee, scrutineeType, armBlock, nextBlock)

		// Build arm body
		b.block = armBlock
		b.buildPatternBindings(arm.Pattern, scrutinee, scrutineeType)

		// Build the arm body expression
		armResult := b.buildExpr(arm.Body)

		// If this is a function that returns a value, emit a return
		if b.fn.Result != nil && !b.fn.Result.Equals(types.Typ[types.Unit]) {
			b.emit(&Instr{Op: OpReturn, Args: []Operand{armResult}})
		} else {
			b.emit(&Instr{Op: OpJump, Args: []Operand{Label(endBlock.Label)}})
		}

		// Continue to next check block
		if i < len(s.Arms)-1 {
			b.block = nextBlock
		}
	}

	b.block = endBlock
}

func (b *Builder) buildForStmt(s *ast.ForStmt) {
	savedLoopStart := b.loopStart
	savedLoopEnd := b.loopEnd

	// For range loops, we need an increment block
	condBlock := b.fn.NewBlock(b.newLabel("for.cond"))
	bodyBlock := b.fn.NewBlock(b.newLabel("for.body"))
	incrBlock := b.fn.NewBlock(b.newLabel("for.incr"))
	endBlock := b.fn.NewBlock(b.newLabel("for.end"))

	// continue jumps to incr (for range) or cond (for others)
	b.loopEnd = endBlock

	// Handle range-based loops: for x in start..end
	var loopVar Operand
	var endVal Operand
	if s.Iter != nil && s.Pattern != nil {
		if rangeExpr, ok := s.Iter.(*ast.RangeExpr); ok {
			// Initialize loop variable
			loopVar = b.fn.NewVReg(types.Typ[types.Int])

			var startVal Operand
			if rangeExpr.Start != nil {
				startVal = b.buildExpr(rangeExpr.Start)
			} else {
				startVal = Imm(0, types.Typ[types.Int])
			}

			b.emit(&Instr{Op: OpCopy, Dest: loopVar, Args: []Operand{startVal}})

			// Build end value
			if rangeExpr.End != nil {
				endVal = b.buildExpr(rangeExpr.End)
			}

			// Bind the pattern variable to the loop variable
			if ident, ok := s.Pattern.(*ast.IdentPattern); ok {
				b.prog.Globals[ident.Name.Name] = loopVar
			}

			b.loopStart = incrBlock // continue should go to increment
		}
	} else {
		b.loopStart = condBlock // continue goes to condition check
	}

	// Jump to condition
	b.emit(&Instr{Op: OpJump, Args: []Operand{Label(condBlock.Label)}})

	// Condition block
	b.block = condBlock
	if s.Cond != nil {
		cond := b.buildExpr(s.Cond)
		b.emit(&Instr{
			Op:   OpBranch,
			Args: []Operand{cond, Label(bodyBlock.Label), Label(endBlock.Label)},
		})
	} else if s.Iter != nil && loopVar.Kind != OpndNone {
		// Range loop: check loopVar < end
		cmp := b.fn.NewVReg(types.Typ[types.Bool])
		b.emit(&Instr{Op: OpLt, Dest: cmp, Args: []Operand{loopVar, endVal}})
		b.emit(&Instr{
			Op:   OpBranch,
			Args: []Operand{cmp, Label(bodyBlock.Label), Label(endBlock.Label)},
		})
	} else {
		// Infinite loop
		b.emit(&Instr{Op: OpJump, Args: []Operand{Label(bodyBlock.Label)}})
	}

	// Body block
	b.block = bodyBlock
	b.buildBlock(s.Body)
	// After body, jump to increment (for range) or condition (for others)
	if b.block != nil && (len(b.block.Instrs) == 0 || b.block.Instrs[len(b.block.Instrs)-1].Op != OpReturn) {
		if loopVar.Kind != OpndNone {
			b.emit(&Instr{Op: OpJump, Args: []Operand{Label(incrBlock.Label)}})
		} else {
			b.emit(&Instr{Op: OpJump, Args: []Operand{Label(condBlock.Label)}})
		}
	}

	// Increment block (for range loops)
	if loopVar.Kind != OpndNone {
		b.block = incrBlock
		one := Imm(1, types.Typ[types.Int])
		newVal := b.fn.NewVReg(types.Typ[types.Int])
		b.emit(&Instr{Op: OpAdd, Dest: newVal, Args: []Operand{loopVar, one}})
		b.emit(&Instr{Op: OpCopy, Dest: loopVar, Args: []Operand{newVal}})
		b.emit(&Instr{Op: OpJump, Args: []Operand{Label(condBlock.Label)}})
	}

	b.block = endBlock
	b.loopStart = savedLoopStart
	b.loopEnd = savedLoopEnd
}

func (b *Builder) buildExpr(expr ast.Expr) Operand {
	switch e := expr.(type) {
	case *ast.Ident:
		return b.buildIdent(e)
	case *ast.IntLit:
		return Imm(e.Value, types.Typ[types.Int])
	case *ast.FloatLit:
		// For simplicity, treat floats as integers for now
		return Imm(int64(e.Value), types.Typ[types.Float64])
	case *ast.BoolLit:
		var v int64
		if e.Value {
			v = 1
		}
		return Imm(v, types.Typ[types.Bool])
	case *ast.StringLit:
		idx := b.prog.AddString(e.Value)
		return StrConst(idx)
	case *ast.CharLit:
		return Imm(int64(e.Value), types.Typ[types.Int])
	case *ast.BinaryExpr:
		return b.buildBinaryExpr(e)
	case *ast.UnaryExpr:
		return b.buildUnaryExpr(e)
	case *ast.CallExpr:
		return b.buildCallExpr(e)
	case *ast.IfExpr:
		return b.buildIfExpr(e)
	case *ast.BlockExpr:
		return b.buildBlock(e.Block)
	case *ast.ArrayExpr:
		return b.buildArrayExpr(e)
	case *ast.MapExpr:
		return b.buildMapExpr(e)
	case *ast.IndexExpr:
		return b.buildIndexExpr(e)
	case *ast.SliceExpr:
		return b.buildSliceExpr(e)
	case *ast.StructExpr:
		return b.buildStructExpr(e)
	case *ast.FieldExpr:
		return b.buildFieldExpr(e)
	case *ast.MethodExpr:
		return b.buildMethodExpr(e)
	case *ast.PathExpr:
		return b.buildPathExpr(e)
	case *ast.MatchExpr:
		return b.buildMatchExpr(e)
	case *ast.TryExpr:
		return b.buildTryExpr(e)
	default:
		return None()
	}
}

func (b *Builder) buildIdent(ident *ast.Ident) Operand {
	// Look up the variable
	if op, ok := b.prog.Globals[ident.Name]; ok {
		// For GlobalRef operands, check if it's mutable
		if op.Kind == OpndGlobal {
			// Check if this is a mutable global
			// For mutable globals, we want to return the address so modifications persist
			// For immutable globals, we can load the value
			sym := b.info.Uses[ident]
			if sym == nil {
				sym = b.info.Defs[ident]
			}

			size := b.calculateTypeSize(op.Type)
			// Check if this is actually a complex type (struct, array, slice)
			// Don't just check size, as 8-byte structs need address semantics
			isComplexType := false
			underlying := op.Type.Underlying()
			switch underlying.(type) {
			case *types.Struct, *types.Array, *types.Slice:
				isComplexType = true
			}

			if isComplexType || size > 8 {
				// Complex type (array, struct) - return address
				if sym != nil && sym.Mutable {
					// Mutable: return the global address directly
					// This allows in-place modifications (like push) to persist
					return op
				} else {
					// Immutable: create a local copy
					result := b.fn.NewVReg(types.NewPointer(op.Type, false))
					b.emit(&Instr{
						Op:   OpAlloc,
						Dest: result,
						Args: []Operand{Imm(int64(size), types.Typ[types.Int])},
					})
					b.emit(&Instr{
						Op:   OpMemCopy,
						Args: []Operand{op, result, Imm(int64(size), types.Typ[types.Int])},
					})
					return result
				}
			} else {
				// Simple type: use OpLoad
				result := b.fn.NewVReg(op.Type)
				b.emit(&Instr{
					Op:   OpLoad,
					Dest: result,
					Args: []Operand{op},
				})
				return result
			}
		}
		return op
	}

	// Check if this is a function reference using semantic analysis info
	// This handles both forward and backward function references
	if sym, ok := b.info.Uses[ident]; ok {
		if sym.Kind == symbols.FuncSymbol {
			return FuncRef(sym.Name, nil)
		}
	}

	// Fallback: check if it's in the already-processed functions
	// (for cases where semantic info might not be available)
	for _, fn := range b.prog.Functions {
		if fn.Name == ident.Name {
			return FuncRef(fn.Name, nil)
		}
	}
	return None()
}

func (b *Builder) buildBinaryExpr(e *ast.BinaryExpr) Operand {
	// Handle assignment specially
	if e.Op.Type == token.Assign {
		return b.buildAssignment(e)
	}

	// Handle short-circuit logical operators
	if e.Op.Type == token.LogicalAnd {
		return b.buildLogicalAnd(e)
	}
	if e.Op.Type == token.LogicalOr {
		return b.buildLogicalOr(e)
	}

	left := b.buildExpr(e.Left)
	right := b.buildExpr(e.Right)

	// Get result type from type info
	resultType := b.info.Types[e]
	if resultType == nil {
		resultType = left.Type
	}

	dest := b.fn.NewVReg(resultType)

	// Check if this is a string operation
	leftType := b.info.Types[e.Left]
	isString := false
	if basic, ok := leftType.(*types.Basic); ok && basic.Kind == types.String {
		isString = true
	}

	var op Op
	switch e.Op.Type {
	case token.Plus:
		if isString {
			op = OpStrConcat
		} else {
			op = OpAdd
		}
	case token.Minus:
		op = OpSub
	case token.Star:
		op = OpMul
	case token.Slash:
		op = OpDiv
	case token.Percent:
		op = OpMod
	case token.Equal:
		if isString {
			op = OpStrEq
		} else {
			op = OpEq
		}
	case token.NotEqual:
		if isString {
			op = OpStrNe
		} else {
			op = OpNe
		}
	case token.Less:
		op = OpLt
	case token.LessEqual:
		op = OpLe
	case token.Greater:
		op = OpGt
	case token.GreaterEqual:
		op = OpGe
	case token.Ampersand:
		op = OpAnd
	case token.Pipe:
		op = OpOr
	case token.Caret:
		op = OpXor
	default:
		return None()
	}

	b.emit(&Instr{
		Op:   op,
		Dest: dest,
		Args: []Operand{left, right},
	})

	return dest
}

// buildLogicalAnd implements short-circuit evaluation for &&
// left && right => if left then right else false
func (b *Builder) buildLogicalAnd(e *ast.BinaryExpr) Operand {
	result := b.fn.NewVReg(types.Typ[types.Bool])

	// Create blocks for short-circuit evaluation
	rightBlock := b.fn.NewBlock(b.newLabel("and.right"))
	falseBlock := b.fn.NewBlock(b.newLabel("and.false"))
	endBlock := b.fn.NewBlock(b.newLabel("and.end"))

	// Evaluate left operand
	left := b.buildExpr(e.Left)

	// If left is true, evaluate right; otherwise short-circuit to false
	b.emit(&Instr{
		Op:   OpBranch,
		Args: []Operand{left, Label(rightBlock.Label), Label(falseBlock.Label)},
	})

	// Right block: evaluate right operand and copy to result
	b.block = rightBlock
	right := b.buildExpr(e.Right)
	b.emit(&Instr{
		Op:   OpCopy,
		Dest: result,
		Args: []Operand{right},
	})
	b.emit(&Instr{Op: OpJump, Args: []Operand{Label(endBlock.Label)}})

	// False block: result = false
	b.block = falseBlock
	b.emit(&Instr{
		Op:   OpCopy,
		Dest: result,
		Args: []Operand{Imm(0, types.Typ[types.Bool])},
	})
	b.emit(&Instr{Op: OpJump, Args: []Operand{Label(endBlock.Label)}})

	// End block
	b.block = endBlock

	return result
}

// buildLogicalOr implements short-circuit evaluation for ||
// left || right => if left then true else right
func (b *Builder) buildLogicalOr(e *ast.BinaryExpr) Operand {
	result := b.fn.NewVReg(types.Typ[types.Bool])

	// Create blocks for short-circuit evaluation
	trueBlock := b.fn.NewBlock(b.newLabel("or.true"))
	rightBlock := b.fn.NewBlock(b.newLabel("or.right"))
	endBlock := b.fn.NewBlock(b.newLabel("or.end"))

	// Evaluate left operand
	left := b.buildExpr(e.Left)

	// If left is true, short-circuit to true; otherwise evaluate right
	b.emit(&Instr{
		Op:   OpBranch,
		Args: []Operand{left, Label(trueBlock.Label), Label(rightBlock.Label)},
	})

	// True block: result = true
	b.block = trueBlock
	b.emit(&Instr{
		Op:   OpCopy,
		Dest: result,
		Args: []Operand{Imm(1, types.Typ[types.Bool])},
	})
	b.emit(&Instr{Op: OpJump, Args: []Operand{Label(endBlock.Label)}})

	// Right block: evaluate right operand and copy to result
	b.block = rightBlock
	right := b.buildExpr(e.Right)
	b.emit(&Instr{
		Op:   OpCopy,
		Dest: result,
		Args: []Operand{right},
	})
	b.emit(&Instr{Op: OpJump, Args: []Operand{Label(endBlock.Label)}})

	// End block
	b.block = endBlock

	return result
}

func (b *Builder) buildAssignment(e *ast.BinaryExpr) Operand {
	// Build the right-hand side value
	right := b.buildExpr(e.Right)

	switch left := e.Left.(type) {
	case *ast.Ident:
		// Simple variable assignment
		return b.buildIdentAssignment(left, right)

	case *ast.FieldExpr:
		// Struct field assignment: obj.field = value
		return b.buildFieldAssignment(left, right)

	case *ast.IndexExpr:
		// Array/slice index assignment: arr[i] = value
		return b.buildIndexAssignment(left, right)

	default:
		return None()
	}
}

func (b *Builder) buildIdentAssignment(ident *ast.Ident, right Operand) Operand {
	// Get the current operand for this variable
	oldOp, exists := b.prog.Globals[ident.Name]
	if !exists {
		return None()
	}

	// Get the variable's type from semantic analysis
	varType := b.info.Types[ident]

	// If the variable has a virtual register, emit a copy instruction
	if oldOp.Kind == OpndVReg {
		// For struct types, copy the struct data
		if varType != nil {
			if _, isStruct := varType.Underlying().(*types.Struct); isStruct {
				size := b.typeSize(varType)
				b.emit(&Instr{
					Op:   OpMemCopy,
					Args: []Operand{right, oldOp, Imm(int64(size), types.Typ[types.Int])},
				})
				return None()
			}
		}
		b.emit(&Instr{
			Op:   OpCopy,
			Dest: oldOp,
			Args: []Operand{right},
		})
	} else if oldOp.Kind == OpndGlobal {
		// For GlobalRef operands, use OpMemCopy for structs, OpStore otherwise
		if varType != nil {
			if _, isStruct := varType.Underlying().(*types.Struct); isStruct {
				size := b.typeSize(varType)
				b.emit(&Instr{
					Op:   OpMemCopy,
					Args: []Operand{right, oldOp, Imm(int64(size), types.Typ[types.Int])},
				})
				return None()
			}
		}
		b.emit(&Instr{
			Op:   OpStore,
			Args: []Operand{right, oldOp},
		})
	} else {
		// Update the binding to point to the new value (for immediates/constants)
		b.prog.Globals[ident.Name] = right
	}

	return None()
}

func (b *Builder) buildFieldAssignment(field *ast.FieldExpr, right Operand) Operand {
	// Get the struct pointer
	structPtr := b.buildExpr(field.Expr)

	// Get the struct type
	exprType := b.info.Types[field.Expr]
	if exprType == nil {
		return None()
	}

	// Handle pointer types (auto-dereference)
	underlying := exprType.Underlying()
	if ptr, ok := underlying.(*types.Pointer); ok {
		underlying = ptr.Elem.Underlying()
	}

	st, ok := underlying.(*types.Struct)
	if !ok {
		return None()
	}

	// Get field info
	fieldName := field.Field.Name
	fieldType := st.FieldByName(fieldName)
	offset := b.fieldOffset(st, fieldName)
	if offset < 0 || fieldType == nil {
		return None()
	}

	// Compute field address
	fieldAddr := b.fn.NewVReg(types.NewPointer(right.Type, false))
	b.emit(&Instr{
		Op:   OpIndexAddr,
		Dest: fieldAddr,
		Args: []Operand{structPtr, Imm(int64(offset), types.Typ[types.Int])},
	})

	// For struct-typed fields, copy the entire struct data
	if _, isStruct := fieldType.Type.Underlying().(*types.Struct); isStruct {
		size := b.typeSize(fieldType.Type)
		b.emit(&Instr{
			Op:   OpMemCopy,
			Args: []Operand{right, fieldAddr, Imm(int64(size), types.Typ[types.Int])},
		})
	} else {
		// Store the value for non-struct types
		b.emit(&Instr{
			Op:   OpStore,
			Args: []Operand{right, fieldAddr},
		})
	}

	return None()
}

func (b *Builder) buildIndexAssignment(index *ast.IndexExpr, right Operand) Operand {
	// Get the array/slice/map pointer
	arrayPtr := b.buildExpr(index.Expr)

	// Get the index value
	idx := b.buildExpr(index.Index)

	// Get the array/slice/map type
	exprType := b.info.Types[index.Expr]
	if exprType == nil {
		return None()
	}

	var elemType types.Type
	var elemSize int64

	switch t := exprType.Underlying().(type) {
	case *types.Array:
		elemType = t.Elem
		elemSize = int64(b.typeSize(elemType))
		// Arrays are fat pointers - load the data pointer from offset 0
		dataPtr := b.fn.NewVReg(types.NewPointer(elemType, false))
		b.emit(&Instr{
			Op:   OpLoad,
			Dest: dataPtr,
			Args: []Operand{arrayPtr},
		})
		arrayPtr = dataPtr
	case *types.Slice:
		elemType = t.Elem
		elemSize = int64(b.typeSize(elemType))
		// Slices are fat pointers - load the data pointer from offset 0
		dataPtr := b.fn.NewVReg(types.NewPointer(elemType, false))
		b.emit(&Instr{
			Op:   OpLoad,
			Dest: dataPtr,
			Args: []Operand{arrayPtr},
		})
		arrayPtr = dataPtr
	case *types.Map:
		// Map assignment: emit OpMapSet
		b.emit(&Instr{
			Op:   OpMapSet,
			Args: []Operand{arrayPtr, idx, right},
		})
		return None()
	default:
		return None()
	}

	// Compute element address: base + index * elem_size
	offset := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpMul,
		Dest: offset,
		Args: []Operand{idx, Imm(elemSize, types.Typ[types.Int])},
	})

	elemAddr := b.fn.NewVReg(types.NewPointer(elemType, false))
	b.emit(&Instr{
		Op:   OpIndexAddr,
		Dest: elemAddr,
		Args: []Operand{arrayPtr, offset},
	})

	// Store the value
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{right, elemAddr},
	})

	return None()
}

func (b *Builder) buildUnaryExpr(e *ast.UnaryExpr) Operand {
	right := b.buildExpr(e.Right)

	resultType := b.info.Types[e]
	if resultType == nil {
		resultType = right.Type
	}

	dest := b.fn.NewVReg(resultType)

	var op Op
	switch e.Op.Type {
	case token.Minus:
		op = OpNeg
	case token.Not:
		op = OpNot
	case token.Tilde:
		op = OpNot
	default:
		return None()
	}

	b.emit(&Instr{
		Op:   op,
		Dest: dest,
		Args: []Operand{right},
	})

	return dest
}

func (b *Builder) buildCallExpr(e *ast.CallExpr) Operand {
	// Check for builtin functions
	if ident, ok := e.Func.(*ast.Ident); ok {
		switch ident.Name {
		case "len":
			return b.buildLenBuiltin(e)
		case "cap":
			return b.buildCapBuiltin(e)
		case "push":
			return b.buildPushBuiltin(e)
		case "print":
			return b.buildPrintBuiltin(e)
		case "heap_alloc":
			return b.buildHeapAllocBuiltin(e)
		case "poke":
			return b.buildPokeBuiltin(e)
		case "peek":
			return b.buildPeekBuiltin(e)
		case "str_len":
			return b.buildStrLenBuiltin(e)
		case "mem_set":
			return b.buildMemSetBuiltin(e)
		case "syscall_open":
			return b.buildSyscallOpen(e)
		case "syscall_read":
			return b.buildSyscallRead(e)
		case "syscall_write":
			return b.buildSyscallWrite(e)
		case "syscall_close":
			return b.buildSyscallClose(e)
		case "str_contains":
			return b.buildStrContains(e)
		case "str_starts_with":
			return b.buildStrStartsWith(e)
		case "str_ends_with":
			return b.buildStrEndsWith(e)
		case "str_index_of":
			return b.buildStrIndexOf(e)
		case "str_substring":
			return b.buildStrSubstring(e)
		case "str_char_at":
			return b.buildStrCharAt(e)
		case "str_concat":
			return b.buildStrConcatBuiltin(e)
		case "str_trim":
			return b.buildStrTrim(e)
		case "str_replace":
			return b.buildStrReplace(e)
		case "str_split":
			return b.buildStrSplit(e)
		}
	}

	// Handle package-qualified builtins (e.g., os.ReadFile)
	if field, ok := e.Func.(*ast.FieldExpr); ok {
		if pkg, ok := field.Expr.(*ast.Ident); ok && pkg.Name == "os" {
			switch field.Field.Name {
			case "ReadFile":
				return b.buildReadFileBuiltin(e)
			case "WriteFile":
				return b.buildWriteFileBuiltin(e)
			}
		}
	}

	// Check if this is a call to a monomorphized generic function
	var fn Operand
	if mangledName, ok := b.info.GenericCalls[e]; ok {
		fn = FuncRef(mangledName, nil)
	} else {
		// Build function operand normally
		fn = b.buildExpr(e.Func)
	}

	// Build arguments
	args := make([]Operand, len(e.Args)+1)
	args[0] = fn
	for i, arg := range e.Args {
		args[i+1] = b.buildExpr(arg)
	}

	// Get result type from type info
	resultType := b.info.Types[e]
	if resultType == nil {
		resultType = types.Typ[types.Unit]
	}

	if resultType.Equals(types.Typ[types.Unit]) {
		b.emit(&Instr{
			Op:   OpCall,
			Args: args,
		})
		return None()
	}

	dest := b.fn.NewVReg(resultType)
	b.emit(&Instr{
		Op:   OpCall,
		Dest: dest,
		Args: args,
	})
	return dest
}

func (b *Builder) buildLenBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	arg := b.buildExpr(e.Args[0])
	argType := b.info.Types[e.Args[0]]

	result := b.fn.NewVReg(types.Typ[types.Int])

	switch argType.Underlying().(type) {
	case *types.Array, *types.Slice:
		// For arrays/slices, load length from fat pointer (offset 8)
		lenAddr := b.fn.NewVReg(types.NewPointer(types.Typ[types.Int], false))
		b.emit(&Instr{
			Op:   OpIndexAddr,
			Dest: lenAddr,
			Args: []Operand{arg, Imm(8, types.Typ[types.Int])},
		})
		b.emit(&Instr{
			Op:   OpLoad,
			Dest: result,
			Args: []Operand{lenAddr},
		})
	case *types.Map:
		// Map length - load count from map header (offset 8)
		b.emit(&Instr{
			Op:   OpMapLen,
			Dest: result,
			Args: []Operand{arg},
		})
	case *types.Basic:
		basic := argType.Underlying().(*types.Basic)
		if basic.Kind == types.String {
			// String length - scans for null terminator
			b.emit(&Instr{
				Op:   OpStrLen,
				Dest: result,
				Args: []Operand{arg},
			})
		} else {
			// Other basic types don't have length
			b.emit(&Instr{
				Op:   OpCopy,
				Dest: result,
				Args: []Operand{Imm(0, types.Typ[types.Int])},
			})
		}
	default:
		b.emit(&Instr{
			Op:   OpCopy,
			Dest: result,
			Args: []Operand{Imm(0, types.Typ[types.Int])},
		})
	}

	return result
}

func (b *Builder) buildCapBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	arg := b.buildExpr(e.Args[0])
	argType := b.info.Types[e.Args[0]]

	result := b.fn.NewVReg(types.Typ[types.Int])

	switch argType.Underlying().(type) {
	case *types.Array, *types.Slice:
		// For arrays/slices, load capacity from fat pointer (offset 16)
		capAddr := b.fn.NewVReg(types.NewPointer(types.Typ[types.Int], false))
		b.emit(&Instr{
			Op:   OpIndexAddr,
			Dest: capAddr,
			Args: []Operand{arg, Imm(16, types.Typ[types.Int])},
		})
		b.emit(&Instr{
			Op:   OpLoad,
			Dest: result,
			Args: []Operand{capAddr},
		})
	default:
		b.emit(&Instr{
			Op:   OpCopy,
			Dest: result,
			Args: []Operand{Imm(0, types.Typ[types.Int])},
		})
	}

	return result
}

func (b *Builder) buildPushBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	arr := b.buildExpr(e.Args[0])
	elem := b.buildExpr(e.Args[1])
	argType := b.info.Types[e.Args[0]]

	// Get element type and size
	var elemType types.Type
	switch t := argType.Underlying().(type) {
	case *types.Array:
		elemType = t.Elem
	case *types.Slice:
		elemType = t.Elem
	default:
		return None()
	}

	elemSize := b.typeSize(elemType)

	// Emit push operation
	b.emit(&Instr{
		Op:   OpArrayPush,
		Args: []Operand{arr, elem, Imm(int64(elemSize), types.Typ[types.Int])},
	})

	return None() // push returns unit
}

func (b *Builder) buildPrintBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	str := b.buildExpr(e.Args[0])

	b.emit(&Instr{
		Op:   OpPrint,
		Args: []Operand{str},
	})

	return None()
}

func (b *Builder) buildHeapAllocBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	size := b.buildExpr(e.Args[0])

	result := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpHeapAlloc,
		Dest: result,
		Args: []Operand{size},
	})

	return result
}

func (b *Builder) buildPokeBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	addr := b.buildExpr(e.Args[0])
	value := b.buildExpr(e.Args[1])

	b.emit(&Instr{
		Op:   OpPoke,
		Args: []Operand{addr, value},
	})

	return None()
}

func (b *Builder) buildPeekBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	addr := b.buildExpr(e.Args[0])

	result := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpPeek,
		Dest: result,
		Args: []Operand{addr},
	})

	return result
}

func (b *Builder) buildStrLenBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	str := b.buildExpr(e.Args[0])

	result := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpStrLen,
		Dest: result,
		Args: []Operand{str},
	})

	return result
}

func (b *Builder) buildMemSetBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 3 {
		return None()
	}

	addr := b.buildExpr(e.Args[0])
	value := b.buildExpr(e.Args[1])
	count := b.buildExpr(e.Args[2])

	b.emit(&Instr{
		Op:   OpMemSet,
		Args: []Operand{addr, value, count},
	})

	return None()
}

func (b *Builder) buildReadFileBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	path := b.buildExpr(e.Args[0])

	dest := b.fn.NewVReg(types.Typ[types.String])
	b.emit(&Instr{
		Op:   OpReadFile,
		Dest: dest,
		Args: []Operand{path},
	})

	return dest
}

func (b *Builder) buildSyscallOpen(e *ast.CallExpr) Operand {
	if len(e.Args) != 3 {
		return None()
	}

	path := b.buildExpr(e.Args[0])
	flags := b.buildExpr(e.Args[1])
	mode := b.buildExpr(e.Args[2])

	result := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpSyscallOpen,
		Dest: result,
		Args: []Operand{path, flags, mode},
	})

	return result
}

func (b *Builder) buildSyscallRead(e *ast.CallExpr) Operand {
	if len(e.Args) != 3 {
		return None()
	}

	fd := b.buildExpr(e.Args[0])
	buf := b.buildExpr(e.Args[1])
	size := b.buildExpr(e.Args[2])

	result := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpSyscallRead,
		Dest: result,
		Args: []Operand{fd, buf, size},
	})

	return result
}

func (b *Builder) buildSyscallWrite(e *ast.CallExpr) Operand {
	if len(e.Args) != 3 {
		return None()
	}

	fd := b.buildExpr(e.Args[0])
	buf := b.buildExpr(e.Args[1])
	size := b.buildExpr(e.Args[2])

	result := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpSyscallWrite,
		Dest: result,
		Args: []Operand{fd, buf, size},
	})

	return result
}

func (b *Builder) buildSyscallClose(e *ast.CallExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	fd := b.buildExpr(e.Args[0])

	result := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpSyscallClose,
		Dest: result,
		Args: []Operand{fd},
	})

	return result
}

func (b *Builder) buildWriteFileBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	path := b.buildExpr(e.Args[0])
	content := b.buildExpr(e.Args[1])

	result := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpWriteFile,
		Dest: result,
		Args: []Operand{path, content},
	})

	return result
}

func (b *Builder) buildOsReadFile(e *ast.MethodExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	path := b.buildExpr(e.Args[0])

	dest := b.fn.NewVReg(types.Typ[types.String])
	b.emit(&Instr{
		Op:   OpReadFile,
		Dest: dest,
		Args: []Operand{path},
	})

	return dest
}

func (b *Builder) buildOsWriteFile(e *ast.MethodExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	path := b.buildExpr(e.Args[0])
	content := b.buildExpr(e.Args[1])

	result := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpWriteFile,
		Dest: result,
		Args: []Operand{path, content},
	})

	return result
}

func (b *Builder) buildOsArgc(e *ast.MethodExpr) Operand {
	dest := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpArgc,
		Dest: dest,
	})
	return dest
}

func (b *Builder) buildOsArgv(e *ast.MethodExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	idx := b.buildExpr(e.Args[0])

	dest := b.fn.NewVReg(types.Typ[types.String])
	b.emit(&Instr{
		Op:   OpArgv,
		Dest: dest,
		Args: []Operand{idx},
	})
	return dest
}

func (b *Builder) buildStrconvItoa(e *ast.MethodExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	n := b.buildExpr(e.Args[0])

	dest := b.fn.NewVReg(types.Typ[types.String])
	b.emit(&Instr{
		Op:   OpIntToStr,
		Dest: dest,
		Args: []Operand{n},
	})
	return dest
}

func (b *Builder) buildStrconvAtoi(e *ast.MethodExpr) Operand {
	if len(e.Args) != 1 {
		return None()
	}

	s := b.buildExpr(e.Args[0])

	dest := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpStrToInt,
		Dest: dest,
		Args: []Operand{s},
	})
	return dest
}

func (b *Builder) buildIfExpr(e *ast.IfExpr) Operand {
	cond := b.buildExpr(e.Cond)

	resultType := b.info.Types[e]
	if resultType == nil {
		resultType = types.Typ[types.Unit]
	}

	var result Operand
	if !resultType.Equals(types.Typ[types.Unit]) {
		result = b.fn.NewVReg(resultType)
	}

	thenBlock := b.fn.NewBlock(b.newLabel("if.then"))
	elseBlock := b.fn.NewBlock(b.newLabel("if.else"))
	endBlock := b.fn.NewBlock(b.newLabel("if.end"))

	b.emit(&Instr{
		Op:   OpBranch,
		Args: []Operand{cond, Label(thenBlock.Label), Label(elseBlock.Label)},
	})

	// Then
	b.block = thenBlock
	thenVal := b.buildBlock(e.Then)
	if result.Kind != OpndNone {
		b.emit(&Instr{Op: OpCopy, Dest: result, Args: []Operand{thenVal}})
	}
	b.emit(&Instr{Op: OpJump, Args: []Operand{Label(endBlock.Label)}})

	// Else
	b.block = elseBlock
	if e.Else != nil {
		var elseVal Operand
		switch elseExpr := e.Else.(type) {
		case *ast.BlockExpr:
			elseVal = b.buildBlock(elseExpr.Block)
		case *ast.IfExpr:
			elseVal = b.buildIfExpr(elseExpr)
		default:
			elseVal = b.buildExpr(elseExpr)
		}
		if result.Kind != OpndNone {
			b.emit(&Instr{Op: OpCopy, Dest: result, Args: []Operand{elseVal}})
		}
	}
	b.emit(&Instr{Op: OpJump, Args: []Operand{Label(endBlock.Label)}})

	b.block = endBlock
	return result
}

func (b *Builder) emit(instr *Instr) {
	if b.block != nil {
		b.block.Instrs = append(b.block.Instrs, instr)
	}
}

func (b *Builder) newLabel(prefix string) string {
	b.labelCount++
	return fmt.Sprintf("%s.%d", prefix, b.labelCount)
}

// buildArrayExpr builds an array literal expression.
// Arrays are represented as fat pointers: [ptr (8 bytes), len (8 bytes)]
func (b *Builder) buildArrayExpr(e *ast.ArrayExpr) Operand {
	// Get the array type from type info
	arrayType := b.info.Types[e]
	if arrayType == nil {
		return None()
	}
	arrType, ok := arrayType.(*types.Array)
	if !ok {
		return None()
	}

	elemType := arrType.Elem
	elemSize := b.typeSize(elemType)
	numElems := int(arrType.Len)

	// Allocate space for elements on stack
	elemDataSize := numElems * elemSize
	elemPtr := b.fn.NewVReg(types.NewPointer(elemType, false))
	b.emit(&Instr{
		Op:   OpAlloc,
		Dest: elemPtr,
		Args: []Operand{Imm(int64(elemDataSize), types.Typ[types.Int])},
	})

	// Store each element
	if e.Repeat != nil {
		// [expr; count] syntax - same value repeated
		val := b.buildExpr(e.Repeat)
		for i := 0; i < numElems; i++ {
			offset := i * elemSize
			addr := b.fn.NewVReg(types.NewPointer(elemType, false))
			b.emit(&Instr{
				Op:   OpIndexAddr,
				Dest: addr,
				Args: []Operand{elemPtr, Imm(int64(offset), types.Typ[types.Int])},
			})
			b.emit(&Instr{
				Op:   OpStore,
				Args: []Operand{val, addr},
			})
		}
	} else {
		// [elem1, elem2, ...] syntax
		for i, elem := range e.Elements {
			val := b.buildExpr(elem)
			offset := i * elemSize
			addr := b.fn.NewVReg(types.NewPointer(elemType, false))
			b.emit(&Instr{
				Op:   OpIndexAddr,
				Dest: addr,
				Args: []Operand{elemPtr, Imm(int64(offset), types.Typ[types.Int])},
			})
			b.emit(&Instr{
				Op:   OpStore,
				Args: []Operand{val, addr},
			})
		}
	}

	// Create fat pointer: allocate 24 bytes for [ptr, len, cap]
	fatPtr := b.fn.NewVReg(arrayType)
	b.emit(&Instr{
		Op:   OpAlloc,
		Dest: fatPtr,
		Args: []Operand{Imm(24, types.Typ[types.Int])},
	})

	// Store ptr at offset 0
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{elemPtr, fatPtr},
	})

	// Store len at offset 8
	lenAddr := b.fn.NewVReg(types.NewPointer(types.Typ[types.Int], false))
	b.emit(&Instr{
		Op:   OpIndexAddr,
		Dest: lenAddr,
		Args: []Operand{fatPtr, Imm(8, types.Typ[types.Int])},
	})
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{Imm(int64(numElems), types.Typ[types.Int]), lenAddr},
	})

	// Store cap at offset 16 (for literals, cap = len)
	capAddr := b.fn.NewVReg(types.NewPointer(types.Typ[types.Int], false))
	b.emit(&Instr{
		Op:   OpIndexAddr,
		Dest: capAddr,
		Args: []Operand{fatPtr, Imm(16, types.Typ[types.Int])},
	})
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{Imm(int64(numElems), types.Typ[types.Int]), capAddr},
	})

	return fatPtr
}

// buildMapExpr builds a map literal expression.
// Maps are represented as a pointer to a map header.
func (b *Builder) buildMapExpr(e *ast.MapExpr) Operand {
	// Get the map type from type info
	mapType := b.info.Types[e]
	if mapType == nil {
		return None()
	}
	mType, ok := mapType.(*types.Map)
	if !ok {
		return None()
	}

	keySize := int64(b.typeSize(mType.Key))
	valSize := int64(b.typeSize(mType.Value))

	// Create a new map with OpMapNew
	mapPtr := b.fn.NewVReg(mapType)
	b.emit(&Instr{
		Op:   OpMapNew,
		Dest: mapPtr,
		Args: []Operand{Imm(keySize, types.Typ[types.Int]), Imm(valSize, types.Typ[types.Int])},
	})

	// Insert each entry with OpMapSet
	for _, entry := range e.Entries {
		key := b.buildExpr(entry.Key)
		val := b.buildExpr(entry.Value)
		b.emit(&Instr{
			Op:   OpMapSet,
			Args: []Operand{mapPtr, key, val},
		})
	}

	return mapPtr
}

// buildIndexExpr builds an array/slice/string/map index expression.
func (b *Builder) buildIndexExpr(e *ast.IndexExpr) Operand {
	// Get the array/slice/string/map expression
	base := b.buildExpr(e.Expr)
	idx := b.buildExpr(e.Index)

	// Get element type and check the container type
	exprType := b.info.Types[e.Expr]
	var elemType types.Type
	isString := false
	isMap := false

	switch t := exprType.Underlying().(type) {
	case *types.Array:
		elemType = t.Elem
	case *types.Slice:
		elemType = t.Elem
	case *types.Map:
		elemType = t.Value
		isMap = true
	case *types.Basic:
		if t.Kind == types.String {
			elemType = types.Typ[types.Int] // Return int for easier use with literals
			isString = true
		}
	}
	if elemType == nil {
		return None()
	}

	// Handle map index
	if isMap {
		result := b.fn.NewVReg(elemType)
		b.emit(&Instr{
			Op:   OpMapGet,
			Dest: result,
			Args: []Operand{base, idx},
		})
		return result
	}

	var dataPtr Operand
	if isString {
		// Strings are raw pointers to null-terminated data
		dataPtr = base
	} else {
		// Arrays/slices are fat pointers - extract data pointer (at offset 0)
		elemSize := b.typeSize(elemType)
		dataPtr = b.fn.NewVReg(types.NewPointer(elemType, false))
		b.emit(&Instr{
			Op:   OpLoad,
			Dest: dataPtr,
			Args: []Operand{base},
		})

		// Compute offset: idx * elemSize
		offset := b.fn.NewVReg(types.Typ[types.Int])
		b.emit(&Instr{
			Op:   OpMul,
			Dest: offset,
			Args: []Operand{idx, Imm(int64(elemSize), types.Typ[types.Int])},
		})

		// Compute element address
		elemAddr := b.fn.NewVReg(types.NewPointer(elemType, false))
		b.emit(&Instr{
			Op:   OpIndexAddr,
			Dest: elemAddr,
			Args: []Operand{dataPtr, offset},
		})

		// For struct types, return the address (pointer semantics)
		// Field access will use this address directly
		if _, isStruct := elemType.Underlying().(*types.Struct); isStruct {
			return elemAddr
		}

		// Load and return the element value for non-struct types
		result := b.fn.NewVReg(elemType)
		b.emit(&Instr{
			Op:   OpLoad,
			Dest: result,
			Args: []Operand{elemAddr},
		})
		return result
	}

	// String indexing: compute address and load byte
	// Compute element address: dataPtr + idx (byte offset)
	elemAddr := b.fn.NewVReg(types.NewPointer(elemType, false))
	b.emit(&Instr{
		Op:   OpIndexAddr,
		Dest: elemAddr,
		Args: []Operand{dataPtr, idx},
	})

	// Load byte and return
	result := b.fn.NewVReg(elemType)
	b.emit(&Instr{
		Op:   OpLoadByte,
		Dest: result,
		Args: []Operand{elemAddr},
	})

	return result
}

// buildSliceExpr builds a string slice expression.
func (b *Builder) buildSliceExpr(e *ast.SliceExpr) Operand {
	base := b.buildExpr(e.Expr)
	exprType := b.info.Types[e.Expr]

	// Only handle strings for now
	basic, ok := exprType.Underlying().(*types.Basic)
	if !ok || basic.Kind != types.String {
		return None()
	}

	// Default start is 0
	var start Operand
	if e.Start != nil {
		start = b.buildExpr(e.Start)
	} else {
		start = Imm(0, types.Typ[types.Int])
	}

	// Default end is string length
	var end Operand
	if e.End != nil {
		end = b.buildExpr(e.End)
	} else {
		// Need to compute string length
		lenResult := b.fn.NewVReg(types.Typ[types.Int])
		b.emit(&Instr{
			Op:   OpStrLen,
			Dest: lenResult,
			Args: []Operand{base},
		})
		end = lenResult
	}

	// Emit string slice operation
	result := b.fn.NewVReg(types.Typ[types.String])
	b.emit(&Instr{
		Op:   OpStrSlice,
		Dest: result,
		Args: []Operand{base, start, end},
	})

	return result
}

// typeSize returns the size in bytes of a type.
func (b *Builder) typeSize(t types.Type) int {
	switch typ := t.Underlying().(type) {
	case *types.Basic:
		return typ.Size()
	case *types.Pointer:
		return 8
	case *types.Array:
		return 24 // fat pointer [ptr, len, cap]
	case *types.Slice:
		return 24 // fat pointer [ptr, len, cap]
	case *types.Struct:
		return b.structSize(typ)
	default:
		return 8 // default to pointer size
	}
}

// structSize returns the total size of a struct in bytes.
func (b *Builder) structSize(s *types.Struct) int {
	size := 0
	for _, f := range s.Fields {
		// Each field is 8-byte aligned
		fieldSize := b.typeSize(f.Type)
		alignedSize := (fieldSize + 7) & ^7
		size += alignedSize
	}
	return size
}

// fieldOffset returns the byte offset of a field within a struct.
func (b *Builder) fieldOffset(s *types.Struct, fieldName string) int {
	offset := 0
	for _, f := range s.Fields {
		if f.Name == fieldName {
			return offset
		}
		// Each field is 8-byte aligned
		fieldSize := b.typeSize(f.Type)
		alignedSize := (fieldSize + 7) & ^7
		offset += alignedSize
	}
	return -1 // field not found
}

// buildStructExpr builds a struct literal expression or enum variant construction.
func (b *Builder) buildStructExpr(e *ast.StructExpr) Operand {
	// Get the type from type info
	exprType := b.info.Types[e]
	if exprType == nil {
		return None()
	}

	// Check if this is an enum variant construction
	if enumType, ok := exprType.(*types.Enum); ok {
		return b.buildEnumVariantExpr(e, enumType)
	}

	// Otherwise, it's a struct literal
	st, ok := exprType.Underlying().(*types.Struct)
	if !ok {
		return None()
	}
	structType := exprType

	// Allocate space for the struct on the stack
	size := b.structSize(st)
	structPtr := b.fn.NewVReg(types.NewPointer(structType, false))
	b.emit(&Instr{
		Op:   OpAlloc,
		Dest: structPtr,
		Args: []Operand{Imm(int64(size), types.Typ[types.Int])},
	})

	// Store each field value
	for _, fi := range e.Fields {
		fieldName := fi.Name.Name
		offset := b.fieldOffset(st, fieldName)
		if offset < 0 {
			continue
		}

		// Get field type
		field := st.FieldByName(fieldName)
		if field == nil {
			continue
		}

		// Build the field value
		var val Operand
		if fi.Value != nil {
			val = b.buildExpr(fi.Value)
		} else {
			// Shorthand: field name is same as variable name
			val = b.buildIdent(fi.Name)
		}

		// Compute field address
		fieldAddr := b.fn.NewVReg(types.NewPointer(field.Type, false))
		b.emit(&Instr{
			Op:   OpIndexAddr,
			Dest: fieldAddr,
			Args: []Operand{structPtr, Imm(int64(offset), types.Typ[types.Int])},
		})

		// For struct-typed fields, arrays, and slices, copy the entire data
		fieldUnderlying := field.Type.Underlying()
		_, isStruct := fieldUnderlying.(*types.Struct)
		_, isArray := fieldUnderlying.(*types.Array)
		_, isSlice := fieldUnderlying.(*types.Slice)

		if isStruct || isArray || isSlice {
			size := b.typeSize(field.Type)
			b.emit(&Instr{
				Op:   OpMemCopy,
				Args: []Operand{val, fieldAddr, Imm(int64(size), types.Typ[types.Int])},
			})
		} else {
			// Store the value for primitive types
			b.emit(&Instr{
				Op:   OpStore,
				Args: []Operand{val, fieldAddr},
			})
		}
	}

	return structPtr
}

// buildFieldExpr builds a field access expression (e.g., p.x).
func (b *Builder) buildFieldExpr(e *ast.FieldExpr) Operand {
	// Build the struct expression
	structPtr := b.buildExpr(e.Expr)

	// Get the struct type
	exprType := b.info.Types[e.Expr]
	if exprType == nil {
		return None()
	}

	// Automatically dereference pointers for field access
	underlying := exprType.Underlying()
	if ptr, ok := underlying.(*types.Pointer); ok {
		underlying = ptr.Elem.Underlying()
	}

	st, ok := underlying.(*types.Struct)
	if !ok {
		return None()
	}

	// Get field info
	fieldName := e.Field.Name
	field := st.FieldByName(fieldName)
	if field == nil {
		return None()
	}

	offset := b.fieldOffset(st, fieldName)
	if offset < 0 {
		return None()
	}

	// Compute field address
	fieldAddr := b.fn.NewVReg(types.NewPointer(field.Type, false))
	b.emit(&Instr{
		Op:   OpIndexAddr,
		Dest: fieldAddr,
		Args: []Operand{structPtr, Imm(int64(offset), types.Typ[types.Int])},
	})

	// For complex types (struct, array), return the address (pointer semantics)
	// - Structs: allows proper copying of the entire struct rather than just 8 bytes
	// - Arrays: len/cap/indexing operations expect a pointer to the fat pointer
	fieldUnderlying := field.Type.Underlying()
	if _, isStruct := fieldUnderlying.(*types.Struct); isStruct {
		return fieldAddr
	}
	if _, isArray := fieldUnderlying.(*types.Array); isArray {
		return fieldAddr
	}
	if _, isSlice := fieldUnderlying.(*types.Slice); isSlice {
		return fieldAddr
	}

	// Load and return the field value for simple types (int, bool, etc.)
	result := b.fn.NewVReg(field.Type)
	b.emit(&Instr{
		Op:   OpLoad,
		Dest: result,
		Args: []Operand{fieldAddr},
	})

	return result
}

// buildMatchExpr builds a match expression.
func (b *Builder) buildMatchExpr(e *ast.MatchExpr) Operand {
	// Get the result type
	resultType := b.info.Types[e]
	if resultType == nil {
		resultType = types.Typ[types.Unit]
	}

	// Build the scrutinee (the value being matched)
	scrutinee := b.buildExpr(e.Expr)
	scrutineeType := b.info.Types[e.Expr]

	// Create a result register if needed
	var result Operand
	if !resultType.Equals(types.Typ[types.Unit]) {
		result = b.fn.NewVReg(resultType)
	}

	// Create end block
	endBlock := b.fn.NewBlock(b.newLabel("match.end"))

	// Build each arm
	for i, arm := range e.Arms {
		armBlock := b.fn.NewBlock(b.newLabel(fmt.Sprintf("match.arm%d", i)))
		var nextBlock *Block
		if i < len(e.Arms)-1 {
			nextBlock = b.fn.NewBlock(b.newLabel(fmt.Sprintf("match.next%d", i)))
		} else {
			nextBlock = endBlock // Last arm falls through to end on no match (shouldn't happen if exhaustive)
		}

		// Build pattern match check
		b.buildPatternCheck(arm.Pattern, scrutinee, scrutineeType, armBlock, nextBlock)

		// Build arm body
		b.block = armBlock
		armResult := b.buildPatternBindings(arm.Pattern, scrutinee, scrutineeType)
		_ = armResult // bindings are stored in globals

		// Build the arm body expression
		bodyResult := b.buildExpr(arm.Body)

		// Store result
		if result.Kind != OpndNone {
			b.emit(&Instr{Op: OpCopy, Dest: result, Args: []Operand{bodyResult}})
		}

		// Jump to end
		b.emit(&Instr{Op: OpJump, Args: []Operand{Label(endBlock.Label)}})

		// Continue to next check block
		if i < len(e.Arms)-1 {
			b.block = nextBlock
		}
	}

	b.block = endBlock
	return result
}

// buildTryExpr builds the ? operator for Result types.
// It checks if the Result is Ok (tag 0) and extracts the value,
// or if it's Err (tag 1) it returns early with the error.
func (b *Builder) buildTryExpr(e *ast.TryExpr) Operand {
	// Get the inner expression's type (should be a Result enum)
	innerType := b.info.Types[e.Expr]
	enumType, ok := innerType.(*types.Enum)
	if !ok {
		return None()
	}

	// Get Ok and Err variants
	okVariant := enumType.VariantByName("Ok")
	errVariant := enumType.VariantByName("Err")
	if okVariant == nil || errVariant == nil {
		return None()
	}

	// Build the inner expression (the Result value)
	resultValue := b.buildExpr(e.Expr)

	// Load the tag from the result (at offset 0)
	tag := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{Op: OpLoad, Dest: tag, Args: []Operand{resultValue}})

	// Create blocks for Ok and Err cases
	okBlock := b.fn.NewBlock(b.newLabel("try.ok"))
	errBlock := b.fn.NewBlock(b.newLabel("try.err"))
	continueBlock := b.fn.NewBlock(b.newLabel("try.continue"))

	// Compare tag with 0 (Ok variant)
	cmp := b.fn.NewVReg(types.Typ[types.Bool])
	b.emit(&Instr{Op: OpEq, Dest: cmp, Args: []Operand{tag, Imm(0, types.Typ[types.Int])}})
	b.emit(&Instr{Op: OpBranch, Args: []Operand{cmp, Label(okBlock.Label), Label(errBlock.Label)}})

	// Ok block: extract the value and continue
	b.block = okBlock
	var okValue Operand
	if len(okVariant.Fields) > 0 {
		// Load the value field (at offset 8, after the tag)
		valueAddr := b.fn.NewVReg(types.NewPointer(okVariant.Fields[0].Type, false))
		b.emit(&Instr{
			Op:   OpIndexAddr,
			Dest: valueAddr,
			Args: []Operand{resultValue, Imm(8, types.Typ[types.Int])},
		})
		okValue = b.fn.NewVReg(okVariant.Fields[0].Type)
		b.emit(&Instr{Op: OpLoad, Dest: okValue, Args: []Operand{valueAddr}})
	} else {
		okValue = Imm(0, types.Typ[types.Unit])
	}
	b.emit(&Instr{Op: OpJump, Args: []Operand{Label(continueBlock.Label)}})

	// Err block: propagate the error by returning it
	b.block = errBlock

	// Get the function's return type (should also be a Result)
	returnType := b.fn.Result
	returnEnum, isResultReturn := returnType.(*types.Enum)

	if isResultReturn {
		// Build a new Result with the error value
		// First, allocate space for the return Result
		totalSize := 8 + returnEnum.MaxVariantSize()
		returnResult := b.fn.NewVReg(returnType)
		b.emit(&Instr{
			Op:   OpAlloc,
			Dest: returnResult,
			Args: []Operand{Imm(int64(totalSize), types.Typ[types.Int])},
		})

		// Set the tag to 1 (Err)
		b.emit(&Instr{Op: OpStore, Args: []Operand{Imm(1, types.Typ[types.Int]), returnResult}})

		// Copy the error value from the input Result to the output Result
		if len(errVariant.Fields) > 0 {
			// Load the error value from the input
			errAddr := b.fn.NewVReg(types.NewPointer(errVariant.Fields[0].Type, false))
			b.emit(&Instr{
				Op:   OpIndexAddr,
				Dest: errAddr,
				Args: []Operand{resultValue, Imm(8, types.Typ[types.Int])},
			})
			errValue := b.fn.NewVReg(errVariant.Fields[0].Type)
			b.emit(&Instr{Op: OpLoad, Dest: errValue, Args: []Operand{errAddr}})

			// Store the error value in the return Result
			returnErrAddr := b.fn.NewVReg(types.NewPointer(errVariant.Fields[0].Type, false))
			b.emit(&Instr{
				Op:   OpIndexAddr,
				Dest: returnErrAddr,
				Args: []Operand{returnResult, Imm(8, types.Typ[types.Int])},
			})
			b.emit(&Instr{Op: OpStore, Args: []Operand{errValue, returnErrAddr}})
		}

		// Return the error Result
		b.emit(&Instr{Op: OpReturn, Args: []Operand{returnResult}})
	} else {
		// If the function doesn't return Result, just return (shouldn't happen after sema)
		b.emit(&Instr{Op: OpReturn})
	}

	// Continue block
	b.block = continueBlock
	return okValue
}

// buildPatternCheck emits code to check if a pattern matches.
func (b *Builder) buildPatternCheck(pattern ast.Pattern, scrutinee Operand, scrutineeType types.Type, matchBlock, noMatchBlock *Block) {
	switch p := pattern.(type) {
	case *ast.IdentPattern:
		// Identifier pattern always matches
		b.emit(&Instr{Op: OpJump, Args: []Operand{Label(matchBlock.Label)}})

	case *ast.WildcardPattern:
		// Wildcard always matches
		b.emit(&Instr{Op: OpJump, Args: []Operand{Label(matchBlock.Label)}})

	case *ast.LiteralPattern:
		// Compare with literal value
		litValue := b.buildExpr(p.Value)
		cmp := b.fn.NewVReg(types.Typ[types.Bool])
		b.emit(&Instr{Op: OpEq, Dest: cmp, Args: []Operand{scrutinee, litValue}})
		b.emit(&Instr{Op: OpBranch, Args: []Operand{cmp, Label(matchBlock.Label), Label(noMatchBlock.Label)}})

	case *ast.EnumPattern:
		// Check enum tag
		b.buildEnumPatternCheck(p, scrutinee, scrutineeType, matchBlock, noMatchBlock)

	default:
		// Unknown pattern, just jump to match (will fail at runtime)
		b.emit(&Instr{Op: OpJump, Args: []Operand{Label(matchBlock.Label)}})
	}
}

// buildEnumPatternCheck checks if an enum matches a specific variant.
func (b *Builder) buildEnumPatternCheck(p *ast.EnumPattern, scrutinee Operand, scrutineeType types.Type, matchBlock, noMatchBlock *Block) {
	enumType, ok := scrutineeType.(*types.Enum)
	if !ok {
		b.emit(&Instr{Op: OpJump, Args: []Operand{Label(matchBlock.Label)}})
		return
	}

	// Get variant name from path
	path, ok := p.Path.(*ast.PathExpr)
	if !ok || len(path.Parts) < 2 {
		b.emit(&Instr{Op: OpJump, Args: []Operand{Label(matchBlock.Label)}})
		return
	}
	variantName := path.Parts[1].Name

	// Get expected tag value
	expectedTag := int64(enumType.VariantIndex(variantName))

	// Load the actual tag from the scrutinee (at offset 0)
	actualTag := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{Op: OpLoad, Dest: actualTag, Args: []Operand{scrutinee}})

	// Compare tags
	cmp := b.fn.NewVReg(types.Typ[types.Bool])
	b.emit(&Instr{Op: OpEq, Dest: cmp, Args: []Operand{actualTag, Imm(expectedTag, types.Typ[types.Int])}})

	// Branch based on comparison
	b.emit(&Instr{Op: OpBranch, Args: []Operand{cmp, Label(matchBlock.Label), Label(noMatchBlock.Label)}})
}

// buildPatternBindings extracts and binds values from a matched pattern.
func (b *Builder) buildPatternBindings(pattern ast.Pattern, scrutinee Operand, scrutineeType types.Type) Operand {
	switch p := pattern.(type) {
	case *ast.IdentPattern:
		// Bind the scrutinee to the identifier
		b.prog.Globals[p.Name.Name] = scrutinee
		return scrutinee

	case *ast.WildcardPattern:
		// No binding
		return scrutinee

	case *ast.EnumPattern:
		// Extract fields from the enum
		return b.buildEnumPatternBindings(p, scrutinee, scrutineeType)

	default:
		return scrutinee
	}
}

// buildEnumPatternBindings extracts and binds fields from an enum variant.
func (b *Builder) buildEnumPatternBindings(p *ast.EnumPattern, scrutinee Operand, scrutineeType types.Type) Operand {
	enumType, ok := scrutineeType.(*types.Enum)
	if !ok {
		return scrutinee
	}

	// Get variant
	path, ok := p.Path.(*ast.PathExpr)
	if !ok || len(path.Parts) < 2 {
		return scrutinee
	}
	variantName := path.Parts[1].Name
	variant := enumType.VariantByName(variantName)
	if variant == nil {
		return scrutinee
	}

	// Bind each field
	fieldOffset := 8 // Skip tag
	for _, fp := range p.Fields {
		// Find the field in the variant
		var variantField *types.Field
		var fieldIdx int
		for i, f := range variant.Fields {
			if f.Name == fp.Name.Name {
				variantField = f
				fieldIdx = i
				break
			}
		}
		if variantField == nil {
			continue
		}

		// Calculate actual offset
		actualOffset := 8 // tag size
		for i := 0; i < fieldIdx; i++ {
			actualOffset += b.typeSize(variant.Fields[i].Type)
		}

		// Load the field value
		fieldAddr := b.fn.NewVReg(types.NewPointer(variantField.Type, false))
		b.emit(&Instr{
			Op:   OpIndexAddr,
			Dest: fieldAddr,
			Args: []Operand{scrutinee, Imm(int64(actualOffset), types.Typ[types.Int])},
		})

		fieldValue := b.fn.NewVReg(variantField.Type)
		b.emit(&Instr{Op: OpLoad, Dest: fieldValue, Args: []Operand{fieldAddr}})

		// Bind the field to the pattern name
		if fp.Pattern != nil {
			// Nested pattern
			b.buildPatternBindings(fp.Pattern, fieldValue, variantField.Type)
		} else {
			// Shorthand: bind to field name
			b.prog.Globals[fp.Name.Name] = fieldValue
		}

		fieldOffset += b.typeSize(variantField.Type)
	}

	return scrutinee
}

// buildEnumVariantExpr builds an enum variant with data (e.g., Option::Some { value: 42 }).
func (b *Builder) buildEnumVariantExpr(e *ast.StructExpr, enumType *types.Enum) Operand {
	// Get the variant name from the path
	path, ok := e.Name.(*ast.PathExpr)
	if !ok || len(path.Parts) < 2 {
		return None()
	}
	variantName := path.Parts[1].Name

	// Get variant index (tag)
	variantIdx := enumType.VariantIndex(variantName)
	if variantIdx < 0 {
		return None()
	}

	variant := enumType.VariantByName(variantName)
	if variant == nil {
		return None()
	}

	// Calculate enum size: tag (8 bytes) + max variant data size
	maxDataSize := enumType.MaxVariantSize()
	enumSize := 8 + maxDataSize

	// Allocate space for the enum
	enumPtr := b.fn.NewVReg(types.NewPointer(enumType, false))
	b.emit(&Instr{
		Op:   OpAlloc,
		Dest: enumPtr,
		Args: []Operand{Imm(int64(enumSize), types.Typ[types.Int])},
	})

	// Store the tag at offset 0
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{Imm(int64(variantIdx), types.Typ[types.Int]), enumPtr},
	})

	// Store each field value (starting at offset 8)
	fieldOffset := 8
	for _, fi := range e.Fields {
		// Find the field in the variant
		var variantField *types.Field
		for _, f := range variant.Fields {
			if f.Name == fi.Name.Name {
				variantField = f
				break
			}
		}
		if variantField == nil {
			continue
		}

		// Build the field value
		var val Operand
		if fi.Value != nil {
			val = b.buildExpr(fi.Value)
		} else {
			val = b.buildIdent(fi.Name)
		}

		// Compute field address
		fieldAddr := b.fn.NewVReg(types.NewPointer(variantField.Type, false))
		b.emit(&Instr{
			Op:   OpIndexAddr,
			Dest: fieldAddr,
			Args: []Operand{enumPtr, Imm(int64(fieldOffset), types.Typ[types.Int])},
		})

		// Store the value
		b.emit(&Instr{
			Op:   OpStore,
			Args: []Operand{val, fieldAddr},
		})

		fieldOffset += b.typeSize(variantField.Type)
	}

	return enumPtr
}

// buildPathExpr builds a path expression (e.g., Color::Red for enum variants).
func (b *Builder) buildPathExpr(e *ast.PathExpr) Operand {
	// Get the type from type info
	exprType := b.info.Types[e]
	if exprType == nil {
		return None()
	}

	// Handle enum variants
	enumType, ok := exprType.(*types.Enum)
	if !ok {
		return None()
	}

	// Get the variant name (second part of path)
	if len(e.Parts) < 2 {
		return None()
	}
	variantName := e.Parts[1].Name

	// Get the variant index (tag)
	variantIdx := enumType.VariantIndex(variantName)
	if variantIdx < 0 {
		return None()
	}

	// Allocate space for the enum (tag + max data size)
	maxDataSize := enumType.MaxVariantSize()
	enumSize := 8 + maxDataSize
	if enumSize < 8 {
		enumSize = 8 // Minimum size is tag only
	}

	enumPtr := b.fn.NewVReg(types.NewPointer(enumType, false))
	b.emit(&Instr{
		Op:   OpAlloc,
		Dest: enumPtr,
		Args: []Operand{Imm(int64(enumSize), types.Typ[types.Int])},
	})

	// Store the tag at offset 0
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{Imm(int64(variantIdx), types.Typ[types.Int]), enumPtr},
	})

	return enumPtr
}

// buildMethodExpr builds a method call expression (e.g., p.sum()).
func (b *Builder) buildMethodExpr(e *ast.MethodExpr) Operand {
	// Check if this is a module-qualified function call (e.g., math.Add)
	// Module functions are resolved by sema and recorded in Uses
	if sym := b.info.Uses[e.Method]; sym != nil {
		// This is a qualified function call to an imported module function
		// Generate a regular function call using the fully qualified name
		funcName := sym.Name

		// Build arguments
		args := make([]Operand, len(e.Args)+1)
		args[0] = FuncRef(funcName, nil)
		for i, arg := range e.Args {
			args[i+1] = b.buildExpr(arg)
		}

		// Get result type from type info
		resultType := b.info.Types[e]
		if resultType == nil {
			resultType = types.Typ[types.Unit]
		}

		if resultType.Equals(types.Typ[types.Unit]) {
			b.emit(&Instr{
				Op:   OpCall,
				Args: args,
			})
			return None()
		}

		dest := b.fn.NewVReg(resultType)
		b.emit(&Instr{
			Op:   OpCall,
			Dest: dest,
			Args: args,
		})
		return dest
	}

	// Handle package-qualified builtins (e.g., os.ReadFile, strconv.Itoa)
	if ident, ok := e.Expr.(*ast.Ident); ok {
		switch ident.Name {
		case "os":
			switch e.Method.Name {
			case "ReadFile":
				return b.buildOsReadFile(e)
			case "WriteFile":
				return b.buildOsWriteFile(e)
			case "Argc":
				return b.buildOsArgc(e)
			case "Argv":
				return b.buildOsArgv(e)
			}
		case "strconv":
			switch e.Method.Name {
			case "Itoa":
				return b.buildStrconvItoa(e)
			case "Atoi":
				return b.buildStrconvAtoi(e)
			}
		case "syscall":
			switch e.Method.Name {
			case "open":
				return b.buildSyscallOpen(&ast.CallExpr{Func: e.Method, Args: e.Args})
			case "read":
				return b.buildSyscallRead(&ast.CallExpr{Func: e.Method, Args: e.Args})
			case "write":
				return b.buildSyscallWrite(&ast.CallExpr{Func: e.Method, Args: e.Args})
			case "close":
				return b.buildSyscallClose(&ast.CallExpr{Func: e.Method, Args: e.Args})
			}
		}
	}

	// Build the receiver expression for actual methods
	receiver := b.buildExpr(e.Expr)

	// Get the receiver type to determine the method name
	receiverType := b.info.Types[e.Expr]
	if receiverType == nil {
		return None()
	}

	var typeName string
	switch t := receiverType.Underlying().(type) {
	case *types.Struct:
		typeName = t.Name
	default:
		return None()
	}

	// Method name is TypeName.MethodName
	methodName := typeName + "." + e.Method.Name

	// Build arguments: first arg is receiver, then the rest
	args := make([]Operand, len(e.Args)+2)
	args[0] = FuncRef(methodName, nil)
	args[1] = receiver
	for i, arg := range e.Args {
		args[i+2] = b.buildExpr(arg)
	}

	// Get result type from type info
	resultType := b.info.Types[e]
	if resultType == nil {
		resultType = types.Typ[types.Unit]
	}

	if resultType.Equals(types.Typ[types.Unit]) {
		b.emit(&Instr{
			Op:   OpCall,
			Args: args,
		})
		return None()
	}

	dest := b.fn.NewVReg(resultType)
	b.emit(&Instr{
		Op:   OpCall,
		Dest: dest,
		Args: args,
	})
	return dest
}

// String operation builders

func (b *Builder) buildStrContains(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	haystack := b.buildExpr(e.Args[0])
	needle := b.buildExpr(e.Args[1])

	dest := b.fn.NewVReg(types.Typ[types.Bool])
	b.emit(&Instr{
		Op:   OpStrContains,
		Dest: dest,
		Args: []Operand{haystack, needle},
	})
	return dest
}

func (b *Builder) buildStrStartsWith(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	s := b.buildExpr(e.Args[0])
	prefix := b.buildExpr(e.Args[1])

	dest := b.fn.NewVReg(types.Typ[types.Bool])
	b.emit(&Instr{
		Op:   OpStrStartsWith,
		Dest: dest,
		Args: []Operand{s, prefix},
	})
	return dest
}

func (b *Builder) buildStrEndsWith(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	s := b.buildExpr(e.Args[0])
	suffix := b.buildExpr(e.Args[1])

	dest := b.fn.NewVReg(types.Typ[types.Bool])
	b.emit(&Instr{
		Op:   OpStrEndsWith,
		Dest: dest,
		Args: []Operand{s, suffix},
	})
	return dest
}

func (b *Builder) buildStrIndexOf(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	s := b.buildExpr(e.Args[0])
	substr := b.buildExpr(e.Args[1])

	dest := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpStrIndexOf,
		Dest: dest,
		Args: []Operand{s, substr},
	})
	return dest
}

func (b *Builder) buildStrSubstring(e *ast.CallExpr) Operand {
	if len(e.Args) != 3 {
		return None()
	}

	s := b.buildExpr(e.Args[0])
	start := b.buildExpr(e.Args[1])
	end := b.buildExpr(e.Args[2])

	dest := b.fn.NewVReg(types.Typ[types.String])
	b.emit(&Instr{
		Op:   OpStrSubstring,
		Dest: dest,
		Args: []Operand{s, start, end},
	})
	return dest
}

func (b *Builder) buildStrCharAt(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	s := b.buildExpr(e.Args[0])
	index := b.buildExpr(e.Args[1])

	dest := b.fn.NewVReg(types.Typ[types.Int])
	b.emit(&Instr{
		Op:   OpStrCharAt,
		Dest: dest,
		Args: []Operand{s, index},
	})
	return dest
}

func (b *Builder) buildStrConcatBuiltin(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	a := b.buildExpr(e.Args[0])
	bb := b.buildExpr(e.Args[1])

	dest := b.fn.NewVReg(types.Typ[types.String])
	b.emit(&Instr{
		Op:   OpStrConcat,
		Dest: dest,
		Args: []Operand{a, bb},
	})
	return dest
}

func (b *Builder) buildStrTrim(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	s := b.buildExpr(e.Args[0])
	chars := b.buildExpr(e.Args[1])

	dest := b.fn.NewVReg(types.Typ[types.String])
	b.emit(&Instr{
		Op:   OpStrTrim,
		Dest: dest,
		Args: []Operand{s, chars},
	})
	return dest
}

func (b *Builder) buildStrReplace(e *ast.CallExpr) Operand {
	if len(e.Args) != 3 {
		return None()
	}

	s := b.buildExpr(e.Args[0])
	old := b.buildExpr(e.Args[1])
	newStr := b.buildExpr(e.Args[2])

	dest := b.fn.NewVReg(types.Typ[types.String])
	b.emit(&Instr{
		Op:   OpStrReplace,
		Dest: dest,
		Args: []Operand{s, old, newStr},
	})
	return dest
}

func (b *Builder) buildStrSplit(e *ast.CallExpr) Operand {
	if len(e.Args) != 2 {
		return None()
	}

	s := b.buildExpr(e.Args[0])
	sep := b.buildExpr(e.Args[1])

	// Return type is []string - array fat pointer
	dest := b.fn.NewVReg(types.NewArray(types.Typ[types.String], 0))
	b.emit(&Instr{
		Op:   OpStrSplit,
		Dest: dest,
		Args: []Operand{s, sep},
	})
	return dest
}

// calculateTypeSize returns the size in bytes for a type
func (b *Builder) calculateTypeSize(t types.Type) int {
	switch typ := t.(type) {
	case *types.Basic:
		switch typ.Kind {
		case types.Int, types.Int64:
			return 8
		case types.Bool:
			return 1
		case types.String:
			return 8 // pointer to null-terminated string
		default:
			return 8
		}
	case *types.Pointer:
		return 8
	case *types.Array, *types.Slice:
		return 24 // fat pointer: ptr + len + cap
	case *types.Struct:
		// Use structSize which properly aligns fields
		return b.structSize(typ)
	default:
		return 8 // default to pointer size
	}
}

// buildGlobalInitializers generates initialization code for complex globals
// This is injected at the start of main() to initialize arrays, structs, etc.
func (b *Builder) buildGlobalInitializers() {
	for _, gv := range b.prog.GlobalVars {
		// Only initialize globals with complex initializers
		if gv.InitVal == nil {
			continue
		}

		// Check if this is a complex type that needs initialization
		switch gv.InitVal.(type) {
		case *ast.ArrayExpr:
			b.buildArrayGlobalInit(gv)
		case *ast.StructExpr:
			b.buildStructGlobalInit(gv)
		case *ast.IntLit, *ast.BoolLit, *ast.StringLit:
			// Simple literals are initialized in the data section
			continue
		default:
			// Other complex types (function calls, etc.)
			// For now, skip - can be added later
			continue
		}
	}
}

// buildArrayGlobalInit generates initialization code for an array global
func (b *Builder) buildArrayGlobalInit(gv *GlobalVar) {
	arrayExpr, ok := gv.InitVal.(*ast.ArrayExpr)
	if !ok {
		return
	}

	// Get the array type
	arrType, ok := gv.Type.(*types.Array)
	if !ok {
		return
	}

	elemType := arrType.Elem
	elemSize := b.typeSize(elemType)
	numElems := int(arrType.Len)

	// Allocate heap space for array elements
	elemDataSize := numElems * elemSize
	elemPtr := b.fn.NewVReg(types.NewPointer(elemType, false))
	b.emit(&Instr{
		Op:   OpHeapAlloc,
		Dest: elemPtr,
		Args: []Operand{Imm(int64(elemDataSize), types.Typ[types.Int])},
	})

	// Store each element
	if arrayExpr.Repeat != nil {
		// [expr; count] syntax - same value repeated
		val := b.buildExpr(arrayExpr.Repeat)
		for i := 0; i < numElems; i++ {
			offset := i * elemSize
			addr := b.fn.NewVReg(types.NewPointer(elemType, false))
			b.emit(&Instr{
				Op:   OpIndexAddr,
				Dest: addr,
				Args: []Operand{
					elemPtr,
					Imm(int64(offset), types.Typ[types.Int]),
				},
			})
			b.emit(&Instr{
				Op:   OpStore,
				Args: []Operand{val, addr},
			})
		}
	} else {
		// [elem1, elem2, ...] syntax
		for i, elemExpr := range arrayExpr.Elements {
			val := b.buildExpr(elemExpr)
			offset := i * elemSize
			addr := b.fn.NewVReg(types.NewPointer(elemType, false))
			b.emit(&Instr{
				Op:   OpIndexAddr,
				Dest: addr,
				Args: []Operand{
					elemPtr,
					Imm(int64(offset), types.Typ[types.Int]),
				},
			})
			b.emit(&Instr{
				Op:   OpStore,
				Args: []Operand{val, addr},
			})
		}
	}

	// Build fat pointer directly in global storage
	// Get the global address
	globalRef := GlobalRef(gv.Name, gv.Type)

	// Store ptr at offset 0
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{elemPtr, globalRef},
	})

	// Reuse a single temp vreg for address calculations to reduce vreg count
	tempAddr := b.fn.NewVReg(types.NewPointer(types.Typ[types.Int], false))

	// Store len at offset 8
	b.emit(&Instr{
		Op:   OpIndexAddr,
		Dest: tempAddr,
		Args: []Operand{globalRef, Imm(8, types.Typ[types.Int])},
	})
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{Imm(int64(numElems), types.Typ[types.Int]), tempAddr},
	})

	// Store cap at offset 16 (reuse tempAddr)
	b.emit(&Instr{
		Op:   OpIndexAddr,
		Dest: tempAddr,
		Args: []Operand{globalRef, Imm(16, types.Typ[types.Int])},
	})
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{Imm(int64(numElems), types.Typ[types.Int]), tempAddr},
	})
}
// buildStructGlobalInit generates initialization code for a struct global
func (b *Builder) buildStructGlobalInit(gv *GlobalVar) {
	structExpr, ok := gv.InitVal.(*ast.StructExpr)
	if !ok {
		return
	}

	// Get the struct type
	structType, ok := gv.Type.(*types.Struct)
	if !ok {
		return
	}

	// Get the global address
	globalRef := GlobalRef(gv.Name, gv.Type)

	// For each field in the struct literal, store its value to the global
	for _, fieldInit := range structExpr.Fields {
		// Find the field in the struct type
		fieldName := fieldInit.Name.Name
		fieldIndex := -1
		var fieldType types.Type
		var fieldOffset int

		currentOffset := 0
		for i, field := range structType.Fields {
			if field.Name == fieldName {
				fieldIndex = i
				fieldType = field.Type
				fieldOffset = currentOffset
				break
			}
			// Accumulate offset (8-byte aligned)
			fieldSize := b.typeSize(field.Type)
			alignedSize := (fieldSize + 7) & ^7
			currentOffset += alignedSize
		}

		if fieldIndex == -1 {
			// Field not found - this should have been caught by sema
			continue
		}

		// Calculate address of this field in the global
		tempAddr := b.fn.NewVReg(types.NewPointer(fieldType, false))
		if fieldOffset == 0 {
			// First field - get global address
			b.emit(&Instr{
				Op:   OpIndexAddr,
				Dest: tempAddr,
				Args: []Operand{globalRef, Imm(0, types.Typ[types.Int])},
			})
		} else {
			// Add offset to global address
			b.emit(&Instr{
				Op:   OpIndexAddr,
				Dest: tempAddr,
				Args: []Operand{globalRef, Imm(int64(fieldOffset), types.Typ[types.Int])},
			})
		}
		fieldAddr := Operand(tempAddr)

		// Check if this field is an array literal that needs special handling
		if arrayExpr, ok := fieldInit.Value.(*ast.ArrayExpr); ok {
			// Initialize array field directly in global storage
			b.buildArrayFieldInit(arrayExpr, fieldAddr, fieldType)
		} else {
			// Build the initializer expression
			fieldValue := b.buildExpr(fieldInit.Value)

			// Store the field value
			fieldSize := b.typeSize(fieldType)
			if fieldSize <= 8 {
				// Simple types (int, bool, pointer) - direct store
				b.emit(&Instr{
					Op:   OpStore,
					Args: []Operand{fieldValue, fieldAddr},
				})
			} else {
				// Complex types (nested structs) - memcopy
				b.emit(&Instr{
					Op:   OpMemCopy,
					Args: []Operand{fieldValue, fieldAddr, Imm(int64(fieldSize), types.Typ[types.Int])},
				})
			}
		}
	}
}
// buildArrayFieldInit initializes an array field within a struct global
func (b *Builder) buildArrayFieldInit(arrayExpr *ast.ArrayExpr, fieldAddr Operand, fieldType types.Type) {
	arrType, ok := fieldType.(*types.Array)
	if !ok {
		return
	}

	elemType := arrType.Elem
	elemSize := b.typeSize(elemType)
	numElems := int(arrType.Len)

	// Allocate heap space for array elements
	elemDataSize := numElems * elemSize
	elemPtr := b.fn.NewVReg(types.NewPointer(elemType, false))
	b.emit(&Instr{
		Op:   OpHeapAlloc,
		Dest: elemPtr,
		Args: []Operand{Imm(int64(elemDataSize), types.Typ[types.Int])},
	})

	// Store each element (if array has elements)
	if arrayExpr.Repeat != nil {
		// [expr; count] syntax - same value repeated
		val := b.buildExpr(arrayExpr.Repeat)
		for i := 0; i < numElems; i++ {
			offset := i * elemSize
			addr := b.fn.NewVReg(types.NewPointer(elemType, false))
			b.emit(&Instr{
				Op:   OpIndexAddr,
				Dest: addr,
				Args: []Operand{
					elemPtr,
					Imm(int64(offset), types.Typ[types.Int]),
				},
			})
			b.emit(&Instr{
				Op:   OpStore,
				Args: []Operand{val, addr},
			})
		}
	} else if len(arrayExpr.Elements) > 0 {
		// [elem1, elem2, ...] syntax
		for i, elemExpr := range arrayExpr.Elements {
			val := b.buildExpr(elemExpr)
			offset := i * elemSize
			addr := b.fn.NewVReg(types.NewPointer(elemType, false))
			b.emit(&Instr{
				Op:   OpIndexAddr,
				Dest: addr,
				Args: []Operand{
					elemPtr,
					Imm(int64(offset), types.Typ[types.Int]),
				},
			})
			b.emit(&Instr{
				Op:   OpStore,
				Args: []Operand{val, addr},
			})
		}
	}

	// Build fat pointer directly at field address
	// Store ptr at offset 0
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{elemPtr, fieldAddr},
	})

	// Reuse a single temp vreg for len/cap calculations
	tempAddr := b.fn.NewVReg(types.NewPointer(types.Typ[types.Int], false))

	// Store len at offset 8
	b.emit(&Instr{
		Op:   OpIndexAddr,
		Dest: tempAddr,
		Args: []Operand{fieldAddr, Imm(8, types.Typ[types.Int])},
	})
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{Imm(int64(numElems), types.Typ[types.Int]), tempAddr},
	})

	// Store cap at offset 16 (reuse tempAddr)
	b.emit(&Instr{
		Op:   OpIndexAddr,
		Dest: tempAddr,
		Args: []Operand{fieldAddr, Imm(16, types.Typ[types.Int])},
	})
	b.emit(&Instr{
		Op:   OpStore,
		Args: []Operand{Imm(int64(numElems), types.Typ[types.Int]), tempAddr},
	})
}
