package sema

import (
	"strings"
	"testing"

	"ease/pkg/lexer"
	"ease/pkg/parser"
	"ease/pkg/types"
)

func parseAndAnalyze(t *testing.T, input string) (*TypeInfo, []Error) {
	l := lexer.New(input, "test")
	p := parser.New(l)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parse errors: %v", p.Errors())
	}

	analyzer := New()
	return analyzer.Analyze(prog)
}

func TestSimpleFunction(t *testing.T) {
	input := `
fn add(a: int, b: int) -> int {
	return a + b
}
`
	info, errs := parseAndAnalyze(t, input)

	if len(errs) > 0 {
		t.Fatalf("Unexpected errors: %v", errs)
	}

	// Check that we recorded type info for expressions
	if len(info.Types) == 0 {
		t.Error("Expected type info for expressions")
	}
}

func TestTypeInference(t *testing.T) {
	input := `
fn test() -> int {
	let x = 42
	return x
}
`
	info, errs := parseAndAnalyze(t, input)

	if len(errs) > 0 {
		t.Fatalf("Unexpected errors: %v", errs)
	}

	// x should be inferred as int
	for ident, sym := range info.Defs {
		if ident.Name == "x" {
			if !sym.Type.Equals(types.TypInt) {
				t.Errorf("x should be int, got %s", sym.Type)
			}
			return
		}
	}
	t.Error("Expected to find x in definitions")
}

func TestTypeMismatch(t *testing.T) {
	input := `
fn test() -> int {
	let x: int = true
	return x
}
`
	_, errs := parseAndAnalyze(t, input)

	if len(errs) == 0 {
		t.Error("Expected type mismatch error")
	}

	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "cannot assign") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'cannot assign' error, got: %v", errs)
	}
}

func TestUndefinedVariable(t *testing.T) {
	input := `
fn test() -> int {
	return x
}
`
	_, errs := parseAndAnalyze(t, input)

	if len(errs) == 0 {
		t.Error("Expected undefined variable error")
	}

	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "undefined") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'undefined' error, got: %v", errs)
	}
}

func TestFunctionCall(t *testing.T) {
	input := `
fn add(a: int, b: int) -> int {
	return a + b
}

fn main() -> int {
	return add(1, 2)
}
`
	_, errs := parseAndAnalyze(t, input)

	if len(errs) > 0 {
		t.Fatalf("Unexpected errors: %v", errs)
	}
}

func TestWrongArgCount(t *testing.T) {
	input := `
fn add(a: int, b: int) -> int {
	return a + b
}

fn main() -> int {
	return add(1)
}
`
	_, errs := parseAndAnalyze(t, input)

	if len(errs) == 0 {
		t.Error("Expected wrong argument count error")
	}

	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "wrong number of arguments") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'wrong number of arguments' error, got: %v", errs)
	}
}

func TestBinaryOperators(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name: "int addition",
			input: `fn test() -> int { return 1 + 2 }`,
			wantErr: false,
		},
		{
			name: "bool and",
			input: `fn test() -> bool { return true && false }`,
			wantErr: false,
		},
		{
			name: "comparison",
			input: `fn test() -> bool { return 1 < 2 }`,
			wantErr: false,
		},
		{
			name: "mixed types",
			input: `fn test() -> int { return 1 + true }`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := parseAndAnalyze(t, tt.input)
			hasErr := len(errs) > 0
			if hasErr != tt.wantErr {
				t.Errorf("wantErr=%v, got errors: %v", tt.wantErr, errs)
			}
		})
	}
}

func TestStructType(t *testing.T) {
	input := `
struct Point {
	X: int,
	Y: int,
}

fn test() -> int {
	let p = Point { X: 10, Y: 20 }
	return p.X
}
`
	_, errs := parseAndAnalyze(t, input)

	if len(errs) > 0 {
		t.Fatalf("Unexpected errors: %v", errs)
	}
}

func TestIfConditionType(t *testing.T) {
	input := `
fn test() -> int {
	if 42 {
		return 1
	}
	return 0
}
`
	_, errs := parseAndAnalyze(t, input)

	if len(errs) == 0 {
		t.Error("Expected condition type error")
	}

	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "condition must be bool") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected 'condition must be bool' error, got: %v", errs)
	}
}
