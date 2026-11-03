package codec

import (
	"errors"
	"reflect"
	"testing"
)

func TestDecodeResponseSpecExamples(t *testing.T) {
	t.Run("§5.6.5 value", func(t *testing.T) {
		got, err := DecodeResponse([]byte(specResponseExample))
		if err != nil {
			t.Fatalf("DecodeResponse: %v", err)
		}
		if got.Fault != nil || !reflect.DeepEqual(got.Value, String("South Dakota")) {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("§5.6.5 fault", func(t *testing.T) {
		got, err := DecodeResponse([]byte(specFaultExample))
		if err != nil {
			t.Fatalf("DecodeResponse: %v", err)
		}
		want := &Fault{Code: 4, String: "Too many parameters."}
		if !reflect.DeepEqual(got.Fault, want) {
			t.Errorf("got %+v, want %+v", got.Fault, want)
		}
		if got.Fault.Error() != "rrcs: XML-RPC fault 4: Too many parameters." {
			t.Errorf("Error() = %q", got.Fault.Error())
		}
	})
	t.Run("§11.1 SetXp answer", func(t *testing.T) {
		got, err := DecodeResponse([]byte(specSetXpResponse))
		if err != nil {
			t.Fatalf("DecodeResponse: %v", err)
		}
		want := Array(String("C0002817191"), Int(0))
		if !reflect.DeepEqual(got.Value, want) {
			t.Errorf("got %+v, want %+v", got.Value, want)
		}
	})
}

// The §11.1 answer, encoded by us, is the specification's document
// without its layout whitespace.
func TestEncodeResponseSetXp(t *testing.T) {
	got, err := EncodeResponse(Array(String("C0002817191"), Int(0)))
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}
	want := `<?xml version="1.0"?><methodResponse><params><param>` +
		`<value><array><data>` +
		`<value><string>C0002817191</string></value>` +
		`<value><int>0</int></value>` +
		`</data></array></value>` +
		`</param></params></methodResponse>`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if stripLayout(specSetXpResponse) != want {
		t.Errorf("the expected bytes are not the specification's document without layout")
	}
}

func TestEncodeResponseRejectsUnknownKind(t *testing.T) {
	inArray := Array(Value{Kind: Kind(-1)})
	inStruct := Struct(Member{"a", Value{Kind: Kind(42)}})
	for _, v := range []Value{inArray, inStruct} {
		if _, err := EncodeResponse(v); !errors.Is(err, ErrType) {
			t.Errorf("got %v, want ErrType", err)
		}
	}
}

func TestEncodeFaultRoundTrip(t *testing.T) {
	doc := EncodeFault(4, "Too many parameters.")
	if string(doc) != stripLayout(specFaultExample) {
		t.Errorf("got  %s\nwant %s", doc, stripLayout(specFaultExample))
	}
	got, err := DecodeResponse(doc)
	if err != nil || got.Fault == nil || got.Fault.Code != 4 {
		t.Errorf("round trip: %+v, %v", got, err)
	}
}

func TestDecodeResponseMalformed(t *testing.T) {
	tests := map[string]string{
		"not xml":           `<methodResponse>`,
		"wrong root":        `<methodCall/>`,
		"empty":             `<methodResponse/>`,
		"fault and params":  `<methodResponse><fault><value><struct/></value></fault><params><param><value>1</value></param></params></methodResponse>`,
		"bare value":        `<methodResponse><value>1</value></methodResponse>`,
		"unknown child":     `<methodResponse><other/></methodResponse>`,
		"two params":        `<methodResponse><params><param><value>1</value></param><param><value>2</value></param></params></methodResponse>`,
		"params not param":  `<methodResponse><params><value>1</value></params></methodResponse>`,
		"param no value":    `<methodResponse><params><param/></params></methodResponse>`,
		"fault no value":    `<methodResponse><fault/></methodResponse>`,
		"fault bad value":   `<methodResponse><fault><value><int>x</int></value></fault></methodResponse>`,
		"fault not struct":  `<methodResponse><fault><value><int>4</int></value></fault></methodResponse>`,
		"fault wrong types": `<methodResponse><fault><value><struct><member><name>faultCode</name><value>4</value></member><member><name>faultString</name><value>x</value></member></struct></value></fault></methodResponse>`,
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeResponse([]byte(doc)); !errors.Is(err, ErrMalformed) {
				t.Errorf("got %v, want ErrMalformed", err)
			}
		})
	}
}
