// Package sema provides semantic analysis for ease.
package sema

import (
	"ease/pkg/ast"
	"ease/pkg/token"
	"ease/pkg/types"
)

// MonomorphizedFn represents a function that has been specialized with concrete types.
type MonomorphizedFn struct {
	OriginalName string
	MangledName  string
	TypeArgs     []types.Type
	Decl         *ast.FnDecl
}

// Monomorphizer handles the creation of specialized generic type/function instances.
type Monomorphizer struct {
	analyzer *Analyzer
	typeMap  types.TypeMap
}

// NewMonomorphizer creates a new Monomorphizer.
func NewMonomorphizer(analyzer *Analyzer) *Monomorphizer {
	return &Monomorphizer{
		analyzer: analyzer,
	}
}

// CloneFunction creates a specialized copy of a generic function with type parameters replaced.
func (m *Monomorphizer) CloneFunction(fn *ast.FnDecl, typeArgs []types.Type, mangledName string) *ast.FnDecl {
	// Build type parameter map
	typeParams := make([]*types.TypeParam, len(fn.TypeParams))
	for i, tp := range fn.TypeParams {
		typeParams[i] = types.NewTypeParam(tp.Name.Name, m.analyzer.typeParamIDCounter)
		m.analyzer.typeParamIDCounter++
	}
	m.typeMap = types.BuildTypeMap(typeParams, typeArgs)

	// Clone the function with substituted types
	cloned := &ast.FnDecl{
		Token:      fn.Token,
		Name:       &ast.Ident{Token: fn.Name.Token, Name: mangledName},
		TypeParams: nil, // Specialized function has no type parameters
		Params:     m.cloneParams(fn.Params),
		ReturnType: m.cloneType(fn.ReturnType),
		Body:       m.cloneBlock(fn.Body),
	}

	return cloned
}

func (m *Monomorphizer) cloneParams(params []ast.Param) []ast.Param {
	if params == nil {
		return nil
	}
	result := make([]ast.Param, len(params))
	for i, p := range params {
		result[i] = ast.Param{
			Name: m.cloneIdent(p.Name),
			Type: m.cloneType(p.Type),
		}
	}
	return result
}

func (m *Monomorphizer) cloneIdent(id *ast.Ident) *ast.Ident {
	if id == nil {
		return nil
	}
	return &ast.Ident{Token: id.Token, Name: id.Name}
}

func (m *Monomorphizer) cloneType(t ast.Type) ast.Type {
	if t == nil {
		return nil
	}

	switch t := t.(type) {
	case *ast.NamedType:
		// Clone type arguments if present
		var typeArgs []ast.Type
		if len(t.TypeArgs) > 0 {
			typeArgs = make([]ast.Type, len(t.TypeArgs))
			for i, ta := range t.TypeArgs {
				typeArgs[i] = m.cloneType(ta)
			}
		}

		return &ast.NamedType{
			Name:     m.cloneIdent(t.Name),
			TypeArgs: typeArgs,
		}

	case *ast.UnitType:
		return &ast.UnitType{Token: t.Token}

	case *ast.ArrayType:
		return &ast.ArrayType{
			Token:   t.Token,
			Element: m.cloneType(t.Element),
			Size:    m.cloneExpr(t.Size),
		}

	case *ast.SliceType:
		return &ast.SliceType{
			Token:   t.Token,
			Element: m.cloneType(t.Element),
		}

	case *ast.RefType:
		return &ast.RefType{
			Token:   t.Token,
			Mutable: t.Mutable,
			Type:    m.cloneType(t.Type),
		}

	case *ast.TupleType:
		elems := make([]ast.Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = m.cloneType(e)
		}
		return &ast.TupleType{Token: t.Token, Elements: elems}

	case *ast.FnType:
		params := make([]ast.Type, len(t.Params))
		for i, p := range t.Params {
			params[i] = m.cloneType(p)
		}
		return &ast.FnType{
			Token:      t.Token,
			Params:     params,
			ReturnType: m.cloneType(t.ReturnType),
		}

	case *ast.ChanType:
		return &ast.ChanType{
			Token:   t.Token,
			Element: m.cloneType(t.Element),
		}

	default:
		return t
	}
}

func (m *Monomorphizer) cloneBlock(block *ast.BlockStmt) *ast.BlockStmt {
	if block == nil {
		return nil
	}

	stmts := make([]ast.Stmt, len(block.Stmts))
	for i, s := range block.Stmts {
		stmts[i] = m.cloneStmt(s)
	}

	return &ast.BlockStmt{
		Token: block.Token,
		Stmts: stmts,
		Expr:  m.cloneExpr(block.Expr),
	}
}

func (m *Monomorphizer) cloneStmt(stmt ast.Stmt) ast.Stmt {
	if stmt == nil {
		return nil
	}

	switch s := stmt.(type) {
	case *ast.LetStmt:
		return &ast.LetStmt{
			Token:   s.Token,
			Mutable: s.Mutable,
			Pattern: m.clonePattern(s.Pattern),
			Type:    m.cloneType(s.Type),
			Value:   m.cloneExpr(s.Value),
		}

	case *ast.ExprStmt:
		return &ast.ExprStmt{Expr: m.cloneExpr(s.Expr)}

	case *ast.ReturnStmt:
		return &ast.ReturnStmt{
			Token: s.Token,
			Value: m.cloneExpr(s.Value),
		}

	case *ast.IfStmt:
		return &ast.IfStmt{
			Token: s.Token,
			Cond:  m.cloneExpr(s.Cond),
			Then:  m.cloneBlock(s.Then),
			Else:  m.cloneStmt(s.Else),
		}

	case *ast.ForStmt:
		return &ast.ForStmt{
			Token:   s.Token,
			Cond:    m.cloneExpr(s.Cond),
			Pattern: m.clonePattern(s.Pattern),
			Iter:    m.cloneExpr(s.Iter),
			Body:    m.cloneBlock(s.Body),
		}

	case *ast.MatchStmt:
		arms := make([]ast.MatchArm, len(s.Arms))
		for i, arm := range s.Arms {
			arms[i] = m.cloneMatchArm(arm)
		}
		return &ast.MatchStmt{
			Token: s.Token,
			Expr:  m.cloneExpr(s.Expr),
			Arms:  arms,
		}

	case *ast.BreakStmt:
		return &ast.BreakStmt{
			Token: s.Token,
			Value: m.cloneExpr(s.Value),
		}

	case *ast.ContinueStmt:
		return &ast.ContinueStmt{Token: s.Token}

	case *ast.BlockStmt:
		return m.cloneBlock(s)

	default:
		return stmt
	}
}

func (m *Monomorphizer) cloneExpr(expr ast.Expr) ast.Expr {
	if expr == nil {
		return nil
	}

	switch e := expr.(type) {
	case *ast.Ident:
		return m.cloneIdent(e)

	case *ast.IntLit:
		return &ast.IntLit{Token: e.Token, Value: e.Value, Raw: e.Raw}

	case *ast.FloatLit:
		return &ast.FloatLit{Token: e.Token, Value: e.Value, Raw: e.Raw}

	case *ast.StringLit:
		return &ast.StringLit{Token: e.Token, Value: e.Value}

	case *ast.CharLit:
		return &ast.CharLit{Token: e.Token, Value: e.Value}

	case *ast.BoolLit:
		return &ast.BoolLit{Token: e.Token, Value: e.Value}

	case *ast.BinaryExpr:
		return &ast.BinaryExpr{
			Left:  m.cloneExpr(e.Left),
			Op:    e.Op,
			Right: m.cloneExpr(e.Right),
		}

	case *ast.UnaryExpr:
		return &ast.UnaryExpr{
			Op:    e.Op,
			Right: m.cloneExpr(e.Right),
		}

	case *ast.CallExpr:
		args := make([]ast.Expr, len(e.Args))
		for i, arg := range e.Args {
			args[i] = m.cloneExpr(arg)
		}
		typeArgs := make([]ast.Type, len(e.TypeArgs))
		for i, ta := range e.TypeArgs {
			typeArgs[i] = m.cloneType(ta)
		}
		return &ast.CallExpr{
			Func:     m.cloneExpr(e.Func),
			TypeArgs: typeArgs,
			Args:     args,
		}

	case *ast.IndexExpr:
		return &ast.IndexExpr{
			Expr:  m.cloneExpr(e.Expr),
			Index: m.cloneExpr(e.Index),
		}

	case *ast.SliceExpr:
		return &ast.SliceExpr{
			Expr:  m.cloneExpr(e.Expr),
			Start: m.cloneExpr(e.Start),
			End:   m.cloneExpr(e.End),
		}

	case *ast.FieldExpr:
		return &ast.FieldExpr{
			Expr:  m.cloneExpr(e.Expr),
			Field: m.cloneIdent(e.Field),
		}

	case *ast.MethodExpr:
		args := make([]ast.Expr, len(e.Args))
		for i, arg := range e.Args {
			args[i] = m.cloneExpr(arg)
		}
		typeArgs := make([]ast.Type, len(e.TypeArgs))
		for i, ta := range e.TypeArgs {
			typeArgs[i] = m.cloneType(ta)
		}
		return &ast.MethodExpr{
			Expr:     m.cloneExpr(e.Expr),
			Method:   m.cloneIdent(e.Method),
			TypeArgs: typeArgs,
			Args:     args,
		}

	case *ast.PathExpr:
		parts := make([]ast.Ident, len(e.Parts))
		for i, p := range e.Parts {
			parts[i] = *m.cloneIdent(&p)
		}
		return &ast.PathExpr{Parts: parts}

	case *ast.IfExpr:
		var elseExpr ast.Expr
		if e.Else != nil {
			elseExpr = m.cloneExpr(e.Else)
		}
		return &ast.IfExpr{
			Token: e.Token,
			Cond:  m.cloneExpr(e.Cond),
			Then:  m.cloneBlock(e.Then),
			Else:  elseExpr,
		}

	case *ast.BlockExpr:
		return &ast.BlockExpr{Block: m.cloneBlock(e.Block)}

	case *ast.MatchExpr:
		arms := make([]ast.MatchArm, len(e.Arms))
		for i, arm := range e.Arms {
			arms[i] = m.cloneMatchArm(arm)
		}
		return &ast.MatchExpr{
			Token: e.Token,
			Expr:  m.cloneExpr(e.Expr),
			Arms:  arms,
		}

	case *ast.StructExpr:
		fields := make([]ast.FieldInit, len(e.Fields))
		for i, f := range e.Fields {
			fields[i] = ast.FieldInit{
				Name:  m.cloneIdent(f.Name),
				Value: m.cloneExpr(f.Value),
			}
		}
		return &ast.StructExpr{
			Name:   m.cloneExpr(e.Name),
			Fields: fields,
		}

	case *ast.ArrayExpr:
		elems := make([]ast.Expr, len(e.Elements))
		for i, el := range e.Elements {
			elems[i] = m.cloneExpr(el)
		}
		return &ast.ArrayExpr{
			Token:    e.Token,
			ElemType: m.cloneType(e.ElemType),
			Elements: elems,
			Repeat:   m.cloneExpr(e.Repeat),
			Count:    m.cloneExpr(e.Count),
		}

	case *ast.TupleExpr:
		elems := make([]ast.Expr, len(e.Elements))
		for i, el := range e.Elements {
			elems[i] = m.cloneExpr(el)
		}
		return &ast.TupleExpr{Token: e.Token, Elements: elems}

	case *ast.RangeExpr:
		return &ast.RangeExpr{
			Start:     m.cloneExpr(e.Start),
			End:       m.cloneExpr(e.End),
			Inclusive: e.Inclusive,
		}

	default:
		return expr
	}
}

func (m *Monomorphizer) clonePattern(pattern ast.Pattern) ast.Pattern {
	if pattern == nil {
		return nil
	}

	switch p := pattern.(type) {
	case *ast.IdentPattern:
		return &ast.IdentPattern{
			Mutable: p.Mutable,
			Name:    m.cloneIdent(p.Name),
		}

	case *ast.WildcardPattern:
		return &ast.WildcardPattern{Token: p.Token}

	case *ast.LiteralPattern:
		return &ast.LiteralPattern{Value: m.cloneExpr(p.Value)}

	case *ast.TuplePattern:
		elems := make([]ast.Pattern, len(p.Elements))
		for i, el := range p.Elements {
			elems[i] = m.clonePattern(el)
		}
		return &ast.TuplePattern{Token: p.Token, Elements: elems}

	case *ast.EnumPattern:
		fields := make([]ast.FieldPattern, len(p.Fields))
		for i, f := range p.Fields {
			fields[i] = ast.FieldPattern{
				Name:    m.cloneIdent(f.Name),
				Pattern: m.clonePattern(f.Pattern),
			}
		}
		return &ast.EnumPattern{
			Path:   m.cloneExpr(p.Path),
			Fields: fields,
		}

	default:
		return pattern
	}
}

func (m *Monomorphizer) cloneMatchArm(arm ast.MatchArm) ast.MatchArm {
	return ast.MatchArm{
		Pattern: m.clonePattern(arm.Pattern),
		Guard:   m.cloneExpr(arm.Guard),
		Body:    m.cloneExpr(arm.Body),
	}
}

// GetMonomorphizedFunctions returns all instantiated generic functions.
// This is used by the IR generator to emit code for all specializations.
func (a *Analyzer) GetMonomorphizedFunctions() []*MonomorphizedFn {
	var result []*MonomorphizedFn

	// Collect all instantiated functions from the cache
	for mangledName := range a.instantiations {
		// Check if this is a function instantiation
		if sym := a.table.Global.Lookup(mangledName); sym != nil {
			if _, ok := sym.Type.(*types.Function); ok {
				// Find the original generic function
				for origName, decl := range a.genericFns {
					if decl != nil {
						// Check if mangledName starts with origName
						if len(mangledName) > len(origName) && mangledName[:len(origName)] == origName {
							result = append(result, &MonomorphizedFn{
								OriginalName: origName,
								MangledName:  mangledName,
								Decl:         decl,
							})
							break
						}
					}
				}
			}
		}
	}

	return result
}

// substituteTypeInAST replaces type parameter references in an AST type node
// with the concrete type name. This is used for monomorphization.
func substituteTypeInAST(t ast.Type, paramName string, concreteTypeName string) ast.Type {
	if t == nil {
		return nil
	}

	switch t := t.(type) {
	case *ast.NamedType:
		if t.Name.Name == paramName {
			return &ast.NamedType{
				Name: &ast.Ident{
					Token: token.Token{Literal: concreteTypeName},
					Name:  concreteTypeName,
				},
			}
		}
		// Recurse into type arguments
		if len(t.TypeArgs) > 0 {
			newArgs := make([]ast.Type, len(t.TypeArgs))
			for i, arg := range t.TypeArgs {
				newArgs[i] = substituteTypeInAST(arg, paramName, concreteTypeName)
			}
			return &ast.NamedType{Name: t.Name, TypeArgs: newArgs}
		}
		return t

	case *ast.ArrayType:
		return &ast.ArrayType{
			Token:   t.Token,
			Element: substituteTypeInAST(t.Element, paramName, concreteTypeName),
			Size:    t.Size,
		}

	case *ast.SliceType:
		return &ast.SliceType{
			Token:   t.Token,
			Element: substituteTypeInAST(t.Element, paramName, concreteTypeName),
		}

	case *ast.RefType:
		return &ast.RefType{
			Token:   t.Token,
			Mutable: t.Mutable,
			Type:    substituteTypeInAST(t.Type, paramName, concreteTypeName),
		}

	case *ast.TupleType:
		newElems := make([]ast.Type, len(t.Elements))
		for i, e := range t.Elements {
			newElems[i] = substituteTypeInAST(e, paramName, concreteTypeName)
		}
		return &ast.TupleType{Token: t.Token, Elements: newElems}

	default:
		return t
	}
}
