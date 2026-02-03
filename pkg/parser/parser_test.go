package parser

import (
	"ease/pkg/ast"
	"ease/pkg/lexer"
	"testing"
)

func TestImports(t *testing.T) {
	input := `import (
		"io"
		"./config"
		"github.com/user/pkg" as pkg
	)`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	if len(program.Imports) != 1 {
		t.Fatalf("expected 1 import declaration, got %d", len(program.Imports))
	}

	imp := program.Imports[0]
	if len(imp.Imports) != 3 {
		t.Fatalf("expected 3 import specs, got %d", len(imp.Imports))
	}

	tests := []struct {
		path  string
		alias string
	}{
		{"io", ""},
		{"./config", ""},
		{"github.com/user/pkg", "pkg"},
	}

	for i, tt := range tests {
		spec := imp.Imports[i]
		if spec.Path != tt.path {
			t.Errorf("import %d: expected path %q, got %q", i, tt.path, spec.Path)
		}
		if spec.Alias != tt.alias {
			t.Errorf("import %d: expected alias %q, got %q", i, tt.alias, spec.Alias)
		}
	}
}

func TestFnDecl(t *testing.T) {
	input := `fn Add(a: int, b: int) -> int {
		return a + b
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	if len(program.Decls) != 1 {
		t.Fatalf("expected 1 declaration, got %d", len(program.Decls))
	}

	fn, ok := program.Decls[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("expected *ast.FnDecl, got %T", program.Decls[0])
	}

	if fn.Name.Name != "Add" {
		t.Errorf("expected function name 'Add', got %q", fn.Name.Name)
	}

	if len(fn.Params) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(fn.Params))
	}

	if fn.Params[0].Name.Name != "a" {
		t.Errorf("expected first param 'a', got %q", fn.Params[0].Name.Name)
	}
}

func TestGenericFn(t *testing.T) {
	// Use Go-style slice syntax []T instead of Rust-style [T]
	input := `fn identity<T>(x: T) -> T {
		return x
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	fn, ok := program.Decls[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("expected *ast.FnDecl, got %T", program.Decls[0])
	}

	if len(fn.TypeParams) != 1 {
		t.Fatalf("expected 1 type param, got %d", len(fn.TypeParams))
	}

	if fn.TypeParams[0].Name.Name != "T" {
		t.Errorf("expected type param 'T', got %q", fn.TypeParams[0].Name.Name)
	}
}

func TestGenericFnWithMultipleTypeParams(t *testing.T) {
	input := `fn pair<T, U>(a: T, b: U) -> T {
		return a
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	fn, ok := program.Decls[0].(*ast.FnDecl)
	if !ok {
		t.Fatalf("expected *ast.FnDecl, got %T", program.Decls[0])
	}

	if len(fn.TypeParams) != 2 {
		t.Fatalf("expected 2 type params, got %d", len(fn.TypeParams))
	}

	if fn.TypeParams[0].Name.Name != "T" {
		t.Errorf("expected first type param 'T', got %q", fn.TypeParams[0].Name.Name)
	}
	if fn.TypeParams[1].Name.Name != "U" {
		t.Errorf("expected second type param 'U', got %q", fn.TypeParams[1].Name.Name)
	}
}

func TestStructDecl(t *testing.T) {
	input := `struct Config {
		Name: string,
		Port: int,
		Debug: bool,
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	s, ok := program.Decls[0].(*ast.StructDecl)
	if !ok {
		t.Fatalf("expected *ast.StructDecl, got %T", program.Decls[0])
	}

	if s.Name.Name != "Config" {
		t.Errorf("expected struct name 'Config', got %q", s.Name.Name)
	}

	if len(s.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(s.Fields))
	}

	expected := []string{"Name", "Port", "Debug"}
	for i, name := range expected {
		if s.Fields[i].Name.Name != name {
			t.Errorf("field %d: expected %q, got %q", i, name, s.Fields[i].Name.Name)
		}
	}
}

func TestEnumDecl(t *testing.T) {
	input := `enum Option<T> {
		Some { value: T },
		None,
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	e, ok := program.Decls[0].(*ast.EnumDecl)
	if !ok {
		t.Fatalf("expected *ast.EnumDecl, got %T", program.Decls[0])
	}

	if e.Name.Name != "Option" {
		t.Errorf("expected enum name 'Option', got %q", e.Name.Name)
	}

	if len(e.TypeParams) != 1 {
		t.Fatalf("expected 1 type param, got %d", len(e.TypeParams))
	}

	if len(e.Variants) != 2 {
		t.Fatalf("expected 2 variants, got %d", len(e.Variants))
	}

	if e.Variants[0].Name.Name != "Some" {
		t.Errorf("expected first variant 'Some', got %q", e.Variants[0].Name.Name)
	}

	if len(e.Variants[0].Fields) != 1 {
		t.Errorf("expected Some to have 1 field, got %d", len(e.Variants[0].Fields))
	}

	if e.Variants[1].Name.Name != "None" {
		t.Errorf("expected second variant 'None', got %q", e.Variants[1].Name.Name)
	}
}

func TestTraitDecl(t *testing.T) {
	input := `trait Validator {
		fn Validate(&self) -> Result<(), Error>
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	tr, ok := program.Decls[0].(*ast.TraitDecl)
	if !ok {
		t.Fatalf("expected *ast.TraitDecl, got %T", program.Decls[0])
	}

	if tr.Name.Name != "Validator" {
		t.Errorf("expected trait name 'Validator', got %q", tr.Name.Name)
	}

	if len(tr.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(tr.Methods))
	}
}

func TestImplDecl(t *testing.T) {
	input := `impl Validator for Config {
		fn Validate(&self) -> Result<(), Error> {
			return error.New("not implemented")
		}
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	impl, ok := program.Decls[0].(*ast.ImplDecl)
	if !ok {
		t.Fatalf("expected *ast.ImplDecl, got %T", program.Decls[0])
	}

	if impl.Trait == nil {
		t.Fatal("expected trait to be set")
	}

	if len(impl.Methods) != 1 {
		t.Fatalf("expected 1 method, got %d", len(impl.Methods))
	}
}

func TestLetStmt(t *testing.T) {
	tests := []struct {
		input   string
		mutable bool
		name    string
	}{
		{"let x = 5", false, "x"},
		{"let mut y = 10", true, "y"},
		{"let z: int = 15", false, "z"},
	}

	for _, tt := range tests {
		input := "fn test() { " + tt.input + " }"
		l := lexer.New(input, "test")
		p := New(l)
		program := p.ParseProgram()
		checkErrors(t, p)

		fn := program.Decls[0].(*ast.FnDecl)
		if len(fn.Body.Stmts) != 1 {
			t.Fatalf("expected 1 statement, got %d", len(fn.Body.Stmts))
		}

		let, ok := fn.Body.Stmts[0].(*ast.LetStmt)
		if !ok {
			t.Fatalf("expected *ast.LetStmt, got %T", fn.Body.Stmts[0])
		}

		if let.Mutable != tt.mutable {
			t.Errorf("expected mutable=%v, got %v", tt.mutable, let.Mutable)
		}

		ident, ok := let.Pattern.(*ast.IdentPattern)
		if !ok {
			t.Fatalf("expected *ast.IdentPattern, got %T", let.Pattern)
		}

		if ident.Name.Name != tt.name {
			t.Errorf("expected name %q, got %q", tt.name, ident.Name.Name)
		}
	}
}

func TestBinaryExpressions(t *testing.T) {
	tests := []struct {
		input string
		left  int64
		op    string
		right int64
	}{
		{"1 + 2", 1, "+", 2},
		{"3 - 4", 3, "-", 4},
		{"5 * 6", 5, "*", 6},
		{"8 / 2", 8, "/", 2},
		{"10 % 3", 10, "%", 3},
	}

	for _, tt := range tests {
		input := "fn test() { " + tt.input + " }"
		l := lexer.New(input, "test")
		p := New(l)
		program := p.ParseProgram()
		checkErrors(t, p)

		fn := program.Decls[0].(*ast.FnDecl)
		expr := fn.Body.Stmts[0].(*ast.ExprStmt).Expr.(*ast.BinaryExpr)

		left := expr.Left.(*ast.IntLit)
		if left.Value != tt.left {
			t.Errorf("expected left %d, got %d", tt.left, left.Value)
		}

		if expr.Op.Literal != tt.op {
			t.Errorf("expected op %q, got %q", tt.op, expr.Op.Literal)
		}

		right := expr.Right.(*ast.IntLit)
		if right.Value != tt.right {
			t.Errorf("expected right %d, got %d", tt.right, right.Value)
		}
	}
}

func TestOperatorPrecedence(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"1 + 2 * 3", "(1 + (2 * 3))"},
		{"1 * 2 + 3", "((1 * 2) + 3)"},
		{"1 + 2 + 3", "((1 + 2) + 3)"},
		{"1 < 2 && 3 > 4", "((1 < 2) && (3 > 4))"},
		{"1 || 2 && 3", "(1 || (2 && 3))"},
	}

	for _, tt := range tests {
		input := "fn test() { " + tt.input + " }"
		l := lexer.New(input, "test")
		p := New(l)
		program := p.ParseProgram()
		checkErrors(t, p)

		fn := program.Decls[0].(*ast.FnDecl)
		expr := fn.Body.Stmts[0].(*ast.ExprStmt).Expr

		actual := exprString(expr)
		if actual != tt.expected {
			t.Errorf("input %q: expected %q, got %q", tt.input, tt.expected, actual)
		}
	}
}

func TestIfExpression(t *testing.T) {
	input := `fn test() {
		if x > 0 {
			return 1
		} else {
			return 0
		}
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	fn := program.Decls[0].(*ast.FnDecl)
	stmt := fn.Body.Stmts[0].(*ast.IfStmt)

	if stmt.Cond == nil {
		t.Fatal("expected condition")
	}

	if stmt.Then == nil {
		t.Fatal("expected then block")
	}

	if stmt.Else == nil {
		t.Fatal("expected else block")
	}
}

func TestMatchExpression(t *testing.T) {
	input := `fn test() {
		match x {
			0 => "zero",
			1 => "one",
			_ => "other",
		}
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	fn := program.Decls[0].(*ast.FnDecl)
	stmt := fn.Body.Stmts[0].(*ast.MatchStmt)

	if len(stmt.Arms) != 3 {
		t.Fatalf("expected 3 match arms, got %d", len(stmt.Arms))
	}
}

func TestEnumPattern(t *testing.T) {
	input := `fn test() {
		match opt {
			Option::Some { value } => value,
			Option::None => 0,
		}
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	fn := program.Decls[0].(*ast.FnDecl)
	stmt := fn.Body.Stmts[0].(*ast.MatchStmt)

	if len(stmt.Arms) != 2 {
		t.Fatalf("expected 2 match arms, got %d", len(stmt.Arms))
	}

	// First arm should be EnumPattern
	_, ok := stmt.Arms[0].Pattern.(*ast.EnumPattern)
	if !ok {
		t.Errorf("expected *ast.EnumPattern, got %T", stmt.Arms[0].Pattern)
	}
}

func TestForLoop(t *testing.T) {
	// Test range loop: for i in 0..10 { }
	t.Run("range", func(t *testing.T) {
		input := `fn test() {
			for i in 0..10 {
				println(i)
			}
		}`

		l := lexer.New(input, "test")
		p := New(l)
		program := p.ParseProgram()
		checkErrors(t, p)

		fn := program.Decls[0].(*ast.FnDecl)
		stmt := fn.Body.Stmts[0].(*ast.ForStmt)

		if stmt.Pattern == nil {
			t.Fatal("expected pattern")
		}

		if stmt.Iter == nil {
			t.Fatal("expected iterator expression")
		}

		if stmt.Cond != nil {
			t.Error("expected no condition for range loop")
		}

		// Check it's a range expression
		_, ok := stmt.Iter.(*ast.RangeExpr)
		if !ok {
			t.Errorf("expected *ast.RangeExpr, got %T", stmt.Iter)
		}
	})

	// Test infinite loop: for { }
	t.Run("infinite", func(t *testing.T) {
		input := `fn test() {
			for {
				break
			}
		}`

		l := lexer.New(input, "test")
		p := New(l)
		program := p.ParseProgram()
		checkErrors(t, p)

		fn := program.Decls[0].(*ast.FnDecl)
		stmt := fn.Body.Stmts[0].(*ast.ForStmt)

		if stmt.Pattern != nil {
			t.Error("expected no pattern for infinite loop")
		}

		if stmt.Iter != nil {
			t.Error("expected no iter for infinite loop")
		}

		if stmt.Cond != nil {
			t.Error("expected no condition for infinite loop")
		}

		if stmt.Body == nil {
			t.Fatal("expected body")
		}
	})

	// Test condition loop: for x > 0 { }
	t.Run("condition", func(t *testing.T) {
		input := `fn test() {
			for x > 0 {
				x = x - 1
			}
		}`

		l := lexer.New(input, "test")
		p := New(l)
		program := p.ParseProgram()
		checkErrors(t, p)

		fn := program.Decls[0].(*ast.FnDecl)
		stmt := fn.Body.Stmts[0].(*ast.ForStmt)

		if stmt.Pattern != nil {
			t.Error("expected no pattern for condition loop")
		}

		if stmt.Iter != nil {
			t.Error("expected no iter for condition loop")
		}

		if stmt.Cond == nil {
			t.Fatal("expected condition")
		}

		// Check condition is a binary expression
		_, ok := stmt.Cond.(*ast.BinaryExpr)
		if !ok {
			t.Errorf("expected *ast.BinaryExpr, got %T", stmt.Cond)
		}
	})
}

func TestClosure(t *testing.T) {
	input := `fn test() {
		let f = |x: int| -> int { x * 2 }
		let g = || 42
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	fn := program.Decls[0].(*ast.FnDecl)

	// First closure with params
	let1 := fn.Body.Stmts[0].(*ast.LetStmt)
	closure1 := let1.Value.(*ast.ClosureExpr)
	if len(closure1.Params) != 1 {
		t.Errorf("expected 1 param, got %d", len(closure1.Params))
	}

	// Second closure without params
	let2 := fn.Body.Stmts[1].(*ast.LetStmt)
	closure2 := let2.Value.(*ast.ClosureExpr)
	if len(closure2.Params) != 0 {
		t.Errorf("expected 0 params, got %d", len(closure2.Params))
	}
}

func TestChannelOps(t *testing.T) {
	input := `fn test() {
		let ch = chan<int>()
		ch <- 42
		let x = <-ch
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	fn := program.Decls[0].(*ast.FnDecl)

	// Send
	send := fn.Body.Stmts[1].(*ast.ExprStmt).Expr.(*ast.ChanSendExpr)
	if send.Chan == nil || send.Value == nil {
		t.Error("expected chan and value in send expression")
	}

	// Receive
	let := fn.Body.Stmts[2].(*ast.LetStmt)
	recv := let.Value.(*ast.ChanRecvExpr)
	if recv.Chan == nil {
		t.Error("expected chan in receive expression")
	}
}

func TestTryOperator(t *testing.T) {
	input := `fn test() {
		let x = foo()?
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	fn := program.Decls[0].(*ast.FnDecl)
	let := fn.Body.Stmts[0].(*ast.LetStmt)

	_, ok := let.Value.(*ast.TryExpr)
	if !ok {
		t.Errorf("expected *ast.TryExpr, got %T", let.Value)
	}
}

func TestStructLiteral(t *testing.T) {
	input := `fn test() {
		let cfg = Config { Name: "test", Port: 8080 }
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	fn := program.Decls[0].(*ast.FnDecl)
	let := fn.Body.Stmts[0].(*ast.LetStmt)

	s, ok := let.Value.(*ast.StructExpr)
	if !ok {
		t.Fatalf("expected *ast.StructExpr, got %T", let.Value)
	}

	if len(s.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(s.Fields))
	}
}

func TestMethodCall(t *testing.T) {
	input := `fn test() {
		x.foo().bar(1, 2)
	}`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	fn := program.Decls[0].(*ast.FnDecl)
	expr := fn.Body.Stmts[0].(*ast.ExprStmt).Expr

	method, ok := expr.(*ast.MethodExpr)
	if !ok {
		t.Fatalf("expected *ast.MethodExpr, got %T", expr)
	}

	if method.Method.Name != "bar" {
		t.Errorf("expected method name 'bar', got %q", method.Method.Name)
	}

	if len(method.Args) != 2 {
		t.Errorf("expected 2 args, got %d", len(method.Args))
	}
}

func TestTestDeclaration(t *testing.T) {
	t.Run("simple test", func(t *testing.T) {
		input := `test "addition works" {
			let result = add(2, 3)
			if result != 5 {
				return error.New("expected 5")
			}
		}`

		l := lexer.New(input, "test")
		p := New(l)
		program := p.ParseProgram()
		checkErrors(t, p)

		if len(program.Decls) != 1 {
			t.Fatalf("expected 1 declaration, got %d", len(program.Decls))
		}

		test, ok := program.Decls[0].(*ast.TestDecl)
		if !ok {
			t.Fatalf("expected *ast.TestDecl, got %T", program.Decls[0])
		}

		if test.Description.Value != "addition works" {
			t.Errorf("expected description 'addition works', got %q", test.Description.Value)
		}

		if test.Body == nil {
			t.Fatal("expected body")
		}
	})

	t.Run("test with attributes", func(t *testing.T) {
		input := `#[slow]
		#[integration]
		test "database test" {
			return error.New("not implemented")
		}`

		l := lexer.New(input, "test")
		p := New(l)
		program := p.ParseProgram()
		checkErrors(t, p)

		test, ok := program.Decls[0].(*ast.TestDecl)
		if !ok {
			t.Fatalf("expected *ast.TestDecl, got %T", program.Decls[0])
		}

		if len(test.Attributes) != 2 {
			t.Fatalf("expected 2 attributes, got %d", len(test.Attributes))
		}

		if test.Attributes[0].Name.Name != "slow" {
			t.Errorf("expected first attribute 'slow', got %q", test.Attributes[0].Name.Name)
		}

		if test.Attributes[1].Name.Name != "integration" {
			t.Errorf("expected second attribute 'integration', got %q", test.Attributes[1].Name.Name)
		}
	})

	t.Run("test with parallel attribute", func(t *testing.T) {
		input := `#[parallel]
		test "concurrent test" {
			let ch = chan<int>()
			go || { ch <- 42 }
			let x = <-ch
		}`

		l := lexer.New(input, "test")
		p := New(l)
		program := p.ParseProgram()
		checkErrors(t, p)

		test := program.Decls[0].(*ast.TestDecl)
		if len(test.Attributes) != 1 {
			t.Fatalf("expected 1 attribute, got %d", len(test.Attributes))
		}

		if test.Attributes[0].Name.Name != "parallel" {
			t.Errorf("expected attribute 'parallel', got %q", test.Attributes[0].Name.Name)
		}
	})
}

func TestFullProgram(t *testing.T) {
	input := `
import (
	"io"
	"./config"
)

enum Result<T, E> {
	Ok { value: T },
	Err { error: E },
}

struct Config {
	Name: string,
	Port: int,
}

fn loadConfig(path: string) -> Result<Config, Error> {
	let content = io.ReadFile(path)?
	return Config { Name: "test", Port: 8080 }
}

fn main() -> Result<(), Error> {
	let cfg = loadConfig("config.json")?

	match cfg {
		Config { Name, Port } if Port > 0 => {
			println(Name)
		},
		_ => {},
	}

	for i in 0..10 {
		println(i)
	}
}
`

	l := lexer.New(input, "test")
	p := New(l)
	program := p.ParseProgram()
	checkErrors(t, p)

	if len(program.Imports) != 1 {
		t.Errorf("expected 1 import, got %d", len(program.Imports))
	}

	// enum, struct, fn loadConfig, fn main
	if len(program.Decls) != 4 {
		t.Errorf("expected 4 declarations, got %d", len(program.Decls))
	}
}

// Helper functions

func checkErrors(t *testing.T, p *Parser) {
	errors := p.Errors()
	if len(errors) == 0 {
		return
	}

	t.Errorf("parser has %d errors:", len(errors))
	for _, msg := range errors {
		t.Errorf("  %s", msg)
	}
	t.FailNow()
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.IntLit:
		return v.Raw
	case *ast.Ident:
		return v.Name
	case *ast.BinaryExpr:
		return "(" + exprString(v.Left) + " " + v.Op.Literal + " " + exprString(v.Right) + ")"
	default:
		return "?"
	}
}
