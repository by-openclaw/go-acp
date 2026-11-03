package codec

import "fmt"

// Kind is the XML-RPC type of a Value (§5.6.2 – §5.6.4).
type Kind int

const (
	// KindString is <string>, and also a <value> with no type tag:
	// "If no type is indicated, the type is string" (§5.6.2).
	KindString Kind = iota
	// KindInt is <i4> or <int>, a four-byte signed integer.
	KindInt
	// KindBool is <boolean>, 0 or 1.
	KindBool
	// KindDouble is <double>.
	KindDouble
	// KindDateTime is <dateTime.iso8601>, kept as printed.
	KindDateTime
	// KindBase64 is <base64>.
	KindBase64
	// KindStruct is <struct>: named members, in wire order.
	KindStruct
	// KindArray is <array>: unnamed values.
	KindArray
)

var kindNames = [...]string{"string", "int", "boolean", "double", "dateTime.iso8601", "base64", "struct", "array"}

// String names the kind by its XML-RPC tag.
func (k Kind) String() string {
	if k < 0 || int(k) >= len(kindNames) {
		return fmt.Sprintf("kind(%d)", int(k))
	}
	return kindNames[k]
}

// Member is one named element of a struct.
type Member struct {
	Name  string
	Value Value
}

// Value is one XML-RPC value. Exactly the field matching Kind is
// meaningful. Members keep their wire order: a struct is encoded in the
// order it was built, which is what makes the encoding deterministic.
type Value struct {
	Kind    Kind
	Int     int32
	Bool    bool
	Double  float64
	Str     string // KindString and KindDateTime
	Bytes   []byte // KindBase64
	Members []Member
	Items   []Value
}

// Int builds an integer value.
func Int(v int32) Value { return Value{Kind: KindInt, Int: v} }

// Bool builds a boolean value.
func Bool(v bool) Value { return Value{Kind: KindBool, Bool: v} }

// String builds a string value.
func String(v string) Value { return Value{Kind: KindString, Str: v} }

// Double builds a double value.
func Double(v float64) Value { return Value{Kind: KindDouble, Double: v} }

// DateTime builds a dateTime.iso8601 value from its printed form.
func DateTime(v string) Value { return Value{Kind: KindDateTime, Str: v} }

// Base64 builds a binary value.
func Base64(v []byte) Value { return Value{Kind: KindBase64, Bytes: v} }

// Struct builds a struct value from members in the order given.
func Struct(members ...Member) Value { return Value{Kind: KindStruct, Members: members} }

// Array builds an array value.
func Array(items ...Value) Value { return Value{Kind: KindArray, Items: items} }

// Field returns the named member of a struct. The second result is
// false when the value is not a struct or has no such member.
func (v Value) Field(name string) (Value, bool) {
	if v.Kind != KindStruct {
		return Value{}, false
	}
	for _, m := range v.Members {
		if m.Name == name {
			return m.Value, true
		}
	}
	return Value{}, false
}

// AsInt returns the integer, or an error naming the kind found.
func (v Value) AsInt() (int32, error) {
	if v.Kind != KindInt {
		return 0, fmt.Errorf("%w: want int, got %s", ErrType, v.Kind)
	}
	return v.Int, nil
}

// AsBool returns the boolean, or an error naming the kind found.
func (v Value) AsBool() (bool, error) {
	if v.Kind != KindBool {
		return false, fmt.Errorf("%w: want boolean, got %s", ErrType, v.Kind)
	}
	return v.Bool, nil
}

// AsString returns the string, or an error naming the kind found.
func (v Value) AsString() (string, error) {
	if v.Kind != KindString {
		return "", fmt.Errorf("%w: want string, got %s", ErrType, v.Kind)
	}
	return v.Str, nil
}
