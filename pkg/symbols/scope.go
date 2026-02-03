// Package symbols provides symbol table management for ease.
package symbols

import (
	"ease/pkg/token"
	"ease/pkg/types"
)

// SymbolKind represents the kind of a symbol.
type SymbolKind int

const (
	VarSymbol SymbolKind = iota
	FuncSymbol
	ConstSymbol
	TypeSymbol
	ParamSymbol
)

func (k SymbolKind) String() string {
	switch k {
	case VarSymbol:
		return "variable"
	case FuncSymbol:
		return "function"
	case ConstSymbol:
		return "constant"
	case TypeSymbol:
		return "type"
	case ParamSymbol:
		return "parameter"
	default:
		return "unknown"
	}
}

// Symbol represents a declared name in the program.
type Symbol struct {
	Name    string
	Kind    SymbolKind
	Type    types.Type
	Pos     token.Position
	Mutable bool   // for variables: let vs let mut
	Used    bool   // for detecting unused symbols
	Offset  int    // stack offset for local variables
}

// NewSymbol creates a new symbol.
func NewSymbol(name string, kind SymbolKind, typ types.Type, pos token.Position) *Symbol {
	return &Symbol{
		Name: name,
		Kind: kind,
		Type: typ,
		Pos:  pos,
	}
}

// Scope represents a lexical scope containing symbol definitions.
type Scope struct {
	Parent   *Scope
	Children []*Scope
	Symbols  map[string]*Symbol
	Name     string // for debugging: "global", "function:add", "block"
}

// NewScope creates a new scope with the given parent.
func NewScope(parent *Scope, name string) *Scope {
	s := &Scope{
		Parent:  parent,
		Symbols: make(map[string]*Symbol),
		Name:    name,
	}
	if parent != nil {
		parent.Children = append(parent.Children, s)
	}
	return s
}

// Define adds a symbol to the current scope.
// Returns false if a symbol with the same name already exists in this scope.
func (s *Scope) Define(sym *Symbol) bool {
	if _, exists := s.Symbols[sym.Name]; exists {
		return false
	}
	s.Symbols[sym.Name] = sym
	return true
}

// Lookup searches for a symbol in the current scope only.
func (s *Scope) Lookup(name string) *Symbol {
	return s.Symbols[name]
}

// Resolve searches for a symbol in the current scope and all parent scopes.
func (s *Scope) Resolve(name string) *Symbol {
	if sym := s.Symbols[name]; sym != nil {
		return sym
	}
	if s.Parent != nil {
		return s.Parent.Resolve(name)
	}
	return nil
}

// ResolveLocal searches for a symbol only in local scopes (not global).
func (s *Scope) ResolveLocal(name string) *Symbol {
	if s.Parent == nil {
		// This is global scope, stop here
		return nil
	}
	if sym := s.Symbols[name]; sym != nil {
		return sym
	}
	return s.Parent.ResolveLocal(name)
}

// IsGlobal returns true if this is the global scope.
func (s *Scope) IsGlobal() bool {
	return s.Parent == nil
}

// Depth returns the nesting depth of this scope (0 for global).
func (s *Scope) Depth() int {
	depth := 0
	for p := s.Parent; p != nil; p = p.Parent {
		depth++
	}
	return depth
}

// Table manages the symbol table with nested scopes.
type Table struct {
	Global  *Scope
	Current *Scope
}

// NewTable creates a new symbol table with a global scope.
func NewTable() *Table {
	global := NewScope(nil, "global")
	return &Table{
		Global:  global,
		Current: global,
	}
}

// EnterScope creates and enters a new child scope.
func (t *Table) EnterScope(name string) *Scope {
	t.Current = NewScope(t.Current, name)
	return t.Current
}

// ExitScope returns to the parent scope.
func (t *Table) ExitScope() *Scope {
	if t.Current.Parent != nil {
		t.Current = t.Current.Parent
	}
	return t.Current
}

// Define adds a symbol to the current scope.
func (t *Table) Define(sym *Symbol) bool {
	return t.Current.Define(sym)
}

// Lookup searches for a symbol in the current scope only.
func (t *Table) Lookup(name string) *Symbol {
	return t.Current.Lookup(name)
}

// Resolve searches for a symbol in the current scope and all parent scopes.
func (t *Table) Resolve(name string) *Symbol {
	return t.Current.Resolve(name)
}

// DefineBuiltins adds builtin types and functions to the global scope.
func (t *Table) DefineBuiltins() {
	// Add builtin types
	builtinTypes := []string{
		"bool", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64", "string",
	}
	for _, name := range builtinTypes {
		typ := types.LookupBasicType(name)
		if typ != nil {
			t.Global.Define(&Symbol{
				Name: name,
				Kind: TypeSymbol,
				Type: typ,
			})
		}
	}

	// Add builtin functions
	for _, builtin := range types.Builtins {
		t.Global.Define(&Symbol{
			Name: builtin.Name,
			Kind: FuncSymbol,
			Type: builtin.Type,
		})
	}
}
