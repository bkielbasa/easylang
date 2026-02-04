// Package types defines the type system for ease.
package types

import (
	"fmt"
	"strings"
)

// Type represents a mylang type.
type Type interface {
	// String returns a human-readable representation of the type.
	String() string

	// Equals checks if two types are equal.
	Equals(other Type) bool

	// Underlying returns the underlying type (for type aliases).
	Underlying() Type

	// private marker method
	isType()
}

// BasicKind represents the kind of a basic type.
type BasicKind int

const (
	Invalid BasicKind = iota

	// Boolean
	Bool

	// Integers
	Int
	Int8
	Int16
	Int32
	Int64

	// Unsigned integers
	Uint
	Uint8
	Uint16
	Uint32
	Uint64

	// Floating point
	Float32
	Float64

	// String
	String

	// Unit type (void/())
	Unit
)

var basicKindNames = [...]string{
	Invalid: "invalid",
	Bool:    "bool",
	Int:     "int",
	Int8:    "int8",
	Int16:   "int16",
	Int32:   "int32",
	Int64:   "int64",
	Uint:    "uint",
	Uint8:   "uint8",
	Uint16:  "uint16",
	Uint32:  "uint32",
	Uint64:  "uint64",
	Float32: "float32",
	Float64: "float64",
	String:  "string",
	Unit:    "()",
}

// Basic represents a basic (primitive) type.
type Basic struct {
	Kind BasicKind
	name string
}

func (b *Basic) String() string     { return b.name }
func (b *Basic) Equals(other Type) bool {
	if ob, ok := other.(*Basic); ok {
		return b.Kind == ob.Kind
	}
	return false
}
func (b *Basic) Underlying() Type { return b }
func (b *Basic) isType()          {}

// NewBasic creates a new basic type.
func NewBasic(kind BasicKind) *Basic {
	return &Basic{Kind: kind, name: basicKindNames[kind]}
}

// IsInteger returns true if the basic type is an integer type.
func (b *Basic) IsInteger() bool {
	return b.Kind >= Int && b.Kind <= Uint64
}

// IsSigned returns true if the basic type is a signed integer.
func (b *Basic) IsSigned() bool {
	return b.Kind >= Int && b.Kind <= Int64
}

// IsUnsigned returns true if the basic type is an unsigned integer.
func (b *Basic) IsUnsigned() bool {
	return b.Kind >= Uint && b.Kind <= Uint64
}

// IsFloat returns true if the basic type is a floating-point type.
func (b *Basic) IsFloat() bool {
	return b.Kind == Float32 || b.Kind == Float64
}

// IsNumeric returns true if the basic type is numeric.
func (b *Basic) IsNumeric() bool {
	return b.IsInteger() || b.IsFloat()
}

// Size returns the size in bytes of the basic type.
func (b *Basic) Size() int {
	switch b.Kind {
	case Bool, Int8, Uint8:
		return 1
	case Int16, Uint16:
		return 2
	case Int32, Uint32, Float32:
		return 4
	case Int, Int64, Uint, Uint64, Float64, String:
		return 8
	case Unit:
		return 0
	default:
		return 0
	}
}

// Function represents a function type.
type Function struct {
	Params  []*Param
	Result  Type
	IsVariadic bool
}

// Param represents a function parameter with name and type.
type Param struct {
	Name string
	Type Type
}

func (f *Function) String() string {
	var params []string
	for _, p := range f.Params {
		if p.Name != "" {
			params = append(params, fmt.Sprintf("%s: %s", p.Name, p.Type))
		} else {
			params = append(params, p.Type.String())
		}
	}
	if f.Result == nil || f.Result.Equals(Typ[Unit]) {
		return fmt.Sprintf("fn(%s)", strings.Join(params, ", "))
	}
	return fmt.Sprintf("fn(%s) -> %s", strings.Join(params, ", "), f.Result)
}

func (f *Function) Equals(other Type) bool {
	of, ok := other.(*Function)
	if !ok {
		return false
	}
	if len(f.Params) != len(of.Params) {
		return false
	}
	for i, p := range f.Params {
		if !p.Type.Equals(of.Params[i].Type) {
			return false
		}
	}
	if f.Result == nil && of.Result == nil {
		return true
	}
	if f.Result == nil || of.Result == nil {
		return false
	}
	return f.Result.Equals(of.Result)
}

func (f *Function) Underlying() Type { return f }
func (f *Function) isType()          {}

// NewFunction creates a new function type.
func NewFunction(params []*Param, result Type) *Function {
	return &Function{Params: params, Result: result}
}

// Struct represents a struct type.
type Struct struct {
	Name   string
	Fields []*Field
}

// Field represents a struct field.
type Field struct {
	Name string
	Type Type
}

func (s *Struct) String() string {
	if s.Name != "" {
		return s.Name
	}
	var fields []string
	for _, f := range s.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", f.Name, f.Type))
	}
	return fmt.Sprintf("struct { %s }", strings.Join(fields, ", "))
}

func (s *Struct) Equals(other Type) bool {
	os, ok := other.(*Struct)
	if !ok {
		return false
	}
	// Named structs are equal if they have the same name
	if s.Name != "" && os.Name != "" {
		return s.Name == os.Name
	}
	// Structural equality for anonymous structs
	if len(s.Fields) != len(os.Fields) {
		return false
	}
	for i, f := range s.Fields {
		if f.Name != os.Fields[i].Name || !f.Type.Equals(os.Fields[i].Type) {
			return false
		}
	}
	return true
}

func (s *Struct) Underlying() Type { return s }
func (s *Struct) isType()          {}

// FieldByName returns the field with the given name, or nil if not found.
func (s *Struct) FieldByName(name string) *Field {
	for _, f := range s.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// NewStruct creates a new struct type.
func NewStruct(name string, fields []*Field) *Struct {
	return &Struct{Name: name, Fields: fields}
}

// Enum represents an enum type with variants.
type Enum struct {
	Name     string
	Variants []*Variant
}

// Variant represents an enum variant.
type Variant struct {
	Name   string
	Fields []*Field // empty for unit variants
}

func (e *Enum) String() string {
	return e.Name
}

func (e *Enum) Equals(other Type) bool {
	oe, ok := other.(*Enum)
	if !ok {
		return false
	}
	return e.Name == oe.Name
}

func (e *Enum) Underlying() Type { return e }
func (e *Enum) isType()          {}

// VariantByName returns the variant with the given name, or nil if not found.
func (e *Enum) VariantByName(name string) *Variant {
	for _, v := range e.Variants {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// VariantIndex returns the index (tag value) of a variant, or -1 if not found.
func (e *Enum) VariantIndex(name string) int {
	for i, v := range e.Variants {
		if v.Name == name {
			return i
		}
	}
	return -1
}

// MaxVariantSize returns the size of the largest variant's data (excluding tag).
func (e *Enum) MaxVariantSize() int {
	maxSize := 0
	for _, v := range e.Variants {
		size := 0
		for _, f := range v.Fields {
			size += TypeSize(f.Type)
		}
		if size > maxSize {
			maxSize = size
		}
	}
	return maxSize
}

// NewEnum creates a new enum type.
func NewEnum(name string, variants []*Variant) *Enum {
	return &Enum{Name: name, Variants: variants}
}

// Array represents a fixed-size array type.
type Array struct {
	Elem Type
	Len  int64
}

func (a *Array) String() string {
	return fmt.Sprintf("[%s; %d]", a.Elem, a.Len)
}

func (a *Array) Equals(other Type) bool {
	oa, ok := other.(*Array)
	if !ok {
		return false
	}
	return a.Len == oa.Len && a.Elem.Equals(oa.Elem)
}

func (a *Array) Underlying() Type { return a }
func (a *Array) isType()          {}

// NewArray creates a new array type.
func NewArray(elem Type, len int64) *Array {
	return &Array{Elem: elem, Len: len}
}

// Slice represents a slice type.
type Slice struct {
	Elem Type
}

func (s *Slice) String() string {
	return fmt.Sprintf("[]%s", s.Elem)
}

func (s *Slice) Equals(other Type) bool {
	os, ok := other.(*Slice)
	if !ok {
		return false
	}
	return s.Elem.Equals(os.Elem)
}

func (s *Slice) Underlying() Type { return s }
func (s *Slice) isType()          {}

// NewSlice creates a new slice type.
func NewSlice(elem Type) *Slice {
	return &Slice{Elem: elem}
}

// Pointer represents a pointer/reference type.
type Pointer struct {
	Elem    Type
	Mutable bool
}

func (p *Pointer) String() string {
	if p.Mutable {
		return fmt.Sprintf("&mut %s", p.Elem)
	}
	return fmt.Sprintf("&%s", p.Elem)
}

func (p *Pointer) Equals(other Type) bool {
	op, ok := other.(*Pointer)
	if !ok {
		return false
	}
	return p.Mutable == op.Mutable && p.Elem.Equals(op.Elem)
}

func (p *Pointer) Underlying() Type { return p }
func (p *Pointer) isType()          {}

// NewPointer creates a new pointer type.
func NewPointer(elem Type, mutable bool) *Pointer {
	return &Pointer{Elem: elem, Mutable: mutable}
}

// Tuple represents a tuple type.
type Tuple struct {
	Elems []Type
}

func (t *Tuple) String() string {
	var elems []string
	for _, e := range t.Elems {
		elems = append(elems, e.String())
	}
	return fmt.Sprintf("(%s)", strings.Join(elems, ", "))
}

func (t *Tuple) Equals(other Type) bool {
	ot, ok := other.(*Tuple)
	if !ok {
		return false
	}
	if len(t.Elems) != len(ot.Elems) {
		return false
	}
	for i, e := range t.Elems {
		if !e.Equals(ot.Elems[i]) {
			return false
		}
	}
	return true
}

func (t *Tuple) Underlying() Type { return t }
func (t *Tuple) isType()          {}

// NewTuple creates a new tuple type.
func NewTuple(elems []Type) *Tuple {
	return &Tuple{Elems: elems}
}

// Channel represents a channel type.
type Channel struct {
	Elem Type
}

func (c *Channel) String() string {
	return fmt.Sprintf("chan<%s>", c.Elem)
}

func (c *Channel) Equals(other Type) bool {
	oc, ok := other.(*Channel)
	if !ok {
		return false
	}
	return c.Elem.Equals(oc.Elem)
}

func (c *Channel) Underlying() Type { return c }
func (c *Channel) isType()          {}

// NewChannel creates a new channel type.
func NewChannel(elem Type) *Channel {
	return &Channel{Elem: elem}
}

// Map represents a map type.
type Map struct {
	Key   Type
	Value Type
}

func (m *Map) String() string {
	return fmt.Sprintf("map[%s]%s", m.Key, m.Value)
}

func (m *Map) Equals(other Type) bool {
	om, ok := other.(*Map)
	if !ok {
		return false
	}
	return m.Key.Equals(om.Key) && m.Value.Equals(om.Value)
}

func (m *Map) Underlying() Type { return m }
func (m *Map) isType()          {}

// NewMap creates a new map type.
func NewMap(key, value Type) *Map {
	return &Map{Key: key, Value: value}
}

// IsValidMapKey reports whether the type is a valid map key type.
// Only comparable types (int, string, bool) are valid map keys.
func IsValidMapKey(t Type) bool {
	switch typ := t.Underlying().(type) {
	case *Basic:
		return typ.Kind == Bool || typ.Kind == Int || typ.Kind == String ||
			typ.Kind == Int8 || typ.Kind == Int16 || typ.Kind == Int32 || typ.Kind == Int64 ||
			typ.Kind == Uint || typ.Kind == Uint8 || typ.Kind == Uint16 || typ.Kind == Uint32 || typ.Kind == Uint64
	default:
		return false
	}
}

// TypeAlias represents a type alias.
type TypeAlias struct {
	Name       string
	Underlying_ Type
}

func (t *TypeAlias) String() string {
	return t.Name
}

func (t *TypeAlias) Equals(other Type) bool {
	return t.Underlying_.Equals(other.Underlying())
}

func (t *TypeAlias) Underlying() Type { return t.Underlying_ }
func (t *TypeAlias) isType()          {}

// NewTypeAlias creates a new type alias.
func NewTypeAlias(name string, underlying Type) *TypeAlias {
	return &TypeAlias{Name: name, Underlying_: underlying}
}

// IsAssignableTo reports whether a value of type src can be assigned to type dst.
func IsAssignableTo(src, dst Type) bool {
	// Same type is always assignable
	if src.Equals(dst) {
		return true
	}

	// Check underlying types
	if src.Underlying().Equals(dst.Underlying()) {
		return true
	}

	// Arrays are assignable to slices of the same element type
	// This allows []int{1, 2, 3} to be passed where []int is expected
	if srcArr, ok := src.Underlying().(*Array); ok {
		if dstSlice, ok := dst.Underlying().(*Slice); ok {
			if srcArr.Elem.Equals(dstSlice.Elem) {
				return true
			}
		}
	}

	return false
}

// IsComparable reports whether values of type t can be compared with == and !=.
func IsComparable(t Type) bool {
	switch t := t.Underlying().(type) {
	case *Basic:
		return t.Kind != Invalid
	case *Pointer:
		return true
	case *Struct:
		for _, f := range t.Fields {
			if !IsComparable(f.Type) {
				return false
			}
		}
		return true
	case *Array:
		return IsComparable(t.Elem)
	case *Tuple:
		for _, e := range t.Elems {
			if !IsComparable(e) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// IsOrdered reports whether values of type t can be compared with <, <=, >, >=.
func IsOrdered(t Type) bool {
	switch t := t.Underlying().(type) {
	case *Basic:
		return t.IsNumeric() || t.Kind == String
	default:
		return false
	}
}

// TypeParam represents a type parameter (e.g., T in fn foo<T>).
type TypeParam struct {
	Name string
	ID   int // unique ID within scope for distinguishing different type params with same name
}

func (t *TypeParam) String() string { return t.Name }
func (t *TypeParam) Equals(other Type) bool {
	if ot, ok := other.(*TypeParam); ok {
		return t.ID == ot.ID
	}
	return false
}
func (t *TypeParam) Underlying() Type { return t }
func (t *TypeParam) isType()          {}

// NewTypeParam creates a new type parameter.
func NewTypeParam(name string, id int) *TypeParam {
	return &TypeParam{Name: name, ID: id}
}

// TypeSize returns the size in bytes of any type.
func TypeSize(t Type) int {
	switch typ := t.Underlying().(type) {
	case *Basic:
		return typ.Size()
	case *Pointer:
		return 8
	case *Array:
		return 24 // fat pointer [ptr, len, cap]
	case *Slice:
		return 24 // fat pointer [ptr, len, cap]
	case *Struct:
		size := 0
		for _, f := range typ.Fields {
			size += TypeSize(f.Type)
		}
		return size
	case *Enum:
		return 8 + typ.MaxVariantSize() // tag + max variant data
	case *Function:
		return 8 // function pointer
	case *Tuple:
		size := 0
		for _, e := range typ.Elems {
			size += TypeSize(e)
		}
		return size
	case *Channel:
		return 8 // channel handle
	case *Map:
		return 8 // pointer to map header
	default:
		return 8 // default to pointer size
	}
}
