package acp1

import (
	"strings"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
)

// Several ACP1 object types share one canonical type, so Parameter.Format is
// the only place the exact ObjectType survives an export. Without it the
// round trip a real device goes through — walk, extract to tree.json, serve
// it back — is lossy for ipv4 and file, silently NARROWING for int32 and
// uint8, and refused outright by the provider for alarm and frame.
//
// Found against the Synapse rack at 10.6.250.105: `extract` walked 190 real
// objects and the provider then refused the whole tree with
//
//	1.2.4.0 (Announcements): boolean has no ACP1 mapping — use enum with
//	Off,On for plain booleans, or set format="alarm" ...
//
// because the canonicalizer had mapped the alarm to boolean and never
// written the hint the provider was asking for.
func TestBuildParameterEmitsTheACP1TypeHint(t *testing.T) {
	for _, tc := range []struct {
		name    string
		kind    consumer.ValueKind
		acpType codec.ObjectType
		want    string
	}{
		{"alarm is refused by the provider without it", consumer.KindAlarm, codec.TypeAlarm, "alarm"},
		{"frame is refused by the provider without it", consumer.KindFrame, codec.TypeFrame, "frame"},
		{"ipv4 would degrade to a plain string", consumer.KindIPAddr, codec.TypeIPAddr, "ipv4"},
		{"file would degrade to a plain string", consumer.KindString, codec.TypeFile, "file"},
		{"int32 would narrow to int16", consumer.KindInt, codec.TypeLong, "int32"},
		{"uint8 would widen to int16", consumer.KindUint, codec.TypeByte, "uint8"},

		// Where the canonical type already determines the ACP1 type, the
		// hint would be noise — and for Integer it would restate the
		// provider's own default.
		{"float needs no hint", consumer.KindFloat, codec.TypeFloat, ""},
		{"enum needs no hint", consumer.KindEnum, codec.TypeEnum, ""},
		{"plain integer needs no hint", consumer.KindInt, codec.TypeInteger, ""},
		{"plain string needs no hint", consumer.KindString, codec.TypeString, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := acp1TypeHint(tc.kind, tc.acpType); got != tc.want {
				t.Errorf("acp1TypeHint(%v, %v) = %q, want %q", tc.kind, tc.acpType, got, tc.want)
			}

			obj := consumer.Object{ID: 3, Label: "Announcements", Kind: tc.kind}
			p := buildParameter(obj, tc.acpType, "1.2.4", "root.alarm")
			if tc.want == "" {
				if p.Format != nil {
					t.Errorf("Format = %q, want none", *p.Format)
				}
				return
			}
			if p.Format == nil {
				t.Fatalf("Format is absent; the provider cannot rebuild %v without it", tc.acpType)
			}
			if *p.Format != tc.want {
				t.Errorf("Format = %q, want %q", *p.Format, tc.want)
			}
		})
	}
}

// maxLen is an attribute, not a type hint, so the two coexist in one
// comma-separated Format — which is the shape the provider's formatParts /
// pickTypeHint pair reads.
func TestBuildParameterKeepsMaxLenBesideTheTypeHint(t *testing.T) {
	obj := consumer.Object{ID: 1, Label: "Config File", Kind: consumer.KindString, MaxLen: 32}
	p := buildParameter(obj, codec.TypeFile, "1.2.1", "root.identity")

	if p.Format == nil {
		t.Fatal("Format is absent")
	}
	parts := strings.Split(*p.Format, ",")
	if len(parts) != 2 || parts[0] != "file" || parts[1] != "maxLen=32" {
		t.Errorf("Format = %q, want the type hint first then the attribute", *p.Format)
	}
}

// A plain string keeps carrying maxLen on its own, as it did before the type
// hint existed.
func TestBuildParameterEmitsMaxLenAlone(t *testing.T) {
	obj := consumer.Object{ID: 1, Label: "Name", Kind: consumer.KindString, MaxLen: 16}
	p := buildParameter(obj, codec.TypeString, "1.2.1", "root.identity")

	if p.Format == nil || *p.Format != "maxLen=16" {
		t.Errorf("Format = %v, want maxLen=16", p.Format)
	}
}
