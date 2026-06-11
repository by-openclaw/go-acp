package acp1

import (
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// Exercises the provider mutation pipeline (applyMutation + mutateInteger /
// Long / Byte / Float / Enum / String / IPAddr + clamp/snap helpers) which had
// no test coverage. Asserts the device-side clamp-to-[min,max] behaviour
// (spec p.28) and the inc/dec/def methods via the stored canonical value.

func newMutServer() *server {
	return &server{tree: &tree{
		entries: map[objectKey]*entry{},
		slots:   map[uint8]*slotCounts{},
	}}
}

func strp(s string) *string { return &s }

func mutate(t *testing.T, e *entry, m codec.Method, in []byte) {
	t.Helper()
	if _, err := newMutServer().applyMutation(e, m, in); err != nil {
		t.Fatalf("applyMutation: %v", err)
	}
}

func TestMutateInteger(t *testing.T) {
	mk := func() *entry {
		return &entry{acpType: codec.TypeInteger, param: &canonical.Parameter{
			Value: int64(0), Minimum: int64(-60), Maximum: int64(12),
			Step: int64(1), Default: int64(3),
		}}
	}
	// setValue above max -> clamp to 12.
	e := mk()
	mutate(t, e, codec.MethodSetValue, []byte{0x03, 0xE7}) // 999
	if e.param.Value != int64(12) {
		t.Errorf("clamp-high = %v, want 12", e.param.Value)
	}
	// setValue below min -> clamp to -60.
	e = mk()
	mutate(t, e, codec.MethodSetValue, []byte{0xFC, 0x19}) // -999
	if e.param.Value != int64(-60) {
		t.Errorf("clamp-low = %v, want -60", e.param.Value)
	}
	// inc / dec / def.
	e = mk()
	mutate(t, e, codec.MethodSetIncValue, nil)
	if e.param.Value != int64(1) {
		t.Errorf("inc = %v, want 1", e.param.Value)
	}
	e = mk()
	mutate(t, e, codec.MethodSetDecValue, nil)
	if e.param.Value != int64(-1) {
		t.Errorf("dec = %v, want -1", e.param.Value)
	}
	e = mk()
	mutate(t, e, codec.MethodSetDefValue, nil)
	if e.param.Value != int64(3) {
		t.Errorf("def = %v, want 3", e.param.Value)
	}
}

func TestMutateByte(t *testing.T) {
	e := &entry{acpType: codec.TypeByte, param: &canonical.Parameter{
		Value: int64(0), Minimum: int64(0), Maximum: int64(32), Step: int64(1), Default: int64(0),
	}}
	mutate(t, e, codec.MethodSetValue, []byte{100}) // > max 32
	if e.param.Value != int64(32) {
		t.Errorf("byte clamp = %v, want 32", e.param.Value)
	}
}

func TestMutateLong(t *testing.T) {
	e := &entry{acpType: codec.TypeLong, param: &canonical.Parameter{
		Value: int64(0), Minimum: int64(-1000), Maximum: int64(1000), Step: int64(1), Default: int64(0),
	}}
	mutate(t, e, codec.MethodSetValue, []byte{0x00, 0x00, 0x13, 0x88}) // 5000 -> clamp 1000
	if e.param.Value != int64(1000) {
		t.Errorf("long clamp = %v, want 1000", e.param.Value)
	}
}

func TestMutateFloat(t *testing.T) {
	e := &entry{acpType: codec.TypeFloat, param: &canonical.Parameter{
		Value: float64(0), Minimum: float64(0), Maximum: float64(10), Step: float64(0), Default: float64(0),
	}}
	mutate(t, e, codec.MethodSetValue, []byte{0x41, 0x48, 0x00, 0x00}) // 12.5 -> clamp 10
	if e.param.Value != float64(10) {
		t.Errorf("float clamp = %v, want 10", e.param.Value)
	}
}

func TestMutateEnum(t *testing.T) {
	mk := func() *entry {
		return &entry{acpType: codec.TypeEnum, param: &canonical.Parameter{
			Value: int64(0), Enumeration: strp("Off,On"),
		}}
	}
	e := mk()
	mutate(t, e, codec.MethodSetValue, []byte{1})
	if e.param.Value != int64(1) {
		t.Errorf("enum set = %v, want 1", e.param.Value)
	}
	// Out-of-range index is rejected (device errors, not clamps).
	if _, err := newMutServer().applyMutation(mk(), codec.MethodSetValue, []byte{5}); err == nil {
		t.Error("enum index 5 (> max): want error")
	}
}

func TestMutateString(t *testing.T) {
	e := &entry{acpType: codec.TypeString, param: &canonical.Parameter{
		Value: "", Format: strp("maxLen=8"),
	}}
	mutate(t, e, codec.MethodSetValue, []byte("verylongstring\x00"))
	if e.param.Value != "verylong" {
		t.Errorf("string truncate = %q, want %q", e.param.Value, "verylong")
	}
}

func TestMutateIPAddr(t *testing.T) {
	e := &entry{acpType: codec.TypeIPAddr, param: &canonical.Parameter{
		Value: "0.0.0.0", Minimum: "0.0.0.0", Maximum: "255.255.255.255", Default: "0.0.0.0",
	}}
	mutate(t, e, codec.MethodSetValue, []byte{0xC0, 0xA8, 0x01, 0x05}) // 192.168.1.5
	if e.param.Value != "192.168.1.5" {
		t.Errorf("ipaddr set = %v, want 192.168.1.5", e.param.Value)
	}
}
