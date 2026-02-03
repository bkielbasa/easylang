package ir

import (
	"strings"
	"testing"

	"ease/pkg/lexer"
	"ease/pkg/parser"
	"ease/pkg/sema"
)

func parseAndBuild(t *testing.T, input string) *Program {
	l := lexer.New(input, "test")
	p := parser.New(l)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parse errors: %v", p.Errors())
	}

	analyzer := sema.New()
	info, errs := analyzer.Analyze(prog)

	if len(errs) > 0 {
		t.Fatalf("Sema errors: %v", errs)
	}

	builder := NewBuilder(info)
	return builder.Build(prog)
}

func TestSimpleFunctionIR(t *testing.T) {
	input := `
fn add(a: int, b: int) -> int {
	return a + b
}
`
	prog := parseAndBuild(t, input)

	if len(prog.Functions) != 1 {
		t.Fatalf("Expected 1 function, got %d", len(prog.Functions))
	}

	fn := prog.Functions[0]
	if fn.Name != "add" {
		t.Errorf("Expected function name 'add', got '%s'", fn.Name)
	}

	if len(fn.Params) != 2 {
		t.Errorf("Expected 2 params, got %d", len(fn.Params))
	}

	// Should have an entry block with instructions
	if fn.Entry == nil {
		t.Error("Expected entry block")
	}

	// Print IR for debugging
	t.Log(fn.String())
}

func TestLetStatementIR(t *testing.T) {
	input := `
fn test() -> int {
	let x = 42
	return x
}
`
	prog := parseAndBuild(t, input)

	fn := prog.Functions[0]
	t.Log(fn.String())

	// Should have copy and return instructions
	found := false
	for _, block := range fn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op == OpReturn {
				found = true
			}
		}
	}
	if !found {
		t.Error("Expected return instruction")
	}
}

func TestBinaryOpIR(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wantOp Op
	}{
		{
			name:   "addition",
			input:  `fn test() -> int { return 1 + 2 }`,
			wantOp: OpAdd,
		},
		{
			name:   "subtraction",
			input:  `fn test() -> int { return 5 - 3 }`,
			wantOp: OpSub,
		},
		{
			name:   "multiplication",
			input:  `fn test() -> int { return 2 * 3 }`,
			wantOp: OpMul,
		},
		{
			name:   "division",
			input:  `fn test() -> int { return 10 / 2 }`,
			wantOp: OpDiv,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prog := parseAndBuild(t, tt.input)
			fn := prog.Functions[0]

			found := false
			for _, block := range fn.Blocks {
				for _, instr := range block.Instrs {
					if instr.Op == tt.wantOp {
						found = true
					}
				}
			}
			if !found {
				t.Errorf("Expected %s instruction, got:\n%s", tt.wantOp, fn.String())
			}
		})
	}
}

func TestCallIR(t *testing.T) {
	input := `
fn add(a: int, b: int) -> int {
	return a + b
}

fn main() -> int {
	return add(2, 3)
}
`
	prog := parseAndBuild(t, input)

	if len(prog.Functions) != 2 {
		t.Fatalf("Expected 2 functions, got %d", len(prog.Functions))
	}

	// Find main function
	var mainFn *Function
	for _, fn := range prog.Functions {
		if fn.Name == "main" {
			mainFn = fn
			break
		}
	}
	if mainFn == nil {
		t.Fatal("Expected main function")
	}

	// Should have a call instruction
	found := false
	for _, block := range mainFn.Blocks {
		for _, instr := range block.Instrs {
			if instr.Op == OpCall {
				found = true
				// First arg should be function reference
				if instr.Args[0].Kind != OpndFunc {
					t.Errorf("Expected function reference, got %v", instr.Args[0].Kind)
				}
			}
		}
	}
	if !found {
		t.Errorf("Expected call instruction, got:\n%s", mainFn.String())
	}
}

func TestIRString(t *testing.T) {
	input := `
fn add(a: int, b: int) -> int {
	return a + b
}
`
	prog := parseAndBuild(t, input)

	str := prog.String()

	// Should contain function declaration
	if !strings.Contains(str, "fn add") {
		t.Error("IR string should contain 'fn add'")
	}

	// Should contain entry block
	if !strings.Contains(str, "entry:") {
		t.Error("IR string should contain 'entry:'")
	}
}
