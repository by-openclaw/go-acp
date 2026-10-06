package codec

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeCallSpecExamples(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want Call
	}{
		{"§5.6.1 structure", specCallExample, Call{Method: "examples.getStateName", Params: []Value{Int(41)}}},
		{"§11.1 SetXp", specSetXpCall, Call{Method: "SetXp", Params: []Value{
			String("C0002817191"), Int(1), Int(2), Int(19), Int(1), Int(5), Int(3),
		}}},
		{"§11.2 UpstreamFailed", specUpstreamFailedCall, Call{Method: "UpstreamFailed", Params: []Value{
			String("R1947584733"), Int(2),
		}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeCall([]byte(tc.doc))
			if err != nil {
				t.Fatalf("DecodeCall: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// The §11.1 request, encoded by us, is the specification's document
// without its layout whitespace.
func TestEncodeCallSetXp(t *testing.T) {
	got, err := EncodeCall("SetXp", String("C0002817191"), Int(1), Int(2), Int(19), Int(1), Int(5), Int(3))
	if err != nil {
		t.Fatalf("EncodeCall: %v", err)
	}
	want := `<?xml version="1.0"?><methodCall><methodName>SetXp</methodName><params>` +
		`<param><value><string>C0002817191</string></value></param>` +
		`<param><value><int>1</int></value></param>` +
		`<param><value><int>2</int></value></param>` +
		`<param><value><int>19</int></value></param>` +
		`<param><value><int>1</int></value></param>` +
		`<param><value><int>5</int></value></param>` +
		`<param><value><int>3</int></value></param>` +
		`</params></methodCall>`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if stripLayout(specSetXpCall) != want {
		t.Errorf("the expected bytes are not the specification's document without layout")
	}
}

// stripLayout removes the line breaks and indentation of a printed
// document, which carry nothing between elements.
func stripLayout(doc string) string {
	var b strings.Builder
	for _, line := range strings.Split(doc, "\n") {
		b.WriteString(strings.TrimSpace(line))
	}
	return b.String()
}

func TestEncodeCallNoParams(t *testing.T) {
	got, err := EncodeCall("GetAlive")
	if err != nil {
		t.Fatalf("EncodeCall: %v", err)
	}
	// §5.6.1: <params> is present "if the procedure call has parameters".
	want := `<?xml version="1.0"?><methodCall><methodName>GetAlive</methodName></methodCall>`
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	back, err := DecodeCall(got)
	if err != nil || back.Method != "GetAlive" || len(back.Params) != 0 {
		t.Errorf("round trip: %+v, %v", back, err)
	}
}

func TestEncodeCallRejects(t *testing.T) {
	for _, name := range []string{"", "Set Xp", "Set<Xp>", "Sét"} {
		if _, err := EncodeCall(name); !errors.Is(err, ErrMalformed) {
			t.Errorf("method %q: got %v, want ErrMalformed", name, err)
		}
	}
	if _, err := EncodeCall("SetXp", Value{Kind: Kind(42)}); !errors.Is(err, ErrType) {
		t.Errorf("unknown kind: got %v, want ErrType", err)
	}
}

func TestMethodNameCharacters(t *testing.T) {
	// §5.6.1 lists them: letters, digits, underscore, dot, colon, slash.
	if !validMethodName("GetAllLogicSources_v2") || !validMethodName("a.b:c/d9") {
		t.Error("a name of allowed characters was refused")
	}
}

func TestDecodeCallMalformed(t *testing.T) {
	tests := map[string]string{
		"not xml":             `<methodCall><methodName>X</methodName>`,
		"empty":               ``,
		"wrong root":          `<methodResponse/>`,
		"no method name":      `<methodCall><params/></methodCall>`,
		"bad method name":     `<methodCall><methodName>a b</methodName></methodCall>`,
		"stray element":       `<methodCall><methodName>X</methodName><extra/></methodCall>`,
		"param outside":       `<methodCall><methodName>X</methodName><param><value>1</value></param></methodCall>`,
		"value outside":       `<methodCall><methodName>X</methodName><value>1</value></methodCall>`,
		"not a param":         `<methodCall><methodName>X</methodName><params><value>1</value></params></methodCall>`,
		"param without value": `<methodCall><methodName>X</methodName><params><param/></params></methodCall>`,
		"param with two":      `<methodCall><methodName>X</methodName><params><param><value>1</value><value>2</value></param></params></methodCall>`,
		"bad value":           `<methodCall><methodName>X</methodName><params><param><value><int>x</int></value></param></params></methodCall>`,
		"two roots":           `<methodCall><methodName>X</methodName></methodCall><methodCall/>`,
	}
	for name, doc := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeCall([]byte(doc)); !errors.Is(err, ErrMalformed) {
				t.Errorf("got %v, want ErrMalformed", err)
			}
		})
	}
}
