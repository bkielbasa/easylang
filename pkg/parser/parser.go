package parser

import (
	"fmt"
	"ease/pkg/ast"
	"ease/pkg/lexer"
	"ease/pkg/token"
	"strconv"
	"strings"
)

type Parser struct {
	l      *lexer.Lexer
	errors []string

	curToken  token.Token
	peekToken token.Token

	// noStruct is set when parsing expressions where { should not start a struct literal
	// (e.g., in if/match conditions where { starts a block instead)
	noStruct bool
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{l: l}
	// Read two tokens to initialize curToken and peekToken
	p.nextToken()
	p.nextToken()
	return p
}

func (p *Parser) Errors() []string {
	return p.errors
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
	// Skip comments
	for p.peekToken.Type == token.Comment {
		p.peekToken = p.l.NextToken()
	}
}

func (p *Parser) curTokenIs(t token.Type) bool {
	return p.curToken.Type == t
}

func (p *Parser) peekTokenIs(t token.Type) bool {
	return p.peekToken.Type == t
}

func (p *Parser) expect(t token.Type) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	}
	p.peekError(t)
	return false
}

func (p *Parser) peekError(t token.Type) {
	msg := fmt.Sprintf("%s: expected %s, got %s",
		p.peekToken.Pos, t, p.peekToken.Type)
	p.errors = append(p.errors, msg)
}

func (p *Parser) error(msg string) {
	p.errors = append(p.errors, fmt.Sprintf("%s: %s", p.curToken.Pos, msg))
}

// ============================================
// Program
// ============================================

func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{}

	// Skip leading comments
	for p.curTokenIs(token.Comment) {
		p.nextToken()
	}

	// Skip package declaration if present
	if p.curTokenIs(token.Package) {
		p.nextToken() // consume 'package'
		p.nextToken() // consume package name (identifier)
	}

	// Parse imports
	for p.curTokenIs(token.Import) {
		imp := p.parseImport()
		if imp != nil {
			program.Imports = append(program.Imports, *imp)
		}
	}

	// Parse declarations
	for !p.curTokenIs(token.EOF) {
		decl := p.parseDeclaration()
		if decl != nil {
			program.Decls = append(program.Decls, decl)
		}
		p.nextToken()
	}

	return program
}

// ============================================
// Imports
// ============================================

func (p *Parser) parseImport() *ast.ImportDecl {
	imp := &ast.ImportDecl{Token: p.curToken}

	if !p.expect(token.LeftParen) {
		return nil
	}
	p.nextToken()

	for !p.curTokenIs(token.RightParen) && !p.curTokenIs(token.EOF) {
		spec := p.parseImportSpec()
		if spec != nil {
			imp.Imports = append(imp.Imports, *spec)
		}
		p.nextToken()
	}

	p.nextToken() // consume ')'
	return imp
}

func (p *Parser) parseImportSpec() *ast.ImportSpec {
	if !p.curTokenIs(token.String) {
		p.error("expected import path string")
		return nil
	}

	spec := &ast.ImportSpec{
		Path:  p.curToken.Literal,
		Token: p.curToken,
	}

	if p.peekTokenIs(token.As) {
		p.nextToken() // consume string
		p.nextToken() // consume 'as'
		if !p.curTokenIs(token.Ident) {
			p.error("expected identifier after 'as'")
			return nil
		}
		spec.Alias = p.curToken.Literal
	}

	return spec
}

// ============================================
// Declarations
// ============================================

func (p *Parser) parseDeclaration() ast.Decl {
	// Collect any attributes before the declaration
	var attrs []ast.Attribute
	for p.curTokenIs(token.HashBracket) {
		attr := p.parseAttribute()
		if attr != nil {
			attrs = append(attrs, *attr)
		}
		p.nextToken()
	}

	switch p.curToken.Type {
	case token.Fn:
		fn := p.parseFnDecl()
		return fn
	case token.Struct:
		return p.parseStructDecl()
	case token.Enum:
		return p.parseEnumDecl()
	case token.Trait:
		return p.parseTraitDecl()
	case token.Impl:
		return p.parseImplDecl()
	case token.TypeKw:
		return p.parseTypeAlias()
	case token.Const:
		return p.parseConstDecl()
	case token.Let:
		return p.parseVarDecl()
	case token.Ident:
		// Check for contextual "test" keyword
		if p.curToken.Literal == "test" {
			return p.parseTestDecl(attrs)
		}
		fallthrough
	default:
		p.error(fmt.Sprintf("unexpected token %s, expected declaration", p.curToken.Type))
		return nil
	}
}

func (p *Parser) parseFnDecl() *ast.FnDecl {
	fn := &ast.FnDecl{Token: p.curToken}

	if !p.expect(token.Ident) {
		return nil
	}
	fn.Name = &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	// Optional generic parameters
	if p.peekTokenIs(token.Less) {
		p.nextToken() // move to '<'
		fn.TypeParams = p.parseTypeParams()
		// After parseTypeParams, curToken is '>'
	}

	if !p.expect(token.LeftParen) {
		return nil
	}

	fn.Params = p.parseParams()

	if !p.curTokenIs(token.RightParen) {
		p.error("expected ')' after parameters")
		return nil
	}

	// Optional return type
	if p.peekTokenIs(token.Arrow) {
		p.nextToken() // consume current token
		p.nextToken() // consume '->'
		fn.ReturnType = p.parseType()
	}

	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' for function body")
		return nil
	}
	p.nextToken()

	fn.Body = p.parseBlockStmt()

	return fn
}

func (p *Parser) parseTypeParams() []ast.TypeParam {
	var params []ast.TypeParam
	// curToken should be '<', move past it
	p.nextToken()

	for !p.curTokenIs(token.Greater) && !p.curTokenIs(token.EOF) {
		if !p.curTokenIs(token.Ident) {
			p.error("expected type parameter name")
			break
		}

		param := ast.TypeParam{
			Name: &ast.Ident{Token: p.curToken, Name: p.curToken.Literal},
		}

		// Optional bounds: T: Trait + OtherTrait
		if p.peekTokenIs(token.Colon) {
			p.nextToken() // move to ':'
			p.nextToken() // move past ':'
			param.Bounds = p.parseTraitBounds()
		}

		params = append(params, param)

		// Move past current param
		if p.peekTokenIs(token.Comma) {
			p.nextToken() // move to ','
			p.nextToken() // move past ','
		} else {
			p.nextToken() // move to '>' (or next param if no comma)
		}
	}

	// curToken is now '>'
	return params
}

func (p *Parser) parseTraitBounds() []ast.Type {
	var bounds []ast.Type

	for {
		bound := p.parseType()
		if bound != nil {
			bounds = append(bounds, bound)
		}

		if !p.peekTokenIs(token.Plus) {
			break
		}
		p.nextToken() // consume type
		p.nextToken() // consume '+'
	}

	return bounds
}

func (p *Parser) parseParams() []ast.Param {
	var params []ast.Param
	p.nextToken() // consume '('

	for !p.curTokenIs(token.RightParen) && !p.curTokenIs(token.EOF) {
		param := p.parseParam()
		if param != nil {
			params = append(params, *param)
		}

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
			p.nextToken()
		} else {
			p.nextToken()
		}
	}

	return params
}

func (p *Parser) parseParam() *ast.Param {
	// Handle &self and &mut self
	if p.curTokenIs(token.Ampersand) {
		param := &ast.Param{}
		p.nextToken()
		mutable := false
		if p.curTokenIs(token.Mut) {
			mutable = true
			p.nextToken()
		}
		if p.curToken.Literal == "self" {
			param.Name = &ast.Ident{Token: p.curToken, Name: "self"}
			param.Type = &ast.RefType{
				Token:   p.curToken,
				Mutable: mutable,
				Type:    &ast.NamedType{Name: &ast.Ident{Token: p.curToken, Name: "Self"}},
			}
			return param
		}
	}

	if !p.curTokenIs(token.Ident) {
		return nil
	}

	param := &ast.Param{
		Name: &ast.Ident{Token: p.curToken, Name: p.curToken.Literal},
	}

	if !p.expect(token.Colon) {
		return nil
	}
	p.nextToken()

	param.Type = p.parseType()
	return param
}

func (p *Parser) parseStructDecl() *ast.StructDecl {
	s := &ast.StructDecl{Token: p.curToken}

	if !p.expect(token.Ident) {
		return nil
	}
	s.Name = &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	if p.peekTokenIs(token.Less) {
		p.nextToken()
		s.TypeParams = p.parseTypeParams()
	}

	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' after struct name")
		return nil
	}
	p.nextToken()
	p.nextToken()

	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		field := p.parseField()
		if field != nil {
			s.Fields = append(s.Fields, *field)
		}

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return s
}

func (p *Parser) parseField() *ast.Field {
	if !p.curTokenIs(token.Ident) {
		return nil
	}

	field := &ast.Field{
		Name: &ast.Ident{Token: p.curToken, Name: p.curToken.Literal},
	}

	if !p.expect(token.Colon) {
		return nil
	}
	p.nextToken()

	field.Type = p.parseType()
	return field
}

func (p *Parser) parseEnumDecl() *ast.EnumDecl {
	e := &ast.EnumDecl{Token: p.curToken}

	if !p.expect(token.Ident) {
		return nil
	}
	e.Name = &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	if p.peekTokenIs(token.Less) {
		p.nextToken()
		e.TypeParams = p.parseTypeParams()
	}

	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' after enum name")
		return nil
	}
	p.nextToken()
	p.nextToken()

	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		variant := p.parseVariant()
		if variant != nil {
			e.Variants = append(e.Variants, *variant)
		}

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return e
}

func (p *Parser) parseVariant() *ast.Variant {
	if !p.curTokenIs(token.Ident) && !p.curTokenIs(token.Some) && !p.curTokenIs(token.None) {
		return nil
	}

	variant := &ast.Variant{
		Name: &ast.Ident{Token: p.curToken, Name: p.curToken.Literal},
	}

	// Optional fields: { field: Type, ... }
	if p.peekTokenIs(token.LeftBrace) {
		p.nextToken()
		p.nextToken()

		for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
			field := p.parseField()
			if field != nil {
				variant.Fields = append(variant.Fields, *field)
			}

			if p.peekTokenIs(token.Comma) {
				p.nextToken()
			}
			p.nextToken()
		}
	}

	return variant
}

func (p *Parser) parseTraitDecl() *ast.TraitDecl {
	t := &ast.TraitDecl{Token: p.curToken}

	if !p.expect(token.Ident) {
		return nil
	}
	t.Name = &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	if p.peekTokenIs(token.Less) {
		p.nextToken()
		t.TypeParams = p.parseTypeParams()
	}

	// Optional super traits: trait Foo: Bar + Baz
	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		p.nextToken()
		t.Bounds = p.parseTraitBounds()
	}

	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' after trait declaration")
		return nil
	}
	p.nextToken()
	p.nextToken()

	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.Fn) {
			method := p.parseTraitMethod()
			if method != nil {
				t.Methods = append(t.Methods, *method)
			}
		} else if p.curTokenIs(token.TypeKw) {
			assoc := p.parseTraitType()
			if assoc != nil {
				t.Types = append(t.Types, *assoc)
			}
		}
		p.nextToken()
	}

	return t
}

func (p *Parser) parseTraitMethod() *ast.TraitMethod {
	m := &ast.TraitMethod{}

	if !p.expect(token.Ident) {
		return nil
	}
	m.Name = &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	if p.peekTokenIs(token.Less) {
		p.nextToken()
		m.TypeParams = p.parseTypeParams()
	}

	if !p.expect(token.LeftParen) {
		return nil
	}

	m.Params = p.parseParams()

	if p.peekTokenIs(token.Arrow) {
		p.nextToken()
		p.nextToken()
		m.ReturnType = p.parseType()
	}

	// Optional default implementation
	if p.peekTokenIs(token.LeftBrace) {
		p.nextToken()
		m.Body = p.parseBlockStmt()
	}

	return m
}

func (p *Parser) parseTraitType() *ast.TraitType {
	t := &ast.TraitType{}

	if !p.expect(token.Ident) {
		return nil
	}
	t.Name = &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		p.nextToken()
		t.Bounds = p.parseTraitBounds()
	}

	return t
}

func (p *Parser) parseImplDecl() *ast.ImplDecl {
	impl := &ast.ImplDecl{Token: p.curToken}

	p.nextToken()

	// Optional generic parameters
	if p.curTokenIs(token.Less) {
		impl.TypeParams = p.parseTypeParams()
	}

	// Parse first type
	firstType := p.parseType()

	// Check for "for" keyword (trait impl)
	if p.peekTokenIs(token.For) {
		impl.Trait = firstType
		p.nextToken() // consume type
		p.nextToken() // consume 'for'
		impl.Type = p.parseType()
	} else {
		impl.Type = firstType
	}

	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' after impl declaration")
		return nil
	}
	p.nextToken()
	p.nextToken()

	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.Fn) {
			fn := p.parseFnDecl()
			if fn != nil {
				impl.Methods = append(impl.Methods, *fn)
			}
		}
		p.nextToken()
	}

	return impl
}

func (p *Parser) parseTypeAlias() *ast.TypeAlias {
	t := &ast.TypeAlias{Token: p.curToken}

	if !p.expect(token.Ident) {
		return nil
	}
	t.Name = &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	if p.peekTokenIs(token.Less) {
		p.nextToken()
		t.TypeParams = p.parseTypeParams()
	}

	if !p.expect(token.Assign) {
		return nil
	}
	p.nextToken()

	t.Type = p.parseType()
	return t
}

func (p *Parser) parseConstDecl() *ast.ConstDecl {
	c := &ast.ConstDecl{Token: p.curToken}

	if !p.expect(token.Ident) {
		return nil
	}
	c.Name = &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		p.nextToken()
		c.Type = p.parseType()
	}

	if !p.expect(token.Assign) {
		return nil
	}
	p.nextToken()

	c.Value = p.parseExpression(LOWEST)
	return c
}

func (p *Parser) parseVarDecl() *ast.VarDecl {
	v := &ast.VarDecl{Token: p.curToken}

	// Check for 'mut' keyword
	if p.peekTokenIs(token.Mut) {
		p.nextToken() // consume 'let'
		v.Mutable = true
	}

	if !p.expect(token.Ident) {
		return nil
	}
	v.Name = &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	// Optional type annotation
	if p.peekTokenIs(token.Colon) {
		p.nextToken() // consume name
		p.nextToken() // consume ':'
		v.Type = p.parseType()
	}

	// Require initializer for global variables
	if !p.expect(token.Assign) {
		p.error("global variables must be initialized")
		return nil
	}
	p.nextToken() // consume '='

	v.Value = p.parseExpression(LOWEST)
	return v
}

func (p *Parser) parseAttribute() *ast.Attribute {
	attr := &ast.Attribute{Token: p.curToken}
	p.nextToken() // consume '#['

	if !p.curTokenIs(token.Ident) {
		p.error("expected attribute name")
		return nil
	}
	attr.Name = &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	// Optional arguments: #[tag(arg1, arg2)]
	if p.peekTokenIs(token.LeftParen) {
		p.nextToken() // move to '('
		p.nextToken() // move past '('

		for !p.curTokenIs(token.RightParen) && !p.curTokenIs(token.EOF) {
			arg := p.parseExpression(LOWEST)
			if arg != nil {
				attr.Args = append(attr.Args, arg)
			}
			if p.peekTokenIs(token.Comma) {
				p.nextToken()
			}
			p.nextToken()
		}
	}

	if !p.expect(token.RightBracket) {
		return nil
	}

	return attr
}

func (p *Parser) parseTestDecl(attrs []ast.Attribute) *ast.TestDecl {
	test := &ast.TestDecl{
		Token:      p.curToken,
		Attributes: attrs,
	}
	p.nextToken() // consume 'test'

	// Parse test description (string literal)
	if !p.curTokenIs(token.String) {
		p.error("expected test description string")
		return nil
	}
	test.Description = &ast.StringLit{Token: p.curToken, Value: p.curToken.Literal}

	// Parse test body
	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' after test description")
		return nil
	}
	p.nextToken()
	test.Body = p.parseBlockStmt()

	return test
}

// ============================================
// Statements
// ============================================

func (p *Parser) parseStatement() ast.Stmt {
	switch p.curToken.Type {
	case token.Let:
		return p.parseLetStmt()
	case token.Return:
		return p.parseReturnStmt()
	case token.If:
		return p.parseIfStmt()
	case token.Match:
		return p.parseMatchStmt()
	case token.For:
		return p.parseForStmt()
	case token.Break:
		return p.parseBreakStmt()
	case token.Continue:
		return p.parseContinueStmt()
	case token.Go:
		return p.parseGoStmt()
	case token.Select:
		return p.parseSelectStmt()
	case token.LeftBrace:
		return p.parseBlockStmt()
	default:
		return p.parseExprStmt()
	}
}

func (p *Parser) parseLetStmt() *ast.LetStmt {
	stmt := &ast.LetStmt{Token: p.curToken}
	p.nextToken()

	if p.curTokenIs(token.Mut) {
		stmt.Mutable = true
		p.nextToken()
	}

	stmt.Pattern = p.parsePattern()

	if p.peekTokenIs(token.Colon) {
		p.nextToken()
		p.nextToken()
		stmt.Type = p.parseType()
	}

	if p.peekTokenIs(token.Assign) {
		p.nextToken()
		p.nextToken()
		stmt.Value = p.parseExpression(LOWEST)
	}

	return stmt
}

func (p *Parser) parseReturnStmt() *ast.ReturnStmt {
	stmt := &ast.ReturnStmt{Token: p.curToken}

	// Check if there's a return value by looking at peek token
	// If peek is }, ;, EOF, or a keyword that starts a new statement, there's no return value
	if p.peekTokenIs(token.RightBrace) || p.peekTokenIs(token.Semicolon) || p.peekTokenIs(token.EOF) {
		// No return value - stay at 'return' token
		return stmt
	}

	// There's a return value - advance and parse expression
	p.nextToken()
	stmt.Value = p.parseExpression(LOWEST)

	return stmt
}

func (p *Parser) parseBlockStmt() *ast.BlockStmt {
	block := &ast.BlockStmt{Token: p.curToken}
	p.nextToken()

	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Stmts = append(block.Stmts, stmt)
		}
		p.nextToken()
	}

	return block
}

func (p *Parser) parseIfStmt() *ast.IfStmt {
	stmt := &ast.IfStmt{Token: p.curToken}
	p.nextToken()

	stmt.Cond = p.parseExpressionNoStruct(LOWEST)

	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' after if condition")
		return nil
	}
	p.nextToken()
	stmt.Then = p.parseBlockStmt()

	if p.peekTokenIs(token.Else) {
		p.nextToken()
		p.nextToken()
		if p.curTokenIs(token.If) {
			stmt.Else = p.parseIfStmt()
		} else if p.curTokenIs(token.LeftBrace) {
			stmt.Else = p.parseBlockStmt()
		}
	}

	return stmt
}

func (p *Parser) parseMatchStmt() *ast.MatchStmt {
	stmt := &ast.MatchStmt{Token: p.curToken}
	p.nextToken()

	stmt.Expr = p.parseExpressionNoStruct(LOWEST)

	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' after match expression")
		return nil
	}
	p.nextToken()
	p.nextToken()

	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		arm := p.parseMatchArm()
		stmt.Arms = append(stmt.Arms, arm)

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseMatchArm() ast.MatchArm {
	arm := ast.MatchArm{}
	arm.Pattern = p.parsePattern()

	// Optional guard
	if p.peekTokenIs(token.If) {
		p.nextToken()
		p.nextToken()
		arm.Guard = p.parseExpression(LOWEST)
	}

	if !p.expect(token.FatArrow) {
		return arm
	}
	p.nextToken()

	if p.curTokenIs(token.LeftBrace) {
		arm.Body = &ast.BlockExpr{Block: p.parseBlockStmt()}
	} else {
		arm.Body = p.parseExpression(LOWEST)
	}

	return arm
}

func (p *Parser) parseForStmt() *ast.ForStmt {
	stmt := &ast.ForStmt{Token: p.curToken}
	p.nextToken()

	// Check for infinite loop: for { }
	if p.curTokenIs(token.LeftBrace) {
		stmt.Body = p.parseBlockStmt()
		return stmt
	}

	// Try to parse as range loop: for pattern in iter { }
	// We need to look ahead to see if there's an "in" keyword
	// First, try to parse a pattern
	startPos := p.curToken

	// Check if this looks like a condition or a pattern
	// If the next significant token after an expression is "in", it's a range loop
	// Otherwise, it's a condition loop

	// Parse the first part as an expression (could be condition or pattern start)
	firstExpr := p.parseExpressionNoStruct(LOWEST)

	if p.peekTokenIs(token.In) {
		// Range loop: for x in iter { }
		stmt.Pattern = exprToPattern(firstExpr)
		p.nextToken() // move past pattern
		p.nextToken() // consume 'in'
		stmt.Iter = p.parseExpressionNoStruct(LOWEST)
	} else if p.peekTokenIs(token.LeftBrace) {
		// Condition loop: for cond { }
		stmt.Cond = firstExpr
	} else {
		p.error("expected '{' or 'in' in for statement at " + startPos.Pos.String())
		return nil
	}

	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' after for clause")
		return nil
	}
	p.nextToken()
	stmt.Body = p.parseBlockStmt()

	return stmt
}

func (p *Parser) parseBreakStmt() *ast.BreakStmt {
	stmt := &ast.BreakStmt{Token: p.curToken}

	// Only advance if the next token could be a value (not } or ;)
	if !p.peekTokenIs(token.RightBrace) && !p.peekTokenIs(token.Semicolon) {
		p.nextToken()
		stmt.Value = p.parseExpression(LOWEST)
	}

	return stmt
}

func (p *Parser) parseContinueStmt() *ast.ContinueStmt {
	return &ast.ContinueStmt{Token: p.curToken}
}

func (p *Parser) parseGoStmt() *ast.GoStmt {
	stmt := &ast.GoStmt{Token: p.curToken}
	p.nextToken()
	stmt.Expr = p.parseExpression(LOWEST)
	return stmt
}

func (p *Parser) parseSelectStmt() *ast.SelectStmt {
	stmt := &ast.SelectStmt{Token: p.curToken}

	if !p.expect(token.LeftBrace) {
		return nil
	}
	p.nextToken()

	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		arm := p.parseSelectArm()
		stmt.Arms = append(stmt.Arms, arm)

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseSelectArm() ast.SelectArm {
	arm := ast.SelectArm{}

	if p.curTokenIs(token.Default) {
		arm.IsDefault = true
	} else {
		// Could be receive: pattern = <-chan
		// Or send: chan <- value
		// For now, simple parsing
		first := p.parseExpression(LOWEST)

		if p.peekTokenIs(token.ChanArrow) {
			// Send: chan <- value
			arm.IsSend = true
			arm.Chan = first
			p.nextToken() // consume expression
			p.nextToken() // consume '<-'
			arm.Value = p.parseExpression(LOWEST)
		} else if p.peekTokenIs(token.Assign) {
			// Receive with binding: pattern = <-chan
			// Convert first to pattern
			arm.Pattern = exprToPattern(first)
			p.nextToken() // consume pattern
			p.nextToken() // consume '='
			if !p.curTokenIs(token.ChanArrow) {
				p.error("expected '<-' in select receive")
				return arm
			}
			p.nextToken() // consume '<-'
			arm.Chan = p.parseExpression(LOWEST)
		}
	}

	if !p.expect(token.FatArrow) {
		return arm
	}
	p.nextToken()

	if p.curTokenIs(token.LeftBrace) {
		arm.Body = &ast.BlockExpr{Block: p.parseBlockStmt()}
	} else {
		arm.Body = p.parseExpression(LOWEST)
	}

	return arm
}

func (p *Parser) parseExprStmt() *ast.ExprStmt {
	stmt := &ast.ExprStmt{Expr: p.parseExpression(LOWEST)}
	return stmt
}

// ============================================
// Expressions - Pratt Parser
// ============================================

type Precedence int

const (
	LOWEST      Precedence = iota
	ASSIGN                 // =, +=, etc.
	OR                     // ||
	AND                    // &&
	EQUALITY               // ==, !=
	COMPARISON             // <, >, <=, >=
	BITOR                  // |
	BITXOR                 // ^
	BITAND                 // &
	SHIFT                  // <<, >>
	RANGE                  // .., ..=
	SUM                    // +, -
	PRODUCT                // *, /, %
	PREFIX                 // -x, !x, &x
	CALL                   // f(x), a[i], a.b
	PATH                   // ::
)

var precedences = map[token.Type]Precedence{
	token.Assign:        ASSIGN,
	token.PlusAssign:    ASSIGN,
	token.MinusAssign:   ASSIGN,
	token.StarAssign:    ASSIGN,
	token.SlashAssign:   ASSIGN,
	token.PercentAssign: ASSIGN,
	token.LogicalOr:     OR,
	token.LogicalAnd:    AND,
	token.Equal:         EQUALITY,
	token.NotEqual:      EQUALITY,
	token.Less:          COMPARISON,
	token.LessEqual:     COMPARISON,
	token.Greater:       COMPARISON,
	token.GreaterEqual:  COMPARISON,
	token.Pipe:          BITOR,
	token.Caret:         BITXOR,
	token.Ampersand:     BITAND,
	token.ShiftLeft:     SHIFT,
	token.ShiftRight:    SHIFT,
	token.DotDot:        RANGE,
	token.DotDotEq:      RANGE,
	token.Plus:          SUM,
	token.Minus:         SUM,
	token.Star:          PRODUCT,
	token.Slash:         PRODUCT,
	token.Percent:       PRODUCT,
	token.LeftParen:     CALL,
	token.LeftBracket:   CALL,
	token.Dot:           CALL,
	token.Question:      CALL,
	token.ChanArrow:     CALL,
	token.ColonColon:    PATH,
}

func (p *Parser) peekPrecedence() Precedence {
	if prec, ok := precedences[p.peekToken.Type]; ok {
		return prec
	}
	return LOWEST
}

func (p *Parser) curPrecedence() Precedence {
	if prec, ok := precedences[p.curToken.Type]; ok {
		return prec
	}
	return LOWEST
}

func (p *Parser) parseExpression(prec Precedence) ast.Expr {
	return p.parseExpressionImpl(prec)
}

// parseExpressionNoStruct parses expression but stops before '{' (for match/if conditions)
// This sets p.noStruct for the entire duration of parsing this expression tree
func (p *Parser) parseExpressionNoStruct(prec Precedence) ast.Expr {
	oldNoStruct := p.noStruct
	p.noStruct = true
	defer func() { p.noStruct = oldNoStruct }()
	return p.parseExpressionImpl(prec)
}

func (p *Parser) parseExpressionImpl(prec Precedence) ast.Expr {
	left := p.parsePrefixExpr()
	if left == nil {
		return nil
	}

	for !p.peekTokenIs(token.EOF) && prec < p.peekPrecedence() {
		// Stop before '{' if noStruct flag is set (for match/if conditions)
		if p.noStruct && p.peekTokenIs(token.LeftBrace) {
			break
		}
		p.nextToken()
		left = p.parseInfixExpr(left)
	}

	// Handle struct literals: Ident { ... } or Path::Name { ... }
	// Only when not in noStruct mode and next token is '{'
	if !p.noStruct && p.peekTokenIs(token.LeftBrace) {
		// Check if left is an identifier or path (valid struct names)
		switch left.(type) {
		case *ast.Ident, *ast.PathExpr:
			p.nextToken() // move to '{'
			left = p.parseStructExpr(left)
		}
	}

	return left
}

func (p *Parser) parsePrefixExpr() ast.Expr {
	switch p.curToken.Type {
	case token.Ident:
		return p.parseIdentOrPath()
	case token.Int:
		return p.parseIntLit()
	case token.Float:
		return p.parseFloatLit()
	case token.String:
		return p.parseStringLit()
	case token.Char:
		return p.parseCharLit()
	case token.True, token.False:
		return p.parseBoolLit()
	case token.None:
		return &ast.Ident{Token: p.curToken, Name: "None"}
	case token.Some:
		return &ast.Ident{Token: p.curToken, Name: "Some"}
	case token.Minus, token.Not, token.Ampersand, token.Star:
		return p.parseUnaryExpr()
	case token.LeftParen:
		return p.parseGroupedOrTuple()
	case token.LeftBracket:
		return p.parseArrayExpr()
	case token.LeftBrace:
		return &ast.BlockExpr{Block: p.parseBlockStmt()}
	case token.If:
		return p.parseIfExpr()
	case token.Match:
		return p.parseMatchExpr()
	case token.Pipe, token.LogicalOr:
		return p.parseClosureExpr()
	case token.Move:
		return p.parseClosureExpr()
	case token.ChanArrow:
		return p.parseChanRecv()
	case token.Chan:
		return p.parseChanCreate()
	case token.Map:
		return p.parseMapExpr()
	default:
		p.error(fmt.Sprintf("unexpected token %s in expression", p.curToken.Type))
		return nil
	}
}

func (p *Parser) parseIdentOrPath() ast.Expr {
	ident := &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	// Check for :: path
	if p.peekTokenIs(token.ColonColon) {
		return p.parsePath(ident)
	}

	// Note: Struct literals (Ident { ... }) are handled in parseInfixExpr
	// to avoid ambiguity with block expressions after match/if/for
	return ident
}

func (p *Parser) parsePath(first *ast.Ident) ast.Expr {
	path := &ast.PathExpr{Parts: []ast.Ident{*first}}

	for p.peekTokenIs(token.ColonColon) {
		p.nextToken() // consume current
		p.nextToken() // consume '::'
		if !p.curTokenIs(token.Ident) && !p.curTokenIs(token.Some) && !p.curTokenIs(token.None) {
			p.error("expected identifier after '::'")
			break
		}
		path.Parts = append(path.Parts, ast.Ident{Token: p.curToken, Name: p.curToken.Literal})
	}

	// Check for struct literal after path: Path::Variant { ... }
	if p.peekTokenIs(token.LeftBrace) {
		p.nextToken() // move to '{'
		return p.parseStructExpr(path)
	}

	return path
}

func (p *Parser) parseStructExpr(name ast.Expr) ast.Expr {
	s := &ast.StructExpr{Name: name}
	// curToken is '{', move past it
	p.nextToken()

	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		field := p.parseFieldInit()
		s.Fields = append(s.Fields, field)

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return s
}

func (p *Parser) parseFieldInit() ast.FieldInit {
	init := ast.FieldInit{
		Name: &ast.Ident{Token: p.curToken, Name: p.curToken.Literal},
	}

	if p.peekTokenIs(token.Colon) {
		p.nextToken() // consume name
		p.nextToken() // consume ':'
		init.Value = p.parseExpression(LOWEST)
	}

	return init
}

func (p *Parser) parseIntLit() *ast.IntLit {
	lit := &ast.IntLit{Token: p.curToken, Raw: p.curToken.Literal}

	// Parse the value
	s := strings.ReplaceAll(p.curToken.Literal, "_", "")
	var val int64
	var err error

	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		val, err = strconv.ParseInt(s[2:], 16, 64)
	} else if strings.HasPrefix(s, "0o") || strings.HasPrefix(s, "0O") {
		val, err = strconv.ParseInt(s[2:], 8, 64)
	} else if strings.HasPrefix(s, "0b") || strings.HasPrefix(s, "0B") {
		val, err = strconv.ParseInt(s[2:], 2, 64)
	} else {
		val, err = strconv.ParseInt(s, 10, 64)
	}

	if err != nil {
		p.error(fmt.Sprintf("could not parse %q as integer", p.curToken.Literal))
	}
	lit.Value = val

	return lit
}

func (p *Parser) parseFloatLit() *ast.FloatLit {
	lit := &ast.FloatLit{Token: p.curToken, Raw: p.curToken.Literal}

	s := strings.ReplaceAll(p.curToken.Literal, "_", "")
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		p.error(fmt.Sprintf("could not parse %q as float", p.curToken.Literal))
	}
	lit.Value = val

	return lit
}

func (p *Parser) parseStringLit() *ast.StringLit {
	return &ast.StringLit{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseCharLit() *ast.CharLit {
	lit := &ast.CharLit{Token: p.curToken}
	if len(p.curToken.Literal) > 0 {
		lit.Value = rune(p.curToken.Literal[0])
	}
	return lit
}

func (p *Parser) parseBoolLit() *ast.BoolLit {
	return &ast.BoolLit{
		Token: p.curToken,
		Value: p.curTokenIs(token.True),
	}
}

func (p *Parser) parseUnaryExpr() *ast.UnaryExpr {
	expr := &ast.UnaryExpr{Op: p.curToken}
	p.nextToken()
	expr.Right = p.parseExpression(PREFIX)
	return expr
}

func (p *Parser) parseGroupedOrTuple() ast.Expr {
	tok := p.curToken
	p.nextToken()

	// Empty parens = unit
	if p.curTokenIs(token.RightParen) {
		return &ast.TupleExpr{Token: tok}
	}

	first := p.parseExpression(LOWEST)

	// Check for tuple: (a, b, ...)
	if p.peekTokenIs(token.Comma) {
		tuple := &ast.TupleExpr{Token: tok, Elements: []ast.Expr{first}}
		for p.peekTokenIs(token.Comma) {
			p.nextToken() // consume expr
			p.nextToken() // consume ','
			if p.curTokenIs(token.RightParen) {
				break
			}
			tuple.Elements = append(tuple.Elements, p.parseExpression(LOWEST))
		}
		if !p.peekTokenIs(token.RightParen) {
			p.nextToken()
		}
		return tuple
	}

	if !p.expect(token.RightParen) {
		return nil
	}

	return first
}

func (p *Parser) parseArrayExpr() *ast.ArrayExpr {
	arr := &ast.ArrayExpr{Token: p.curToken}
	p.nextToken() // consume '['

	// Go-style: []type{elements} or [size]type{elements}
	if p.curTokenIs(token.RightBracket) {
		// []type{...} - slice literal
		p.nextToken() // consume ']'
		// Parse element type
		arr.ElemType = p.parseType()
		// Expect '{'
		if !p.expect(token.LeftBrace) {
			return nil
		}
		// Parse elements
		arr.Elements = p.parseExprList(token.RightBrace)
		// parseExprList returns with:
		// - current at '}' for empty arrays
		// - current at last expression, peek at '}' for non-empty arrays
		// We want to end with current at '}'
		if p.curTokenIs(token.RightBrace) {
			// Empty array: current is already at '}', nothing to do
		} else if p.peekTokenIs(token.RightBrace) {
			// Non-empty array: advance past last expression to '}'
			p.nextToken() // move to '}'
		} else {
			p.error("expected } after array elements")
			return nil
		}
		return arr
	}

	// Could be [size]type{...} or old-style [elem, elem, ...]
	first := p.parseExpression(LOWEST)

	// Check for [size]type{...} - size followed by ]
	if p.peekTokenIs(token.RightBracket) {
		p.nextToken() // consume size expr
		p.nextToken() // consume ']'

		// Check if followed by a type (identifier, [], etc.)
		if p.curTokenIs(token.Ident) || p.curTokenIs(token.LeftBracket) {
			// [size]type{...} syntax
			arr.Count = first
			arr.ElemType = p.parseType()
			// Expect '{'
			if !p.expect(token.LeftBrace) {
				return nil
			}
			// Parse elements
			arr.Elements = p.parseExprList(token.RightBrace)
			// parseExprList returns with:
			// - current at '}' for empty arrays
			// - current at last expression, peek at '}' for non-empty arrays
			// We want to end with current at '}'
			if p.curTokenIs(token.RightBrace) {
				// Empty array: current is already at '}', nothing to do
			} else if p.peekTokenIs(token.RightBrace) {
				// Non-empty array: advance past last expression to '}'
				p.nextToken() // move to '}'
			} else {
				p.error("expected } after array elements")
				return nil
			}
			return arr
		}
		// Otherwise it was just [expr] which isn't valid as a standalone expression
		p.error("expected type after array size")
		return nil
	}

	// Check for repeat syntax: [expr; count] (keep for backwards compat)
	if p.peekTokenIs(token.Semicolon) {
		arr.Repeat = first
		p.nextToken() // consume expr
		p.nextToken() // consume ';'
		arr.Count = p.parseExpression(LOWEST)
		if !p.expect(token.RightBracket) {
			return nil
		}
		return arr
	}

	// Old-style [elem, elem, ...] - keep for backwards compatibility during transition
	arr.Elements = []ast.Expr{first}

	for p.peekTokenIs(token.Comma) {
		p.nextToken() // consume expr
		p.nextToken() // consume ','
		if p.curTokenIs(token.RightBracket) {
			break
		}
		arr.Elements = append(arr.Elements, p.parseExpression(LOWEST))
	}

	if !p.expect(token.RightBracket) {
		return nil
	}

	return arr
}

func (p *Parser) parseExprList(end token.Type) []ast.Expr {
	var list []ast.Expr

	if p.curTokenIs(end) {
		return list
	}

	p.nextToken() // move past '{'
	if p.curTokenIs(end) {
		return list
	}

	list = append(list, p.parseExpression(LOWEST))

	for p.peekTokenIs(token.Comma) {
		p.nextToken() // consume expr
		p.nextToken() // consume ','
		if p.curTokenIs(end) {
			break
		}
		list = append(list, p.parseExpression(LOWEST))
	}

	return list
}

func (p *Parser) parseIfExpr() *ast.IfExpr {
	expr := &ast.IfExpr{Token: p.curToken}
	p.nextToken()

	expr.Cond = p.parseExpressionNoStruct(LOWEST)

	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' after if condition")
		return nil
	}
	p.nextToken()
	expr.Then = p.parseBlockStmt()

	if p.peekTokenIs(token.Else) {
		p.nextToken()
		p.nextToken()
		if p.curTokenIs(token.If) {
			expr.Else = p.parseIfExpr()
		} else if p.curTokenIs(token.LeftBrace) {
			expr.Else = &ast.BlockExpr{Block: p.parseBlockStmt()}
		}
	}

	return expr
}

func (p *Parser) parseMatchExpr() *ast.MatchExpr {
	expr := &ast.MatchExpr{Token: p.curToken}
	p.nextToken()

	expr.Expr = p.parseExpression(LOWEST)

	if !p.peekTokenIs(token.LeftBrace) {
		p.error("expected '{' after match expression")
		return nil
	}
	p.nextToken()
	p.nextToken()

	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		arm := p.parseMatchArm()
		expr.Arms = append(expr.Arms, arm)

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return expr
}

func (p *Parser) parseClosureExpr() *ast.ClosureExpr {
	closure := &ast.ClosureExpr{Token: p.curToken}

	if p.curTokenIs(token.Move) {
		closure.Move = true
		p.nextToken()
	}

	// Handle || for empty params or | params |
	if p.curTokenIs(token.LogicalOr) {
		// Empty params
		p.nextToken()
	} else if p.curTokenIs(token.Pipe) {
		p.nextToken()
		closure.Params = p.parseClosureParams()
		if !p.curTokenIs(token.Pipe) {
			p.error("expected '|' after closure parameters")
			return nil
		}
		p.nextToken()
	}

	// Optional return type
	if p.curTokenIs(token.Arrow) {
		p.nextToken()
		closure.ReturnType = p.parseType()
		p.nextToken()
	}

	// Body
	if p.curTokenIs(token.LeftBrace) {
		closure.Body = &ast.BlockExpr{Block: p.parseBlockStmt()}
	} else {
		closure.Body = p.parseExpression(LOWEST)
	}

	return closure
}

func (p *Parser) parseClosureParams() []ast.Param {
	var params []ast.Param

	for !p.curTokenIs(token.Pipe) && !p.curTokenIs(token.EOF) {
		param := ast.Param{
			Name: &ast.Ident{Token: p.curToken, Name: p.curToken.Literal},
		}

		if p.peekTokenIs(token.Colon) {
			p.nextToken()
			p.nextToken()
			param.Type = p.parseType()
		}

		params = append(params, param)

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return params
}

func (p *Parser) parseChanRecv() *ast.ChanRecvExpr {
	expr := &ast.ChanRecvExpr{Token: p.curToken}
	p.nextToken()
	expr.Chan = p.parseExpression(PREFIX)
	return expr
}

func (p *Parser) parseChanCreate() ast.Expr {
	// chan<T>() or chan<T>(size)
	expr := &ast.ChanMakeExpr{Token: p.curToken}

	if !p.expect(token.Less) {
		return nil
	}
	p.nextToken()

	expr.Element = p.parseType()

	if !p.expect(token.Greater) {
		return nil
	}

	// Optional (size) for buffered channel
	if p.peekTokenIs(token.LeftParen) {
		p.nextToken() // move to '('
		p.nextToken() // move past '('
		if !p.curTokenIs(token.RightParen) {
			expr.Size = p.parseExpression(LOWEST)
			if !p.expect(token.RightParen) {
				return nil
			}
		}
	}

	return expr
}

func (p *Parser) parseMapExpr() *ast.MapExpr {
	m := &ast.MapExpr{Token: p.curToken}

	// Parse map type: map[K]V
	m.MapType = p.parseMapType()
	if m.MapType == nil {
		return nil
	}

	// Expect '{' after type
	if !p.expect(token.LeftBrace) {
		return nil
	}
	p.nextToken() // move past '{'

	// Parse key: value pairs
	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		entry := ast.MapEntry{}

		// Parse key
		entry.Key = p.parseExpression(LOWEST)

		// Expect ':'
		if !p.expect(token.Colon) {
			return nil
		}
		p.nextToken() // move past ':'

		// Parse value
		entry.Value = p.parseExpression(LOWEST)

		m.Entries = append(m.Entries, entry)

		// Handle comma
		if p.peekTokenIs(token.Comma) {
			p.nextToken() // move to ','
			p.nextToken() // move past ','
		} else {
			p.nextToken() // move to '}'
		}
	}

	return m
}

func (p *Parser) parseInfixExpr(left ast.Expr) ast.Expr {
	switch p.curToken.Type {
	case token.Plus, token.Minus, token.Star, token.Slash, token.Percent,
		token.Equal, token.NotEqual, token.Less, token.LessEqual,
		token.Greater, token.GreaterEqual, token.LogicalAnd, token.LogicalOr,
		token.Ampersand, token.Pipe, token.Caret, token.ShiftLeft, token.ShiftRight:
		return p.parseBinaryExpr(left)

	case token.Assign, token.PlusAssign, token.MinusAssign,
		token.StarAssign, token.SlashAssign, token.PercentAssign:
		return p.parseAssignExpr(left)

	case token.DotDot, token.DotDotEq:
		return p.parseRangeExpr(left)

	case token.LeftParen:
		return p.parseCallExpr(left)

	case token.LeftBracket:
		return p.parseIndexExpr(left)

	case token.Dot:
		return p.parseFieldOrMethod(left)

	case token.Question:
		return &ast.TryExpr{Expr: left}

	case token.ChanArrow:
		return p.parseChanSend(left)

	case token.ColonColon:
		// Continue path
		if ident, ok := left.(*ast.Ident); ok {
			return p.parsePath(ident)
		}
		p.error("unexpected '::' after non-identifier")
		return left

	default:
		return left
	}
}

func (p *Parser) parseBinaryExpr(left ast.Expr) *ast.BinaryExpr {
	expr := &ast.BinaryExpr{
		Left: left,
		Op:   p.curToken,
	}
	prec := p.curPrecedence()
	p.nextToken()
	// Use parseExpressionImpl to respect p.noStruct context
	expr.Right = p.parseExpressionImpl(prec)
	return expr
}

func (p *Parser) parseAssignExpr(left ast.Expr) *ast.BinaryExpr {
	expr := &ast.BinaryExpr{
		Left: left,
		Op:   p.curToken,
	}
	p.nextToken()
	expr.Right = p.parseExpression(LOWEST)
	return expr
}

func (p *Parser) parseRangeExpr(left ast.Expr) *ast.RangeExpr {
	expr := &ast.RangeExpr{
		Start:     left,
		Inclusive: p.curTokenIs(token.DotDotEq),
	}
	p.nextToken()

	// Check if there's an end expression
	if !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.RightBracket) &&
		!p.curTokenIs(token.Comma) && !p.curTokenIs(token.EOF) {
		expr.End = p.parseExpression(RANGE)
	}

	return expr
}

func (p *Parser) parseCallExpr(left ast.Expr) *ast.CallExpr {
	call := &ast.CallExpr{Func: left}
	p.nextToken() // consume '('

	for !p.curTokenIs(token.RightParen) && !p.curTokenIs(token.EOF) {
		call.Args = append(call.Args, p.parseExpression(LOWEST))
		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return call
}

func (p *Parser) parseIndexExpr(left ast.Expr) ast.Expr {
	p.nextToken() // consume '['

	// Check for slice with no start: [:end]
	if p.curTokenIs(token.Colon) {
		p.nextToken() // consume ':'
		var end ast.Expr
		if !p.curTokenIs(token.RightBracket) {
			end = p.parseExpression(LOWEST)
			p.nextToken() // move past expression
		}
		if !p.curTokenIs(token.RightBracket) {
			p.error("expected ]")
			return nil
		}
		return &ast.SliceExpr{Expr: left, Start: nil, End: end}
	}

	// Parse start expression
	start := p.parseExpression(LOWEST)

	// Check for slice: [start:end] or [start:]
	if p.peekTokenIs(token.Colon) {
		p.nextToken() // move to ':'
		p.nextToken() // consume ':'
		var end ast.Expr
		if !p.curTokenIs(token.RightBracket) {
			end = p.parseExpression(LOWEST)
			p.nextToken() // move past expression
		}
		if !p.curTokenIs(token.RightBracket) {
			p.error("expected ]")
			return nil
		}
		return &ast.SliceExpr{Expr: left, Start: start, End: end}
	}

	// Regular index expression
	if !p.expect(token.RightBracket) {
		return nil
	}
	return &ast.IndexExpr{Expr: left, Index: start}
}

func (p *Parser) parseFieldOrMethod(left ast.Expr) ast.Expr {
	p.nextToken() // consume '.'

	if !p.curTokenIs(token.Ident) {
		p.error("expected identifier after '.'")
		return left
	}

	name := &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	// Check for method call
	if p.peekTokenIs(token.LeftParen) {
		method := &ast.MethodExpr{Expr: left, Method: name}
		p.nextToken() // consume name
		p.nextToken() // consume '('

		for !p.curTokenIs(token.RightParen) && !p.curTokenIs(token.EOF) {
			method.Args = append(method.Args, p.parseExpression(LOWEST))
			if p.peekTokenIs(token.Comma) {
				p.nextToken()
			}
			p.nextToken()
		}

		return method
	}

	return &ast.FieldExpr{Expr: left, Field: name}
}

func (p *Parser) parseChanSend(left ast.Expr) *ast.ChanSendExpr {
	p.nextToken() // consume '<-'
	return &ast.ChanSendExpr{
		Chan:  left,
		Value: p.parseExpression(LOWEST),
	}
}

// ============================================
// Types
// ============================================

func (p *Parser) parseType() ast.Type {
	switch p.curToken.Type {
	case token.Ident:
		return p.parseNamedType()
	case token.LeftParen:
		return p.parseTupleOrUnitType()
	case token.LeftBracket:
		return p.parseArrayOrSliceType()
	case token.Fn:
		return p.parseFnType()
	case token.Ampersand:
		return p.parseRefType()
	case token.Chan:
		return p.parseChanType()
	case token.Map:
		return p.parseMapType()
	default:
		p.error(fmt.Sprintf("unexpected token %s in type", p.curToken.Type))
		return nil
	}
}

func (p *Parser) parseNamedType() ast.Type {
	name := &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	// Check for path type: Foo::Bar
	if p.peekTokenIs(token.ColonColon) {
		return p.parsePathType(name)
	}

	t := &ast.NamedType{Name: name}

	// Check for generic args: Foo<T, U>
	if p.peekTokenIs(token.Less) {
		p.nextToken() // consume name
		p.nextToken() // consume '<'
		t.TypeArgs = p.parseTypeArgs()
	}

	return t
}

func (p *Parser) parsePathType(first *ast.Ident) *ast.PathType {
	path := &ast.PathType{Parts: []ast.Ident{*first}}

	for p.peekTokenIs(token.ColonColon) {
		p.nextToken() // consume current
		p.nextToken() // consume '::'
		if !p.curTokenIs(token.Ident) {
			p.error("expected identifier after '::'")
			break
		}
		path.Parts = append(path.Parts, ast.Ident{Token: p.curToken, Name: p.curToken.Literal})
	}

	if p.peekTokenIs(token.Less) {
		p.nextToken()
		p.nextToken()
		path.TypeArgs = p.parseTypeArgs()
	}

	return path
}

func (p *Parser) parseTypeArgs() []ast.Type {
	var args []ast.Type

	for !p.curTokenIs(token.Greater) && !p.curTokenIs(token.EOF) {
		arg := p.parseType()
		if arg != nil {
			args = append(args, arg)
		}

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return args
}

func (p *Parser) parseTupleOrUnitType() ast.Type {
	tok := p.curToken
	p.nextToken()

	// Unit type: ()
	if p.curTokenIs(token.RightParen) {
		return &ast.UnitType{Token: tok}
	}

	first := p.parseType()

	// Check for tuple: (T, U)
	if p.peekTokenIs(token.Comma) {
		tuple := &ast.TupleType{Token: tok, Elements: []ast.Type{first}}
		for p.peekTokenIs(token.Comma) {
			p.nextToken() // consume type
			p.nextToken() // consume ','
			if p.curTokenIs(token.RightParen) {
				break
			}
			tuple.Elements = append(tuple.Elements, p.parseType())
		}
		if !p.peekTokenIs(token.RightParen) {
			p.nextToken()
		}
		return tuple
	}

	// Just parenthesized type
	if !p.expect(token.RightParen) {
		return nil
	}

	return first
}

func (p *Parser) parseArrayOrSliceType() ast.Type {
	tok := p.curToken
	p.nextToken() // consume '['

	// Go-style: []T for slice, [size]T for array
	if p.curTokenIs(token.RightBracket) {
		// []T - slice type
		p.nextToken() // consume ']', now at type
		elem := p.parseType()
		return &ast.SliceType{Token: tok, Element: elem}
	}

	// [size]T - array type with size
	size := p.parseExpression(LOWEST)
	if !p.expect(token.RightBracket) {
		return nil
	}
	p.nextToken() // move past ']' to the type
	elem := p.parseType()
	return &ast.ArrayType{Token: tok, Element: elem, Size: size}
}

func (p *Parser) parseFnType() *ast.FnType {
	fn := &ast.FnType{Token: p.curToken}

	if !p.expect(token.LeftParen) {
		return nil
	}
	p.nextToken()

	for !p.curTokenIs(token.RightParen) && !p.curTokenIs(token.EOF) {
		fn.Params = append(fn.Params, p.parseType())
		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	if p.peekTokenIs(token.Arrow) {
		p.nextToken()
		p.nextToken()
		fn.ReturnType = p.parseType()
	}

	return fn
}

func (p *Parser) parseRefType() *ast.RefType {
	ref := &ast.RefType{Token: p.curToken}
	p.nextToken()

	if p.curTokenIs(token.Mut) {
		ref.Mutable = true
		p.nextToken()
	}

	ref.Type = p.parseType()
	return ref
}

func (p *Parser) parseChanType() *ast.ChanType {
	ch := &ast.ChanType{Token: p.curToken}

	if !p.expect(token.Less) {
		return nil
	}
	p.nextToken()

	ch.Element = p.parseType()

	if !p.expect(token.Greater) {
		return nil
	}

	return ch
}

func (p *Parser) parseMapType() *ast.MapType {
	m := &ast.MapType{Token: p.curToken}

	// Expect '[' after 'map'
	if !p.expect(token.LeftBracket) {
		return nil
	}
	p.nextToken() // move to key type

	m.Key = p.parseType()

	// Expect ']' after key type
	if !p.expect(token.RightBracket) {
		return nil
	}
	p.nextToken() // move to value type

	m.Value = p.parseType()

	return m
}

// ============================================
// Patterns
// ============================================

func (p *Parser) parsePattern() ast.Pattern {
	var pattern ast.Pattern

	switch p.curToken.Type {
	case token.Ident:
		pattern = p.parseIdentOrStructPattern()
	case token.Int, token.Float, token.String, token.Char, token.True, token.False:
		pattern = &ast.LiteralPattern{Value: p.parsePrefixExpr()}
	case token.None:
		pattern = &ast.IdentPattern{Name: &ast.Ident{Token: p.curToken, Name: "None"}}
	case token.Some:
		pattern = p.parseEnumPatternFromIdent(&ast.Ident{Token: p.curToken, Name: "Some"})
	case token.LeftParen:
		pattern = p.parseTuplePattern()
	case token.LeftBracket:
		pattern = p.parseSlicePattern()
	case token.Mut:
		p.nextToken()
		pattern = &ast.IdentPattern{
			Mutable: true,
			Name:    &ast.Ident{Token: p.curToken, Name: p.curToken.Literal},
		}
	default:
		p.error(fmt.Sprintf("unexpected token %s in pattern", p.curToken.Type))
		return nil
	}

	// Check for or-pattern
	if p.peekTokenIs(token.Pipe) {
		p.nextToken() // consume first pattern
		p.nextToken() // consume '|'
		right := p.parsePattern()
		pattern = &ast.OrPattern{Left: pattern, Right: right}
	}

	// Check for range pattern
	if p.peekTokenIs(token.DotDot) || p.peekTokenIs(token.DotDotEq) {
		inclusive := p.peekTokenIs(token.DotDotEq)
		p.nextToken() // consume first pattern
		p.nextToken() // consume '..' or '..='
		end := p.parsePattern()
		pattern = &ast.RangePattern{Start: pattern, End: end, Inclusive: inclusive}
	}

	return pattern
}

func (p *Parser) parseIdentOrStructPattern() ast.Pattern {
	if p.curToken.Literal == "_" {
		return &ast.WildcardPattern{Token: p.curToken}
	}

	ident := &ast.Ident{Token: p.curToken, Name: p.curToken.Literal}

	// Check for path: Foo::Bar
	if p.peekTokenIs(token.ColonColon) {
		return p.parseEnumPattern(ident)
	}

	// Check for struct pattern: Foo { ... }
	if p.peekTokenIs(token.LeftBrace) {
		return p.parseStructPatternFromIdent(ident)
	}

	return &ast.IdentPattern{Name: ident}
}

func (p *Parser) parseEnumPattern(first *ast.Ident) ast.Pattern {
	// Build the path
	parts := []ast.Ident{*first}

	for p.peekTokenIs(token.ColonColon) {
		p.nextToken() // consume current
		p.nextToken() // consume '::'
		if !p.curTokenIs(token.Ident) && !p.curTokenIs(token.Some) && !p.curTokenIs(token.None) {
			p.error("expected identifier after '::'")
			break
		}
		parts = append(parts, ast.Ident{Token: p.curToken, Name: p.curToken.Literal})
	}

	path := &ast.PathExpr{Parts: parts}
	pattern := &ast.EnumPattern{Path: path}

	// Optional fields: { field, field: pattern }
	if p.peekTokenIs(token.LeftBrace) {
		p.nextToken() // consume path
		p.nextToken() // consume '{'

		for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
			fp := p.parseFieldPattern()
			pattern.Fields = append(pattern.Fields, fp)

			if p.peekTokenIs(token.Comma) {
				p.nextToken()
			}
			p.nextToken()
		}
	}

	return pattern
}

func (p *Parser) parseEnumPatternFromIdent(ident *ast.Ident) ast.Pattern {
	pattern := &ast.EnumPattern{Path: ident}

	if p.peekTokenIs(token.LeftBrace) {
		p.nextToken() // consume ident
		p.nextToken() // consume '{'

		for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
			fp := p.parseFieldPattern()
			pattern.Fields = append(pattern.Fields, fp)

			if p.peekTokenIs(token.Comma) {
				p.nextToken()
			}
			p.nextToken()
		}
	}

	return pattern
}

func (p *Parser) parseStructPatternFromIdent(ident *ast.Ident) ast.Pattern {
	pattern := &ast.StructPattern{Name: ident}
	p.nextToken() // consume ident
	p.nextToken() // consume '{'

	for !p.curTokenIs(token.RightBrace) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.DotDot) {
			pattern.Rest = true
			break
		}

		fp := p.parseFieldPattern()
		pattern.Fields = append(pattern.Fields, fp)

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return pattern
}

func (p *Parser) parseFieldPattern() ast.FieldPattern {
	fp := ast.FieldPattern{
		Name: &ast.Ident{Token: p.curToken, Name: p.curToken.Literal},
	}

	// Check for : pattern
	if p.peekTokenIs(token.Colon) {
		p.nextToken() // consume name
		p.nextToken() // consume ':'
		fp.Pattern = p.parsePattern()
	}

	return fp
}

func (p *Parser) parseTuplePattern() ast.Pattern {
	pattern := &ast.TuplePattern{Token: p.curToken}
	p.nextToken()

	for !p.curTokenIs(token.RightParen) && !p.curTokenIs(token.EOF) {
		pattern.Elements = append(pattern.Elements, p.parsePattern())
		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return pattern
}

func (p *Parser) parseSlicePattern() ast.Pattern {
	pattern := &ast.SlicePattern{Token: p.curToken}
	p.nextToken()

	for !p.curTokenIs(token.RightBracket) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.DotDot) {
			p.nextToken()
			if !p.curTokenIs(token.RightBracket) && !p.curTokenIs(token.Comma) {
				pattern.Rest = p.parsePattern()
			}
		} else {
			pattern.Elements = append(pattern.Elements, p.parsePattern())
		}

		if p.peekTokenIs(token.Comma) {
			p.nextToken()
		}
		p.nextToken()
	}

	return pattern
}

// Helper to convert expression to pattern (for select statements)
func exprToPattern(e ast.Expr) ast.Pattern {
	switch v := e.(type) {
	case *ast.Ident:
		return &ast.IdentPattern{Name: v}
	default:
		return &ast.IdentPattern{Name: &ast.Ident{Name: "_"}}
	}
}
