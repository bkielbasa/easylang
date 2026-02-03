package types

import (
	"testing"
)

func TestBasicTypes(t *testing.T) {
	tests := []struct {
		name string
		kind BasicKind
		want string
	}{
		{"int", Int, "int"},
		{"int64", Int64, "int64"},
		{"bool", Bool, "bool"},
		{"string", String, "string"},
		{"unit", Unit, "()"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typ := Typ[tt.kind]
			if typ.String() != tt.want {
				t.Errorf("Typ[%v].String() = %q, want %q", tt.kind, typ.String(), tt.want)
			}
		})
	}
}

func TestTypeEquality(t *testing.T) {
	// Same basic types should be equal
	if !TypInt.Equals(TypInt) {
		t.Error("int should equal int")
	}

	// Different basic types should not be equal
	if TypInt.Equals(TypBool) {
		t.Error("int should not equal bool")
	}

	// Function types
	fn1 := NewFunction([]*Param{{Name: "a", Type: TypInt}}, TypInt)
	fn2 := NewFunction([]*Param{{Name: "a", Type: TypInt}}, TypInt)
	fn3 := NewFunction([]*Param{{Name: "a", Type: TypBool}}, TypInt)

	if !fn1.Equals(fn2) {
		t.Error("identical function types should be equal")
	}
	if fn1.Equals(fn3) {
		t.Error("different function types should not be equal")
	}
}

func TestBasicTypeProperties(t *testing.T) {
	// Integer properties
	if !TypInt.IsInteger() {
		t.Error("int should be an integer")
	}
	if !TypInt.IsSigned() {
		t.Error("int should be signed")
	}
	if TypInt.IsFloat() {
		t.Error("int should not be a float")
	}

	// Float properties
	if !TypFloat64.IsFloat() {
		t.Error("float64 should be a float")
	}
	if TypFloat64.IsInteger() {
		t.Error("float64 should not be an integer")
	}

	// Bool properties
	if TypBool.IsNumeric() {
		t.Error("bool should not be numeric")
	}
}

func TestStructType(t *testing.T) {
	s := NewStruct("Point", []*Field{
		{Name: "x", Type: TypInt},
		{Name: "y", Type: TypInt},
	})

	if s.String() != "Point" {
		t.Errorf("struct.String() = %q, want %q", s.String(), "Point")
	}

	xField := s.FieldByName("x")
	if xField == nil {
		t.Error("should find field x")
	}
	if !xField.Type.Equals(TypInt) {
		t.Error("field x should be int")
	}

	zField := s.FieldByName("z")
	if zField != nil {
		t.Error("should not find field z")
	}
}

func TestArrayType(t *testing.T) {
	arr := NewArray(TypInt, 10)

	if arr.String() != "[int; 10]" {
		t.Errorf("array.String() = %q, want %q", arr.String(), "[int; 10]")
	}

	arr2 := NewArray(TypInt, 10)
	if !arr.Equals(arr2) {
		t.Error("identical arrays should be equal")
	}

	arr3 := NewArray(TypInt, 5)
	if arr.Equals(arr3) {
		t.Error("different sized arrays should not be equal")
	}
}

func TestFunctionType(t *testing.T) {
	fn := NewFunction(
		[]*Param{
			{Name: "a", Type: TypInt},
			{Name: "b", Type: TypInt},
		},
		TypInt,
	)

	want := "fn(a: int, b: int) -> int"
	if fn.String() != want {
		t.Errorf("fn.String() = %q, want %q", fn.String(), want)
	}

	// Unit return type
	fn2 := NewFunction([]*Param{{Name: "x", Type: TypString}}, TypUnit)
	want2 := "fn(x: string)"
	if fn2.String() != want2 {
		t.Errorf("fn2.String() = %q, want %q", fn2.String(), want2)
	}
}
