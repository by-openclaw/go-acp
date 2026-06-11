package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// TestEncodeValue_ErrorBranches drives the per-type coercion-failure returns of
// encodeValue (the getValue payload path) — feeding a stored value the type
// cannot serialise. Also reaches asBool / asInt32 / asFloat32 / asString /
// ipv4ToUint32 / frameSlotStatuses error arms.
func TestEncodeValue_ErrorBranches(t *testing.T) {
	cases := []struct {
		name string
		e    *entry
	}{
		{"integer", &entry{acpType: codec.TypeInteger, param: &canonical.Parameter{Value: "x"}}},
		{"ipaddr", &entry{acpType: codec.TypeIPAddr, param: &canonical.Parameter{Value: "bad"}}},
		{"float", &entry{acpType: codec.TypeFloat, param: &canonical.Parameter{Value: "x"}}},
		{"enum", &entry{acpType: codec.TypeEnum, param: &canonical.Parameter{Value: "x"}}},
		{"string", &entry{acpType: codec.TypeString, param: &canonical.Parameter{Value: 123}}},
		{"alarm", &entry{acpType: codec.TypeAlarm, param: &canonical.Parameter{Value: "x"}}},
		{"long", &entry{acpType: codec.TypeLong, param: &canonical.Parameter{Value: "x"}}},
		{"byte", &entry{acpType: codec.TypeByte, param: &canonical.Parameter{Value: "x"}}},
		// File num_fragments comes from Default; nil → coercion error.
		{"file", &entry{acpType: codec.TypeFile, param: &canonical.Parameter{Value: "fw.bin", Default: nil}}},
		// Frame value must be []any.
		{"frame", &entry{acpType: codec.TypeFrame, param: &canonical.Parameter{Value: "not-a-frame"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := encodeValue(c.e); err == nil {
				t.Errorf("encodeValue(%s) with bad stored value: expected error", c.name)
			}
		})
	}
}

// TestEncodeObject_RootAndUnsupported covers encodeObject's Root delegation
// (which always errors) and the unsupported-type default arm.
func TestEncodeObject_RootAndUnsupported(t *testing.T) {
	if _, err := encodeObject(&entry{acpType: codec.TypeRoot, param: &canonical.Parameter{}}); err == nil {
		t.Error("encodeObject(Root) should error (synthesised by session.go)")
	}
	if _, err := encodeObject(&entry{acpType: codec.ObjectType(99), param: &canonical.Parameter{}}); err == nil {
		t.Error("encodeObject(unsupported) should error")
	}
}
