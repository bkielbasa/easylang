// Package sema provides semantic analysis for ease.
package sema

import (
	"fmt"
	"os"

	"ease/pkg/ast"
	"ease/pkg/lexer"
	"ease/pkg/parser"
	"ease/pkg/symbols"
	"ease/pkg/token"
	"ease/pkg/types"
)

// Error represents a semantic error.
type Error struct {
	Pos     token.Position
	Message string
}

func (e Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.Pos.Filename, e.Pos.Line, e.Pos.Column, e.Message)
}

// TypeInfo stores type information computed during analysis.
type TypeInfo struct {
	// Types maps expressions to their types
	Types map[ast.Expr]types.Type

	// Defs maps identifiers to their definitions
	Defs map[*ast.Ident]*symbols.Symbol

	// Uses maps identifiers to the symbols they reference
	Uses map[*ast.Ident]*symbols.Symbol

	// GenericCalls maps call expressions to their monomorphized function names
	// This is used by the IR builder to call the correct specialized function
	GenericCalls map[*ast.CallExpr]string
}

// ModuleInfo stores information about an imported module.
type ModuleInfo struct {
	Name    string           // module name (last path segment or alias)
	Path    string           // full import path
	Program *ast.Program     // parsed AST
	Symbols map[string]*symbols.Symbol  // exported symbols
}

// NewTypeInfo creates a new TypeInfo.
func NewTypeInfo() *TypeInfo {
	return &TypeInfo{
		Types:        make(map[ast.Expr]types.Type),
		Defs:         make(map[*ast.Ident]*symbols.Symbol),
		Uses:         make(map[*ast.Ident]*symbols.Symbol),
		GenericCalls: make(map[*ast.CallExpr]string),
	}
}

// Analyzer performs semantic analysis on an AST.
type Analyzer struct {
	table           *symbols.Table
	info            *TypeInfo
	errors          []Error
	currentFunc     *types.Function // current function being analyzed
	currentSelfType types.Type      // current type for Self in impl blocks
	fnDecls         map[string]*ast.FnDecl // forward declarations
	structDecls     map[string]*ast.StructDecl
	enumDecls       map[string]*ast.EnumDecl
	methods         map[string]map[string]*types.Function // type name -> method name -> function
	methodDecls     map[string]map[string]*ast.FnDecl     // type name -> method name -> declaration

	// Generic declarations (uninstantiated templates)
	genericFns     map[string]*ast.FnDecl
	genericStructs map[string]*ast.StructDecl
	genericEnums   map[string]*ast.EnumDecl

	// Instantiation cache: mangled name -> instantiated type
	instantiations map[string]types.Type

	// Type parameter scope: maps type param names to TypeParam during generic analysis
	typeParamScope map[string]*types.TypeParam

	// Type parameter ID counter for unique identification
	typeParamIDCounter int

	// Program reference for adding monomorphized declarations
	prog *ast.Program

	// Module system
	modules        map[string]*ModuleInfo    // module name -> module info
	importAliases  map[string]string         // alias -> module name
	currentFile    string                    // current file being analyzed
	loadedFiles    map[string]*ast.Program   // file path -> parsed program
	usedModules    map[string]bool           // tracks which imported modules are actually used
}

// New creates a new Analyzer.
func New() *Analyzer {
	table := symbols.NewTable()
	table.DefineBuiltins()
	return &Analyzer{
		table:          table,
		info:           NewTypeInfo(),
		fnDecls:        make(map[string]*ast.FnDecl),
		structDecls:    make(map[string]*ast.StructDecl),
		enumDecls:      make(map[string]*ast.EnumDecl),
		methods:        make(map[string]map[string]*types.Function),
		methodDecls:    make(map[string]map[string]*ast.FnDecl),
		genericFns:     make(map[string]*ast.FnDecl),
		genericStructs: make(map[string]*ast.StructDecl),
		genericEnums:   make(map[string]*ast.EnumDecl),
		instantiations: make(map[string]types.Type),
		typeParamScope: make(map[string]*types.TypeParam),
		modules:        make(map[string]*ModuleInfo),
		importAliases:  make(map[string]string),
		loadedFiles:    make(map[string]*ast.Program),
		usedModules:    make(map[string]bool),
	}
}

// Analyze performs semantic analysis on the program.
func (a *Analyzer) Analyze(prog *ast.Program) (*TypeInfo, []Error) {
	// Store program reference for adding monomorphized declarations
	a.prog = prog

	// Process imports first
	a.processImports(prog)

	// First pass: collect all declarations (forward references)
	a.collectDeclarations(prog)

	// Second pass: analyze all declarations
	for _, decl := range prog.Decls {
		a.analyzeDecl(decl)
	}

	// Check for unused imports
	a.checkUnusedImports(prog)

	return a.info, a.errors
}

// Errors returns the accumulated errors.
func (a *Analyzer) Errors() []Error {
	return a.errors
}

// error records a semantic error.
func (a *Analyzer) error(pos token.Position, format string, args ...interface{}) {
	a.errors = append(a.errors, Error{
		Pos:     pos,
		Message: fmt.Sprintf(format, args...),
	})
}

// ============================================
// Module System
// ============================================

// checkUnusedImports reports errors for imported modules that are never used.
func (a *Analyzer) checkUnusedImports(prog *ast.Program) {
	for _, imp := range prog.Imports {
		for _, spec := range imp.Imports {
			moduleName := a.getModuleName(spec)
			if _, used := a.usedModules[moduleName]; !used {
				// Only report if the module was successfully loaded
				if _, loaded := a.modules[moduleName]; loaded {
					a.error(spec.Token.Pos, "'%s' imported and not used", moduleName)
				}
			}
		}
	}
}

// processImports loads and analyzes all imported modules.
func (a *Analyzer) processImports(prog *ast.Program) {
	for _, imp := range prog.Imports {
		for _, spec := range imp.Imports {
			a.loadModule(spec)
		}
	}
}

// loadModule loads and analyzes an imported module.
func (a *Analyzer) loadModule(spec ast.ImportSpec) {
	// Resolve import path to file path (relative to current file)
	filePath := a.resolveImportPath(spec.Path, spec.Token.Pos.Filename)
	if filePath == "" {
		a.error(spec.Token.Pos, "cannot resolve import path '%s'", spec.Path)
		return
	}

	// Check if already loaded
	if _, loaded := a.loadedFiles[filePath]; loaded {
		return
	}

	// Parse the module
	prog, err := a.parseModule(filePath)
	if err != nil {
		a.error(spec.Token.Pos, "cannot load module '%s': %v", spec.Path, err)
		return
	}

	a.loadedFiles[filePath] = prog

	// Determine module name (last path segment or alias)
	moduleName := a.getModuleName(spec)

	// Analyze the module recursively
	moduleAnalyzer := New()
	moduleAnalyzer.currentFile = filePath
	_, errors := moduleAnalyzer.Analyze(prog)

	if len(errors) > 0 {
		// Report errors from imported module
		for _, e := range errors {
			a.errors = append(a.errors, e)
		}
		return
	}

	// Collect exported symbols (uppercase names only)
	exported := make(map[string]*symbols.Symbol)
	for name, sym := range moduleAnalyzer.table.Global.Symbols {
		if len(name) > 0 && 'A' <= name[0] && name[0] <= 'Z' {
			exported[name] = sym
		}
	}

	// Add exported function declarations to the main program
	// This allows them to be compiled and linked
	for _, decl := range prog.Decls {
		if fnDecl, ok := decl.(*ast.FnDecl); ok {
			// Only add exported (uppercase) functions
			if len(fnDecl.Name.Name) > 0 && 'A' <= fnDecl.Name.Name[0] && fnDecl.Name.Name[0] <= 'Z' {
				a.prog.Decls = append(a.prog.Decls, fnDecl)
			}
		}
	}

	// Store module info
	a.modules[moduleName] = &ModuleInfo{
		Name:    moduleName,
		Path:    spec.Path,
		Program: prog,
		Symbols: exported,
	}

	// Store alias mapping if present
	if spec.Alias != "" {
		a.importAliases[spec.Alias] = moduleName
	}
}

// resolveImportPath converts an import path to a file path.
func (a *Analyzer) resolveImportPath(importPath string, currentFile string) string {
	// Local imports (start with ./ or ../)
	if len(importPath) > 0 && importPath[0] == '.' {
		// Resolve relative to the importing file's directory
		// Get directory of current file
		dir := ""
		for i := len(currentFile) - 1; i >= 0; i-- {
			if currentFile[i] == '/' {
				dir = currentFile[:i+1]
				break
			}
		}
		// Append import path and .ease extension
		return dir + importPath + ".ease"
	}

	// Stdlib imports (bare names like "io", "strings")
	// TODO: Implement stdlib path resolution
	// For now, look in stdlib/ directory
	return "stdlib/" + importPath + ".ease"
}

// parseModule parses a module file.
func (a *Analyzer) parseModule(filePath string) (*ast.Program, error) {
	// Read the file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %w", err)
	}

	// Create lexer
	l := lexer.New(string(content), filePath)

	// Create parser and parse
	p := parser.New(l)
	prog := p.ParseProgram()

	// Check for parse errors
	if len(p.Errors()) > 0 {
		return nil, fmt.Errorf("parse errors: %v", p.Errors()[0])
	}

	return prog, nil
}

// getModuleName extracts the module name from an import spec.
func (a *Analyzer) getModuleName(spec ast.ImportSpec) string {
	// If alias is provided, use it
	if spec.Alias != "" {
		return spec.Alias
	}

	// Otherwise use last path segment
	path := spec.Path
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

// collectDeclarations performs the first pass to collect all declarations.
func (a *Analyzer) collectDeclarations(prog *ast.Program) {
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *ast.FnDecl:
			a.collectFnDecl(d)
		case *ast.StructDecl:
			a.collectStructDecl(d)
		case *ast.EnumDecl:
			a.collectEnumDecl(d)
		case *ast.ConstDecl:
			a.collectConstDecl(d)
		case *ast.VarDecl:
			a.collectVarDecl(d)
		case *ast.ImplDecl:
			a.collectImplDecl(d)
		}
	}
}

func (a *Analyzer) collectFnDecl(fn *ast.FnDecl) {
	name := fn.Name.Name

	// Check for generic function (has type parameters)
	if len(fn.TypeParams) > 0 {
		// Store as generic template, don't register symbol yet
		if a.genericFns[name] != nil {
			a.error(fn.Pos(), "redeclaration of generic function '%s'", name)
			return
		}
		a.genericFns[name] = fn
		return
	}

	if a.table.Global.Lookup(name) != nil {
		a.error(fn.Pos(), "redeclaration of '%s'", name)
		return
	}

	// Build function type
	params := make([]*types.Param, len(fn.Params))
	for i, p := range fn.Params {
		params[i] = &types.Param{
			Name: p.Name.Name,
			Type: a.resolveType(p.Type),
		}
	}

	var retType types.Type = types.Typ[types.Unit]
	if fn.ReturnType != nil {
		retType = a.resolveType(fn.ReturnType)
	}

	fnType := types.NewFunction(params, retType)

	sym := symbols.NewSymbol(name, symbols.FuncSymbol, fnType, fn.Pos())
	a.table.Global.Define(sym)
	a.info.Defs[fn.Name] = sym
	a.fnDecls[name] = fn
}

func (a *Analyzer) collectStructDecl(s *ast.StructDecl) {
	name := s.Name.Name

	// Check for generic struct (has type parameters)
	if len(s.TypeParams) > 0 {
		// Store as generic template, don't register symbol yet
		if a.genericStructs[name] != nil {
			a.error(s.Pos(), "redeclaration of generic struct '%s'", name)
			return
		}
		a.genericStructs[name] = s
		return
	}

	if a.table.Global.Lookup(name) != nil {
		a.error(s.Pos(), "redeclaration of '%s'", name)
		return
	}

	// Build struct type
	fields := make([]*types.Field, len(s.Fields))
	for i, f := range s.Fields {
		fields[i] = &types.Field{
			Name: f.Name.Name,
			Type: a.resolveType(f.Type),
		}
	}

	structType := types.NewStruct(name, fields)

	sym := symbols.NewSymbol(name, symbols.TypeSymbol, structType, s.Pos())
	a.table.Global.Define(sym)
	a.info.Defs[s.Name] = sym
	a.structDecls[name] = s
}

func (a *Analyzer) collectEnumDecl(e *ast.EnumDecl) {
	name := e.Name.Name

	// Check for generic enum (has type parameters)
	if len(e.TypeParams) > 0 {
		// Store as generic template, don't register symbol yet
		if a.genericEnums[name] != nil {
			a.error(e.Pos(), "redeclaration of generic enum '%s'", name)
			return
		}
		a.genericEnums[name] = e
		return
	}

	if a.table.Global.Lookup(name) != nil {
		a.error(e.Pos(), "redeclaration of '%s'", name)
		return
	}

	// Build enum type
	variants := make([]*types.Variant, len(e.Variants))
	for i, v := range e.Variants {
		fields := make([]*types.Field, len(v.Fields))
		for j, f := range v.Fields {
			fields[j] = &types.Field{
				Name: f.Name.Name,
				Type: a.resolveType(f.Type),
			}
		}
		variants[i] = &types.Variant{
			Name:   v.Name.Name,
			Fields: fields,
		}
	}

	enumType := types.NewEnum(name, variants)

	sym := symbols.NewSymbol(name, symbols.TypeSymbol, enumType, e.Pos())
	a.table.Global.Define(sym)
	a.info.Defs[e.Name] = sym
	a.enumDecls[name] = e
}

func (a *Analyzer) collectConstDecl(c *ast.ConstDecl) {
	name := c.Name.Name
	if a.table.Global.Lookup(name) != nil {
		a.error(c.Pos(), "redeclaration of '%s'", name)
		return
	}

	// Analyze the constant value to determine type
	var constType types.Type
	if c.Type != nil {
		constType = a.resolveType(c.Type)
	} else {
		// Infer from value
		constType = a.analyzeExpr(c.Value)
	}

	sym := symbols.NewSymbol(name, symbols.ConstSymbol, constType, c.Pos())
	a.table.Global.Define(sym)
	a.info.Defs[c.Name] = sym
}

func (a *Analyzer) collectVarDecl(v *ast.VarDecl) {
	name := v.Name.Name
	if a.table.Global.Lookup(name) != nil {
		a.error(v.Pos(), "redeclaration of '%s'", name)
		return
	}

	// Analyze the initializer value to determine type
	var varType types.Type
	if v.Type != nil {
		varType = a.resolveType(v.Type)
		// Type check the initializer against declared type
		valueType := a.analyzeExpr(v.Value)
		if !types.IsAssignableTo(valueType, varType) {
			a.error(v.Pos(), "cannot assign %s to %s", valueType, varType)
		}
	} else {
		// Infer from value
		varType = a.analyzeExpr(v.Value)
	}

	// Use VarSymbol for mutable globals, ConstSymbol for immutable
	kind := symbols.VarSymbol
	if !v.Mutable {
		kind = symbols.ConstSymbol
	}

	sym := symbols.NewSymbol(name, kind, varType, v.Pos())
	sym.Mutable = v.Mutable  // Set mutability
	a.table.Global.Define(sym)
	a.info.Defs[v.Name] = sym
}

func (a *Analyzer) collectImplDecl(impl *ast.ImplDecl) {
	// Get the type name that we're implementing methods for
	var typeName string
	switch t := impl.Type.(type) {
	case *ast.NamedType:
		typeName = t.Name.Name
	default:
		a.error(impl.Pos(), "complex impl types not yet supported")
		return
	}

	// Resolve the type and set currentSelfType for Self resolution in method signatures
	sym := a.table.Global.Lookup(typeName)
	if sym != nil && sym.Kind == symbols.TypeSymbol {
		a.currentSelfType = sym.Type
	}

	// Initialize method maps for this type if not present
	if a.methods[typeName] == nil {
		a.methods[typeName] = make(map[string]*types.Function)
		a.methodDecls[typeName] = make(map[string]*ast.FnDecl)
	}

	// Collect each method
	for i := range impl.Methods {
		method := &impl.Methods[i]
		methodName := method.Name.Name

		// Check for duplicate method
		if a.methods[typeName][methodName] != nil {
			a.error(method.Pos(), "duplicate method '%s' for type '%s'", methodName, typeName)
			continue
		}

		// Build function type for the method
		params := make([]*types.Param, len(method.Params))
		for j, p := range method.Params {
			params[j] = &types.Param{
				Name: p.Name.Name,
				Type: a.resolveType(p.Type),
			}
		}

		var retType types.Type = types.Typ[types.Unit]
		if method.ReturnType != nil {
			retType = a.resolveType(method.ReturnType)
		}

		fnType := types.NewFunction(params, retType)

		a.methods[typeName][methodName] = fnType
		a.methodDecls[typeName][methodName] = method
	}

	a.currentSelfType = nil
}

// resolveType converts an AST type to a types.Type.
func (a *Analyzer) resolveType(t ast.Type) types.Type {
	if t == nil {
		return types.Typ[types.Unit]
	}

	switch t := t.(type) {
	case *ast.NamedType:
		name := t.Name.Name

		// Handle Self type in impl blocks
		if name == "Self" && a.currentSelfType != nil {
			return a.currentSelfType
		}

		// Check for type parameter in scope
		if tp, ok := a.typeParamScope[name]; ok {
			return tp
		}

		// Check for builtin types
		if basic := types.LookupBasicType(name); basic != nil {
			return basic
		}

		// Check if this is a generic type with type arguments
		if len(t.TypeArgs) > 0 {
			return a.instantiateGenericType(name, t.TypeArgs, t.Pos())
		}

		// Check if using a generic type without type arguments
		if _, ok := a.genericEnums[name]; ok {
			a.error(t.Pos(), "generic type '%s' requires type arguments", name)
			return types.Typ[types.Invalid]
		}
		if _, ok := a.genericStructs[name]; ok {
			a.error(t.Pos(), "generic type '%s' requires type arguments", name)
			return types.Typ[types.Invalid]
		}

		// Look up user-defined type
		sym := a.table.Resolve(name)
		if sym == nil {
			a.error(t.Pos(), "undefined type '%s'", name)
			return types.Typ[types.Invalid]
		}
		if sym.Kind != symbols.TypeSymbol {
			a.error(t.Pos(), "'%s' is not a type", name)
			return types.Typ[types.Invalid]
		}
		return sym.Type

	case *ast.UnitType:
		return types.Typ[types.Unit]

	case *ast.ArrayType:
		elem := a.resolveType(t.Element)
		// For now, assume size is an integer literal
		var size int64 = 0
		if lit, ok := t.Size.(*ast.IntLit); ok {
			size = lit.Value
		}
		return types.NewArray(elem, size)

	case *ast.SliceType:
		elem := a.resolveType(t.Element)
		return types.NewSlice(elem)

	case *ast.RefType:
		elem := a.resolveType(t.Type)
		return types.NewPointer(elem, t.Mutable)

	case *ast.TupleType:
		elems := make([]types.Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = a.resolveType(e)
		}
		return types.NewTuple(elems)

	case *ast.FnType:
		params := make([]*types.Param, len(t.Params))
		for i, p := range t.Params {
			params[i] = &types.Param{Type: a.resolveType(p)}
		}
		var ret types.Type = types.Typ[types.Unit]
		if t.ReturnType != nil {
			ret = a.resolveType(t.ReturnType)
		}
		return types.NewFunction(params, ret)

	case *ast.ChanType:
		elem := a.resolveType(t.Element)
		return types.NewChannel(elem)

	case *ast.MapType:
		keyType := a.resolveType(t.Key)
		if !types.IsValidMapKey(keyType) {
			a.error(t.Pos(), "invalid map key type: %s", keyType)
		}
		valueType := a.resolveType(t.Value)
		return types.NewMap(keyType, valueType)

	default:
		a.error(t.Pos(), "unsupported type")
		return types.Typ[types.Invalid]
	}
}

// resolveTypeWithSubstitution resolves an AST type, directly substituting type parameter
// names with concrete types from the provided map.
func (a *Analyzer) resolveTypeWithSubstitution(t ast.Type, subst map[string]types.Type) types.Type {
	if t == nil {
		return types.Typ[types.Unit]
	}

	switch t := t.(type) {
	case *ast.NamedType:
		name := t.Name.Name

		// Check if this is a type parameter to substitute
		if concrete, ok := subst[name]; ok {
			return concrete
		}

		// Check for builtin types
		if basic := types.LookupBasicType(name); basic != nil {
			return basic
		}

		// Handle generic types with type arguments (e.g., Option<T>)
		if len(t.TypeArgs) > 0 {
			// Resolve type arguments with substitution
			typeArgs := make([]types.Type, len(t.TypeArgs))
			for i, arg := range t.TypeArgs {
				typeArgs[i] = a.resolveTypeWithSubstitution(arg, subst)
			}

			// Generate mangled name with concrete types
			mangledName := types.MangledName(name, typeArgs)

			// Check if already instantiated
			if cached, ok := a.instantiations[mangledName]; ok {
				return cached
			}

			// Instantiate the generic type
			if enumDecl, ok := a.genericEnums[name]; ok {
				return a.instantiateGenericEnumWithTypes(enumDecl, typeArgs, mangledName, t.Pos())
			}
			if structDecl, ok := a.genericStructs[name]; ok {
				return a.instantiateGenericStructWithTypes(structDecl, typeArgs, mangledName, t.Pos())
			}
		}

		// Look up user-defined type
		sym := a.table.Resolve(name)
		if sym == nil {
			a.error(t.Pos(), "undefined type '%s'", name)
			return types.Typ[types.Invalid]
		}
		if sym.Kind != symbols.TypeSymbol {
			a.error(t.Pos(), "'%s' is not a type", name)
			return types.Typ[types.Invalid]
		}
		return sym.Type

	case *ast.UnitType:
		return types.Typ[types.Unit]

	case *ast.ArrayType:
		elem := a.resolveTypeWithSubstitution(t.Element, subst)
		var size int64 = 0
		if lit, ok := t.Size.(*ast.IntLit); ok {
			size = lit.Value
		}
		return types.NewArray(elem, size)

	case *ast.SliceType:
		elem := a.resolveTypeWithSubstitution(t.Element, subst)
		return types.NewSlice(elem)

	case *ast.RefType:
		elem := a.resolveTypeWithSubstitution(t.Type, subst)
		return types.NewPointer(elem, t.Mutable)

	case *ast.TupleType:
		elems := make([]types.Type, len(t.Elements))
		for i, e := range t.Elements {
			elems[i] = a.resolveTypeWithSubstitution(e, subst)
		}
		return types.NewTuple(elems)

	case *ast.FnType:
		params := make([]*types.Param, len(t.Params))
		for i, p := range t.Params {
			params[i] = &types.Param{Type: a.resolveTypeWithSubstitution(p, subst)}
		}
		var ret types.Type = types.Typ[types.Unit]
		if t.ReturnType != nil {
			ret = a.resolveTypeWithSubstitution(t.ReturnType, subst)
		}
		return types.NewFunction(params, ret)

	case *ast.ChanType:
		elem := a.resolveTypeWithSubstitution(t.Element, subst)
		return types.NewChannel(elem)

	case *ast.MapType:
		keyType := a.resolveTypeWithSubstitution(t.Key, subst)
		valueType := a.resolveTypeWithSubstitution(t.Value, subst)
		return types.NewMap(keyType, valueType)

	default:
		a.error(t.Pos(), "unsupported type")
		return types.Typ[types.Invalid]
	}
}

// instantiateGenericEnumWithTypes creates a specialized enum type given concrete type arguments.
func (a *Analyzer) instantiateGenericEnumWithTypes(decl *ast.EnumDecl, typeArgs []types.Type, mangledName string, pos token.Position) types.Type {
	// Verify type argument count
	if len(typeArgs) != len(decl.TypeParams) {
		a.error(pos, "wrong number of type arguments for '%s': expected %d, got %d",
			decl.Name.Name, len(decl.TypeParams), len(typeArgs))
		return types.Typ[types.Invalid]
	}

	// Create substitution map
	subst := make(map[string]types.Type)
	for i, tp := range decl.TypeParams {
		subst[tp.Name.Name] = typeArgs[i]
	}

	// Build specialized variants
	variants := make([]*types.Variant, len(decl.Variants))
	for i, v := range decl.Variants {
		fields := make([]*types.Field, len(v.Fields))
		for j, f := range v.Fields {
			fieldType := a.resolveTypeWithSubstitution(f.Type, subst)
			fields[j] = &types.Field{
				Name: f.Name.Name,
				Type: fieldType,
			}
		}
		variants[i] = &types.Variant{
			Name:   v.Name.Name,
			Fields: fields,
		}
	}

	// Create the specialized enum type
	enumType := types.NewEnum(mangledName, variants)

	// Cache and register in symbol table
	a.instantiations[mangledName] = enumType
	sym := symbols.NewSymbol(mangledName, symbols.TypeSymbol, enumType, pos)
	a.table.Global.Define(sym)

	return enumType
}

// instantiateGenericStructWithTypes creates a specialized struct type given concrete type arguments.
func (a *Analyzer) instantiateGenericStructWithTypes(decl *ast.StructDecl, typeArgs []types.Type, mangledName string, pos token.Position) types.Type {
	// Verify type argument count
	if len(typeArgs) != len(decl.TypeParams) {
		a.error(pos, "wrong number of type arguments for '%s': expected %d, got %d",
			decl.Name.Name, len(decl.TypeParams), len(typeArgs))
		return types.Typ[types.Invalid]
	}

	// Create substitution map
	subst := make(map[string]types.Type)
	for i, tp := range decl.TypeParams {
		subst[tp.Name.Name] = typeArgs[i]
	}

	// Build specialized fields
	fields := make([]*types.Field, len(decl.Fields))
	for i, f := range decl.Fields {
		fieldType := a.resolveTypeWithSubstitution(f.Type, subst)
		fields[i] = &types.Field{
			Name: f.Name.Name,
			Type: fieldType,
		}
	}

	// Create the specialized struct type
	structType := types.NewStruct(mangledName, fields)

	// Cache and register in symbol table
	a.instantiations[mangledName] = structType
	sym := symbols.NewSymbol(mangledName, symbols.TypeSymbol, structType, pos)
	a.table.Global.Define(sym)

	return structType
}

// instantiateGenericType creates a specialized version of a generic type.
func (a *Analyzer) instantiateGenericType(name string, astTypeArgs []ast.Type, pos token.Position) types.Type {
	// Resolve all type arguments to concrete types
	typeArgs := make([]types.Type, len(astTypeArgs))
	for i, arg := range astTypeArgs {
		typeArgs[i] = a.resolveType(arg)
	}

	// Generate mangled name
	mangledName := types.MangledName(name, typeArgs)

	// Check instantiation cache
	if cached, ok := a.instantiations[mangledName]; ok {
		return cached
	}

	// Try to instantiate as enum
	if enumDecl, ok := a.genericEnums[name]; ok {
		return a.instantiateGenericEnum(enumDecl, typeArgs, mangledName, pos)
	}

	// Try to instantiate as struct
	if structDecl, ok := a.genericStructs[name]; ok {
		return a.instantiateGenericStruct(structDecl, typeArgs, mangledName, pos)
	}

	a.error(pos, "undefined generic type '%s'", name)
	return types.Typ[types.Invalid]
}

// instantiateGenericEnum creates a specialized enum type.
func (a *Analyzer) instantiateGenericEnum(decl *ast.EnumDecl, typeArgs []types.Type, mangledName string, pos token.Position) types.Type {
	// Verify type argument count
	if len(typeArgs) != len(decl.TypeParams) {
		a.error(pos, "wrong number of type arguments for '%s': expected %d, got %d",
			decl.Name.Name, len(decl.TypeParams), len(typeArgs))
		return types.Typ[types.Invalid]
	}

	// Build type parameter map
	typeParams := make([]*types.TypeParam, len(decl.TypeParams))
	for i, tp := range decl.TypeParams {
		typeParams[i] = types.NewTypeParam(tp.Name.Name, a.typeParamIDCounter)
		a.typeParamIDCounter++
	}
	typeMap := types.BuildTypeMap(typeParams, typeArgs)

	// Enter type param scope for resolving variant field types
	oldScope := a.typeParamScope
	a.typeParamScope = make(map[string]*types.TypeParam)
	for i, tp := range decl.TypeParams {
		a.typeParamScope[tp.Name.Name] = typeParams[i]
	}

	// Build specialized variants
	variants := make([]*types.Variant, len(decl.Variants))
	for i, v := range decl.Variants {
		fields := make([]*types.Field, len(v.Fields))
		for j, f := range v.Fields {
			fieldType := a.resolveType(f.Type)
			// Substitute type parameters with concrete types
			fieldType = types.Substitute(fieldType, typeMap)
			fields[j] = &types.Field{
				Name: f.Name.Name,
				Type: fieldType,
			}
		}
		variants[i] = &types.Variant{
			Name:   v.Name.Name,
			Fields: fields,
		}
	}

	// Restore type param scope
	a.typeParamScope = oldScope

	// Create the specialized enum type
	enumType := types.NewEnum(mangledName, variants)

	// Cache and register in symbol table
	a.instantiations[mangledName] = enumType
	sym := symbols.NewSymbol(mangledName, symbols.TypeSymbol, enumType, pos)
	a.table.Global.Define(sym)

	return enumType
}

// instantiateGenericStruct creates a specialized struct type.
func (a *Analyzer) instantiateGenericStruct(decl *ast.StructDecl, typeArgs []types.Type, mangledName string, pos token.Position) types.Type {
	// Verify type argument count
	if len(typeArgs) != len(decl.TypeParams) {
		a.error(pos, "wrong number of type arguments for '%s': expected %d, got %d",
			decl.Name.Name, len(decl.TypeParams), len(typeArgs))
		return types.Typ[types.Invalid]
	}

	// Build type parameter map
	typeParams := make([]*types.TypeParam, len(decl.TypeParams))
	for i, tp := range decl.TypeParams {
		typeParams[i] = types.NewTypeParam(tp.Name.Name, a.typeParamIDCounter)
		a.typeParamIDCounter++
	}
	typeMap := types.BuildTypeMap(typeParams, typeArgs)

	// Enter type param scope for resolving field types
	oldScope := a.typeParamScope
	a.typeParamScope = make(map[string]*types.TypeParam)
	for i, tp := range decl.TypeParams {
		a.typeParamScope[tp.Name.Name] = typeParams[i]
	}

	// Build specialized fields
	fields := make([]*types.Field, len(decl.Fields))
	for i, f := range decl.Fields {
		fieldType := a.resolveType(f.Type)
		// Substitute type parameters with concrete types
		fieldType = types.Substitute(fieldType, typeMap)
		fields[i] = &types.Field{
			Name: f.Name.Name,
			Type: fieldType,
		}
	}

	// Restore type param scope
	a.typeParamScope = oldScope

	// Create the specialized struct type
	structType := types.NewStruct(mangledName, fields)

	// Cache and register in symbol table
	a.instantiations[mangledName] = structType
	sym := symbols.NewSymbol(mangledName, symbols.TypeSymbol, structType, pos)
	a.table.Global.Define(sym)

	return structType
}

// analyzeDecl analyzes a declaration.
func (a *Analyzer) analyzeDecl(decl ast.Decl) {
	switch d := decl.(type) {
	case *ast.FnDecl:
		a.analyzeFnDecl(d)
	case *ast.StructDecl:
		// Already analyzed during collection
	case *ast.EnumDecl:
		// Already analyzed during collection
	case *ast.ConstDecl:
		// Already analyzed during collection
	case *ast.VarDecl:
		// Already analyzed during collection
	case *ast.ImplDecl:
		a.analyzeImplDecl(d)
	case *ast.TestDecl:
		a.analyzeTestDecl(d)
	}
}

func (a *Analyzer) analyzeFnDecl(fn *ast.FnDecl) {
	// Get the function symbol
	sym := a.table.Global.Lookup(fn.Name.Name)
	if sym == nil {
		return // error already reported
	}

	fnType := sym.Type.(*types.Function)
	a.analyzeFnDeclWithType(fn, fnType)
}

// analyzeFnDeclWithType analyzes a function given its already-resolved type.
// This is used for monomorphized functions where the type has been computed separately.
func (a *Analyzer) analyzeFnDeclWithType(fn *ast.FnDecl, fnType *types.Function) {
	// Save and restore currentFunc in case we're analyzing during another function
	savedFunc := a.currentFunc
	a.currentFunc = fnType

	// Enter function scope
	a.table.EnterScope("function:" + fn.Name.Name)

	// Define parameters
	for i, p := range fn.Params {
		paramSym := symbols.NewSymbol(p.Name.Name, symbols.ParamSymbol, fnType.Params[i].Type, p.Name.Pos())
		if !a.table.Define(paramSym) {
			a.error(p.Name.Pos(), "redeclaration of parameter '%s'", p.Name.Name)
		}
		a.info.Defs[p.Name] = paramSym
	}

	// Analyze body
	if fn.Body != nil {
		a.analyzeBlock(fn.Body)
	}

	a.table.ExitScope()
	a.currentFunc = savedFunc
}

func (a *Analyzer) analyzeImplDecl(impl *ast.ImplDecl) {
	// Get the type name
	var typeName string
	switch t := impl.Type.(type) {
	case *ast.NamedType:
		typeName = t.Name.Name
	default:
		return // error already reported
	}

	// Resolve the type and set currentSelfType
	sym := a.table.Resolve(typeName)
	if sym != nil && sym.Kind == symbols.TypeSymbol {
		a.currentSelfType = sym.Type
	}

	// Analyze each method body
	for i := range impl.Methods {
		method := &impl.Methods[i]
		methodType := a.methods[typeName][method.Name.Name]
		if methodType == nil {
			continue // error already reported
		}

		a.currentFunc = methodType

		// Enter method scope
		a.table.EnterScope("method:" + typeName + "." + method.Name.Name)

		// Define parameters (including self)
		for j, p := range method.Params {
			paramSym := symbols.NewSymbol(p.Name.Name, symbols.ParamSymbol, methodType.Params[j].Type, p.Name.Pos())
			if !a.table.Define(paramSym) {
				a.error(p.Name.Pos(), "redeclaration of parameter '%s'", p.Name.Name)
			}
			a.info.Defs[p.Name] = paramSym
		}

		// Analyze body
		if method.Body != nil {
			a.analyzeBlock(method.Body)
		}

		a.table.ExitScope()
		a.currentFunc = nil
	}

	a.currentSelfType = nil
}

func (a *Analyzer) analyzeTestDecl(t *ast.TestDecl) {
	// Tests have no parameters and return Result<(), Error>
	// For simplicity, treat them like void functions
	a.currentFunc = types.NewFunction(nil, types.Typ[types.Unit])

	a.table.EnterScope("test:" + t.Description.Value)

	if t.Body != nil {
		a.analyzeBlock(t.Body)
	}

	a.table.ExitScope()
	a.currentFunc = nil
}

func (a *Analyzer) analyzeBlock(block *ast.BlockStmt) types.Type {
	var lastExprType types.Type

	for i, stmt := range block.Stmts {
		// Check if this is the last statement and it's an ExprStmt
		// If so, use its type as the block's type
		if i == len(block.Stmts)-1 {
			if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
				lastExprType = a.analyzeExpr(exprStmt.Expr)
				continue
			}
		}
		a.analyzeStmt(stmt)
	}

	if block.Expr != nil {
		return a.analyzeExpr(block.Expr)
	}

	// If the last statement was an expression statement, return its type
	// This handles cases like `{ a }` in if expressions where we want the block to evaluate to `a`
	if lastExprType != nil {
		return lastExprType
	}

	return types.Typ[types.Unit]
}

func (a *Analyzer) analyzeStmt(stmt ast.Stmt) {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		a.analyzeLetStmt(s)
	case *ast.ExprStmt:
		a.analyzeExpr(s.Expr)
	case *ast.ReturnStmt:
		a.analyzeReturnStmt(s)
	case *ast.IfStmt:
		a.analyzeIfStmt(s)
	case *ast.ForStmt:
		a.analyzeForStmt(s)
	case *ast.MatchStmt:
		a.analyzeMatchStmt(s)
	case *ast.BreakStmt:
		// Break is valid in loops
	case *ast.ContinueStmt:
		// Continue is valid in loops
	case *ast.BlockStmt:
		a.table.EnterScope("block")
		a.analyzeBlock(s)
		a.table.ExitScope()
	}
}

func (a *Analyzer) analyzeLetStmt(s *ast.LetStmt) {
	var varType types.Type

	// If there's a type annotation, use it
	if s.Type != nil {
		varType = a.resolveType(s.Type)
	}

	// If there's a value, analyze it and use its type or check compatibility
	if s.Value != nil {
		valType := a.analyzeExpr(s.Value)
		if varType == nil {
			varType = valType
		} else if !types.IsAssignableTo(valType, varType) {
			a.error(s.Value.Pos(), "cannot assign %s to %s", valType, varType)
		}
	}

	if varType == nil {
		a.error(s.Pos(), "cannot infer type for variable")
		varType = types.Typ[types.Invalid]
	}

	// Handle the pattern
	switch p := s.Pattern.(type) {
	case *ast.IdentPattern:
		sym := symbols.NewSymbol(p.Name.Name, symbols.VarSymbol, varType, p.Name.Pos())
		sym.Mutable = s.Mutable
		if !a.table.Define(sym) {
			a.error(p.Name.Pos(), "redeclaration of '%s'", p.Name.Name)
		}
		a.info.Defs[p.Name] = sym
	default:
		// For simplicity, only handle simple identifier patterns for now
		a.error(s.Pos(), "complex patterns not yet supported")
	}
}

func (a *Analyzer) analyzeReturnStmt(s *ast.ReturnStmt) {
	if a.currentFunc == nil {
		a.error(s.Pos(), "return outside of function")
		return
	}

	if s.Value == nil {
		if !a.currentFunc.Result.Equals(types.Typ[types.Unit]) {
			a.error(s.Pos(), "function expects return value of type %s", a.currentFunc.Result)
		}
	} else {
		valType := a.analyzeExpr(s.Value)
		if !types.IsAssignableTo(valType, a.currentFunc.Result) {
			a.error(s.Value.Pos(), "cannot return %s as %s", valType, a.currentFunc.Result)
		}
	}
}

func (a *Analyzer) analyzeIfStmt(s *ast.IfStmt) {
	condType := a.analyzeExpr(s.Cond)
	if !condType.Equals(types.Typ[types.Bool]) {
		a.error(s.Cond.Pos(), "condition must be bool, got %s", condType)
	}

	a.table.EnterScope("if:then")
	a.analyzeBlock(s.Then)
	a.table.ExitScope()

	if s.Else != nil {
		a.table.EnterScope("if:else")
		a.analyzeStmt(s.Else)
		a.table.ExitScope()
	}
}

func (a *Analyzer) analyzeForStmt(s *ast.ForStmt) {
	a.table.EnterScope("for")

	// Condition-based loop
	if s.Cond != nil {
		condType := a.analyzeExpr(s.Cond)
		if !condType.Equals(types.Typ[types.Bool]) {
			a.error(s.Cond.Pos(), "condition must be bool, got %s", condType)
		}
	}

	// Range-based loop
	if s.Pattern != nil && s.Iter != nil {
		iterType := a.analyzeExpr(s.Iter)

		// Determine element type from iterator
		var elemType types.Type
		switch t := iterType.Underlying().(type) {
		case *types.Array:
			elemType = t.Elem
		case *types.Slice:
			elemType = t.Elem
		default:
			// For range expressions like 0..10, use int
			if _, ok := s.Iter.(*ast.RangeExpr); ok {
				elemType = types.Typ[types.Int]
			} else {
				a.error(s.Iter.Pos(), "cannot iterate over %s", iterType)
				elemType = types.Typ[types.Invalid]
			}
		}

		// Bind the pattern
		if ident, ok := s.Pattern.(*ast.IdentPattern); ok {
			sym := symbols.NewSymbol(ident.Name.Name, symbols.VarSymbol, elemType, ident.Name.Pos())
			a.table.Define(sym)
			a.info.Defs[ident.Name] = sym
		}
	}

	a.analyzeBlock(s.Body)
	a.table.ExitScope()
}

func (a *Analyzer) analyzeExpr(expr ast.Expr) types.Type {
	if expr == nil {
		return types.Typ[types.Unit]
	}

	var typ types.Type

	switch e := expr.(type) {
	case *ast.Ident:
		typ = a.analyzeIdent(e)
	case *ast.IntLit:
		typ = types.Typ[types.Int]
	case *ast.FloatLit:
		typ = types.Typ[types.Float64]
	case *ast.StringLit:
		typ = types.Typ[types.String]
	case *ast.BoolLit:
		typ = types.Typ[types.Bool]
	case *ast.CharLit:
		typ = types.Typ[types.Int] // character code as int for easy comparison
	case *ast.BinaryExpr:
		typ = a.analyzeBinaryExpr(e)
	case *ast.UnaryExpr:
		typ = a.analyzeUnaryExpr(e)
	case *ast.CallExpr:
		typ = a.analyzeCallExpr(e)
	case *ast.FieldExpr:
		typ = a.analyzeFieldExpr(e)
	case *ast.IndexExpr:
		typ = a.analyzeIndexExpr(e)
	case *ast.SliceExpr:
		typ = a.analyzeSliceExpr(e)
	case *ast.IfExpr:
		typ = a.analyzeIfExpr(e)
	case *ast.BlockExpr:
		a.table.EnterScope("block")
		typ = a.analyzeBlock(e.Block)
		a.table.ExitScope()
	case *ast.RangeExpr:
		typ = a.analyzeRangeExpr(e)
	case *ast.StructExpr:
		typ = a.analyzeStructExpr(e)
	case *ast.ArrayExpr:
		typ = a.analyzeArrayExpr(e)
	case *ast.MapExpr:
		typ = a.analyzeMapExpr(e)
	case *ast.TupleExpr:
		typ = a.analyzeTupleExpr(e)
	case *ast.MethodExpr:
		typ = a.analyzeMethodExpr(e)
	case *ast.PathExpr:
		typ = a.analyzePathExpr(e)
	case *ast.MatchExpr:
		typ = a.analyzeMatchExpr(e)
	case *ast.TryExpr:
		typ = a.analyzeTryExpr(e)
	default:
		a.error(expr.Pos(), "unsupported expression type %T", expr)
		typ = types.Typ[types.Invalid]
	}

	a.info.Types[expr] = typ
	return typ
}

func (a *Analyzer) analyzeIdent(ident *ast.Ident) types.Type {
	sym := a.table.Resolve(ident.Name)
	if sym == nil {
		a.error(ident.Pos(), "undefined: %s", ident.Name)
		return types.Typ[types.Invalid]
	}
	sym.Used = true
	a.info.Uses[ident] = sym
	return sym.Type
}

func (a *Analyzer) analyzeBinaryExpr(e *ast.BinaryExpr) types.Type {
	leftType := a.analyzeExpr(e.Left)
	rightType := a.analyzeExpr(e.Right)

	switch e.Op.Type {
	case token.Assign:
		// Assignment operator
		// Check that left side is assignable
		if !a.checkAssignable(e.Left) {
			return types.Typ[types.Invalid]
		}
		// Check type compatibility
		if !types.IsAssignableTo(rightType, leftType) {
			a.error(e.Right.Pos(), "cannot assign %s to %s", rightType, leftType)
			return types.Typ[types.Invalid]
		}
		return types.Typ[types.Unit]

	case token.Plus:
		// Plus: arithmetic or string concatenation
		if isString(leftType) {
			if !isString(rightType) {
				a.error(e.Right.Pos(), "cannot concatenate string with %s", rightType)
				return types.Typ[types.Invalid]
			}
			return types.Typ[types.String]
		}
		if !isNumeric(leftType) {
			a.error(e.Left.Pos(), "operator %s not defined for %s", e.Op.Literal, leftType)
			return types.Typ[types.Invalid]
		}
		if !leftType.Equals(rightType) {
			a.error(e.Right.Pos(), "mismatched types: %s and %s", leftType, rightType)
			return types.Typ[types.Invalid]
		}
		return leftType

	case token.Minus, token.Star, token.Slash, token.Percent:
		// Arithmetic operators
		if !isNumeric(leftType) {
			a.error(e.Left.Pos(), "operator %s not defined for %s", e.Op.Literal, leftType)
			return types.Typ[types.Invalid]
		}
		if !leftType.Equals(rightType) {
			a.error(e.Right.Pos(), "mismatched types: %s and %s", leftType, rightType)
			return types.Typ[types.Invalid]
		}
		return leftType

	case token.Equal, token.NotEqual:
		// Equality operators
		if !types.IsComparable(leftType) {
			a.error(e.Left.Pos(), "operator %s not defined for %s", e.Op.Literal, leftType)
			return types.Typ[types.Invalid]
		}
		if !leftType.Equals(rightType) {
			a.error(e.Right.Pos(), "mismatched types: %s and %s", leftType, rightType)
			return types.Typ[types.Invalid]
		}
		return types.Typ[types.Bool]

	case token.Less, token.LessEqual, token.Greater, token.GreaterEqual:
		// Comparison operators
		if !types.IsOrdered(leftType) {
			a.error(e.Left.Pos(), "operator %s not defined for %s", e.Op.Literal, leftType)
			return types.Typ[types.Invalid]
		}
		if !leftType.Equals(rightType) {
			a.error(e.Right.Pos(), "mismatched types: %s and %s", leftType, rightType)
			return types.Typ[types.Invalid]
		}
		return types.Typ[types.Bool]

	case token.LogicalAnd, token.LogicalOr:
		// Logical operators
		if !leftType.Equals(types.Typ[types.Bool]) {
			a.error(e.Left.Pos(), "operator %s requires bool, got %s", e.Op.Literal, leftType)
			return types.Typ[types.Invalid]
		}
		if !rightType.Equals(types.Typ[types.Bool]) {
			a.error(e.Right.Pos(), "operator %s requires bool, got %s", e.Op.Literal, rightType)
			return types.Typ[types.Invalid]
		}
		return types.Typ[types.Bool]

	case token.Ampersand, token.Pipe, token.Caret:
		// Bitwise operators
		if !isInteger(leftType) {
			a.error(e.Left.Pos(), "operator %s not defined for %s", e.Op.Literal, leftType)
			return types.Typ[types.Invalid]
		}
		if !leftType.Equals(rightType) {
			a.error(e.Right.Pos(), "mismatched types: %s and %s", leftType, rightType)
			return types.Typ[types.Invalid]
		}
		return leftType

	default:
		a.error(e.Pos(), "unknown binary operator: %s", e.Op.Literal)
		return types.Typ[types.Invalid]
	}
}

func (a *Analyzer) analyzeUnaryExpr(e *ast.UnaryExpr) types.Type {
	rightType := a.analyzeExpr(e.Right)

	switch e.Op.Type {
	case token.Minus:
		if !isNumeric(rightType) {
			a.error(e.Right.Pos(), "operator - not defined for %s", rightType)
			return types.Typ[types.Invalid]
		}
		return rightType

	case token.Not:
		if !rightType.Equals(types.Typ[types.Bool]) {
			a.error(e.Right.Pos(), "operator ! requires bool, got %s", rightType)
			return types.Typ[types.Invalid]
		}
		return types.Typ[types.Bool]

	case token.Tilde:
		if !isInteger(rightType) {
			a.error(e.Right.Pos(), "operator ~ not defined for %s", rightType)
			return types.Typ[types.Invalid]
		}
		return rightType

	default:
		a.error(e.Pos(), "unknown unary operator: %s", e.Op.Literal)
		return types.Typ[types.Invalid]
	}
}

func (a *Analyzer) analyzeCallExpr(e *ast.CallExpr) types.Type {
	// Handle builtin functions specially
	if ident, ok := e.Func.(*ast.Ident); ok {
		switch ident.Name {
		case "len":
			return a.analyzeLenBuiltin(e)
		case "cap":
			return a.analyzeCapBuiltin(e)
		case "push":
			return a.analyzePushBuiltin(e)
		case "print":
			return a.analyzePrintBuiltin(e)
		case "poke", "mem_set":
			// poke(addr, value) and mem_set(addr, value, count) return ()
			for _, arg := range e.Args {
				a.analyzeExpr(arg)
			}
			return types.Typ[types.Unit]
		case "peek", "str_len":
			// peek(addr) and str_len(s) return int
			for _, arg := range e.Args {
				a.analyzeExpr(arg)
			}
			return types.Typ[types.Int]
		}

		// Check if this is a call to a generic function
		if genericFn, ok := a.genericFns[ident.Name]; ok {
			return a.analyzeGenericCall(genericFn, e)
		}
	}

	// Handle package-qualified builtins (e.g., os.ReadFile, os.WriteFile)
	if field, ok := e.Func.(*ast.FieldExpr); ok {
		if pkg, ok := field.Expr.(*ast.Ident); ok && pkg.Name == "os" {
			switch field.Field.Name {
			case "ReadFile":
				return a.analyzeOsReadFileCall(e)
			case "WriteFile":
				return a.analyzeOsWriteFileCall(e)
			}
		}
	}

	fnType := a.analyzeExpr(e.Func)

	fn, ok := fnType.(*types.Function)
	if !ok {
		a.error(e.Func.Pos(), "cannot call non-function %s", fnType)
		return types.Typ[types.Invalid]
	}

	// Check argument count
	if len(e.Args) != len(fn.Params) && !fn.IsVariadic {
		a.error(e.Pos(), "wrong number of arguments: expected %d, got %d", len(fn.Params), len(e.Args))
		return fn.Result
	}

	// Check argument types
	for i, arg := range e.Args {
		argType := a.analyzeExpr(arg)
		if i < len(fn.Params) {
			if !types.IsAssignableTo(argType, fn.Params[i].Type) {
				a.error(arg.Pos(), "cannot use %s as %s in argument to function", argType, fn.Params[i].Type)
			}
		}
	}

	return fn.Result
}

// analyzeGenericCall handles calls to generic functions by inferring type arguments
// and instantiating a specialized version of the function.
func (a *Analyzer) analyzeGenericCall(genericFn *ast.FnDecl, call *ast.CallExpr) types.Type {
	// First, analyze all argument types
	argTypes := make([]types.Type, len(call.Args))
	for i, arg := range call.Args {
		argTypes[i] = a.analyzeExpr(arg)
	}

	// Check if explicit type arguments were provided
	var typeArgs []types.Type
	if len(call.TypeArgs) > 0 {
		// Explicit type arguments
		typeArgs = make([]types.Type, len(call.TypeArgs))
		for i, ta := range call.TypeArgs {
			typeArgs[i] = a.resolveType(ta)
		}
	} else {
		// Infer type arguments from call arguments
		typeArgs = a.inferTypeArgs(genericFn, argTypes, call.Pos())
		if typeArgs == nil {
			return types.Typ[types.Invalid]
		}
	}

	// Verify type argument count
	if len(typeArgs) != len(genericFn.TypeParams) {
		a.error(call.Pos(), "wrong number of type arguments for '%s': expected %d, got %d",
			genericFn.Name.Name, len(genericFn.TypeParams), len(typeArgs))
		return types.Typ[types.Invalid]
	}

	// Generate mangled name and check cache
	mangledName := types.MangledName(genericFn.Name.Name, typeArgs)

	// Record the mangled name for the IR builder
	a.info.GenericCalls[call] = mangledName

	// Check if we already have this instantiation
	if sym := a.table.Global.Lookup(mangledName); sym != nil {
		if fnType, ok := sym.Type.(*types.Function); ok {
			// Verify argument types against instantiated function
			a.checkFunctionArgs(fnType, argTypes, call)
			return fnType.Result
		}
	}

	// Instantiate the generic function
	return a.instantiateGenericFunction(genericFn, typeArgs, argTypes, call)
}

// inferTypeArgs attempts to infer type arguments from the call argument types.
func (a *Analyzer) inferTypeArgs(genericFn *ast.FnDecl, argTypes []types.Type, pos token.Position) []types.Type {
	if len(argTypes) != len(genericFn.Params) {
		a.error(pos, "wrong number of arguments for '%s': expected %d, got %d",
			genericFn.Name.Name, len(genericFn.Params), len(argTypes))
		return nil
	}

	// Create type parameters for inference
	typeParams := make([]*types.TypeParam, len(genericFn.TypeParams))
	for i, tp := range genericFn.TypeParams {
		typeParams[i] = types.NewTypeParam(tp.Name.Name, a.typeParamIDCounter+i)
	}

	// Map from type param ID to inferred concrete type
	inferred := make(map[int]types.Type)

	// Set up type param scope temporarily to resolve parameter types
	oldScope := a.typeParamScope
	a.typeParamScope = make(map[string]*types.TypeParam)
	for i, tp := range genericFn.TypeParams {
		a.typeParamScope[tp.Name.Name] = typeParams[i]
	}

	// Try to match each argument type with the parameter type
	for i, param := range genericFn.Params {
		paramType := a.resolveType(param.Type)
		if !a.unifyTypes(paramType, argTypes[i], inferred) {
			// Could not unify - type inference failed
			a.typeParamScope = oldScope
			a.error(pos, "cannot infer type arguments for '%s'", genericFn.Name.Name)
			return nil
		}
	}

	a.typeParamScope = oldScope

	// Build result type args array
	result := make([]types.Type, len(typeParams))
	for i, tp := range typeParams {
		if concrete, ok := inferred[tp.ID]; ok {
			result[i] = concrete
		} else {
			a.error(pos, "cannot infer type argument '%s' for '%s'", tp.Name, genericFn.Name.Name)
			return nil
		}
	}

	return result
}

// unifyTypes attempts to match a parameterized type with a concrete type,
// recording type parameter bindings in the inferred map.
func (a *Analyzer) unifyTypes(paramType, argType types.Type, inferred map[int]types.Type) bool {
	switch pt := paramType.(type) {
	case *types.TypeParam:
		// If we've already inferred this type param, check consistency
		if existing, ok := inferred[pt.ID]; ok {
			return existing.Equals(argType)
		}
		// Record the inference
		inferred[pt.ID] = argType
		return true

	case *types.Basic:
		return pt.Equals(argType)

	case *types.Array:
		if at, ok := argType.(*types.Array); ok {
			return pt.Len == at.Len && a.unifyTypes(pt.Elem, at.Elem, inferred)
		}
		return false

	case *types.Slice:
		if st, ok := argType.(*types.Slice); ok {
			return a.unifyTypes(pt.Elem, st.Elem, inferred)
		}
		return false

	case *types.Pointer:
		if pt2, ok := argType.(*types.Pointer); ok {
			return pt.Mutable == pt2.Mutable && a.unifyTypes(pt.Elem, pt2.Elem, inferred)
		}
		return false

	case *types.Struct:
		if st, ok := argType.(*types.Struct); ok {
			// For named types, just compare names (monomorphized types have unique names)
			return pt.Name == st.Name
		}
		return false

	case *types.Enum:
		if et, ok := argType.(*types.Enum); ok {
			// Exact name match
			if pt.Name == et.Name {
				return true
			}
			// Check if arg is a monomorphized version of param (e.g., Option_int vs Option)
			// If param has type parameters in its fields, try to unify with the arg's fields
			if len(pt.Variants) == len(et.Variants) {
				allMatch := true
				for i, pv := range pt.Variants {
					ev := et.Variants[i]
					if pv.Name != ev.Name || len(pv.Fields) != len(ev.Fields) {
						allMatch = false
						break
					}
					for j, pf := range pv.Fields {
						ef := ev.Fields[j]
						if pf.Name != ef.Name || !a.unifyTypes(pf.Type, ef.Type, inferred) {
							allMatch = false
							break
						}
					}
					if !allMatch {
						break
					}
				}
				if allMatch {
					return true
				}
			}
		}
		return false

	case *types.Tuple:
		if tt, ok := argType.(*types.Tuple); ok {
			if len(pt.Elems) != len(tt.Elems) {
				return false
			}
			for i := range pt.Elems {
				if !a.unifyTypes(pt.Elems[i], tt.Elems[i], inferred) {
					return false
				}
			}
			return true
		}
		return false

	case *types.Function:
		if ft, ok := argType.(*types.Function); ok {
			if len(pt.Params) != len(ft.Params) {
				return false
			}
			for i := range pt.Params {
				if !a.unifyTypes(pt.Params[i].Type, ft.Params[i].Type, inferred) {
					return false
				}
			}
			if pt.Result != nil && ft.Result != nil {
				return a.unifyTypes(pt.Result, ft.Result, inferred)
			}
			return pt.Result == nil && ft.Result == nil
		}
		return false

	default:
		// For other types, require exact equality
		return paramType.Equals(argType)
	}
}

// instantiateGenericFunction creates and analyzes a specialized version of a generic function.
func (a *Analyzer) instantiateGenericFunction(genericFn *ast.FnDecl, typeArgs []types.Type, argTypes []types.Type, call *ast.CallExpr) types.Type {
	mangledName := types.MangledName(genericFn.Name.Name, typeArgs)

	// Create a direct mapping from type param names to concrete types
	// This avoids the intermediate TypeParam stage
	concreteTypeMap := make(map[string]types.Type)
	for i, tp := range genericFn.TypeParams {
		concreteTypeMap[tp.Name.Name] = typeArgs[i]
	}

	// Build the specialized function type using concrete type resolution
	params := make([]*types.Param, len(genericFn.Params))
	for i, p := range genericFn.Params {
		paramType := a.resolveTypeWithSubstitution(p.Type, concreteTypeMap)
		params[i] = &types.Param{
			Name: p.Name.Name,
			Type: paramType,
		}
	}

	var retType types.Type = types.Typ[types.Unit]
	if genericFn.ReturnType != nil {
		retType = a.resolveTypeWithSubstitution(genericFn.ReturnType, concreteTypeMap)
	}

	fnType := types.NewFunction(params, retType)

	// Register the specialized function in the symbol table
	sym := symbols.NewSymbol(mangledName, symbols.FuncSymbol, fnType, genericFn.Pos())
	a.table.Global.Define(sym)

	// Clone the generic function with specialized types using the Monomorphizer
	mono := NewMonomorphizer(a)
	clonedFn := mono.CloneFunction(genericFn, typeArgs, mangledName)

	// Add the cloned function to the program for IR generation
	if a.prog != nil {
		a.prog.Decls = append(a.prog.Decls, clonedFn)
	}

	// Store the cloned declaration and record that we defined it
	a.fnDecls[mangledName] = clonedFn
	a.info.Defs[clonedFn.Name] = sym

	// Verify argument types
	a.checkFunctionArgs(fnType, argTypes, call)

	// Analyze the cloned function body
	a.analyzeFnDeclWithType(clonedFn, fnType)

	return fnType.Result
}

// checkFunctionArgs verifies that argument types match parameter types.
func (a *Analyzer) checkFunctionArgs(fnType *types.Function, argTypes []types.Type, call *ast.CallExpr) {
	if len(argTypes) != len(fnType.Params) && !fnType.IsVariadic {
		a.error(call.Pos(), "wrong number of arguments: expected %d, got %d",
			len(fnType.Params), len(argTypes))
		return
	}

	for i, argType := range argTypes {
		if i < len(fnType.Params) {
			if !types.IsAssignableTo(argType, fnType.Params[i].Type) {
				a.error(call.Args[i].Pos(), "cannot use %s as %s in argument to function",
					argType, fnType.Params[i].Type)
			}
		}
	}
}

func (a *Analyzer) analyzeLenBuiltin(e *ast.CallExpr) types.Type {
	if len(e.Args) != 1 {
		a.error(e.Pos(), "len requires exactly 1 argument")
		return types.Typ[types.Int]
	}

	argType := a.analyzeExpr(e.Args[0])

	// len works on strings, arrays, slices, and maps
	switch argType.Underlying().(type) {
	case *types.Basic:
		if argType.Underlying().(*types.Basic).Kind != types.String {
			a.error(e.Args[0].Pos(), "len argument must be string, array, slice, or map; got %s", argType)
		}
	case *types.Array, *types.Slice, *types.Map:
		// OK
	default:
		a.error(e.Args[0].Pos(), "len argument must be string, array, slice, or map; got %s", argType)
	}

	return types.Typ[types.Int]
}

func (a *Analyzer) analyzeCapBuiltin(e *ast.CallExpr) types.Type {
	if len(e.Args) != 1 {
		a.error(e.Pos(), "cap requires exactly 1 argument")
		return types.Typ[types.Int]
	}

	argType := a.analyzeExpr(e.Args[0])

	// cap works on arrays and slices
	switch argType.Underlying().(type) {
	case *types.Array, *types.Slice:
		// OK
	default:
		a.error(e.Args[0].Pos(), "cap argument must be array or slice; got %s", argType)
	}

	return types.Typ[types.Int]
}

func (a *Analyzer) analyzePushBuiltin(e *ast.CallExpr) types.Type {
	if len(e.Args) != 2 {
		a.error(e.Pos(), "push requires exactly 2 arguments (array, element)")
		return types.Typ[types.Unit]
	}

	arrType := a.analyzeExpr(e.Args[0])
	elemType := a.analyzeExpr(e.Args[1])

	// First arg must be mutable array/slice reference
	var expectedElemType types.Type
	switch t := arrType.Underlying().(type) {
	case *types.Array:
		expectedElemType = t.Elem
	case *types.Slice:
		expectedElemType = t.Elem
	case *types.Pointer:
		// Pushing to pointer to array
		if arr, ok := t.Elem.(*types.Array); ok {
			expectedElemType = arr.Elem
		} else if slice, ok := t.Elem.(*types.Slice); ok {
			expectedElemType = slice.Elem
		} else {
			a.error(e.Args[0].Pos(), "push requires array or slice; got %s", arrType)
			return types.Typ[types.Unit]
		}
	default:
		a.error(e.Args[0].Pos(), "push requires array or slice; got %s", arrType)
		return types.Typ[types.Unit]
	}

	if expectedElemType != nil && !types.IsAssignableTo(elemType, expectedElemType) {
		a.error(e.Args[1].Pos(), "cannot push %s to array of %s", elemType, expectedElemType)
	}

	return types.Typ[types.Unit]
}

func (a *Analyzer) analyzePrintBuiltin(e *ast.CallExpr) types.Type {
	if len(e.Args) != 1 {
		a.error(e.Pos(), "print requires exactly 1 argument")
		return types.Typ[types.Unit]
	}

	argType := a.analyzeExpr(e.Args[0])
	if !isString(argType) {
		a.error(e.Args[0].Pos(), "print argument must be string; got %s", argType)
	}

	return types.Typ[types.Unit]
}

func (a *Analyzer) analyzeReadFileBuiltin(e *ast.CallExpr) types.Type {
	if len(e.Args) != 1 {
		a.error(e.Pos(), "readFile requires exactly 1 argument")
		return types.Typ[types.String]
	}

	argType := a.analyzeExpr(e.Args[0])
	if !isString(argType) {
		a.error(e.Args[0].Pos(), "readFile argument must be string; got %s", argType)
	}

	return types.Typ[types.String]
}

func (a *Analyzer) analyzeOsReadFile(e *ast.MethodExpr) types.Type {
	if len(e.Args) != 1 {
		a.error(e.Pos(), "os.ReadFile requires exactly 1 argument")
		return types.Typ[types.String]
	}

	argType := a.analyzeExpr(e.Args[0])
	if !isString(argType) {
		a.error(e.Args[0].Pos(), "os.ReadFile argument must be string; got %s", argType)
	}

	return types.Typ[types.String]
}

func (a *Analyzer) analyzeOsWriteFile(e *ast.MethodExpr) types.Type {
	if len(e.Args) != 2 {
		a.error(e.Pos(), "os.WriteFile requires exactly 2 arguments (path, content)")
		return types.Typ[types.Int]
	}

	pathType := a.analyzeExpr(e.Args[0])
	if !isString(pathType) {
		a.error(e.Args[0].Pos(), "os.WriteFile path must be string; got %s", pathType)
	}

	contentType := a.analyzeExpr(e.Args[1])
	if !isString(contentType) {
		a.error(e.Args[1].Pos(), "os.WriteFile content must be string; got %s", contentType)
	}

	return types.Typ[types.Int] // returns 0 on success, -1 on error
}

func (a *Analyzer) analyzeOsReadFileCall(e *ast.CallExpr) types.Type {
	if len(e.Args) != 1 {
		a.error(e.Pos(), "os.ReadFile requires exactly 1 argument")
		return types.Typ[types.String]
	}

	argType := a.analyzeExpr(e.Args[0])
	if !isString(argType) {
		a.error(e.Args[0].Pos(), "os.ReadFile argument must be string; got %s", argType)
	}

	return types.Typ[types.String]
}

func (a *Analyzer) analyzeOsWriteFileCall(e *ast.CallExpr) types.Type {
	if len(e.Args) != 2 {
		a.error(e.Pos(), "os.WriteFile requires exactly 2 arguments (path, content)")
		return types.Typ[types.Int]
	}

	pathType := a.analyzeExpr(e.Args[0])
	if !isString(pathType) {
		a.error(e.Args[0].Pos(), "os.WriteFile path must be string; got %s", pathType)
	}

	contentType := a.analyzeExpr(e.Args[1])
	if !isString(contentType) {
		a.error(e.Args[1].Pos(), "os.WriteFile content must be string; got %s", contentType)
	}

	return types.Typ[types.Int] // returns 0 on success, -1 on error
}

func (a *Analyzer) analyzeOsArgc(e *ast.MethodExpr) types.Type {
	if len(e.Args) != 0 {
		a.error(e.Pos(), "os.Argc takes no arguments")
	}
	return types.Typ[types.Int]
}

func (a *Analyzer) analyzeOsArgv(e *ast.MethodExpr) types.Type {
	if len(e.Args) != 1 {
		a.error(e.Pos(), "os.Argv requires exactly 1 argument (index)")
		return types.Typ[types.String]
	}

	argType := a.analyzeExpr(e.Args[0])
	if !isInteger(argType) {
		a.error(e.Args[0].Pos(), "os.Argv argument must be int; got %s", argType)
	}

	return types.Typ[types.String]
}

func (a *Analyzer) analyzeStrconvItoa(e *ast.MethodExpr) types.Type {
	if len(e.Args) != 1 {
		a.error(e.Pos(), "strconv.Itoa requires exactly 1 argument")
		return types.Typ[types.String]
	}

	argType := a.analyzeExpr(e.Args[0])
	if !isInteger(argType) {
		a.error(e.Args[0].Pos(), "strconv.Itoa argument must be int; got %s", argType)
	}

	return types.Typ[types.String]
}

func (a *Analyzer) analyzeStrconvAtoi(e *ast.MethodExpr) types.Type {
	if len(e.Args) != 1 {
		a.error(e.Pos(), "strconv.Atoi requires exactly 1 argument")
		return types.Typ[types.Int]
	}

	argType := a.analyzeExpr(e.Args[0])
	if basic, ok := argType.Underlying().(*types.Basic); !ok || basic.Kind != types.String {
		a.error(e.Args[0].Pos(), "strconv.Atoi argument must be string; got %s", argType)
	}

	return types.Typ[types.Int]
}

func (a *Analyzer) analyzeSyscallOpen(e *ast.MethodExpr) types.Type {
	if len(e.Args) != 3 {
		a.error(e.Pos(), "syscall.open requires exactly 3 arguments (path, flags, mode)")
		return types.Typ[types.Int]
	}

	// Check path is string
	pathType := a.analyzeExpr(e.Args[0])
	if basic, ok := pathType.Underlying().(*types.Basic); !ok || basic.Kind != types.String {
		a.error(e.Args[0].Pos(), "syscall.open path must be string; got %s", pathType)
	}

	// Check flags is int
	flagsType := a.analyzeExpr(e.Args[1])
	if basic, ok := flagsType.Underlying().(*types.Basic); !ok || basic.Kind != types.Int {
		a.error(e.Args[1].Pos(), "syscall.open flags must be int; got %s", flagsType)
	}

	// Check mode is int
	modeType := a.analyzeExpr(e.Args[2])
	if basic, ok := modeType.Underlying().(*types.Basic); !ok || basic.Kind != types.Int {
		a.error(e.Args[2].Pos(), "syscall.open mode must be int; got %s", modeType)
	}

	return types.Typ[types.Int] // returns file descriptor
}

func (a *Analyzer) analyzeSyscallRead(e *ast.MethodExpr) types.Type {
	if len(e.Args) != 3 {
		a.error(e.Pos(), "syscall.read requires exactly 3 arguments (fd, buf, count)")
		return types.Typ[types.Int]
	}

	// Check fd is int
	fdType := a.analyzeExpr(e.Args[0])
	if basic, ok := fdType.Underlying().(*types.Basic); !ok || basic.Kind != types.Int {
		a.error(e.Args[0].Pos(), "syscall.read fd must be int; got %s", fdType)
	}

	// Check buf is string or int (buffer pointer)
	bufType := a.analyzeExpr(e.Args[1])
	if basic, ok := bufType.Underlying().(*types.Basic); !ok || (basic.Kind != types.String && basic.Kind != types.Int) {
		a.error(e.Args[1].Pos(), "syscall.read buf must be string or int (pointer); got %s", bufType)
	}

	// Check count is int
	countType := a.analyzeExpr(e.Args[2])
	if basic, ok := countType.Underlying().(*types.Basic); !ok || basic.Kind != types.Int {
		a.error(e.Args[2].Pos(), "syscall.read count must be int; got %s", countType)
	}

	return types.Typ[types.Int] // returns bytes read
}

func (a *Analyzer) analyzeSyscallWrite(e *ast.MethodExpr) types.Type {
	if len(e.Args) != 3 {
		a.error(e.Pos(), "syscall.write requires exactly 3 arguments (fd, buf, count)")
		return types.Typ[types.Int]
	}

	// Check fd is int
	fdType := a.analyzeExpr(e.Args[0])
	if basic, ok := fdType.Underlying().(*types.Basic); !ok || basic.Kind != types.Int {
		a.error(e.Args[0].Pos(), "syscall.write fd must be int; got %s", fdType)
	}

	// Check buf is string or int (buffer pointer)
	bufType := a.analyzeExpr(e.Args[1])
	if basic, ok := bufType.Underlying().(*types.Basic); !ok || (basic.Kind != types.String && basic.Kind != types.Int) {
		a.error(e.Args[1].Pos(), "syscall.write buf must be string or int (pointer); got %s", bufType)
	}

	// Check count is int
	countType := a.analyzeExpr(e.Args[2])
	if basic, ok := countType.Underlying().(*types.Basic); !ok || basic.Kind != types.Int {
		a.error(e.Args[2].Pos(), "syscall.write count must be int; got %s", countType)
	}

	return types.Typ[types.Int] // returns bytes written
}

func (a *Analyzer) analyzeSyscallClose(e *ast.MethodExpr) types.Type {
	if len(e.Args) != 1 {
		a.error(e.Pos(), "syscall.close requires exactly 1 argument (fd)")
		return types.Typ[types.Int]
	}

	// Check fd is int
	fdType := a.analyzeExpr(e.Args[0])
	if basic, ok := fdType.Underlying().(*types.Basic); !ok || basic.Kind != types.Int {
		a.error(e.Args[0].Pos(), "syscall.close fd must be int; got %s", fdType)
	}

	return types.Typ[types.Int] // returns 0 on success, -1 on error
}

func (a *Analyzer) analyzeFieldExpr(e *ast.FieldExpr) types.Type {
	// Check if base expression is a module identifier BEFORE analyzing it
	// This prevents "undefined: math" errors for module-qualified names
	if ident, ok := e.Expr.(*ast.Ident); ok {
		if module, exists := a.modules[ident.Name]; exists {
			// Qualified name: module.Symbol
			a.usedModules[ident.Name] = true

			symbol, found := module.Symbols[e.Field.Name]
			if !found {
				a.error(e.Field.Pos(), "module %s has no exported symbol %s", ident.Name, e.Field.Name)
				return types.Typ[types.Invalid]
			}

			// Record the use of this symbol
			a.info.Uses[e.Field] = symbol

			return symbol.Type
		}
	}

	// Otherwise, it's a struct field access - analyze the base expression
	exprType := a.analyzeExpr(e.Expr)

	// Automatically dereference pointers for field access
	underlying := exprType.Underlying()
	if ptr, ok := underlying.(*types.Pointer); ok {
		underlying = ptr.Elem.Underlying()
	}

	structType, ok := underlying.(*types.Struct)
	if !ok {
		a.error(e.Expr.Pos(), "%s has no field %s", exprType, e.Field.Name)
		return types.Typ[types.Invalid]
	}

	field := structType.FieldByName(e.Field.Name)
	if field == nil {
		a.error(e.Field.Pos(), "%s has no field %s", structType.Name, e.Field.Name)
		return types.Typ[types.Invalid]
	}

	return field.Type
}

func (a *Analyzer) analyzeIndexExpr(e *ast.IndexExpr) types.Type {
	exprType := a.analyzeExpr(e.Expr)
	indexType := a.analyzeExpr(e.Index)

	switch t := exprType.Underlying().(type) {
	case *types.Array:
		if !isInteger(indexType) {
			a.error(e.Index.Pos(), "index must be integer, got %s", indexType)
		}
		return t.Elem
	case *types.Slice:
		if !isInteger(indexType) {
			a.error(e.Index.Pos(), "index must be integer, got %s", indexType)
		}
		return t.Elem
	case *types.Map:
		if !indexType.Equals(t.Key) {
			a.error(e.Index.Pos(), "map key must be %s, got %s", t.Key, indexType)
		}
		return t.Value
	case *types.Basic:
		if t.Kind == types.String {
			if !isInteger(indexType) {
				a.error(e.Index.Pos(), "index must be integer, got %s", indexType)
			}
			return types.Typ[types.Int] // character as int for easier use with literals
		}
		a.error(e.Expr.Pos(), "cannot index %s", exprType)
		return types.Typ[types.Invalid]
	default:
		a.error(e.Expr.Pos(), "cannot index %s", exprType)
		return types.Typ[types.Invalid]
	}
}

func (a *Analyzer) analyzeSliceExpr(e *ast.SliceExpr) types.Type {
	exprType := a.analyzeExpr(e.Expr)

	if e.Start != nil {
		startType := a.analyzeExpr(e.Start)
		if !isInteger(startType) {
			a.error(e.Start.Pos(), "slice start must be integer, got %s", startType)
		}
	}

	if e.End != nil {
		endType := a.analyzeExpr(e.End)
		if !isInteger(endType) {
			a.error(e.End.Pos(), "slice end must be integer, got %s", endType)
		}
	}

	// Slicing a string returns a string
	if basic, ok := exprType.Underlying().(*types.Basic); ok && basic.Kind == types.String {
		return types.Typ[types.String]
	}

	// Slicing an array or slice returns a slice
	switch t := exprType.Underlying().(type) {
	case *types.Array:
		return types.NewSlice(t.Elem)
	case *types.Slice:
		return t
	default:
		a.error(e.Expr.Pos(), "cannot slice %s", exprType)
		return types.Typ[types.Invalid]
	}
}

func (a *Analyzer) analyzeIfExpr(e *ast.IfExpr) types.Type {
	condType := a.analyzeExpr(e.Cond)
	if !condType.Equals(types.Typ[types.Bool]) {
		a.error(e.Cond.Pos(), "condition must be bool, got %s", condType)
	}

	a.table.EnterScope("if:then")
	thenType := a.analyzeBlock(e.Then)
	a.table.ExitScope()

	if e.Else == nil {
		return types.Typ[types.Unit]
	}

	a.table.EnterScope("if:else")
	var elseType types.Type
	switch elseExpr := e.Else.(type) {
	case *ast.BlockExpr:
		elseType = a.analyzeBlock(elseExpr.Block)
	case *ast.IfExpr:
		elseType = a.analyzeIfExpr(elseExpr)
	default:
		elseType = a.analyzeExpr(elseExpr)
	}
	a.table.ExitScope()

	if !thenType.Equals(elseType) {
		a.error(e.Pos(), "if branches have different types: %s and %s", thenType, elseType)
	}

	return thenType
}

func (a *Analyzer) analyzeRangeExpr(e *ast.RangeExpr) types.Type {
	var startType, endType types.Type

	if e.Start != nil {
		startType = a.analyzeExpr(e.Start)
		if !isInteger(startType) {
			a.error(e.Start.Pos(), "range bound must be integer, got %s", startType)
		}
	}

	if e.End != nil {
		endType = a.analyzeExpr(e.End)
		if !isInteger(endType) {
			a.error(e.End.Pos(), "range bound must be integer, got %s", endType)
		}
	}

	if startType != nil && endType != nil && !startType.Equals(endType) {
		a.error(e.Pos(), "range bounds have different types: %s and %s", startType, endType)
	}

	// Return a range type (for now, just int)
	return types.Typ[types.Int]
}

func (a *Analyzer) analyzeStructExpr(e *ast.StructExpr) types.Type {
	// Handle both struct construction and enum variant construction
	switch name := e.Name.(type) {
	case *ast.Ident:
		// Simple struct: Point { x: 1, y: 2 }
		return a.analyzeStructLiteral(name, e.Fields)

	case *ast.PathExpr:
		// Enum variant: Option::Some { value: 42 }
		return a.analyzeEnumVariantExpr(name, e.Fields)

	default:
		a.error(e.Pos(), "unsupported struct/enum construction")
		return types.Typ[types.Invalid]
	}
}

func (a *Analyzer) analyzeStructLiteral(name *ast.Ident, fields []ast.FieldInit) types.Type {
	// First, check if this is a generic struct
	if genericDecl, ok := a.genericStructs[name.Name]; ok {
		return a.analyzeGenericStructLiteral(genericDecl, name.Name, fields, name.Pos())
	}

	sym := a.table.Resolve(name.Name)
	if sym == nil {
		a.error(name.Pos(), "undefined: %s", name.Name)
		return types.Typ[types.Invalid]
	}

	structType, ok := sym.Type.(*types.Struct)
	if !ok {
		a.error(name.Pos(), "%s is not a struct", name.Name)
		return types.Typ[types.Invalid]
	}

	a.info.Uses[name] = sym

	// Check fields
	for _, fi := range fields {
		field := structType.FieldByName(fi.Name.Name)
		if field == nil {
			a.error(fi.Name.Pos(), "struct %s has no field %s", structType.Name, fi.Name.Name)
			continue
		}

		if fi.Value != nil {
			valType := a.analyzeExpr(fi.Value)
			if !types.IsAssignableTo(valType, field.Type) {
				a.error(fi.Value.Pos(), "cannot use %s as %s in field %s", valType, field.Type, fi.Name.Name)
			}
		}
	}

	return structType
}

// analyzeGenericStructLiteral handles construction of generic struct literals by inferring type arguments.
func (a *Analyzer) analyzeGenericStructLiteral(genericDecl *ast.StructDecl, structName string, fields []ast.FieldInit, pos token.Position) types.Type {
	// Create type parameters for inference
	typeParams := make([]*types.TypeParam, len(genericDecl.TypeParams))
	for i, tp := range genericDecl.TypeParams {
		typeParams[i] = types.NewTypeParam(tp.Name.Name, a.typeParamIDCounter+i)
	}

	// Set up type param scope temporarily
	oldScope := a.typeParamScope
	a.typeParamScope = make(map[string]*types.TypeParam)
	for i, tp := range genericDecl.TypeParams {
		a.typeParamScope[tp.Name.Name] = typeParams[i]
	}

	// Build a map from field names to their declared types (with type params)
	fieldTypes := make(map[string]types.Type)
	for _, f := range genericDecl.Fields {
		fieldTypes[f.Name.Name] = a.resolveType(f.Type)
	}

	a.typeParamScope = oldScope

	// Infer type arguments from provided field values
	inferred := make(map[int]types.Type)
	for _, fi := range fields {
		if fi.Value == nil {
			continue
		}

		valType := a.analyzeExpr(fi.Value)
		paramType, ok := fieldTypes[fi.Name.Name]
		if !ok {
			a.error(fi.Name.Pos(), "struct %s has no field %s", structName, fi.Name.Name)
			continue
		}

		a.unifyTypes(paramType, valType, inferred)
	}

	// Build the type arguments from inferred types
	typeArgs := make([]types.Type, len(typeParams))
	for i, tp := range typeParams {
		if concrete, ok := inferred[tp.ID]; ok {
			typeArgs[i] = concrete
		} else {
			a.error(pos, "cannot infer type argument '%s' for struct %s", tp.Name, structName)
			return types.Typ[types.Invalid]
		}
	}

	// Now instantiate the generic struct with the inferred type arguments
	mangledName := types.MangledName(structName, typeArgs)

	// Check cache first
	if cached, ok := a.instantiations[mangledName]; ok {
		return cached
	}

	// Instantiate the struct type
	return a.instantiateGenericStruct(genericDecl, typeArgs, mangledName, pos)
}

func (a *Analyzer) analyzeEnumVariantExpr(path *ast.PathExpr, fields []ast.FieldInit) types.Type {
	if len(path.Parts) < 2 {
		a.error(path.Pos(), "invalid enum variant path")
		return types.Typ[types.Invalid]
	}

	// Get the enum type
	enumName := path.Parts[0].Name
	variantName := path.Parts[1].Name

	// First, check if this is a generic enum
	if genericDecl, ok := a.genericEnums[enumName]; ok {
		// For generic enums, we need to infer type arguments from the field values
		return a.analyzeGenericEnumVariantExpr(genericDecl, enumName, variantName, fields, path.Pos())
	}

	sym := a.table.Resolve(enumName)
	if sym == nil {
		a.error(path.Parts[0].Pos(), "undefined: %s", enumName)
		return types.Typ[types.Invalid]
	}

	enumType, ok := sym.Type.(*types.Enum)
	if !ok {
		a.error(path.Parts[0].Pos(), "%s is not an enum", enumName)
		return types.Typ[types.Invalid]
	}

	// Get the variant
	variant := enumType.VariantByName(variantName)
	if variant == nil {
		a.error(path.Parts[1].Pos(), "enum %s has no variant %s", enumName, variantName)
		return types.Typ[types.Invalid]
	}

	// Check fields
	for _, fi := range fields {
		var variantField *types.Field
		for _, f := range variant.Fields {
			if f.Name == fi.Name.Name {
				variantField = f
				break
			}
		}

		if variantField == nil {
			a.error(fi.Name.Pos(), "variant %s::%s has no field %s", enumName, variantName, fi.Name.Name)
			continue
		}

		if fi.Value != nil {
			valType := a.analyzeExpr(fi.Value)
			if !types.IsAssignableTo(valType, variantField.Type) {
				a.error(fi.Value.Pos(), "cannot use %s as %s in field %s", valType, variantField.Type, fi.Name.Name)
			}
		}
	}

	return enumType
}

// analyzeGenericEnumVariantExpr handles construction of generic enum variants by inferring type arguments.
func (a *Analyzer) analyzeGenericEnumVariantExpr(genericDecl *ast.EnumDecl, enumName, variantName string, fields []ast.FieldInit, pos token.Position) types.Type {
	// Find the variant in the generic declaration
	var variantDecl *ast.Variant
	for i := range genericDecl.Variants {
		if genericDecl.Variants[i].Name.Name == variantName {
			variantDecl = &genericDecl.Variants[i]
			break
		}
	}

	if variantDecl == nil {
		a.error(pos, "enum %s has no variant %s", enumName, variantName)
		return types.Typ[types.Invalid]
	}

	// Create type parameters for inference
	typeParams := make([]*types.TypeParam, len(genericDecl.TypeParams))
	for i, tp := range genericDecl.TypeParams {
		typeParams[i] = types.NewTypeParam(tp.Name.Name, a.typeParamIDCounter+i)
	}

	// Set up type param scope temporarily
	oldScope := a.typeParamScope
	a.typeParamScope = make(map[string]*types.TypeParam)
	for i, tp := range genericDecl.TypeParams {
		a.typeParamScope[tp.Name.Name] = typeParams[i]
	}

	// Build a map from field names to their declared types (with type params)
	fieldTypes := make(map[string]types.Type)
	for _, f := range variantDecl.Fields {
		fieldTypes[f.Name.Name] = a.resolveType(f.Type)
	}

	a.typeParamScope = oldScope

	// Infer type arguments from provided field values
	inferred := make(map[int]types.Type)
	for _, fi := range fields {
		if fi.Value == nil {
			continue
		}

		valType := a.analyzeExpr(fi.Value)
		paramType, ok := fieldTypes[fi.Name.Name]
		if !ok {
			a.error(fi.Name.Pos(), "variant %s::%s has no field %s", enumName, variantName, fi.Name.Name)
			continue
		}

		a.unifyTypes(paramType, valType, inferred)
	}

	// Build the type arguments from inferred types
	typeArgs := make([]types.Type, len(typeParams))
	for i, tp := range typeParams {
		if concrete, ok := inferred[tp.ID]; ok {
			typeArgs[i] = concrete
		} else {
			// For unit variants or variants where type can't be inferred, we can't proceed
			a.error(pos, "cannot infer type argument '%s' for %s::%s", tp.Name, enumName, variantName)
			return types.Typ[types.Invalid]
		}
	}

	// Now instantiate the generic enum with the inferred type arguments
	mangledName := types.MangledName(enumName, typeArgs)

	// Check cache first
	if cached, ok := a.instantiations[mangledName]; ok {
		return cached
	}

	// Instantiate the enum type
	return a.instantiateGenericEnum(genericDecl, typeArgs, mangledName, pos)
}

func (a *Analyzer) analyzeArrayExpr(e *ast.ArrayExpr) types.Type {
	// Go-style: []type{elements} or [count]type{elements}
	if e.ElemType != nil {
		elemType := a.resolveType(e.ElemType)

		// Determine count
		var count int64
		if e.Count != nil {
			// [count]type{...} - fixed-size array
			countType := a.analyzeExpr(e.Count)
			if !isInteger(countType) {
				a.error(e.Count.Pos(), "array size must be integer, got %s", countType)
			}
			if lit, ok := e.Count.(*ast.IntLit); ok {
				count = lit.Value
			}
		} else {
			// []type{...} - size from elements
			count = int64(len(e.Elements))
		}

		// Check element types
		for _, elem := range e.Elements {
			t := a.analyzeExpr(elem)
			if !types.IsAssignableTo(t, elemType) {
				a.error(elem.Pos(), "cannot use %s as %s in array literal", t, elemType)
			}
		}

		return types.NewArray(elemType, count)
	}

	// Old-style: [expr; count] repeat syntax
	if e.Repeat != nil {
		elemType := a.analyzeExpr(e.Repeat)
		countType := a.analyzeExpr(e.Count)
		if !isInteger(countType) {
			a.error(e.Count.Pos(), "array count must be integer, got %s", countType)
		}
		var count int64 = 0
		if lit, ok := e.Count.(*ast.IntLit); ok {
			count = lit.Value
		}
		return types.NewArray(elemType, count)
	}

	// Old-style: [elem1, elem2, ...] - infer type from elements
	if len(e.Elements) == 0 {
		a.error(e.Pos(), "cannot infer type of empty array")
		return types.NewArray(types.Typ[types.Invalid], 0)
	}

	elemType := a.analyzeExpr(e.Elements[0])
	for i := 1; i < len(e.Elements); i++ {
		t := a.analyzeExpr(e.Elements[i])
		if !t.Equals(elemType) {
			a.error(e.Elements[i].Pos(), "array element type mismatch: expected %s, got %s", elemType, t)
		}
	}

	return types.NewArray(elemType, int64(len(e.Elements)))
}

func (a *Analyzer) analyzeMapExpr(e *ast.MapExpr) types.Type {
	// Resolve map type from the map type annotation
	if e.MapType == nil {
		a.error(e.Pos(), "map expression requires type annotation")
		return types.Typ[types.Invalid]
	}

	keyType := a.resolveType(e.MapType.Key)
	if !types.IsValidMapKey(keyType) {
		a.error(e.MapType.Pos(), "invalid map key type: %s", keyType)
	}
	valueType := a.resolveType(e.MapType.Value)

	// Check entry types
	for _, entry := range e.Entries {
		k := a.analyzeExpr(entry.Key)
		if !types.IsAssignableTo(k, keyType) {
			a.error(entry.Key.Pos(), "cannot use %s as map key type %s", k, keyType)
		}
		v := a.analyzeExpr(entry.Value)
		if !types.IsAssignableTo(v, valueType) {
			a.error(entry.Value.Pos(), "cannot use %s as map value type %s", v, valueType)
		}
	}

	return types.NewMap(keyType, valueType)
}

func (a *Analyzer) analyzeTupleExpr(e *ast.TupleExpr) types.Type {
	elems := make([]types.Type, len(e.Elements))
	for i, elem := range e.Elements {
		elems[i] = a.analyzeExpr(elem)
	}
	return types.NewTuple(elems)
}

func (a *Analyzer) analyzeMatchStmt(s *ast.MatchStmt) {
	// Analyze the scrutinee expression
	scrutineeType := a.analyzeExpr(s.Expr)

	// Analyze each arm
	for _, arm := range s.Arms {
		// Enter a new scope for each arm (for pattern bindings)
		a.table.EnterScope("match:arm")

		// Analyze the pattern and bind variables
		a.analyzePattern(arm.Pattern, scrutineeType)

		// Analyze the guard (if present)
		if arm.Guard != nil {
			guardType := a.analyzeExpr(arm.Guard)
			if !guardType.Equals(types.Typ[types.Bool]) {
				a.error(arm.Guard.Pos(), "match guard must be bool, got %s", guardType)
			}
		}

		// Analyze the arm body
		a.analyzeExpr(arm.Body)

		a.table.ExitScope()
	}
}

func (a *Analyzer) analyzeMatchExpr(e *ast.MatchExpr) types.Type {
	// Analyze the scrutinee expression
	scrutineeType := a.analyzeExpr(e.Expr)

	// Analyze each arm
	var resultType types.Type
	for i, arm := range e.Arms {
		// Enter a new scope for each arm (for pattern bindings)
		a.table.EnterScope("match:arm")

		// Analyze the pattern and bind variables
		a.analyzePattern(arm.Pattern, scrutineeType)

		// Analyze the guard (if present)
		if arm.Guard != nil {
			guardType := a.analyzeExpr(arm.Guard)
			if !guardType.Equals(types.Typ[types.Bool]) {
				a.error(arm.Guard.Pos(), "match guard must be bool, got %s", guardType)
			}
		}

		// Analyze the arm body
		armType := a.analyzeExpr(arm.Body)

		// Check that all arms have the same type
		if i == 0 {
			resultType = armType
		} else if !resultType.Equals(armType) {
			a.error(arm.Body.Pos(), "match arms have different types: %s and %s", resultType, armType)
		}

		a.table.ExitScope()
	}

	if resultType == nil {
		resultType = types.Typ[types.Unit]
	}

	return resultType
}

func (a *Analyzer) analyzeTryExpr(e *ast.TryExpr) types.Type {
	// The ? operator can only be used inside a function
	if a.currentFunc == nil {
		a.error(e.Pos(), "? operator can only be used inside a function")
		return types.Typ[types.Invalid]
	}

	// Analyze the inner expression
	innerType := a.analyzeExpr(e.Expr)

	// The expression must be a Result type (enum with Ok and Err variants)
	enumType, ok := innerType.(*types.Enum)
	if !ok {
		a.error(e.Pos(), "? operator requires Result type, got %s", innerType)
		return types.Typ[types.Invalid]
	}

	// Check that it has Ok and Err variants
	okVariant := enumType.VariantByName("Ok")
	errVariant := enumType.VariantByName("Err")
	if okVariant == nil || errVariant == nil {
		a.error(e.Pos(), "? operator requires Result type with Ok and Err variants, got %s", enumType.Name)
		return types.Typ[types.Invalid]
	}

	// The enclosing function must also return a Result type
	returnType := a.currentFunc.Result
	returnEnum, ok := returnType.(*types.Enum)
	if !ok {
		a.error(e.Pos(), "? operator can only be used in functions returning Result, this function returns %s", returnType)
		return types.Typ[types.Invalid]
	}

	// The return type must also have Ok and Err variants
	returnOk := returnEnum.VariantByName("Ok")
	returnErr := returnEnum.VariantByName("Err")
	if returnOk == nil || returnErr == nil {
		a.error(e.Pos(), "? operator can only be used in functions returning Result type with Ok and Err variants")
		return types.Typ[types.Invalid]
	}

	// The error types should be compatible (same type for now)
	// For simplicity, we require the enum types to be the same (both Result_T_E)
	// A more advanced implementation would check that E is compatible

	// Return the type of the Ok variant's value
	if len(okVariant.Fields) == 0 {
		return types.Typ[types.Unit]
	}
	// Assume the first field is the value
	return okVariant.Fields[0].Type
}

func (a *Analyzer) analyzePattern(pattern ast.Pattern, expectedType types.Type) {
	switch p := pattern.(type) {
	case *ast.IdentPattern:
		// Bind the identifier to the expected type
		sym := symbols.NewSymbol(p.Name.Name, symbols.VarSymbol, expectedType, p.Name.Pos())
		a.table.Define(sym)
		a.info.Defs[p.Name] = sym

	case *ast.WildcardPattern:
		// Wildcard matches anything, no binding

	case *ast.LiteralPattern:
		// Check that the literal type matches
		litType := a.analyzeExpr(p.Value)
		if !types.IsAssignableTo(litType, expectedType) {
			a.error(p.Value.Pos(), "pattern type %s doesn't match expected type %s", litType, expectedType)
		}

	case *ast.EnumPattern:
		// Handle patterns like Option::Some { value }
		a.analyzeEnumPattern(p, expectedType)

	default:
		a.error(pattern.Pos(), "unsupported pattern type %T", pattern)
	}
}

func (a *Analyzer) analyzeEnumPattern(p *ast.EnumPattern, expectedType types.Type) {
	// Get the enum type from the path
	path, ok := p.Path.(*ast.PathExpr)
	if !ok {
		a.error(p.Pos(), "invalid enum pattern")
		return
	}

	if len(path.Parts) < 2 {
		a.error(p.Pos(), "enum pattern requires Enum::Variant format")
		return
	}

	enumName := path.Parts[0].Name
	variantName := path.Parts[1].Name

	// Verify the expected type is the enum
	enumType, ok := expectedType.(*types.Enum)
	if !ok {
		a.error(p.Pos(), "expected enum type, got %s", expectedType)
		return
	}

	if enumType.Name != enumName {
		a.error(p.Pos(), "pattern for %s doesn't match expected enum %s", enumName, enumType.Name)
		return
	}

	// Get the variant
	variant := enumType.VariantByName(variantName)
	if variant == nil {
		a.error(path.Parts[1].Pos(), "enum %s has no variant %s", enumName, variantName)
		return
	}

	// Bind pattern fields to variant fields
	for _, fp := range p.Fields {
		var variantField *types.Field
		for _, f := range variant.Fields {
			if f.Name == fp.Name.Name {
				variantField = f
				break
			}
		}

		if variantField == nil {
			a.error(fp.Name.Pos(), "variant %s::%s has no field %s", enumName, variantName, fp.Name.Name)
			continue
		}

		// Bind the field pattern
		if fp.Pattern != nil {
			a.analyzePattern(fp.Pattern, variantField.Type)
		} else {
			// Shorthand: just the field name becomes a binding
			sym := symbols.NewSymbol(fp.Name.Name, symbols.VarSymbol, variantField.Type, fp.Name.Pos())
			a.table.Define(sym)
			a.info.Defs[fp.Name] = sym
		}
	}
}

func (a *Analyzer) analyzePathExpr(e *ast.PathExpr) types.Type {
	// Handle paths like Color::Red (enum variant) or Module::Type
	if len(e.Parts) < 2 {
		a.error(e.Pos(), "invalid path expression")
		return types.Typ[types.Invalid]
	}

	// For now, handle Enum::Variant pattern
	typeName := e.Parts[0].Name
	variantName := e.Parts[1].Name

	// Look up the type
	sym := a.table.Resolve(typeName)
	if sym == nil {
		a.error(e.Parts[0].Pos(), "undefined: %s", typeName)
		return types.Typ[types.Invalid]
	}

	// Check if it's an enum
	enumType, ok := sym.Type.(*types.Enum)
	if !ok {
		a.error(e.Parts[0].Pos(), "%s is not an enum", typeName)
		return types.Typ[types.Invalid]
	}

	// Look up the variant
	variant := enumType.VariantByName(variantName)
	if variant == nil {
		a.error(e.Parts[1].Pos(), "enum %s has no variant %s", typeName, variantName)
		return types.Typ[types.Invalid]
	}

	// Unit variants (no fields) can be used directly as values
	// Variants with fields need to be constructed like structs
	if len(variant.Fields) > 0 {
		a.error(e.Pos(), "variant %s::%s has fields and must be constructed with { }", typeName, variantName)
		return types.Typ[types.Invalid]
	}

	return enumType
}

func (a *Analyzer) analyzeMethodExpr(e *ast.MethodExpr) types.Type {
	// Handle imported modules (e.g., math.Add, strings.Split)
	if ident, ok := e.Expr.(*ast.Ident); ok {
		if module, exists := a.modules[ident.Name]; exists {
			// Module-qualified function call
			a.usedModules[ident.Name] = true

			symbol, found := module.Symbols[e.Method.Name]
			if !found {
				a.error(e.Method.Pos(), "module %s has no exported symbol %s", ident.Name, e.Method.Name)
				return types.Typ[types.Invalid]
			}

			// Get function type
			fnType, ok := symbol.Type.(*types.Function)
			if !ok {
				a.error(e.Method.Pos(), "%s.%s is not a function", ident.Name, e.Method.Name)
				return types.Typ[types.Invalid]
			}

			// Check argument count
			if len(e.Args) != len(fnType.Params) {
				a.error(e.Pos(), "wrong number of arguments: expected %d, got %d", len(fnType.Params), len(e.Args))
				return fnType.Result
			}

			// Check argument types
			for i, arg := range e.Args {
				argType := a.analyzeExpr(arg)
				if !types.IsAssignableTo(argType, fnType.Params[i].Type) {
					a.error(arg.Pos(), "cannot use %s as %s in argument to %s.%s",
						argType, fnType.Params[i].Type, ident.Name, e.Method.Name)
				}
			}

			// Record the use of this symbol
			a.info.Uses[e.Method] = symbol

			return fnType.Result
		}

		// Handle built-in package-qualified functions (e.g., os.ReadFile, strconv.Itoa)
		switch ident.Name {
		case "os":
			switch e.Method.Name {
			case "ReadFile":
				return a.analyzeOsReadFile(e)
			case "WriteFile":
				return a.analyzeOsWriteFile(e)
			case "Argc":
				return a.analyzeOsArgc(e)
			case "Argv":
				return a.analyzeOsArgv(e)
			}
		case "strconv":
			switch e.Method.Name {
			case "Itoa":
				return a.analyzeStrconvItoa(e)
			case "Atoi":
				return a.analyzeStrconvAtoi(e)
			}
		case "syscall":
			switch e.Method.Name {
			case "open":
				return a.analyzeSyscallOpen(e)
			case "read":
				return a.analyzeSyscallRead(e)
			case "write":
				return a.analyzeSyscallWrite(e)
			case "close":
				return a.analyzeSyscallClose(e)
			}
		}
	}

	// Analyze the receiver expression to get its type
	receiverType := a.analyzeExpr(e.Expr)

	// Get the type name
	var typeName string
	switch t := receiverType.Underlying().(type) {
	case *types.Struct:
		typeName = t.Name
	default:
		a.error(e.Expr.Pos(), "cannot call method on %s", receiverType)
		return types.Typ[types.Invalid]
	}

	// Look up the method
	methodMap := a.methods[typeName]
	if methodMap == nil {
		a.error(e.Method.Pos(), "type %s has no methods", typeName)
		return types.Typ[types.Invalid]
	}

	methodType := methodMap[e.Method.Name]
	if methodType == nil {
		a.error(e.Method.Pos(), "type %s has no method %s", typeName, e.Method.Name)
		return types.Typ[types.Invalid]
	}

	// Check argument count (method already includes 'self' as first param)
	expectedArgs := len(methodType.Params) - 1 // subtract 1 for 'self'
	if len(e.Args) != expectedArgs {
		a.error(e.Pos(), "wrong number of arguments: expected %d, got %d", expectedArgs, len(e.Args))
		return methodType.Result
	}

	// Check argument types (starting from index 1, skipping 'self')
	for i, arg := range e.Args {
		argType := a.analyzeExpr(arg)
		paramType := methodType.Params[i+1].Type // +1 to skip 'self'
		if !types.IsAssignableTo(argType, paramType) {
			a.error(arg.Pos(), "cannot use %s as %s in argument to method", argType, paramType)
		}
	}

	return methodType.Result
}

// Helper functions

func isNumeric(t types.Type) bool {
	if basic, ok := t.Underlying().(*types.Basic); ok {
		return basic.IsNumeric()
	}
	return false
}

// checkAssignable checks if an expression can be assigned to.
// Returns true if assignable, false otherwise (and reports error).
func (a *Analyzer) checkAssignable(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		// Simple variable - check mutability
		sym := a.table.Resolve(e.Name)
		if sym == nil {
			return false // error already reported
		}
		if !sym.Mutable {
			a.error(e.Pos(), "cannot assign to immutable variable '%s'", e.Name)
			return false
		}
		return true

	case *ast.FieldExpr:
		// Struct field - check that the root is mutable
		return a.checkAssignable(e.Expr)

	case *ast.IndexExpr:
		// Array/slice index - check that the root is mutable
		return a.checkAssignable(e.Expr)

	default:
		a.error(expr.Pos(), "cannot assign to %T", expr)
		return false
	}
}

func isInteger(t types.Type) bool {
	if basic, ok := t.Underlying().(*types.Basic); ok {
		return basic.IsInteger()
	}
	return false
}

func isString(t types.Type) bool {
	if basic, ok := t.Underlying().(*types.Basic); ok {
		return basic.Kind == types.String
	}
	return false
}
