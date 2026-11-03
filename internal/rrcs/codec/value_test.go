package codec

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSpecStructAndArray(t *testing.T) {
	t.Run("§5.6.3 struct", func(t *testing.T) {
		got, err := specValue(specStructExample)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		want := Struct(Member{"lowerBound", Int(18)}, Member{"upperBound", Int(139)})
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	t.Run("§5.6.4 array", func(t *testing.T) {
		got, err := specValue(specArrayExample)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		want := Array(Int(12), String("Egypt"), Bool(false), Int(-31))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
}

// Every scalar of the §5.6.2 table, with the example the table prints.
func TestScalarTable(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want Value
	}{
		{"i4", `<value><i4>-12</i4></value>`, Int(-12)},
		{"int", `<value><int>-12</int></value>`, Int(-12)},
		{"boolean", `<value><boolean>1</boolean></value>`, Bool(true)},
		{"string", `<value><string>hello world</string></value>`, String("hello world")},
		{"double", `<value><double>-12.214</double></value>`, Double(-12.214)},
		{"dateTime", `<value><dateTime.iso8601>19980717T14:08:55</dateTime.iso8601></value>`, DateTime("19980717T14:08:55")},
		{"base64", `<value><base64>eW91IGNhbid0IHJlYWQgdGhpcyE=</base64></value>`, Base64([]byte("you can't read this!"))},
		{"untyped is a string", `<value>hello world</value>`, String("hello world")},
		{"empty untyped", `<value></value>`, String("")},
		{"padded int", `<value> <int> 7 </int> </value>`, Int(7)},
		{"wrapped base64", "<value><base64>eW91IGNhbid0\n IHJlYWQgdGhpcyE=</base64></value>", Base64([]byte("you can't read this!"))},
		{"empty struct", `<value><struct></struct></value>`, Struct()},
		{"empty array", `<value><array><data></data></array></value>`, Array()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := specValue(tc.doc)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if !valuesEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// valuesEqual compares two values, treating a nil and an empty slice
// alike: an empty struct or array decodes to an empty, non-nil slice.
func valuesEqual(a, b Value) bool {
	if a.Kind != b.Kind || a.Int != b.Int || a.Bool != b.Bool || a.Double != b.Double || a.Str != b.Str {
		return false
	}
	if string(a.Bytes) != string(b.Bytes) || len(a.Members) != len(b.Members) || len(a.Items) != len(b.Items) {
		return false
	}
	for i := range a.Members {
		if a.Members[i].Name != b.Members[i].Name || !valuesEqual(a.Members[i].Value, b.Members[i].Value) {
			return false
		}
	}
	for i := range a.Items {
		if !valuesEqual(a.Items[i], b.Items[i]) {
			return false
		}
	}
	return true
}

func TestValueMalformed(t *testing.T) {
	tests := map[string]string{
		"two types":          `<value><int>1</int><int>2</int></value>`,
		"unknown type":       `<value><nil/></value>`,
		"int overflow":       `<value><int>2147483648</int></value>`,
		"int text":           `<value><i4>twelve</i4></value>`,
		"boolean 2":          `<value><boolean>2</boolean></value>`,
		"boolean word":       `<value><boolean>true</boolean></value>`,
		"double text":        `<value><double>abc</double></value>`,
		"base64 text":        `<value><base64>!!!</base64></value>`,
		"struct stray":       `<value><struct><value>1</value></struct></value>`,
		"member no name":     `<value><struct><member><value>1</value></member></struct></value>`,
		"member no value":    `<value><struct><member><name>a</name></member></struct></value>`,
		"member bad value":   `<value><struct><member><name>a</name><value><int>x</int></value></member></struct></value>`,
		"array no data":      `<value><array></array></value>`,
		"array two children": `<value><array><data/><data/></array></value>`,
		"array stray":        `<value><array><data><int>1</int></data></array></value>`,
		"array bad value":    `<value><array><data><value><int>x</int></value></data></array></value>`,
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := specValue(doc); !errors.Is(err, ErrMalformed) {
				t.Errorf("got %v, want ErrMalformed", err)
			}
		})
	}
}

// Every kind survives encode then decode, and text is escaped.
func TestRoundTripEveryKind(t *testing.T) {
	v := Struct(
		Member{"s", String(`a<b>&"c"`)},
		Member{"i", Int(-2147483648)},
		Member{"b", Bool(true)},
		Member{"f", Bool(false)},
		Member{"d", Double(12.5)},
		Member{"t", DateTime("19980717T14:08:55")},
		Member{"x", Base64([]byte{0, 1, 2, 255})},
		Member{"a", Array(Int(1), Array(String("nested")), Struct(Member{"k", Int(2)}))},
	)
	doc, err := EncodeResponse(v)
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	if !strings.Contains(string(doc), "a&lt;b&gt;&amp;") {
		t.Errorf("text was not escaped: %s", doc)
	}
	got, err := DecodeResponse(doc)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if !valuesEqual(got.Value, v) {
		t.Errorf("got  %+v\nwant %+v", got.Value, v)
	}
}

func TestKindString(t *testing.T) {
	want := map[Kind]string{
		KindString: "string", KindInt: "int", KindBool: "boolean", KindDouble: "double",
		KindDateTime: "dateTime.iso8601", KindBase64: "base64", KindStruct: "struct", KindArray: "array",
		Kind(-1): "kind(-1)", Kind(99): "kind(99)",
	}
	for k, s := range want {
		if k.String() != s {
			t.Errorf("Kind(%d).String() = %q, want %q", int(k), k.String(), s)
		}
	}
}

func TestAccessors(t *testing.T) {
	s := Struct(Member{"a", Int(1)})
	if v, ok := s.Field("a"); !ok || v.Int != 1 {
		t.Errorf("Field(a) = %+v, %v", v, ok)
	}
	if _, ok := s.Field("b"); ok {
		t.Error("Field(b) found a member that is not there")
	}
	if _, ok := Int(1).Field("a"); ok {
		t.Error("Field on a non-struct found a member")
	}
	if v, err := Int(7).AsInt(); err != nil || v != 7 {
		t.Errorf("AsInt = %d, %v", v, err)
	}
	if v, err := Bool(true).AsBool(); err != nil || !v {
		t.Errorf("AsBool = %v, %v", v, err)
	}
	if v, err := String("x").AsString(); err != nil || v != "x" {
		t.Errorf("AsString = %q, %v", v, err)
	}
	if _, err := String("x").AsInt(); !errors.Is(err, ErrType) {
		t.Errorf("AsInt on a string: %v", err)
	}
	if _, err := Int(1).AsBool(); !errors.Is(err, ErrType) {
		t.Errorf("AsBool on an int: %v", err)
	}
	if _, err := Int(1).AsString(); !errors.Is(err, ErrType) {
		t.Errorf("AsString on an int: %v", err)
	}
}
