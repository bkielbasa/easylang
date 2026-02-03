package lexer

import (
	"ease/pkg/token"
)

type Lexer struct {
	input    string
	filename string

	pos      int  // current position in input (points to current char)
	readPos  int  // current reading position (after current char)
	ch       byte // current char under examination
	line     int  // current line number (1-based)
	column   int  // current column number (1-based)
	lineStart int // position where current line starts
}

func New(input string, filename string) *Lexer {
	l := &Lexer{
		input:    input,
		filename: filename,
		line:     1,
		column:   0, // will be 1 after first readChar
	}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPos]
	}
	l.pos = l.readPos
	l.readPos++
	l.column++

	if l.ch == '\n' {
		l.line++
		l.column = 0
		l.lineStart = l.readPos
	}
}

func (l *Lexer) peekChar() byte {
	if l.readPos >= len(l.input) {
		return 0
	}
	return l.input[l.readPos]
}

func (l *Lexer) peekCharN(n int) byte {
	pos := l.readPos + n - 1
	if pos >= len(l.input) {
		return 0
	}
	return l.input[pos]
}

func (l *Lexer) currentPos() token.Position {
	return token.Position{
		Filename: l.filename,
		Offset:   l.pos,
		Line:     l.line,
		Column:   l.column,
	}
}

func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		l.readChar()
	}
}

func (l *Lexer) NextToken() token.Token {
	l.skipWhitespace()

	pos := l.currentPos()

	// Handle comments
	if l.ch == '/' {
		if l.peekChar() == '/' {
			return l.readLineComment(pos)
		}
		if l.peekChar() == '*' {
			return l.readBlockComment(pos)
		}
	}

	var tok token.Token
	tok.Pos = pos

	switch l.ch {
	case 0:
		tok.Type = token.EOF
		tok.Literal = ""

	// Operators
	case '+':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.PlusAssign
			tok.Literal = "+="
		} else {
			tok.Type = token.Plus
			tok.Literal = "+"
		}
	case '-':
		if l.peekChar() == '>' {
			l.readChar()
			tok.Type = token.Arrow
			tok.Literal = "->"
		} else if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.MinusAssign
			tok.Literal = "-="
		} else {
			tok.Type = token.Minus
			tok.Literal = "-"
		}
	case '*':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.StarAssign
			tok.Literal = "*="
		} else {
			tok.Type = token.Star
			tok.Literal = "*"
		}
	case '/':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.SlashAssign
			tok.Literal = "/="
		} else {
			tok.Type = token.Slash
			tok.Literal = "/"
		}
	case '%':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.PercentAssign
			tok.Literal = "%="
		} else {
			tok.Type = token.Percent
			tok.Literal = "%"
		}

	case '&':
		if l.peekChar() == '&' {
			l.readChar()
			tok.Type = token.LogicalAnd
			tok.Literal = "&&"
		} else {
			tok.Type = token.Ampersand
			tok.Literal = "&"
		}
	case '|':
		if l.peekChar() == '|' {
			l.readChar()
			tok.Type = token.LogicalOr
			tok.Literal = "||"
		} else {
			tok.Type = token.Pipe
			tok.Literal = "|"
		}
	case '^':
		tok.Type = token.Caret
		tok.Literal = "^"
	case '~':
		tok.Type = token.Tilde
		tok.Literal = "~"

	case '#':
		if l.peekChar() == '[' {
			l.readChar()
			tok.Type = token.HashBracket
			tok.Literal = "#["
		} else {
			tok.Type = token.Hash
			tok.Literal = "#"
		}

	case '!':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.NotEqual
			tok.Literal = "!="
		} else {
			tok.Type = token.Not
			tok.Literal = "!"
		}

	case '=':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.Equal
			tok.Literal = "=="
		} else if l.peekChar() == '>' {
			l.readChar()
			tok.Type = token.FatArrow
			tok.Literal = "=>"
		} else {
			tok.Type = token.Assign
			tok.Literal = "="
		}

	case '<':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.LessEqual
			tok.Literal = "<="
		} else if l.peekChar() == '<' {
			l.readChar()
			tok.Type = token.ShiftLeft
			tok.Literal = "<<"
		} else if l.peekChar() == '-' {
			l.readChar()
			tok.Type = token.ChanArrow
			tok.Literal = "<-"
		} else {
			tok.Type = token.Less
			tok.Literal = "<"
		}

	case '>':
		if l.peekChar() == '=' {
			l.readChar()
			tok.Type = token.GreaterEqual
			tok.Literal = ">="
		} else if l.peekChar() == '>' {
			l.readChar()
			tok.Type = token.ShiftRight
			tok.Literal = ">>"
		} else {
			tok.Type = token.Greater
			tok.Literal = ">"
		}

	case '?':
		tok.Type = token.Question
		tok.Literal = "?"

	case '.':
		if l.peekChar() == '.' {
			l.readChar()
			if l.peekChar() == '=' {
				l.readChar()
				tok.Type = token.DotDotEq
				tok.Literal = "..="
			} else {
				tok.Type = token.DotDot
				tok.Literal = ".."
			}
		} else {
			tok.Type = token.Dot
			tok.Literal = "."
		}

	// Delimiters
	case '(':
		tok.Type = token.LeftParen
		tok.Literal = "("
	case ')':
		tok.Type = token.RightParen
		tok.Literal = ")"
	case '{':
		tok.Type = token.LeftBrace
		tok.Literal = "{"
	case '}':
		tok.Type = token.RightBrace
		tok.Literal = "}"
	case '[':
		tok.Type = token.LeftBracket
		tok.Literal = "["
	case ']':
		tok.Type = token.RightBracket
		tok.Literal = "]"
	case ',':
		tok.Type = token.Comma
		tok.Literal = ","
	case ':':
		if l.peekChar() == ':' {
			l.readChar()
			tok.Type = token.ColonColon
			tok.Literal = "::"
		} else {
			tok.Type = token.Colon
			tok.Literal = ":"
		}
	case ';':
		tok.Type = token.Semicolon
		tok.Literal = ";"

	// String literals
	case '"':
		tok.Type = token.String
		tok.Literal = l.readString()

	// Char literals
	case '\'':
		tok.Type = token.Char
		tok.Literal = l.readChar_()

	default:
		if isLetter(l.ch) {
			// Check for raw string: r"..." or r#"..."#
			if l.ch == 'r' && (l.peekChar() == '"' || l.peekChar() == '#') {
				tok.Type = token.String
				tok.Literal = l.readRawString()
				return tok
			}

			tok.Literal = l.readIdentifier()
			tok.Type = token.LookupIdent(tok.Literal)
			return tok
		} else if isDigit(l.ch) {
			return l.readNumber(pos)
		} else {
			tok.Type = token.Illegal
			tok.Literal = string(l.ch)
		}
	}

	l.readChar()
	return tok
}

func (l *Lexer) readIdentifier() string {
	start := l.pos
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readNumber(pos token.Position) token.Token {
	tok := token.Token{Pos: pos}
	start := l.pos

	// Check for hex, octal, binary
	if l.ch == '0' {
		switch l.peekChar() {
		case 'x', 'X':
			l.readChar() // consume '0'
			l.readChar() // consume 'x'
			for isHexDigit(l.ch) || l.ch == '_' {
				l.readChar()
			}
			tok.Type = token.Int
			tok.Literal = l.input[start:l.pos]
			return tok
		case 'o', 'O':
			l.readChar() // consume '0'
			l.readChar() // consume 'o'
			for isOctalDigit(l.ch) || l.ch == '_' {
				l.readChar()
			}
			tok.Type = token.Int
			tok.Literal = l.input[start:l.pos]
			return tok
		case 'b', 'B':
			l.readChar() // consume '0'
			l.readChar() // consume 'b'
			for l.ch == '0' || l.ch == '1' || l.ch == '_' {
				l.readChar()
			}
			tok.Type = token.Int
			tok.Literal = l.input[start:l.pos]
			return tok
		}
	}

	// Decimal integer or float
	for isDigit(l.ch) || l.ch == '_' {
		l.readChar()
	}

	isFloat := false

	// Check for decimal point (but not ..)
	if l.ch == '.' && l.peekChar() != '.' {
		isFloat = true
		l.readChar() // consume '.'
		for isDigit(l.ch) || l.ch == '_' {
			l.readChar()
		}
	}

	// Check for exponent
	if l.ch == 'e' || l.ch == 'E' {
		isFloat = true
		l.readChar() // consume 'e'
		if l.ch == '+' || l.ch == '-' {
			l.readChar()
		}
		for isDigit(l.ch) || l.ch == '_' {
			l.readChar()
		}
	}

	tok.Literal = l.input[start:l.pos]
	if isFloat {
		tok.Type = token.Float
	} else {
		tok.Type = token.Int
	}
	return tok
}

func (l *Lexer) readString() string {
	var result []byte
	l.readChar() // consume opening "

	for l.ch != '"' && l.ch != 0 {
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				result = append(result, '\n')
			case 'r':
				result = append(result, '\r')
			case 't':
				result = append(result, '\t')
			case '\\':
				result = append(result, '\\')
			case '"':
				result = append(result, '"')
			case '0':
				result = append(result, 0)
			case 'x':
				// \xNN - hex escape
				l.readChar()
				hi := hexVal(l.ch)
				l.readChar()
				lo := hexVal(l.ch)
				result = append(result, byte(hi<<4|lo))
			default:
				result = append(result, l.ch)
			}
		} else {
			result = append(result, l.ch)
		}
		l.readChar()
	}

	return string(result)
}

func (l *Lexer) readRawString() string {
	l.readChar() // consume 'r'

	hashCount := 0
	for l.ch == '#' {
		hashCount++
		l.readChar()
	}

	if l.ch != '"' {
		return "" // error: expected "
	}
	l.readChar() // consume opening "

	var result []byte
	for {
		if l.ch == 0 {
			break // error: unterminated string
		}
		if l.ch == '"' {
			// Check for matching closing hashes
			matches := true
			for i := 0; i < hashCount; i++ {
				if l.peekCharN(i+1) != '#' {
					matches = false
					break
				}
			}
			if matches {
				// Consume closing " and hashes
				l.readChar()
				for i := 0; i < hashCount; i++ {
					l.readChar()
				}
				break
			}
		}
		result = append(result, l.ch)
		l.readChar()
	}

	return string(result)
}

func (l *Lexer) readChar_() string {
	l.readChar() // consume opening '

	var ch byte
	if l.ch == '\\' {
		l.readChar()
		switch l.ch {
		case 'n':
			ch = '\n'
		case 'r':
			ch = '\r'
		case 't':
			ch = '\t'
		case '\\':
			ch = '\\'
		case '\'':
			ch = '\''
		case '0':
			ch = 0
		case 'x':
			l.readChar()
			hi := hexVal(l.ch)
			l.readChar()
			lo := hexVal(l.ch)
			ch = byte(hi<<4 | lo)
		default:
			ch = l.ch
		}
	} else {
		ch = l.ch
	}

	l.readChar() // move past char
	// l.ch should now be closing '

	return string(ch)
}

func (l *Lexer) readLineComment(pos token.Position) token.Token {
	l.readChar() // consume first /
	l.readChar() // consume second /

	start := l.pos
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}

	return token.Token{
		Type:    token.Comment,
		Literal: l.input[start:l.pos],
		Pos:     pos,
	}
}

func (l *Lexer) readBlockComment(pos token.Position) token.Token {
	l.readChar() // consume /
	l.readChar() // consume *

	start := l.pos
	for {
		if l.ch == 0 {
			break // error: unterminated comment
		}
		if l.ch == '*' && l.peekChar() == '/' {
			end := l.pos
			l.readChar() // consume *
			l.readChar() // consume /
			return token.Token{
				Type:    token.Comment,
				Literal: l.input[start:end],
				Pos:     pos,
			}
		}
		l.readChar()
	}

	return token.Token{
		Type:    token.Comment,
		Literal: l.input[start:l.pos],
		Pos:     pos,
	}
}

func isLetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isHexDigit(ch byte) bool {
	return isDigit(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func isOctalDigit(ch byte) bool {
	return ch >= '0' && ch <= '7'
}

func hexVal(ch byte) int {
	switch {
	case ch >= '0' && ch <= '9':
		return int(ch - '0')
	case ch >= 'a' && ch <= 'f':
		return int(ch - 'a' + 10)
	case ch >= 'A' && ch <= 'F':
		return int(ch - 'A' + 10)
	}
	return 0
}
