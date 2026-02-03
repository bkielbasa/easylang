// Package types defines the type system for ease.
package types

import (
	"sort"
	"strings"
)

// TypeMap maps type parameter IDs to concrete types.
type TypeMap map[int]Type

// Substitute replaces type parameters with concrete types according to the map.
// Returns a new type with all TypeParam references replaced.
func Substitute(t Type, typeMap TypeMap) Type {
	if t == nil || len(typeMap) == 0 {
		return t
	}

	switch t := t.(type) {
	case *TypeParam:
		if concrete, ok := typeMap[t.ID]; ok {
			return concrete
		}
		return t

	case *Basic:
		return t

	case *Struct:
		// Create new struct with substituted field types
		newFields := make([]*Field, len(t.Fields))
		changed := false
		for i, f := range t.Fields {
			newType := Substitute(f.Type, typeMap)
			if newType != f.Type {
				changed = true
			}
			newFields[i] = &Field{Name: f.Name, Type: newType}
		}
		if !changed {
			return t
		}
		return &Struct{Name: t.Name, Fields: newFields}

	case *Enum:
		// Create new enum with substituted variant field types
		newVariants := make([]*Variant, len(t.Variants))
		changed := false
		for i, v := range t.Variants {
			newFields := make([]*Field, len(v.Fields))
			for j, f := range v.Fields {
				newType := Substitute(f.Type, typeMap)
				if newType != f.Type {
					changed = true
				}
				newFields[j] = &Field{Name: f.Name, Type: newType}
			}
			newVariants[i] = &Variant{Name: v.Name, Fields: newFields}
		}
		if !changed {
			return t
		}
		return &Enum{Name: t.Name, Variants: newVariants}

	case *Function:
		// Substitute param types and return type
		newParams := make([]*Param, len(t.Params))
		changed := false
		for i, p := range t.Params {
			newType := Substitute(p.Type, typeMap)
			if newType != p.Type {
				changed = true
			}
			newParams[i] = &Param{Name: p.Name, Type: newType}
		}
		newResult := Substitute(t.Result, typeMap)
		if newResult != t.Result {
			changed = true
		}
		if !changed {
			return t
		}
		return &Function{Params: newParams, Result: newResult, IsVariadic: t.IsVariadic}

	case *Array:
		newElem := Substitute(t.Elem, typeMap)
		if newElem == t.Elem {
			return t
		}
		return &Array{Elem: newElem, Len: t.Len}

	case *Slice:
		newElem := Substitute(t.Elem, typeMap)
		if newElem == t.Elem {
			return t
		}
		return &Slice{Elem: newElem}

	case *Pointer:
		newElem := Substitute(t.Elem, typeMap)
		if newElem == t.Elem {
			return t
		}
		return &Pointer{Elem: newElem, Mutable: t.Mutable}

	case *Tuple:
		newElems := make([]Type, len(t.Elems))
		changed := false
		for i, e := range t.Elems {
			newElems[i] = Substitute(e, typeMap)
			if newElems[i] != e {
				changed = true
			}
		}
		if !changed {
			return t
		}
		return &Tuple{Elems: newElems}

	case *Channel:
		newElem := Substitute(t.Elem, typeMap)
		if newElem == t.Elem {
			return t
		}
		return &Channel{Elem: newElem}

	case *TypeAlias:
		newUnderlying := Substitute(t.Underlying_, typeMap)
		if newUnderlying == t.Underlying_ {
			return t
		}
		return &TypeAlias{Name: t.Name, Underlying_: newUnderlying}

	default:
		return t
	}
}

// MangledName generates a unique name for a monomorphized type or function.
// Example: MangledName("Option", [int]) -> "Option_int"
// Example: MangledName("Result", [int, string]) -> "Result_int_string"
func MangledName(baseName string, typeArgs []Type) string {
	if len(typeArgs) == 0 {
		return baseName
	}

	var parts []string
	parts = append(parts, baseName)
	for _, t := range typeArgs {
		parts = append(parts, mangleType(t))
	}
	return strings.Join(parts, "_")
}

// mangleType returns a string representation of a type suitable for name mangling.
func mangleType(t Type) string {
	switch t := t.(type) {
	case *Basic:
		return t.name
	case *Struct:
		return t.Name
	case *Enum:
		return t.Name
	case *Array:
		return "arr" + mangleType(t.Elem)
	case *Slice:
		return "slice" + mangleType(t.Elem)
	case *Pointer:
		if t.Mutable {
			return "mutptr" + mangleType(t.Elem)
		}
		return "ptr" + mangleType(t.Elem)
	case *Tuple:
		var parts []string
		for _, e := range t.Elems {
			parts = append(parts, mangleType(e))
		}
		return "tup" + strings.Join(parts, "_")
	case *TypeParam:
		return t.Name
	default:
		return "unknown"
	}
}

// TypeArgsString returns a human-readable string of type arguments.
// Example: TypeArgsString([int, string]) -> "<int, string>"
func TypeArgsString(typeArgs []Type) string {
	if len(typeArgs) == 0 {
		return ""
	}
	var parts []string
	for _, t := range typeArgs {
		parts = append(parts, t.String())
	}
	return "<" + strings.Join(parts, ", ") + ">"
}

// ContainsTypeParam checks if a type contains any type parameters.
func ContainsTypeParam(t Type) bool {
	if t == nil {
		return false
	}

	switch t := t.(type) {
	case *TypeParam:
		return true
	case *Basic:
		return false
	case *Struct:
		for _, f := range t.Fields {
			if ContainsTypeParam(f.Type) {
				return true
			}
		}
		return false
	case *Enum:
		for _, v := range t.Variants {
			for _, f := range v.Fields {
				if ContainsTypeParam(f.Type) {
					return true
				}
			}
		}
		return false
	case *Function:
		for _, p := range t.Params {
			if ContainsTypeParam(p.Type) {
				return true
			}
		}
		return ContainsTypeParam(t.Result)
	case *Array:
		return ContainsTypeParam(t.Elem)
	case *Slice:
		return ContainsTypeParam(t.Elem)
	case *Pointer:
		return ContainsTypeParam(t.Elem)
	case *Tuple:
		for _, e := range t.Elems {
			if ContainsTypeParam(e) {
				return true
			}
		}
		return false
	case *Channel:
		return ContainsTypeParam(t.Elem)
	default:
		return false
	}
}

// BuildTypeMap creates a TypeMap from parallel slices of type parameters and concrete types.
func BuildTypeMap(params []*TypeParam, args []Type) TypeMap {
	m := make(TypeMap)
	for i, p := range params {
		if i < len(args) {
			m[p.ID] = args[i]
		}
	}
	return m
}

// TypeMapKeys returns the keys of a TypeMap in sorted order (for deterministic output).
func TypeMapKeys(m TypeMap) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
