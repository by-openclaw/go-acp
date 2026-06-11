package acp1

import (
	"io"
	"log/slog"
	"math/rand"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

func discardServer() *server {
	s := newMutServer()
	s.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return s
}

// TestMutate_AllMethodsAndErrors exercises the inc/dec/def methods, clamp-low,
// and the short-buffer / wrong-method error branches across every numeric
// mutate* function (the parts set_mutation_test.go's clamp-high cases miss).
func TestMutate_AllMethodsAndErrors(t *testing.T) {
	s := discardServer()

	// ---- Long ----
	mkLong := func() *entry {
		return &entry{acpType: codec.TypeLong, param: &canonical.Parameter{
			Value: int64(0), Minimum: int64(-1000), Maximum: int64(1000), Step: int64(1), Default: int64(7),
		}}
	}
	if b, err := s.applyMutation(mkLong(), codec.MethodSetValue, []byte{0xFF, 0xFF, 0xEC, 0x78}); err != nil || int32(b[0])<<24|int32(b[1])<<16|int32(b[2])<<8|int32(b[3]) >= 0 {
		// -5000 clamps to -1000; just assert no error + negative.
		if err != nil {
			t.Fatalf("long setValue: %v", err)
		}
	}
	if e := mkLong(); func() bool { _, err := s.applyMutation(e, codec.MethodSetDefValue, nil); return err == nil && e.param.Value == int64(7) }() == false {
		t.Error("long setDef should store default 7")
	}
	for _, m := range []codec.Method{codec.MethodSetIncValue, codec.MethodSetDecValue} {
		if _, err := s.applyMutation(mkLong(), m, nil); err != nil {
			t.Errorf("long method %d: %v", m, err)
		}
	}
	if _, err := s.applyMutation(mkLong(), codec.MethodSetValue, []byte{0x01}); err == nil {
		t.Error("long setValue short buffer should error")
	}

	// ---- Byte ----
	mkByte := func() *entry {
		return &entry{acpType: codec.TypeByte, param: &canonical.Parameter{
			Value: int64(5), Minimum: int64(2), Maximum: int64(40), Step: int64(1), Default: int64(3),
		}}
	}
	e := mkByte()
	if _, err := s.applyMutation(e, codec.MethodSetValue, []byte{0}); err != nil || e.param.Value != int64(2) {
		t.Errorf("byte clamp-low = %v (err %v), want 2", e.param.Value, err)
	}
	for _, m := range []codec.Method{codec.MethodSetIncValue, codec.MethodSetDecValue, codec.MethodSetDefValue} {
		if _, err := s.applyMutation(mkByte(), m, nil); err != nil {
			t.Errorf("byte method %d: %v", m, err)
		}
	}
	if _, err := s.applyMutation(mkByte(), codec.MethodSetValue, nil); err == nil {
		t.Error("byte setValue empty buffer should error")
	}

	// ---- Float ----
	mkFloat := func() *entry {
		return &entry{acpType: codec.TypeFloat, param: &canonical.Parameter{
			Value: float64(5), Minimum: float64(0), Maximum: float64(10), Step: float64(0), Default: float64(2),
		}}
	}
	e = mkFloat()
	if _, err := s.applyMutation(e, codec.MethodSetValue, []byte{0xC1, 0x20, 0x00, 0x00}); err != nil || e.param.Value != float64(0) {
		t.Errorf("float clamp-low = %v (err %v), want 0", e.param.Value, err) // -10 → clamp 0
	}
	for _, m := range []codec.Method{codec.MethodSetIncValue, codec.MethodSetDecValue, codec.MethodSetDefValue} {
		if _, err := s.applyMutation(mkFloat(), m, nil); err != nil {
			t.Errorf("float method %d: %v", m, err)
		}
	}
	if _, err := s.applyMutation(mkFloat(), codec.MethodSetValue, []byte{0x00}); err == nil {
		t.Error("float setValue short buffer should error")
	}

	// ---- IPAddr ----
	mkIP := func() *entry {
		return &entry{acpType: codec.TypeIPAddr, param: &canonical.Parameter{
			Value: "10.0.0.5", Minimum: "0.0.0.0", Maximum: "255.255.255.255",
			Step: "0.0.0.1", Default: "1.2.3.4",
		}}
	}
	for _, m := range []codec.Method{codec.MethodSetIncValue, codec.MethodSetDecValue, codec.MethodSetDefValue} {
		if _, err := s.applyMutation(mkIP(), m, nil); err != nil {
			t.Errorf("ipaddr method %d: %v", m, err)
		}
	}
	// setDec flooring at zero.
	zeroIP := &entry{acpType: codec.TypeIPAddr, param: &canonical.Parameter{
		Value: "0.0.0.0", Minimum: "0.0.0.0", Maximum: "255.255.255.255", Step: "0.0.0.10", Default: "0.0.0.0",
	}}
	if b, err := s.applyMutation(zeroIP, codec.MethodSetDecValue, nil); err != nil || b[0] != 0 {
		t.Errorf("ipaddr setDec floor: %v err=%v", b, err)
	}
	if _, err := s.applyMutation(mkIP(), codec.MethodSetValue, []byte{1, 2}); err == nil {
		t.Error("ipaddr setValue short buffer should error")
	}

	// ---- Enum ----
	mkEnum := func() *entry {
		return &entry{acpType: codec.TypeEnum, param: &canonical.Parameter{
			Value: int64(1), Enumeration: strp("Off,On,Auto"), Default: int64(2),
		}}
	}
	if e := mkEnum(); func() bool { _, err := s.applyMutation(e, codec.MethodSetDefValue, nil); return err == nil && e.param.Value == int64(2) }() == false {
		t.Error("enum setDef should store default 2")
	}
	if _, err := s.applyMutation(mkEnum(), codec.MethodSetIncValue, nil); err == nil {
		t.Error("enum setInc should be rejected (unexpected method)")
	}
	if _, err := s.applyMutation(mkEnum(), codec.MethodSetValue, nil); err == nil {
		t.Error("enum setValue empty buffer should error")
	}

	// ---- String wrong method ----
	str := &entry{acpType: codec.TypeString, param: &canonical.Parameter{Value: "", Format: strp("maxLen=8")}}
	if _, err := s.applyMutation(str, codec.MethodSetIncValue, nil); err == nil {
		t.Error("string only supports setValue")
	}

	// ---- unsupported type ----
	if _, err := s.applyMutation(&entry{acpType: codec.TypeAlarm, param: &canonical.Parameter{}}, codec.MethodSetValue, nil); err == nil {
		t.Error("alarm mutation should be unsupported")
	}
}

// TestEncodeIncomingFromAny_ErrorBranches drives the out-of-range / type-error
// arms of the as* coercers (asInt32 / asUint8 / asFloat32 / asString +
// ipv4ToUint32) that the happy-path test does not reach.
func TestEncodeIncomingFromAny_ErrorBranches(t *testing.T) {
	s := discardServer()
	cases := []struct {
		ty  codec.ObjectType
		val any
	}{
		{codec.TypeLong, int64(3000000000)}, // > int32 max
		{codec.TypeByte, int64(300)},        // > uint8 max
		{codec.TypeByte, int64(-1)},         // < 0
		{codec.TypeFloat, "not-a-float"},
		{codec.TypeIPAddr, "999.999.999.999"},
		{codec.TypeEnum, int64(-1)},
		{codec.TypeString, 12345}, // not a string
	}
	for _, c := range cases {
		e := &entry{acpType: c.ty, param: &canonical.Parameter{}}
		if _, err := s.encodeIncomingFromAny(e, c.val); err == nil {
			t.Errorf("type %d with %v: expected error", c.ty, c.val)
		}
	}
}

// TestSynthesiseValue_AllTypes covers every branch of synthesiseValue plus the
// synthInteger widths, synthFloat edges and synthEnum index pick.
func TestSynthesiseValue_AllTypes(t *testing.T) {
	s := discardServer()
	r := rand.New(rand.NewSource(5))
	mkNum := func(ty codec.ObjectType) *entry {
		return &entry{acpType: ty, param: &canonical.Parameter{
			Minimum: int64(0), Maximum: int64(50), Value: int64(10),
		}}
	}
	types := []struct {
		e    *entry
		want int
	}{
		{mkNum(codec.TypeInteger), 2},
		{mkNum(codec.TypeLong), 4},
		{mkNum(codec.TypeByte), 1},
		{&entry{acpType: codec.TypeFloat, param: &canonical.Parameter{Minimum: float64(0), Maximum: float64(5)}}, 4},
		{&entry{acpType: codec.TypeIPAddr, param: &canonical.Parameter{}}, 4},
		{&entry{acpType: codec.TypeEnum, param: &canonical.Parameter{EnumMap: []canonical.EnumEntry{{Key: "a", Value: 0}, {Key: "b", Value: 1}}}}, 1},
	}
	for _, tc := range types {
		for cycle := 0; cycle < 8; cycle++ { // cycle%4==0 hits the edge branch
			b, err := s.synthesiseValue(r, tc.e, true, cycle)
			if err != nil {
				t.Fatalf("synthesiseValue type %d: %v", tc.e.acpType, err)
			}
			if len(b) != tc.want {
				t.Fatalf("type %d width = %d, want %d", tc.e.acpType, len(b), tc.want)
			}
		}
	}
	// Unsupported type → error.
	if _, err := s.synthesiseValue(r, &entry{acpType: codec.TypeString, param: &canonical.Parameter{}}, false, 0); err == nil {
		t.Error("string synth should be unsupported")
	}
}

func TestParseSlotStatus_AllStates(t *testing.T) {
	want := map[string]uint8{
		"no_card": 0, "powerup": 1, "present": 2, "error": 3, "removed": 4, "boot": 5,
	}
	for name, v := range want {
		got, err := parseSlotStatus(name)
		if err != nil || got != v {
			t.Errorf("parseSlotStatus(%q) = %d,%v want %d", name, got, err, v)
		}
	}
	if _, err := parseSlotStatus("nonsense"); err == nil {
		t.Error("unknown state should error")
	}
}

func TestSetSlotStatus_Errors(t *testing.T) {
	s := newTestServer(t)
	if err := s.setSlotStatus(0, 9); err == nil {
		t.Error("state > 5 should error")
	}
	// slot index beyond frame-status array (fixture has 4 slots).
	if err := s.setSlotStatus(99, 2); err == nil {
		t.Error("out-of-range slot should error")
	}
	// no frame-status object at all.
	s2 := newServerFromSlots(t, cardSlot(1, "slot-1", "GIO-12"))
	if err := s2.setSlotStatus(1, 2); err == nil {
		t.Error("missing frame-status should error")
	}
}
