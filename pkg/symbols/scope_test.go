package symbols

import (
	"testing"

	"ease/pkg/token"
	"ease/pkg/types"
)

func TestScope(t *testing.T) {
	global := NewScope(nil, "global")

	// Define a symbol
	sym := NewSymbol("x", VarSymbol, types.TypInt, token.Position{})
	if !global.Define(sym) {
		t.Error("should be able to define x")
	}

	// Can't redefine in same scope
	sym2 := NewSymbol("x", VarSymbol, types.TypBool, token.Position{})
	if global.Define(sym2) {
		t.Error("should not be able to redefine x")
	}

	// Lookup should find it
	found := global.Lookup("x")
	if found == nil {
		t.Error("should find x")
	}
	if !found.Type.Equals(types.TypInt) {
		t.Error("x should have type int")
	}
}

func TestNestedScopes(t *testing.T) {
	global := NewScope(nil, "global")
	child := NewScope(global, "child")

	// Define in global
	globalSym := NewSymbol("g", VarSymbol, types.TypInt, token.Position{})
	global.Define(globalSym)

	// Define in child with same name
	childSym := NewSymbol("g", VarSymbol, types.TypBool, token.Position{})
	if !child.Define(childSym) {
		t.Error("should be able to shadow in child scope")
	}

	// Lookup finds local
	found := child.Lookup("g")
	if !found.Type.Equals(types.TypBool) {
		t.Error("child.Lookup should find local bool")
	}

	// Resolve finds local first
	found = child.Resolve("g")
	if !found.Type.Equals(types.TypBool) {
		t.Error("child.Resolve should find local bool")
	}

	// Define only in global
	global.Define(NewSymbol("h", VarSymbol, types.TypString, token.Position{}))

	// Resolve in child finds parent's symbol
	found = child.Resolve("h")
	if found == nil {
		t.Error("child should resolve h from parent")
	}
	if !found.Type.Equals(types.TypString) {
		t.Error("h should be string")
	}
}

func TestSymbolTable(t *testing.T) {
	table := NewTable()

	// Define in global
	table.Define(NewSymbol("global_var", VarSymbol, types.TypInt, token.Position{}))

	// Enter function scope
	table.EnterScope("function:test")

	// Define local
	table.Define(NewSymbol("local_var", VarSymbol, types.TypBool, token.Position{}))

	// Can resolve both
	if table.Resolve("global_var") == nil {
		t.Error("should resolve global_var")
	}
	if table.Resolve("local_var") == nil {
		t.Error("should resolve local_var")
	}

	// Exit scope
	table.ExitScope()

	// Can only resolve global now
	if table.Resolve("global_var") == nil {
		t.Error("should still resolve global_var")
	}
	if table.Resolve("local_var") != nil {
		t.Error("should not resolve local_var after exit")
	}
}

func TestBuiltins(t *testing.T) {
	table := NewTable()
	table.DefineBuiltins()

	// Should have builtin types
	intSym := table.Resolve("int")
	if intSym == nil {
		t.Error("should have builtin int type")
	}
	if intSym.Kind != TypeSymbol {
		t.Error("int should be a type symbol")
	}

	// Should have builtin functions
	printlnSym := table.Resolve("println")
	if printlnSym == nil {
		t.Error("should have builtin println function")
	}
	if printlnSym.Kind != FuncSymbol {
		t.Error("println should be a function symbol")
	}
}
