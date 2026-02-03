package token

type Type int

const (
	// Special tokens
	Illegal Type = iota
	EOF
	Comment

	// Identifiers and literals
	Ident   // main, foo, Config
	Int     // 123, 0x1F, 0b1010
	Float   // 3.14, 1e10
	String  // "hello"
	Char    // 'a'

	// Operators
	Plus     // +
	Minus    // -
	Star     // *
	Slash    // /
	Percent  // %

	Ampersand  // &
	Pipe       // |
	Caret      // ^
	Tilde      // ~
	ShiftLeft  // <<
	ShiftRight // >>

	LogicalAnd // &&
	LogicalOr  // ||
	Not        // !

	Equal        // ==
	NotEqual     // !=
	Less         // <
	LessEqual    // <=
	Greater      // >
	GreaterEqual // >=

	Assign       // =
	PlusAssign   // +=
	MinusAssign  // -=
	StarAssign   // *=
	SlashAssign  // /=
	PercentAssign // %=

	Arrow     // ->
	FatArrow  // =>
	Question  // ?
	ChanArrow // <-

	Dot       // .
	DotDot    // ..
	DotDotEq  // ..=

	// Delimiters
	LeftParen    // (
	RightParen   // )
	LeftBrace    // {
	RightBrace   // }
	LeftBracket  // [
	RightBracket // ]

	Comma      // ,
	Colon      // :
	ColonColon // ::
	Semicolon  // ;

	// Keywords
	keywordStart
	Fn
	Let
	Mut
	Const
	Struct
	Trait
	Enum
	Impl
	TypeKw // 'type' keyword
	Import
	As
	If
	Else
	Match
	For
	In
	Break
	Continue
	Return
	Go
	Chan
	Select
	Default
	Map
	True
	False
	None
	Some
	Move
	Pub  // reserved but not used (Go-style visibility)
	Test // test keyword for testing
	keywordEnd
)

const (
	// Attributes
	Hash       Type = iota + 200 // #
	HashBracket                   // #[
)

var tokenNames = map[Type]string{
	Illegal: "Illegal",
	EOF:     "EOF",
	Comment: "Comment",

	Ident:  "Ident",
	Int:    "Int",
	Float:  "Float",
	String: "String",
	Char:   "Char",

	Plus:    "+",
	Minus:   "-",
	Star:    "*",
	Slash:   "/",
	Percent: "%",

	Ampersand:  "&",
	Pipe:       "|",
	Caret:      "^",
	Tilde:      "~",
	ShiftLeft:  "<<",
	ShiftRight: ">>",

	LogicalAnd: "&&",
	LogicalOr:  "||",
	Not:        "!",

	Equal:        "==",
	NotEqual:     "!=",
	Less:         "<",
	LessEqual:    "<=",
	Greater:      ">",
	GreaterEqual: ">=",

	Assign:        "=",
	PlusAssign:    "+=",
	MinusAssign:   "-=",
	StarAssign:    "*=",
	SlashAssign:   "/=",
	PercentAssign: "%=",

	Arrow:     "->",
	FatArrow:  "=>",
	Question:  "?",
	ChanArrow: "<-",

	Dot:      ".",
	DotDot:   "..",
	DotDotEq: "..=",

	LeftParen:    "(",
	RightParen:   ")",
	LeftBrace:    "{",
	RightBrace:   "}",
	LeftBracket:  "[",
	RightBracket: "]",

	Comma:      ",",
	Colon:      ":",
	ColonColon: "::",
	Semicolon:  ";",

	Fn:       "fn",
	Let:      "let",
	Mut:      "mut",
	Const:    "const",
	Struct:   "struct",
	Trait:    "trait",
	Enum:     "enum",
	Impl:     "impl",
	TypeKw:   "type",
	Import:   "import",
	As:       "as",
	If:       "if",
	Else:     "else",
	Match:    "match",
	For:      "for",
	In:       "in",
	Break:    "break",
	Continue: "continue",
	Return:   "return",
	Go:       "go",
	Chan:     "chan",
	Select:   "select",
	Default:  "default",
	Map:      "map",
	True:     "true",
	False:    "false",
	None:     "None",
	Some:     "Some",
	Move:        "move",
	Pub:         "pub",
	Test:        "test",
	Hash:        "#",
	HashBracket: "#[",
}

func (t Type) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return "Unknown"
}

var keywords = map[string]Type{
	"fn":       Fn,
	"let":      Let,
	"mut":      Mut,
	"const":    Const,
	"struct":   Struct,
	"trait":    Trait,
	"enum":     Enum,
	"impl":     Impl,
	"type":     TypeKw,
	"import":   Import,
	"as":       As,
	"if":       If,
	"else":     Else,
	"match":    Match,
	"for":      For,
	"in":       In,
	"break":    Break,
	"continue": Continue,
	"return":   Return,
	"go":       Go,
	"chan":     Chan,
	"select":   Select,
	"default":  Default,
	"map":      Map,
	"true":     True,
	"false":    False,
	"None":     None,
	"Some":     Some,
	"move":     Move,
	"pub":      Pub,
	// Note: "test" is NOT a keyword - it's handled contextually in the parser
	// This allows "test" to be used as an identifier (e.g., function name)
}

func LookupIdent(ident string) Type {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return Ident
}

func IsKeyword(t Type) bool {
	return t > keywordStart && t < keywordEnd
}

type Position struct {
	Filename string
	Offset   int // byte offset
	Line     int // 1-based
	Column   int // 1-based, in bytes
}

func (p Position) String() string {
	if p.Filename != "" {
		return p.Filename + ":" + itoa(p.Line) + ":" + itoa(p.Column)
	}
	return itoa(p.Line) + ":" + itoa(p.Column)
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

type Token struct {
	Type    Type
	Literal string
	Pos     Position
}

func (t Token) String() string {
	if t.Literal != "" {
		return t.Type.String() + "(" + t.Literal + ")"
	}
	return t.Type.String()
}
