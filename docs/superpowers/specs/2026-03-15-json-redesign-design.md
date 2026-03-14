# JSON Redesign + Runtime Reflection Design

## Goal

Redesign the JSON package for maximum developer friendliness: unified Value type instead of 21 type-specific methods, plus struct-based `json.Marshal(struct)` / `json.Unmarshal[User](string)` powered by a new `reflect` stdlib module.

## Layer 1: `reflect` Package

A new stdlib module that exposes struct metadata at runtime. The compiler already tracks struct field info during struct registration in `compiler.ease`. This data gets emitted into the compiled binary as LLVM IR global arrays.

### API

```ease
import "reflect"

// Query struct fields
n := reflect.NumField(user)          // 3
name := reflect.FieldName(user, 0)   // "name"
tag := reflect.FieldType(user, 0)    // reflect.STRING
val := reflect.FieldValue(user, 0)   // the raw i64 value at that offset
reflect.SetFieldValue(user, 0, val)  // write back

// Type tag constants (mirror ast.ease TYPE_* constants)
const INT = 1
const BOOL = 2
const STRING = 3
const STRUCT = 7
const FLOAT = 12
```

**`FieldValue` semantics**: Returns the raw i64 stored at the field's memory slot. For `int` and `bool` fields, this is the value itself. For `string` fields, the i64 is a pointer to the string — it can be used directly anywhere a string is expected (since all values are i64 under type erasure). Callers should use `FieldType` to interpret the returned value correctly. Struct-typed fields are out of scope for v1 (see Scope section).

### Compiler Changes

1. **Extend `RegisterStruct` to accept field types**: Currently `RegisterStruct(name, field_names)` only stores field names. Extend to `RegisterStruct(name, field_names, field_types)` where `field_types` is `[]int` containing TYPE_* constants for each field. Store in a new global array `g_struct_reg_field_types`.

   **Call site**: In `compiler.ease` (the self-hosting compiler — note: `bootstrap/compiler.ease` is the bootstrap compiler, which is the same code compiled by itself), the struct registration loop at ~line 820-837 already iterates over `DECL_FIELD` nodes and has access to `nodes[fi].type_tag`. Collect these into a `field_types_list` alongside `field_names_list` and pass both to `RegisterStruct`. Do NOT use `sema.register_field_type` — that registry is keyed by field name alone (not per-struct), so structs with same-named fields of different types would collide.

2. **Emit struct metadata into binary**: In `llvm.ease`, during the LLVM IR emission phase in `emit_llvm_ir()` — after string constant emission and before function definitions — iterate over the struct registry and emit constant arrays for each registered struct:

   ```llvm
   @reflect.User.count = private constant i64 2
   @reflect.User.names = private constant [2 x ptr] [ptr @.str.name, ptr @.str.age]
   @reflect.User.types = private constant [2 x i64] [i64 3, i64 1]  ; STRING=3, INT=1
   ```

   The struct registry data (`g_struct_reg_names`, `g_struct_reg_field_counts`, `g_struct_reg_field_starts`, `g_struct_reg_field_names`, and the new `g_struct_reg_field_types`) provides all needed info.

3. **New IR opcodes** (added after `OP_ARRAY_SLICE = 87`):
   - `OP_REFLECT_NUM_FIELD = 88` — takes a struct vreg (arg1), returns field count (dest)
   - `OP_REFLECT_FIELD_NAME = 89` — takes struct vreg (arg1) + field index (arg2), returns field name string (dest)
   - `OP_REFLECT_FIELD_TYPE = 90` — takes struct vreg (arg1) + field index (arg2), returns type tag int (dest)
   - `OP_REFLECT_FIELD_VALUE = 91` — takes struct vreg (arg1) + field index (arg2), loads the i64 value at offset `base + index * 8` (dest)
   - `OP_REFLECT_SET_FIELD_VALUE = 92` — takes struct vreg (arg1) + field index (arg2) + value (arg3), stores value at offset. No meaningful dest (assign to a dummy vreg).

4. **Struct type resolution via `str_val`**: The reflect builtins need to know which struct type a value is. The compiler already tracks this via `get_vreg_struct_name()`. During IR generation, when the compiler sees a call to `reflect.NumField(v)`, it resolves `v`'s struct type name and embeds it as `str_val` on the IR instruction. Codegen uses `str_val` to look up the right metadata tables (e.g., `str_val="User"` → `@reflect.User.count`).

5. **Register opcodes in `collect_vregs()`**: Each new opcode must be added to the appropriate vreg collection lists in `collect_vregs()`:
   - dest vreg list: all 5 opcodes (88-92)
   - arg1 vreg list: all 5 opcodes (struct vreg)
   - arg2 vreg list: opcodes 89-92 (field index vreg)
   - arg3 vreg list: opcode 92 only (value vreg for SET_FIELD_VALUE)

### Runtime Data Layout

For each struct `User { name: string, age: int }`, emit into the binary:

```llvm
@reflect.User.count = private constant i64 2
@reflect.User.names = private constant [2 x ptr] [ptr @.str.name, ptr @.str.age]
@reflect.User.types = private constant [2 x i64] [i64 3, i64 1]  ; STRING=3, INT=1
```

Codegen for `OP_REFLECT_FIELD_NAME` with `str_val="User"` loads from `@reflect.User.names[index]`.

## Layer 2: Redesigned `json` Package

### Value Type

Replace the parallel-array storage with a unified Value approach. Internally, `Value` is represented as a two-field struct `{ type: int, data: int }` where `data` holds either the value directly (int, bool) or a pointer (string, doc id):

```ease
struct Value {
    type_tag: int,  // TYPE_STRING, TYPE_NUM, etc.
    data: int,      // the value or pointer
}

const TYPE_STRING = 1
const TYPE_NUM = 2
const TYPE_BOOL = 3
const TYPE_NULL = 4
const TYPE_OBJECT = 5
const TYPE_ARRAY = 6
```

Note: Using a struct instead of an enum avoids the `Doc` vs `Value` conversion issue — `json.Object()` returns a `Value` with `type_tag=TYPE_OBJECT` and `data=doc_id`, so nesting works naturally: `doc.Set("addr", json.Object())`.

### Building JSON

```ease
// Objects
doc := json.Object()
doc.Set("name", json.Str("alice"))
doc.Set("age", json.Num(42))
doc.Set("active", json.Bool(true))

// Nested objects
addr := json.Object()
addr.Set("city", json.Str("NYC"))
doc.Set("address", addr)

// Arrays
tags := json.Array()
tags.Push(json.Str("admin"))
tags.Push(json.Str("user"))
doc.Set("tags", tags)

// Marshal
s := doc.Marshal()  // {"name":"alice","age":42,...}
```

Constructor functions:
- `json.Str(s: string) -> Value` — wraps a string
- `json.Num(n: int) -> Value` — wraps an int
- `json.Bool(b: int) -> Value` — wraps a bool (0=false, nonzero=true)
- `json.Null() -> Value` — null value
- `json.Object() -> Value` — new empty object (TYPE_OBJECT, data=doc_id)
- `json.Array() -> Value` — new empty array (TYPE_ARRAY, data=doc_id)

### Reading JSON

```ease
doc := json.Parse(`{"name":"alice","age":30}`)

// Unified getters
doc.Get("name").String()    // "alice"
doc.Get("age").Int()        // 30
doc.Get("name").Type()      // json.TYPE_STRING

// Array access
arr := doc.Get("tags")
arr.At(0).String()          // "admin"
arr.Len()                   // 2

// Existence check
doc.Has("name")             // 1
```

Value methods:
- `.String() -> string` — extract string (returns "" if not a string)
- `.Int() -> int` — extract int (returns 0 if not a number)
- `.Bool() -> int` — extract bool (returns 0 if not a bool)
- `.Type() -> int` — returns TYPE_STRING/TYPE_NUM/etc.
- `.IsNull() -> int` — returns 1 if null
- `.Set(key: string, val: Value)` — set key-value (only valid for TYPE_OBJECT)
- `.Get(key: string) -> Value` — get value by key (only valid for TYPE_OBJECT)
- `.Has(key: string) -> int` — check if key exists (only valid for TYPE_OBJECT)
- `.At(index: int) -> Value` — get value at index (only valid for TYPE_ARRAY)
- `.Len() -> int` — number of entries (valid for TYPE_OBJECT and TYPE_ARRAY)
- `.Push(val: Value)` — append value (only valid for TYPE_ARRAY)
- `.Marshal() -> string` — serialize to JSON string

### Struct Marshal/Unmarshal

Both `json.Marshal` and `json.Unmarshal` are **compiler builtins** (like `len` and `append`), not regular functions. This is necessary because Ease uses type erasure for generics — the struct type is not available at runtime. The compiler resolves the struct type at compile time via `get_vreg_struct_name()` and emits the correct reflect metadata lookups.

```ease
// Marshal: struct → JSON string
u := User { name: "alice", age: 30, active: true }
s := json.Marshal(u)  // {"name":"alice","age":30,"active":true}

// Unmarshal: JSON string → struct
u2 := json.Unmarshal[User](s)
print(u2.name)  // "alice"
```

**Implementation approach — compile-time resolved runtime helpers**:

The compiler generates calls to Ease runtime helper functions, but resolves the struct type at compile time and passes pointers to the pre-emitted reflect metadata arrays. This avoids both runtime string-based lookup AND generating complex JSON logic as inline LLVM IR.

**`json.Marshal(v)` — compiler builtin (opcode `OP_JSON_MARSHAL = 93`)**:

During IR generation, the compiler recognizes `json.Marshal(expr)`:
1. Resolves the struct type of `expr` via `get_vreg_struct_name()` → e.g., "User"
2. Emits `OP_JSON_MARSHAL` with `arg1=struct_vreg`, `str_val="User"`

During LLVM codegen, `OP_JSON_MARSHAL` with `str_val="User"`:
1. Emits a call to `@json_marshal_struct(ptr %struct_ptr, ptr @reflect.User.names, ptr @reflect.User.types, i64 field_count)` — a runtime Ease function in the json module
2. The runtime function iterates fields using the passed metadata arrays, reads values at `base + i * 8`, and builds the JSON string

**`json.Unmarshal[User](s)` — compiler builtin (opcode `OP_JSON_UNMARSHAL = 94`)**:

During IR generation, the compiler recognizes `json.Unmarshal[T](expr)`:
1. The type parameter `T` (e.g., `User`) is known at compile time (before erasure)
2. Emits `OP_JSON_UNMARSHAL` with `arg1=string_vreg`, `str_val="User"`

During LLVM codegen, `OP_JSON_UNMARSHAL` with `str_val="User"`:
1. Emits a call to `@json_unmarshal_struct(ptr %json_str, ptr @reflect.User.names, ptr @reflect.User.types, i64 field_count, i64 struct_size)` — a runtime Ease function in the json module
2. The runtime function: parses JSON, allocates struct of `struct_size` bytes, for each field finds matching JSON key, converts value, stores at `base + i * 8`, returns struct pointer

**Runtime helpers** (in json stdlib):
```ease
// Called by compiler-generated code — NOT user-facing
fn MarshalStruct(v: int, names_ptr: int, types_ptr: int, count: int) -> string
fn UnmarshalStruct(json_str: string, names_ptr: int, types_ptr: int, count: int, size: int) -> int
```

These helpers use raw pointer arithmetic to read metadata arrays and struct fields. They are internal to the json module and not part of the public API.

### Internal Storage

The global parallel-array pattern is kept internally (it works and the compiler uses it). The new `Value` struct is the public API layer. Internally, `Set("key", json.Str("val"))` still appends to the global arrays — but users never see them.

### Backward Compatibility

Keep old methods as deprecated wrappers:
```ease
fn (d: Doc) SetString(key: string, val: string) { ... }
fn (d: Doc) GetString(key: string) -> string { ... }
// ... etc for all old methods
```

Note: The old API used `Doc` as the primary type. The new API uses `Value`. The deprecated wrappers bridge the gap — old code creates a `Doc` (which is now the internal struct that backs TYPE_OBJECT/TYPE_ARRAY values), and the wrapper methods delegate to the new Value-based internals.

Existing compiler code and tests continue to work unchanged. Remove deprecated methods in a future release.

## Scope

### In scope
- `reflect` package: NumField, FieldName, FieldType, FieldValue, SetFieldValue (5 new IR opcodes: 88-92)
- Extend `RegisterStruct` to store field types (collected from AST, not sema)
- Emit struct metadata as LLVM globals in `emit_llvm_ir()`
- `json.Value` struct and constructor functions (Str, Num, Bool, Null, Object, Array)
- Value methods: Set/Get/At/Has/Len/Push/Marshal/Parse/String/Int/Bool/Type/IsNull
- `json.Marshal(struct)` and `json.Unmarshal[T](string)` as compiler builtins (2 new IR opcodes: 93-94)
- Runtime helper functions for marshal/unmarshal using metadata array pointers
- Backward-compatible deprecated wrappers for old API
- Tests for all of the above

### Out of scope
- JSON float support (parser truncates float values to int — `3.14` becomes `3`)
- JSON struct tags (like Go's `json:"field_name"`)
- Streaming/incremental parsing
- Pretty-printing with indentation
- Nested struct marshal/unmarshal (v1 handles flat structs with int/string/bool fields only)
- Struct-typed fields in reflect (FieldValue returns the raw pointer but there's no way to discover the nested struct's type name)

## Implementation Order

1. **Extend `RegisterStruct`** to accept and store field types — minimal change, no bootstrap impact
2. **Emit reflect metadata** as LLVM globals in `emit_llvm_ir()` — adds data to binary, no new opcodes yet
3. **Reflect opcodes** (5 new: 88-92) — implement IR generation + LLVM codegen + `collect_vregs`
4. **Reflect stdlib** — thin wrapper functions that emit the new opcodes
5. **JSON Value struct + constructors** — pure Ease, no compiler changes
6. **JSON Value methods** (Set/Get/At/Has/Len/Push/Marshal) — wraps internal parallel arrays with Value API
7. **JSON Parse update** — update parser to return Values instead of raw entries
8. **Backward compat wrappers** — old API delegates to new internals
9. **json.Marshal builtin** (opcode 93) — IR generation + codegen + runtime helper
10. **json.Unmarshal builtin** (opcode 94) — IR generation + codegen + runtime helper
11. **Bootstrap convergence** — update seed.ll through gen3/gen4

## Testing

- **Reflect**: field count, names, types, values for structs with string/int/bool fields
- **JSON Value**: construction with Str/Num/Bool/Null/Object/Array, Type(), String(), Int(), Bool(), IsNull()
- **JSON Build**: Object/Array creation, Set/Get/At/Len/Push/Marshal
- **JSON Parse**: round-trip parse → get → verify for string, number, bool, null, nested objects, arrays
- **JSON errors**: Get on nonexistent key returns null Value, At on out-of-bounds returns null Value, Parse of malformed JSON returns empty object
- **Struct Marshal**: various struct shapes (all-strings, mixed types, bool fields)
- **Struct Unmarshal**: JSON string → struct with field verification; missing fields get zero values; extra JSON keys are ignored
- **Backward compat**: old SetString/GetString API still works via wrappers
- **Self-hosting convergence**: compiler uses json module, must still converge
