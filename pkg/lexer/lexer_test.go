package lexer

import (
	"ease/pkg/token"
	"testing"
)

func TestNextToken(t *testing.T) {
	input := `
import (
	"io"
	"./config"
	"github.com/user/pkg"
)

enum Option<T> {
	Some { value: T },
	None,
}

enum Result<T, E> {
	Ok { value: T },
	Err { error: E },
}

struct Config {
	Name: string,
	Port: int,
}

fn main() -> Result<(), Error> {
	let config = loadConfig("config.json")?
	let mut count = 0

	if config.Port > 0 {
		count += 1
	}

	match config.Name {
		"dev" => println("development"),
		"prod" => println("production"),
		_ => println("unknown"),
	}

	for i in 0..10 {
		count += i
	}

	// Channel operations
	let ch = chan<int>()
	go || { ch <- 42 }
	let value = <-ch

	return error.New("something went wrong")
}
`

	tests := []struct {
		expectedType    token.Type
		expectedLiteral string
	}{
		{token.Import, "import"},
		{token.LeftParen, "("},
		{token.String, "io"},
		{token.String, "./config"},
		{token.String, "github.com/user/pkg"},
		{token.RightParen, ")"},

		// enum Option
		{token.Enum, "enum"},
		{token.Ident, "Option"},
		{token.Less, "<"},
		{token.Ident, "T"},
		{token.Greater, ">"},
		{token.LeftBrace, "{"},
		{token.Some, "Some"},
		{token.LeftBrace, "{"},
		{token.Ident, "value"},
		{token.Colon, ":"},
		{token.Ident, "T"},
		{token.RightBrace, "}"},
		{token.Comma, ","},
		{token.None, "None"},
		{token.Comma, ","},
		{token.RightBrace, "}"},

		// enum Result
		{token.Enum, "enum"},
		{token.Ident, "Result"},
		{token.Less, "<"},
		{token.Ident, "T"},
		{token.Comma, ","},
		{token.Ident, "E"},
		{token.Greater, ">"},
		{token.LeftBrace, "{"},
		{token.Ident, "Ok"},
		{token.LeftBrace, "{"},
		{token.Ident, "value"},
		{token.Colon, ":"},
		{token.Ident, "T"},
		{token.RightBrace, "}"},
		{token.Comma, ","},
		{token.Ident, "Err"},
		{token.LeftBrace, "{"},
		{token.Ident, "error"},
		{token.Colon, ":"},
		{token.Ident, "E"},
		{token.RightBrace, "}"},
		{token.Comma, ","},
		{token.RightBrace, "}"},

		// struct Config
		{token.Struct, "struct"},
		{token.Ident, "Config"},
		{token.LeftBrace, "{"},
		{token.Ident, "Name"},
		{token.Colon, ":"},
		{token.Ident, "string"},
		{token.Comma, ","},
		{token.Ident, "Port"},
		{token.Colon, ":"},
		{token.Ident, "int"},
		{token.Comma, ","},
		{token.RightBrace, "}"},

		// fn main
		{token.Fn, "fn"},
		{token.Ident, "main"},
		{token.LeftParen, "("},
		{token.RightParen, ")"},
		{token.Arrow, "->"},
		{token.Ident, "Result"},
		{token.Less, "<"},
		{token.LeftParen, "("},
		{token.RightParen, ")"},
		{token.Comma, ","},
		{token.Ident, "Error"},
		{token.Greater, ">"},
		{token.LeftBrace, "{"},

		// let config = loadConfig("config.json")?
		{token.Let, "let"},
		{token.Ident, "config"},
		{token.Assign, "="},
		{token.Ident, "loadConfig"},
		{token.LeftParen, "("},
		{token.String, "config.json"},
		{token.RightParen, ")"},
		{token.Question, "?"},

		// let mut count = 0
		{token.Let, "let"},
		{token.Mut, "mut"},
		{token.Ident, "count"},
		{token.Assign, "="},
		{token.Int, "0"},

		// if config.Port > 0 { count += 1 }
		{token.If, "if"},
		{token.Ident, "config"},
		{token.Dot, "."},
		{token.Ident, "Port"},
		{token.Greater, ">"},
		{token.Int, "0"},
		{token.LeftBrace, "{"},
		{token.Ident, "count"},
		{token.PlusAssign, "+="},
		{token.Int, "1"},
		{token.RightBrace, "}"},

		// match config.Name { ... }
		{token.Match, "match"},
		{token.Ident, "config"},
		{token.Dot, "."},
		{token.Ident, "Name"},
		{token.LeftBrace, "{"},
		{token.String, "dev"},
		{token.FatArrow, "=>"},
		{token.Ident, "println"},
		{token.LeftParen, "("},
		{token.String, "development"},
		{token.RightParen, ")"},
		{token.Comma, ","},
		{token.String, "prod"},
		{token.FatArrow, "=>"},
		{token.Ident, "println"},
		{token.LeftParen, "("},
		{token.String, "production"},
		{token.RightParen, ")"},
		{token.Comma, ","},
		{token.Ident, "_"},
		{token.FatArrow, "=>"},
		{token.Ident, "println"},
		{token.LeftParen, "("},
		{token.String, "unknown"},
		{token.RightParen, ")"},
		{token.Comma, ","},
		{token.RightBrace, "}"},

		// for i in 0..10 { count += i }
		{token.For, "for"},
		{token.Ident, "i"},
		{token.In, "in"},
		{token.Int, "0"},
		{token.DotDot, ".."},
		{token.Int, "10"},
		{token.LeftBrace, "{"},
		{token.Ident, "count"},
		{token.PlusAssign, "+="},
		{token.Ident, "i"},
		{token.RightBrace, "}"},

		// // Channel operations (comment)
		{token.Comment, " Channel operations"},

		// let ch = chan<int>()
		{token.Let, "let"},
		{token.Ident, "ch"},
		{token.Assign, "="},
		{token.Chan, "chan"},
		{token.Less, "<"},
		{token.Ident, "int"},
		{token.Greater, ">"},
		{token.LeftParen, "("},
		{token.RightParen, ")"},

		// go || { ch <- 42 }
		// Note: || is lexed as LogicalOr, parser handles closure context
		{token.Go, "go"},
		{token.LogicalOr, "||"},
		{token.LeftBrace, "{"},
		{token.Ident, "ch"},
		{token.ChanArrow, "<-"},
		{token.Int, "42"},
		{token.RightBrace, "}"},

		// let value = <-ch
		{token.Let, "let"},
		{token.Ident, "value"},
		{token.Assign, "="},
		{token.ChanArrow, "<-"},
		{token.Ident, "ch"},

		// return error.New("something went wrong")
		{token.Return, "return"},
		{token.Ident, "error"},
		{token.Dot, "."},
		{token.Ident, "New"},
		{token.LeftParen, "("},
		{token.String, "something went wrong"},
		{token.RightParen, ")"},

		{token.RightBrace, "}"},
		{token.EOF, ""},
	}

	l := New(input, "test.lang")

	for i, tt := range tests {
		tok := l.NextToken()

		if tok.Type != tt.expectedType {
			t.Fatalf("tests[%d] - tokentype wrong. expected=%q (%d), got=%q (%d)",
				i, tt.expectedType, tt.expectedType, tok.Type, tok.Type)
		}

		if tok.Literal != tt.expectedLiteral {
			t.Fatalf("tests[%d] - literal wrong. expected=%q, got=%q",
				i, tt.expectedLiteral, tok.Literal)
		}
	}
}

func TestNumbers(t *testing.T) {
	tests := []struct {
		input    string
		expected []struct {
			typ     token.Type
			literal string
		}
	}{
		{
			"123 456",
			[]struct {
				typ     token.Type
				literal string
			}{
				{token.Int, "123"},
				{token.Int, "456"},
			},
		},
		{
			"0x1F 0o77 0b1010",
			[]struct {
				typ     token.Type
				literal string
			}{
				{token.Int, "0x1F"},
				{token.Int, "0o77"},
				{token.Int, "0b1010"},
			},
		},
		{
			"3.14 1e10 2.5e-3",
			[]struct {
				typ     token.Type
				literal string
			}{
				{token.Float, "3.14"},
				{token.Float, "1e10"},
				{token.Float, "2.5e-3"},
			},
		},
		{
			"1_000_000 0xFF_FF",
			[]struct {
				typ     token.Type
				literal string
			}{
				{token.Int, "1_000_000"},
				{token.Int, "0xFF_FF"},
			},
		},
	}

	for _, tt := range tests {
		l := New(tt.input, "test")
		for i, exp := range tt.expected {
			tok := l.NextToken()
			if tok.Type != exp.typ {
				t.Errorf("input %q, token %d: expected type %v, got %v",
					tt.input, i, exp.typ, tok.Type)
			}
			if tok.Literal != exp.literal {
				t.Errorf("input %q, token %d: expected literal %q, got %q",
					tt.input, i, exp.literal, tok.Literal)
			}
		}
	}
}

func TestStrings(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`"hello"`, "hello"},
		{`"hello\nworld"`, "hello\nworld"},
		{`"tab\there"`, "tab\there"},
		{`"quote\"here"`, "quote\"here"},
		{`"\x41\x42"`, "AB"},
	}

	for _, tt := range tests {
		l := New(tt.input, "test")
		tok := l.NextToken()
		if tok.Type != token.String {
			t.Errorf("input %q: expected String, got %v", tt.input, tok.Type)
		}
		if tok.Literal != tt.expected {
			t.Errorf("input %q: expected %q, got %q", tt.input, tt.expected, tok.Literal)
		}
	}
}

func TestRawStrings(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`r"hello\nworld"`, `hello\nworld`},
		{`r#"hello "world""#`, `hello "world"`},
	}

	for _, tt := range tests {
		l := New(tt.input, "test")
		tok := l.NextToken()
		if tok.Type != token.String {
			t.Errorf("input %q: expected String, got %v", tt.input, tok.Type)
		}
		if tok.Literal != tt.expected {
			t.Errorf("input %q: expected %q, got %q", tt.input, tt.expected, tok.Literal)
		}
	}
}

func TestEnumSyntax(t *testing.T) {
	input := `enum Option<T> { Some { value: T }, None }
let x = Option::Some { value: 42 }
match x {
    Option::Some { value } => value,
    Option::None => 0,
}`

	expected := []struct {
		typ     token.Type
		literal string
	}{
		{token.Enum, "enum"},
		{token.Ident, "Option"},
		{token.Less, "<"},
		{token.Ident, "T"},
		{token.Greater, ">"},
		{token.LeftBrace, "{"},
		{token.Some, "Some"},
		{token.LeftBrace, "{"},
		{token.Ident, "value"},
		{token.Colon, ":"},
		{token.Ident, "T"},
		{token.RightBrace, "}"},
		{token.Comma, ","},
		{token.None, "None"},
		{token.RightBrace, "}"},

		{token.Let, "let"},
		{token.Ident, "x"},
		{token.Assign, "="},
		{token.Ident, "Option"},
		{token.ColonColon, "::"},
		{token.Some, "Some"},
		{token.LeftBrace, "{"},
		{token.Ident, "value"},
		{token.Colon, ":"},
		{token.Int, "42"},
		{token.RightBrace, "}"},

		{token.Match, "match"},
		{token.Ident, "x"},
		{token.LeftBrace, "{"},
		{token.Ident, "Option"},
		{token.ColonColon, "::"},
		{token.Some, "Some"},
		{token.LeftBrace, "{"},
		{token.Ident, "value"},
		{token.RightBrace, "}"},
		{token.FatArrow, "=>"},
		{token.Ident, "value"},
		{token.Comma, ","},
		{token.Ident, "Option"},
		{token.ColonColon, "::"},
		{token.None, "None"},
		{token.FatArrow, "=>"},
		{token.Int, "0"},
		{token.Comma, ","},
		{token.RightBrace, "}"},
		{token.EOF, ""},
	}

	l := New(input, "test")
	for i, exp := range expected {
		tok := l.NextToken()
		if tok.Type != exp.typ {
			t.Fatalf("token %d: expected type %v (%s), got %v (%s)",
				i, exp.typ, exp.typ, tok.Type, tok.Type)
		}
		if tok.Literal != exp.literal {
			t.Fatalf("token %d: expected literal %q, got %q",
				i, exp.literal, tok.Literal)
		}
	}
}

func TestPositions(t *testing.T) {
	input := "fn main() {\n    let x = 1\n}"

	l := New(input, "test.lang")

	// fn - line 1, col 1
	tok := l.NextToken()
	if tok.Pos.Line != 1 || tok.Pos.Column != 1 {
		t.Errorf("fn position: expected 1:1, got %d:%d", tok.Pos.Line, tok.Pos.Column)
	}

	// main - line 1, col 4
	tok = l.NextToken()
	if tok.Pos.Line != 1 || tok.Pos.Column != 4 {
		t.Errorf("main position: expected 1:4, got %d:%d", tok.Pos.Line, tok.Pos.Column)
	}

	// Skip to let on line 2
	l.NextToken() // (
	l.NextToken() // )
	l.NextToken() // {
	tok = l.NextToken() // let
	if tok.Pos.Line != 2 {
		t.Errorf("let position: expected line 2, got line %d", tok.Pos.Line)
	}
}
