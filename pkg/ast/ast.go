package ast

import "ease/pkg/token"

// Node is the interface all AST nodes implement
type Node interface {
	Pos() token.Position
	node()
}

// Expression nodes
type Expr interface {
	Node
	expr()
}

// Statement nodes
type Stmt interface {
	Node
	stmt()
}

// Declaration nodes
type Decl interface {
	Node
	decl()
}

// Type nodes
type Type interface {
	Node
	typeNode()
}

// Pattern nodes (for pattern matching)
type Pattern interface {
	Node
	pattern()
}

// ============================================
// Program
// ============================================

type Program struct {
	Imports []ImportDecl
	Decls   []Decl
}

func (p *Program) Pos() token.Position {
	if len(p.Imports) > 0 {
		return p.Imports[0].Pos()
	}
	if len(p.Decls) > 0 {
		return p.Decls[0].Pos()
	}
	return token.Position{}
}
func (p *Program) node() {}

// ============================================
// Attributes
// ============================================

// Attribute represents #[name] or #[name(args)]
type Attribute struct {
	Token token.Token // '#['
	Name  *Ident
	Args  []Expr // optional arguments
}

func (a *Attribute) Pos() token.Position { return a.Token.Pos }
func (a *Attribute) node()               {}

// ============================================
// Imports
// ============================================

type ImportDecl struct {
	Token   token.Token // 'import'
	Imports []ImportSpec
}

type ImportSpec struct {
	Path  string // "io", "./config", "github.com/user/pkg"
	Alias string // optional alias (from "as" keyword)
	Token token.Token
}

func (i *ImportDecl) Pos() token.Position { return i.Token.Pos }
func (i *ImportDecl) node()               {}

// ============================================
// Declarations
// ============================================

type FnDecl struct {
	Token      token.Token // 'fn'
	Name       *Ident
	TypeParams []TypeParam // generic parameters
	Params     []Param
	ReturnType Type // nil if no return type
	Body       *BlockStmt
}

type TypeParam struct {
	Name   *Ident
	Bounds []Type // trait bounds
}

type Param struct {
	Name *Ident
	Type Type
}

func (f *FnDecl) Pos() token.Position { return f.Token.Pos }
func (f *FnDecl) node()               {}
func (f *FnDecl) decl()               {}

type StructDecl struct {
	Token      token.Token
	Name       *Ident
	TypeParams []TypeParam
	Fields     []Field
}

type Field struct {
	Name *Ident
	Type Type
}

func (s *StructDecl) Pos() token.Position { return s.Token.Pos }
func (s *StructDecl) node()               {}
func (s *StructDecl) decl()               {}

type EnumDecl struct {
	Token      token.Token
	Name       *Ident
	TypeParams []TypeParam
	Variants   []Variant
}

type Variant struct {
	Name   *Ident
	Fields []Field // empty for unit variants
}

func (e *EnumDecl) Pos() token.Position { return e.Token.Pos }
func (e *EnumDecl) node()               {}
func (e *EnumDecl) decl()               {}

type TraitDecl struct {
	Token      token.Token
	Name       *Ident
	TypeParams []TypeParam
	Bounds     []Type // super traits
	Methods    []TraitMethod
	Types      []TraitType
}

type TraitMethod struct {
	Name       *Ident
	TypeParams []TypeParam
	Params     []Param
	ReturnType Type
	Body       *BlockStmt // nil for trait method without default impl
}

type TraitType struct {
	Name   *Ident
	Bounds []Type
}

func (t *TraitDecl) Pos() token.Position { return t.Token.Pos }
func (t *TraitDecl) node()               {}
func (t *TraitDecl) decl()               {}

type ImplDecl struct {
	Token      token.Token
	TypeParams []TypeParam
	Trait      Type // nil for inherent impl
	Type       Type
	Methods    []FnDecl
}

func (i *ImplDecl) Pos() token.Position { return i.Token.Pos }
func (i *ImplDecl) node()               {}
func (i *ImplDecl) decl()               {}

type TypeAlias struct {
	Token      token.Token
	Name       *Ident
	TypeParams []TypeParam
	Type       Type
}

func (t *TypeAlias) Pos() token.Position { return t.Token.Pos }
func (t *TypeAlias) node()               {}
func (t *TypeAlias) decl()               {}

type ConstDecl struct {
	Token token.Token
	Name  *Ident
	Type  Type // optional
	Value Expr
}

func (c *ConstDecl) Pos() token.Position { return c.Token.Pos }
func (c *ConstDecl) node()               {}
func (c *ConstDecl) decl()               {}

// TestDecl represents a test declaration: test "description" { }
type TestDecl struct {
	Token       token.Token  // 'test'
	Attributes  []Attribute  // #[slow], #[parallel], etc.
	Description *StringLit   // test description
	Body        *BlockStmt
}

func (t *TestDecl) Pos() token.Position { return t.Token.Pos }
func (t *TestDecl) node()               {}
func (t *TestDecl) decl()               {}

// ============================================
// Statements
// ============================================

type LetStmt struct {
	Token   token.Token
	Mutable bool
	Pattern Pattern
	Type    Type // optional
	Value   Expr // optional (but usually present)
}

func (l *LetStmt) Pos() token.Position { return l.Token.Pos }
func (l *LetStmt) node()               {}
func (l *LetStmt) stmt()               {}

type ExprStmt struct {
	Expr Expr
}

func (e *ExprStmt) Pos() token.Position { return e.Expr.Pos() }
func (e *ExprStmt) node()               {}
func (e *ExprStmt) stmt()               {}

type ReturnStmt struct {
	Token token.Token
	Value Expr // nil for bare return
}

func (r *ReturnStmt) Pos() token.Position { return r.Token.Pos }
func (r *ReturnStmt) node()               {}
func (r *ReturnStmt) stmt()               {}

type BlockStmt struct {
	Token token.Token // '{'
	Stmts []Stmt
	Expr  Expr // optional trailing expression (for expression blocks)
}

func (b *BlockStmt) Pos() token.Position { return b.Token.Pos }
func (b *BlockStmt) node()               {}
func (b *BlockStmt) stmt()               {}

type IfStmt struct {
	Token token.Token
	Cond  Expr
	Then  *BlockStmt
	Else  Stmt // *BlockStmt or *IfStmt, or nil
}

func (i *IfStmt) Pos() token.Position { return i.Token.Pos }
func (i *IfStmt) node()               {}
func (i *IfStmt) stmt()               {}

type MatchStmt struct {
	Token token.Token
	Expr  Expr
	Arms  []MatchArm
}

type MatchArm struct {
	Pattern Pattern
	Guard   Expr // optional "if" guard
	Body    Expr // can be expression or block
}

func (m *MatchStmt) Pos() token.Position { return m.Token.Pos }
func (m *MatchStmt) node()               {}
func (m *MatchStmt) stmt()               {}

// ForStmt handles all loop forms (Go-style):
// - for { }              infinite loop (Cond, Pattern, Iter all nil)
// - for cond { }         condition loop (Cond set, Pattern/Iter nil)
// - for x in iter { }    range loop (Pattern and Iter set, Cond nil)
type ForStmt struct {
	Token   token.Token
	Cond    Expr    // condition for "for cond { }" form
	Pattern Pattern // binding for "for x in iter { }" form
	Iter    Expr    // iterator for "for x in iter { }" form
	Body    *BlockStmt
}

func (f *ForStmt) Pos() token.Position { return f.Token.Pos }
func (f *ForStmt) node()               {}
func (f *ForStmt) stmt()               {}

type BreakStmt struct {
	Token token.Token
	Value Expr // optional
}

func (b *BreakStmt) Pos() token.Position { return b.Token.Pos }
func (b *BreakStmt) node()               {}
func (b *BreakStmt) stmt()               {}

type ContinueStmt struct {
	Token token.Token
}

func (c *ContinueStmt) Pos() token.Position { return c.Token.Pos }
func (c *ContinueStmt) node()               {}
func (c *ContinueStmt) stmt()               {}

type GoStmt struct {
	Token token.Token
	Expr  Expr
}

func (g *GoStmt) Pos() token.Position { return g.Token.Pos }
func (g *GoStmt) node()               {}
func (g *GoStmt) stmt()               {}

type SelectStmt struct {
	Token token.Token
	Arms  []SelectArm
}

type SelectArm struct {
	Pattern Pattern // for receive
	Chan    Expr    // channel expression
	Value   Expr    // for send
	IsSend  bool
	IsDefault bool
	Body    Expr
}

func (s *SelectStmt) Pos() token.Position { return s.Token.Pos }
func (s *SelectStmt) node()               {}
func (s *SelectStmt) stmt()               {}

// ============================================
// Expressions
// ============================================

type Ident struct {
	Token token.Token
	Name  string
}

func (i *Ident) Pos() token.Position { return i.Token.Pos }
func (i *Ident) node()               {}
func (i *Ident) expr()               {}

type IntLit struct {
	Token token.Token
	Value int64
	Raw   string // original text (for hex, binary, etc)
}

func (i *IntLit) Pos() token.Position { return i.Token.Pos }
func (i *IntLit) node()               {}
func (i *IntLit) expr()               {}

type FloatLit struct {
	Token token.Token
	Value float64
	Raw   string
}

func (f *FloatLit) Pos() token.Position { return f.Token.Pos }
func (f *FloatLit) node()               {}
func (f *FloatLit) expr()               {}

type StringLit struct {
	Token token.Token
	Value string
}

func (s *StringLit) Pos() token.Position { return s.Token.Pos }
func (s *StringLit) node()               {}
func (s *StringLit) expr()               {}

type CharLit struct {
	Token token.Token
	Value rune
}

func (c *CharLit) Pos() token.Position { return c.Token.Pos }
func (c *CharLit) node()               {}
func (c *CharLit) expr()               {}

type BoolLit struct {
	Token token.Token
	Value bool
}

func (b *BoolLit) Pos() token.Position { return b.Token.Pos }
func (b *BoolLit) node()               {}
func (b *BoolLit) expr()               {}

type BinaryExpr struct {
	Left  Expr
	Op    token.Token
	Right Expr
}

func (b *BinaryExpr) Pos() token.Position { return b.Left.Pos() }
func (b *BinaryExpr) node()               {}
func (b *BinaryExpr) expr()               {}

type UnaryExpr struct {
	Op    token.Token
	Right Expr
}

func (u *UnaryExpr) Pos() token.Position { return u.Op.Pos }
func (u *UnaryExpr) node()               {}
func (u *UnaryExpr) expr()               {}

type CallExpr struct {
	Func     Expr
	TypeArgs []Type // generic type arguments
	Args     []Expr
}

func (c *CallExpr) Pos() token.Position { return c.Func.Pos() }
func (c *CallExpr) node()               {}
func (c *CallExpr) expr()               {}

type IndexExpr struct {
	Expr  Expr
	Index Expr
}

func (i *IndexExpr) Pos() token.Position { return i.Expr.Pos() }
func (i *IndexExpr) node()               {}
func (i *IndexExpr) expr()               {}

// SliceExpr represents a slice expression: expr[start:end]
// Start or End may be nil for open-ended slices
type SliceExpr struct {
	Expr  Expr
	Start Expr // may be nil
	End   Expr // may be nil
}

func (s *SliceExpr) Pos() token.Position { return s.Expr.Pos() }
func (s *SliceExpr) node()               {}
func (s *SliceExpr) expr()               {}

type FieldExpr struct {
	Expr  Expr
	Field *Ident
}

func (f *FieldExpr) Pos() token.Position { return f.Expr.Pos() }
func (f *FieldExpr) node()               {}
func (f *FieldExpr) expr()               {}

type MethodExpr struct {
	Expr     Expr
	Method   *Ident
	TypeArgs []Type
	Args     []Expr
}

func (m *MethodExpr) Pos() token.Position { return m.Expr.Pos() }
func (m *MethodExpr) node()               {}
func (m *MethodExpr) expr()               {}

type PathExpr struct {
	Parts []Ident // e.g., Option::Some
}

func (p *PathExpr) Pos() token.Position {
	if len(p.Parts) > 0 {
		return p.Parts[0].Pos()
	}
	return token.Position{}
}
func (p *PathExpr) node() {}
func (p *PathExpr) expr() {}

type TryExpr struct {
	Expr Expr
}

func (t *TryExpr) Pos() token.Position { return t.Expr.Pos() }
func (t *TryExpr) node()               {}
func (t *TryExpr) expr()               {}

type IfExpr struct {
	Token token.Token
	Cond  Expr
	Then  *BlockStmt
	Else  Expr // *BlockStmt or *IfExpr
}

func (i *IfExpr) Pos() token.Position { return i.Token.Pos }
func (i *IfExpr) node()               {}
func (i *IfExpr) expr()               {}

type MatchExpr struct {
	Token token.Token
	Expr  Expr
	Arms  []MatchArm
}

func (m *MatchExpr) Pos() token.Position { return m.Token.Pos }
func (m *MatchExpr) node()               {}
func (m *MatchExpr) expr()               {}

type BlockExpr struct {
	Block *BlockStmt
}

func (b *BlockExpr) Pos() token.Position { return b.Block.Pos() }
func (b *BlockExpr) node()               {}
func (b *BlockExpr) expr()               {}

type ClosureExpr struct {
	Token  token.Token // '|' or '||'
	Move   bool
	Params []Param
	ReturnType Type
	Body   Expr // expression or block
}

func (c *ClosureExpr) Pos() token.Position { return c.Token.Pos }
func (c *ClosureExpr) node()               {}
func (c *ClosureExpr) expr()               {}

type StructExpr struct {
	Name   Expr // type name (can be path like Foo::Bar)
	Fields []FieldInit
}

type FieldInit struct {
	Name  *Ident
	Value Expr // nil for shorthand (name = name: name)
}

func (s *StructExpr) Pos() token.Position { return s.Name.Pos() }
func (s *StructExpr) node()               {}
func (s *StructExpr) expr()               {}

type ArrayExpr struct {
	Token    token.Token
	ElemType Type   // element type for Go-style: []int{...} or [3]int{...}
	Elements []Expr
	// Or repeat syntax: [expr; count]
	Repeat Expr
	Count  Expr
}

func (a *ArrayExpr) Pos() token.Position { return a.Token.Pos }
func (a *ArrayExpr) node()               {}
func (a *ArrayExpr) expr()               {}

type MapExpr struct {
	Token   token.Token // 'map'
	MapType *MapType
	Entries []MapEntry
}

type MapEntry struct {
	Key   Expr
	Value Expr
}

func (m *MapExpr) Pos() token.Position { return m.Token.Pos }
func (m *MapExpr) node()               {}
func (m *MapExpr) expr()               {}

type TupleExpr struct {
	Token    token.Token
	Elements []Expr
}

func (t *TupleExpr) Pos() token.Position { return t.Token.Pos }
func (t *TupleExpr) node()               {}
func (t *TupleExpr) expr()               {}

type RangeExpr struct {
	Start     Expr // nil for ..end
	End       Expr // nil for start..
	Inclusive bool // ..= vs ..
}

func (r *RangeExpr) Pos() token.Position {
	if r.Start != nil {
		return r.Start.Pos()
	}
	return r.End.Pos()
}
func (r *RangeExpr) node() {}
func (r *RangeExpr) expr() {}

type ChanSendExpr struct {
	Chan  Expr
	Value Expr
}

func (c *ChanSendExpr) Pos() token.Position { return c.Chan.Pos() }
func (c *ChanSendExpr) node()               {}
func (c *ChanSendExpr) expr()               {}

type ChanRecvExpr struct {
	Token token.Token // '<-'
	Chan  Expr
}

func (c *ChanRecvExpr) Pos() token.Position { return c.Token.Pos }
func (c *ChanRecvExpr) node()               {}
func (c *ChanRecvExpr) expr()               {}

type ChanMakeExpr struct {
	Token   token.Token // 'chan'
	Element Type        // element type
	Size    Expr        // optional buffer size
}

func (c *ChanMakeExpr) Pos() token.Position { return c.Token.Pos }
func (c *ChanMakeExpr) node()               {}
func (c *ChanMakeExpr) expr()               {}

// ============================================
// Types
// ============================================

type NamedType struct {
	Name     *Ident
	TypeArgs []Type
}

func (n *NamedType) Pos() token.Position  { return n.Name.Pos() }
func (n *NamedType) node()                {}
func (n *NamedType) typeNode()            {}

type PathType struct {
	Parts    []Ident
	TypeArgs []Type
}

func (p *PathType) Pos() token.Position {
	if len(p.Parts) > 0 {
		return p.Parts[0].Pos()
	}
	return token.Position{}
}
func (p *PathType) node()     {}
func (p *PathType) typeNode() {}

type TupleType struct {
	Token    token.Token
	Elements []Type
}

func (t *TupleType) Pos() token.Position { return t.Token.Pos }
func (t *TupleType) node()               {}
func (t *TupleType) typeNode()           {}

type ArrayType struct {
	Token   token.Token
	Element Type
	Size    Expr // compile-time constant
}

func (a *ArrayType) Pos() token.Position { return a.Token.Pos }
func (a *ArrayType) node()               {}
func (a *ArrayType) typeNode()           {}

type SliceType struct {
	Token   token.Token
	Element Type
}

func (s *SliceType) Pos() token.Position { return s.Token.Pos }
func (s *SliceType) node()               {}
func (s *SliceType) typeNode()           {}

type FnType struct {
	Token      token.Token
	Params     []Type
	ReturnType Type
}

func (f *FnType) Pos() token.Position { return f.Token.Pos }
func (f *FnType) node()               {}
func (f *FnType) typeNode()           {}

type RefType struct {
	Token   token.Token
	Mutable bool
	Type    Type
}

func (r *RefType) Pos() token.Position { return r.Token.Pos }
func (r *RefType) node()               {}
func (r *RefType) typeNode()           {}

type ChanType struct {
	Token   token.Token
	Element Type
}

func (c *ChanType) Pos() token.Position { return c.Token.Pos }
func (c *ChanType) node()               {}
func (c *ChanType) typeNode()           {}

type UnitType struct {
	Token token.Token // '('
}

func (u *UnitType) Pos() token.Position { return u.Token.Pos }
func (u *UnitType) node()               {}
func (u *UnitType) typeNode()           {}

type MapType struct {
	Token token.Token // 'map'
	Key   Type
	Value Type
}

func (m *MapType) Pos() token.Position { return m.Token.Pos }
func (m *MapType) node()               {}
func (m *MapType) typeNode()           {}

// ============================================
// Patterns
// ============================================

type LiteralPattern struct {
	Value Expr // IntLit, FloatLit, StringLit, BoolLit
}

func (l *LiteralPattern) Pos() token.Position { return l.Value.Pos() }
func (l *LiteralPattern) node()               {}
func (l *LiteralPattern) pattern()            {}

type IdentPattern struct {
	Mutable bool
	Name    *Ident
}

func (i *IdentPattern) Pos() token.Position { return i.Name.Pos() }
func (i *IdentPattern) node()               {}
func (i *IdentPattern) pattern()            {}

type WildcardPattern struct {
	Token token.Token // '_'
}

func (w *WildcardPattern) Pos() token.Position { return w.Token.Pos }
func (w *WildcardPattern) node()               {}
func (w *WildcardPattern) pattern()            {}

type TuplePattern struct {
	Token    token.Token
	Elements []Pattern
}

func (t *TuplePattern) Pos() token.Position { return t.Token.Pos }
func (t *TuplePattern) node()               {}
func (t *TuplePattern) pattern()            {}

type StructPattern struct {
	Name   Expr // type name or path
	Fields []FieldPattern
	Rest   bool // has ".." at end
}

type FieldPattern struct {
	Name    *Ident
	Pattern Pattern // nil for shorthand
}

func (s *StructPattern) Pos() token.Position { return s.Name.Pos() }
func (s *StructPattern) node()               {}
func (s *StructPattern) pattern()            {}

type EnumPattern struct {
	Path    Expr // Option::Some, etc.
	Fields  []FieldPattern
}

func (e *EnumPattern) Pos() token.Position { return e.Path.Pos() }
func (e *EnumPattern) node()               {}
func (e *EnumPattern) pattern()            {}

type SlicePattern struct {
	Token    token.Token
	Elements []Pattern
	Rest     Pattern // the ".." element, if any
}

func (s *SlicePattern) Pos() token.Position { return s.Token.Pos }
func (s *SlicePattern) node()               {}
func (s *SlicePattern) pattern()            {}

type RangePattern struct {
	Start     Pattern
	End       Pattern
	Inclusive bool
}

func (r *RangePattern) Pos() token.Position { return r.Start.Pos() }
func (r *RangePattern) node()               {}
func (r *RangePattern) pattern()            {}

type OrPattern struct {
	Left  Pattern
	Right Pattern
}

func (o *OrPattern) Pos() token.Position { return o.Left.Pos() }
func (o *OrPattern) node()               {}
func (o *OrPattern) pattern()            {}
