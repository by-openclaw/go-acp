package registers

import (
	"strings"
	"testing"
)

// A duplicate URN is a data-authoring bug that must stop the process
// at init, not silently shadow the first entry: two registers that
// disagree about what a URN accepts would make Accepts() depend on
// link order.
func TestRegisterPanicsOnDuplicateURN(t *testing.T) {
	const dup = "urn:x-nmos:cap:format:media_type"
	before := len(urns)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on duplicate URN")
		}
		if msg, _ := r.(string); !strings.Contains(msg, dup) {
			t.Fatalf("panic = %v, want it to name %s", r, dup)
		}
		if len(urns) != before {
			t.Fatalf("catalogue grew from %d to %d despite the panic", before, len(urns))
		}
	}()
	register(Register{Name: "dup", Version: "v0", Params: []Param{{URN: dup, Kind: KindEnum}}})
}

// Accepts is only decisive for enum/boolean; numeric and rational
// kinds defer to the caller holding the parsed value, and a string
// parameter accepts any label.
func TestAcceptsPerKind(t *testing.T) {
	cases := []struct {
		name  string
		param Param
		value string
		want  bool
	}{
		{"boolean true", Param{Kind: KindBoolean}, "true", true},
		{"boolean false", Param{Kind: KindBoolean}, "false", true},
		{"boolean other word", Param{Kind: KindBoolean}, "yes", false},
		{"integer defers to caller", Param{Kind: KindInteger}, "anything", true},
		{"number defers to caller", Param{Kind: KindNumber}, "1.5", true},
		{"rational defers to caller", Param{Kind: KindRational}, "x", true},
		{"string accepts any label", Param{Kind: KindString}, "label", true},
		{"enum listed", Param{Kind: KindEnum, Values: []string{"a", "b"}}, "b", true},
		{"enum unlisted", Param{Kind: KindEnum, Values: []string{"a", "b"}}, "c", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.param.Accepts(tc.value); got != tc.want {
				t.Fatalf("Accepts(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestLookupUnknownURN(t *testing.T) {
	if p, ok := Lookup("urn:x-nmos:cap:format:nonexistent"); ok || p.URN != "" {
		t.Fatalf("Lookup(unknown) = (%+v, %v), want zero Param and false", p, ok)
	}
}
