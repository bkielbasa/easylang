// Package types defines the type system for ease.
package types

// Typ contains the predefined basic types indexed by their kind.
var Typ = []*Basic{
	Invalid: {Invalid, "invalid"},
	Bool:    {Bool, "bool"},
	Int:     {Int, "int"},
	Int8:    {Int8, "int8"},
	Int16:   {Int16, "int16"},
	Int32:   {Int32, "int32"},
	Int64:   {Int64, "int64"},
	Uint:    {Uint, "uint"},
	Uint8:   {Uint8, "uint8"},
	Uint16:  {Uint16, "uint16"},
	Uint32:  {Uint32, "uint32"},
	Uint64:  {Uint64, "uint64"},
	Float32: {Float32, "float32"},
	Float64: {Float64, "float64"},
	String:  {String, "string"},
	Unit:    {Unit, "()"},
}

// Convenience aliases for commonly used types.
var (
	TypInvalid = Typ[Invalid]
	TypBool    = Typ[Bool]
	TypInt     = Typ[Int]
	TypInt8    = Typ[Int8]
	TypInt16   = Typ[Int16]
	TypInt32   = Typ[Int32]
	TypInt64   = Typ[Int64]
	TypUint    = Typ[Uint]
	TypUint8   = Typ[Uint8]
	TypUint16  = Typ[Uint16]
	TypUint32  = Typ[Uint32]
	TypUint64  = Typ[Uint64]
	TypFloat32 = Typ[Float32]
	TypFloat64 = Typ[Float64]
	TypString  = Typ[String]
	TypUnit    = Typ[Unit]
)

// LookupBasicType returns the basic type for the given name, or nil if not found.
func LookupBasicType(name string) *Basic {
	switch name {
	case "bool":
		return TypBool
	case "int":
		return TypInt
	case "int8":
		return TypInt8
	case "int16":
		return TypInt16
	case "int32":
		return TypInt32
	case "int64":
		return TypInt64
	case "uint":
		return TypUint
	case "uint8":
		return TypUint8
	case "uint16":
		return TypUint16
	case "uint32":
		return TypUint32
	case "uint64":
		return TypUint64
	case "float32":
		return TypFloat32
	case "float64":
		return TypFloat64
	case "string":
		return TypString
	default:
		return nil
	}
}

// BuiltinFunction represents a builtin function.
type BuiltinFunction struct {
	Name string
	Type *Function
}

// Builtins contains the predefined builtin functions.
var Builtins = []*BuiltinFunction{
	{
		Name: "println",
		Type: &Function{
			Params:     []*Param{{Name: "args", Type: TypString}},
			Result:     TypUnit,
			IsVariadic: true,
		},
	},
	{
		Name: "print",
		Type: &Function{
			Params:     []*Param{{Name: "args", Type: TypString}},
			Result:     TypUnit,
			IsVariadic: true,
		},
	},
	{
		Name: "len",
		Type: &Function{
			Params: []*Param{{Name: "v", Type: TypString}}, // polymorphic, handled specially
			Result: TypInt,
		},
	},
	{
		Name: "panic",
		Type: &Function{
			Params: []*Param{{Name: "msg", Type: TypString}},
			Result: TypUnit, // actually never returns
		},
	},
	{
		Name: "heap_alloc",
		Type: &Function{
			Params: []*Param{{Name: "size", Type: TypInt}},
			Result: TypInt, // returns pointer as int (address)
		},
	},
	// Low-level file syscalls
	{
		Name: "syscall_open",
		Type: &Function{
			Params: []*Param{
				{Name: "path", Type: TypString},
				{Name: "flags", Type: TypInt},
				{Name: "mode", Type: TypInt},
			},
			Result: TypInt, // returns fd or -1 on error
		},
	},
	{
		Name: "syscall_read",
		Type: &Function{
			Params: []*Param{
				{Name: "fd", Type: TypInt},
				{Name: "buf", Type: TypInt}, // pointer as int (from heap_alloc)
				{Name: "size", Type: TypInt},
			},
			Result: TypInt, // returns bytes read or -1 on error
		},
	},
	{
		Name: "syscall_write",
		Type: &Function{
			Params: []*Param{
				{Name: "fd", Type: TypInt},
				{Name: "buf", Type: TypString}, // string (passed as pointer)
				{Name: "size", Type: TypInt},
			},
			Result: TypInt, // returns bytes written or -1 on error
		},
	},
	{
		Name: "syscall_close",
		Type: &Function{
			Params: []*Param{{Name: "fd", Type: TypInt}},
			Result: TypInt, // returns 0 on success, -1 on error
		},
	},
	// String operations (used by stdlib/strings module)
	{
		Name: "str_contains",
		Type: &Function{
			Params: []*Param{
				{Name: "s", Type: TypString},
				{Name: "substr", Type: TypString},
			},
			Result: TypBool,
		},
	},
	{
		Name: "str_starts_with",
		Type: &Function{
			Params: []*Param{
				{Name: "s", Type: TypString},
				{Name: "prefix", Type: TypString},
			},
			Result: TypBool,
		},
	},
	{
		Name: "str_ends_with",
		Type: &Function{
			Params: []*Param{
				{Name: "s", Type: TypString},
				{Name: "suffix", Type: TypString},
			},
			Result: TypBool,
		},
	},
	{
		Name: "str_index_of",
		Type: &Function{
			Params: []*Param{
				{Name: "s", Type: TypString},
				{Name: "substr", Type: TypString},
			},
			Result: TypInt,
		},
	},
	{
		Name: "str_substring",
		Type: &Function{
			Params: []*Param{
				{Name: "s", Type: TypString},
				{Name: "start", Type: TypInt},
				{Name: "end", Type: TypInt},
			},
			Result: TypString,
		},
	},
	{
		Name: "str_char_at",
		Type: &Function{
			Params: []*Param{
				{Name: "s", Type: TypString},
				{Name: "i", Type: TypInt},
			},
			Result: TypInt,
		},
	},
	{
		Name: "str_concat",
		Type: &Function{
			Params: []*Param{
				{Name: "a", Type: TypString},
				{Name: "b", Type: TypString},
			},
			Result: TypString,
		},
	},
	{
		Name: "str_trim",
		Type: &Function{
			Params: []*Param{
				{Name: "s", Type: TypString},
				{Name: "chars", Type: TypString},
			},
			Result: TypString,
		},
	},
	{
		Name: "str_replace",
		Type: &Function{
			Params: []*Param{
				{Name: "s", Type: TypString},
				{Name: "old", Type: TypString},
				{Name: "new", Type: TypString},
			},
			Result: TypString,
		},
	},
	{
		Name: "str_split",
		Type: &Function{
			Params: []*Param{
				{Name: "s", Type: TypString},
				{Name: "sep", Type: TypString},
			},
			Result: &Slice{Elem: TypString},
		},
	},
	{
		Name: "str_eq",
		Type: &Function{
			Params: []*Param{
				{Name: "a", Type: TypString},
				{Name: "b", Type: TypString},
			},
			Result: TypBool,
		},
	},
	{
		Name: "str_ne",
		Type: &Function{
			Params: []*Param{
				{Name: "a", Type: TypString},
				{Name: "b", Type: TypString},
			},
			Result: TypBool,
		},
	},
	// OS/File operations (used by stdlib/io module)
	{
		Name: "os.ReadFile",
		Type: &Function{
			Params: []*Param{{Name: "path", Type: TypString}},
			Result: TypString,
		},
	},
	{
		Name: "os.WriteFile",
		Type: &Function{
			Params: []*Param{
				{Name: "path", Type: TypString},
				{Name: "content", Type: TypString},
			},
			Result: TypUnit,
		},
	},
}

// LookupBuiltin returns the builtin function with the given name, or nil if not found.
func LookupBuiltin(name string) *BuiltinFunction {
	for _, b := range Builtins {
		if b.Name == name {
			return b
		}
	}
	return nil
}
