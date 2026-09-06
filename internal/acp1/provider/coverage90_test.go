package acp1

import (
	"context"
	"dhs/internal/plugin"
	"io"
	"log/slog"
	"math/rand"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/consumer"
	"dhs/internal/devicemodel"
	"dhs/internal/export"
	"dhs/internal/export/canonical"
)

// ---------------------------------------------------------------- play loops

// TestRunStatusPlay_GoroutineLoop drives the real oscillator launcher with a
// 1ms tick across good + bad + empty paths, then cancels. Covers RunStatusPlay,
// playLoop, walkInt and readIntBound under the goroutine path the helper-only
// tests skip.
func TestRunStatusPlay_GoroutineLoop(t *testing.T) {
	s := playTreeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	// "1.1.2.0" = slot0 Byte, "1.2.2.0" = slot1 Integer (walk mode exercises
	// walkInt); "" and "9.9" exercise the skip/bad-path branches.
	s.RunStatusPlay(ctx, []string{"1.1.2.0", "1.2.2.0", "", "9.9", "1.9.2.0"}, time.Millisecond, false)
	time.Sleep(40 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)
}

func TestRunStatusPlayAll_GoroutineLoop(t *testing.T) {
	s := playTreeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	s.RunStatusPlayAll(ctx, time.Millisecond, true) // fullRange → uniformInt
	time.Sleep(40 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)
}

func TestRunFrameStatusPlay_GoroutineLoop(t *testing.T) {
	s := playTreeServer(t) // has slot0 frame-status
	ctx, cancel := context.WithCancel(context.Background())
	s.RunFrameStatusPlay(ctx, time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)

	// No frame-status → early return (debug log, no goroutine).
	s2 := newServerFromSlots(t, cardSlot(1, "slot-1", "GIO-12"))
	s2.RunFrameStatusPlay(context.Background(), time.Millisecond)
}

func TestWalkInt_Branches(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	// Hit toward-nominal (both directions), == nominal, jitter, and clamp.
	cases := []struct{ cur, nominal, min, max int64 }{
		{cur: 0, nominal: 10, min: -5, max: 20},  // cur < nominal
		{cur: 20, nominal: 0, min: -5, max: 20},  // cur > nominal, clamp-high pressure
		{cur: 5, nominal: 5, min: 5, max: 5},     // == nominal, degenerate range
		{cur: -5, nominal: -5, min: -5, max: -5}, // clamp-low pressure
	}
	for _, c := range cases {
		for i := 0; i < 200; i++ {
			v := walkInt(r, c.cur, c.nominal, c.min, c.max)
			if v < c.min || v > c.max {
				t.Fatalf("walkInt out of [%d,%d]: %d", c.min, c.max, v)
			}
		}
	}
}

// ---------------------------------------------------------------- demo loop

func TestRunAnnounceDemo_Loop(t *testing.T) {
	s := newTestServer(t) // slot1 control[0] Level is Integer min=-60 max=12
	// RunAnnounceDemo blocks until ctx is cancelled (unlike RunStatusPlay,
	// which spawns its own goroutines), so drive it from a goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.RunAnnounceDemo(ctx, 1, 2, 0, time.Millisecond); close(done) }()
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunAnnounceDemo did not return after cancel")
	}

	// Target not found → immediate return (no loop entered).
	s.RunAnnounceDemo(context.Background(), 9, 2, 0, time.Millisecond)
	// Target not Integer (identity[0] is a string) → immediate return.
	s.RunAnnounceDemo(context.Background(), 1, 1, 0, time.Millisecond)
}

func TestDemoAsInt16(t *testing.T) {
	cases := []struct {
		in   any
		want int16
		ok   bool
	}{
		{int(12), 12, true},
		{int64(-60), -60, true},
		{float64(7), 7, true},
		{float32(3), 3, true},
		{int(99999), 0, false}, // out of int16 range
		{"nope", 0, false},     // unsupported type
	}
	for _, c := range cases {
		got, ok := demoAsInt16(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("demoAsInt16(%v) = %d,%v want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// ---------------------------------------------------------- fuzz synthesisers

func TestSynthFloatAndEnum(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	fe := &entry{acpType: codec.TypeFloat, param: &canonical.Parameter{
		Minimum: float64(0), Maximum: float64(10),
	}}
	for i := 0; i < 50; i++ {
		if b := synthFloat(r, fe, i%2 == 0); len(b) != 4 {
			t.Fatalf("synthFloat width = %d, want 4", len(b))
		}
	}
	ee := &entry{acpType: codec.TypeEnum, param: &canonical.Parameter{
		EnumMap: []canonical.EnumEntry{{Key: "Off", Value: 0}, {Key: "On", Value: 1}},
	}}
	for i := 0; i < 50; i++ {
		b := synthEnum(r, ee)
		if len(b) != 1 || b[0] > 1 {
			t.Fatalf("synthEnum = %v, want single index in 0..1", b)
		}
	}
	// No enum metadata → random byte fallback.
	if b := synthEnum(r, &entry{param: &canonical.Parameter{}}); len(b) != 1 {
		t.Fatalf("synthEnum fallback width = %d", len(b))
	}
}

func TestReadFloatBounds(t *testing.T) {
	min, max := readFloatBounds(&entry{param: &canonical.Parameter{
		Minimum: float64(-2.5), Maximum: float64(7.5),
	}})
	if min != -2.5 || max != 7.5 {
		t.Errorf("readFloatBounds = %v,%v want -2.5,7.5", min, max)
	}
	// int-typed bounds coerced to float; nil param → defensive [0,1].
	min, max = readFloatBounds(&entry{param: &canonical.Parameter{Minimum: int64(1), Maximum: int64(1)}})
	if min != 0 || max != 1 {
		t.Errorf("degenerate bounds = %v,%v want 0,1", min, max)
	}
	if min, max = readFloatBounds(&entry{}); min != 0 || max != 1 {
		t.Errorf("nil-param bounds = %v,%v want 0,1", min, max)
	}
}

// --------------------------------------------------------- encoder helpers

func TestAnyToInt(t *testing.T) {
	ok := []struct {
		in   any
		want int64
	}{
		{int(5), 5}, {int64(6), 6}, {uint8(7), 7}, {uint16(8), 8}, {uint32(9), 9},
		{int16(10), 10}, {int32(11), 11}, {float32(12), 12}, {float64(13), 13},
		{true, 1}, {false, 0}, {"42", 42},
	}
	for _, c := range ok {
		got, err := anyToInt(c.in, "f")
		if err != nil || got != c.want {
			t.Errorf("anyToInt(%v) = %d,%v want %d", c.in, got, err, c.want)
		}
	}
	bad := []any{nil, float64(1.5), float32(1.5), "x", []byte{1}}
	for _, c := range bad {
		if _, err := anyToInt(c, "f"); err == nil {
			t.Errorf("anyToInt(%v): want error", c)
		}
	}
}

func TestAnyToFloat(t *testing.T) {
	ok := []struct {
		in   any
		want float64
	}{
		{float32(1.5), 1.5}, {float64(2.5), 2.5}, {int(3), 3}, {int64(4), 4},
		{"5.5", 5.5}, {uint8(6), 6}, // falls through to anyToInt
	}
	for _, c := range ok {
		got, err := anyToFloat(c.in, "f")
		if err != nil || got != c.want {
			t.Errorf("anyToFloat(%v) = %v,%v want %v", c.in, got, err, c.want)
		}
	}
	for _, c := range []any{nil, "x", []byte{1}} {
		if _, err := anyToFloat(c, "f"); err == nil {
			t.Errorf("anyToFloat(%v): want error", c)
		}
	}
}

func TestEncodeRoot_AlwaysErrors(t *testing.T) {
	if _, err := encodeRoot(&entry{}); err == nil {
		t.Fatal("encodeRoot must return an error (Root is synthesised by session.go)")
	}
}

func TestEncodeIncomingFromAny_AllTypes(t *testing.T) {
	s := &server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cases := []struct {
		ty   codec.ObjectType
		val  any
		want int // expected wire length
	}{
		{codec.TypeInteger, int64(5), 2},
		{codec.TypeLong, int64(70000), 4},
		{codec.TypeByte, int64(200), 1},
		{codec.TypeFloat, float64(1.5), 4},
		{codec.TypeIPAddr, "192.168.1.5", 4},
		{codec.TypeEnum, int64(1), 1},
		{codec.TypeString, "hi", 3}, // "hi" + NUL
	}
	for _, c := range cases {
		e := &entry{acpType: c.ty, param: &canonical.Parameter{}}
		b, err := s.encodeIncomingFromAny(e, c.val)
		if err != nil {
			t.Fatalf("encodeIncomingFromAny(%d, %v): %v", c.ty, c.val, err)
		}
		if len(b) != c.want {
			t.Errorf("type %d wire len = %d, want %d", c.ty, len(b), c.want)
		}
	}
	// Unsupported type → error.
	if _, err := s.encodeIncomingFromAny(&entry{acpType: codec.TypeFrame, param: &canonical.Parameter{}}, 1); err == nil {
		t.Error("Frame setValue should be unsupported")
	}
	// Type-mismatch (string into Integer) → error.
	if _, err := s.encodeIncomingFromAny(&entry{acpType: codec.TypeInteger, param: &canonical.Parameter{}}, "nope"); err == nil {
		t.Error("non-numeric into Integer should error")
	}
}

func TestMethodName(t *testing.T) {
	cases := map[codec.Method]string{
		codec.MethodGetValue:    "getValue",
		codec.MethodSetValue:    "setValue",
		codec.MethodSetIncValue: "setIncValue",
		codec.MethodSetDecValue: "setDecValue",
		codec.MethodSetDefValue: "setDefValue",
		codec.MethodGetObject:   "getObject",
		codec.Method(0xFE):      "unknown",
	}
	for m, want := range cases {
		if got := methodName(m); got != want {
			t.Errorf("methodName(%d) = %q want %q", m, got, want)
		}
	}
}

// --------------------------------------------------------- snapshot → params

// TestSnapshotToEntries_MultiType drives every Kind/type branch of the DM
// snapshot conversion (acpTypeFromObject, objectToParameter, canonicalTypeFor,
// formatHintFor, valueAny, anyAsInt64, intExceedsI16, accessByteToString).
func TestSnapshotToEntries_MultiType(t *testing.T) {
	objs := []consumer.Object{
		{Group: "control", ID: 0, Label: "Int", Kind: consumer.KindInt,
			Access: 0x03, Min: int64(-10), Max: int64(10), Def: int64(0), Step: int64(1),
			Value: consumer.Value{Kind: consumer.KindInt, Int: 3}},
		{Group: "control", ID: 1, Label: "Long", Kind: consumer.KindInt,
			Access: 0x03, Min: int64(0), Max: int64(100000), // exceeds int16 → Long
			Value: consumer.Value{Kind: consumer.KindInt, Int: 5}},
		{Group: "control", ID: 2, Label: "Byte", Kind: consumer.KindUint,
			Access: 0x03, Unit: "dB", Value: consumer.Value{Kind: consumer.KindUint, Uint: 7}},
		{Group: "control", ID: 3, Label: "Float", Kind: consumer.KindFloat,
			Access: 0x02, Min: float64(0), Max: float64(1),
			Value: consumer.Value{Kind: consumer.KindFloat, Float: 0.5}},
		{Group: "control", ID: 4, Label: "Enum", Kind: consumer.KindEnum,
			Access: 0x03, EnumItems: []string{"Off", "On"},
			Value: consumer.Value{Kind: consumer.KindEnum, Enum: 1}},
		{Group: "status", ID: 0, Label: "IP", Kind: consumer.KindIPAddr,
			Access: 0x01, Value: consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 0, 0, 1}}},
		{Group: "identity", ID: 0, Label: "Name", Kind: consumer.KindString,
			Access: 0x01, MaxLen: 16, Value: consumer.Value{Kind: consumer.KindString, Str: "card"}},
		{Group: "alarm", ID: 0, Label: "Alarm", Kind: consumer.KindAlarm,
			Access: 0x01, AlarmOnMsg: "FAULT", AlarmOffMsg: "OK",
			Value: consumer.Value{Kind: consumer.KindAlarm, Bool: true}},
		{Group: "control", ID: 9, Label: "Hinted", Kind: consumer.KindInt,
			Access: 0x03, Meta: map[string]any{"acp1_type": float64(codec.TypeFloat)}},
		// Skipped: frame group + unknown group.
		{Group: "frame", ID: 0, Label: "frame-status", Kind: consumer.KindFrame, Access: 0x01},
		{Group: "bogus", ID: 0, Label: "x", Kind: consumer.KindInt, Access: 0x03},
	}
	snap := &export.Snapshot{Slots: []export.SlotDump{{Slot: 1, Objects: objs}}}

	entries, counts, err := snapshotToEntries(1, snap)
	if err != nil {
		t.Fatalf("snapshotToEntries: %v", err)
	}
	byID := map[objectKey]*entry{}
	for _, e := range entries {
		byID[e.key] = e
	}
	want := map[objectKey]codec.ObjectType{
		{slot: 1, group: codec.GroupControl, id: 0}:  codec.TypeInteger,
		{slot: 1, group: codec.GroupControl, id: 1}:  codec.TypeLong,
		{slot: 1, group: codec.GroupControl, id: 2}:  codec.TypeByte,
		{slot: 1, group: codec.GroupControl, id: 3}:  codec.TypeFloat,
		{slot: 1, group: codec.GroupControl, id: 4}:  codec.TypeEnum,
		{slot: 1, group: codec.GroupStatus, id: 0}:   codec.TypeIPAddr,
		{slot: 1, group: codec.GroupIdentity, id: 0}: codec.TypeString,
		{slot: 1, group: codec.GroupAlarm, id: 0}:    codec.TypeAlarm,
		{slot: 1, group: codec.GroupControl, id: 9}:  codec.TypeFloat, // meta hint wins
	}
	for k, ty := range want {
		e, ok := byID[k]
		if !ok {
			t.Errorf("missing entry %+v", k)
			continue
		}
		if e.acpType != ty {
			t.Errorf("entry %+v type = %d, want %d", k, e.acpType, ty)
		}
	}
	if _, leaked := byID[objectKey{slot: 1, group: codec.GroupFrame, id: 0}]; leaked {
		t.Error("frame group must be dropped from slot.load snapshot")
	}
	if counts.numControl == 0 || counts.numIdentity == 0 {
		t.Errorf("counts not populated: %+v", counts)
	}
}

func TestSnapshotToEntries_Errors(t *testing.T) {
	if _, _, err := snapshotToEntries(1, nil); err == nil {
		t.Error("nil snapshot should error")
	}
	// snapshot present but no matching slot.
	snap := &export.Snapshot{Slots: []export.SlotDump{{Slot: 5}, {Slot: 6}}}
	if _, _, err := snapshotToEntries(1, snap); err == nil {
		t.Error("missing slot should error")
	}
}

func TestAnyAsInt64_AndIntExceedsI16(t *testing.T) {
	if !intExceedsI16(int64(40000)) || !intExceedsI16(int64(-40000)) {
		t.Error("values beyond int16 must be flagged")
	}
	if intExceedsI16(int64(100)) || intExceedsI16(nil) {
		t.Error("in-range and nil must not be flagged")
	}
	if n, ok := anyAsInt64("123"); !ok || n != 123 {
		t.Errorf("anyAsInt64(string) = %d,%v", n, ok)
	}
	if _, ok := anyAsInt64("xx"); ok {
		t.Error("non-numeric string should fail")
	}
	if _, ok := anyAsInt64(nil); ok {
		t.Error("nil should fail")
	}
}

// --------------------------------------------------------- misc 0% functions

func TestInsertTiming_String(t *testing.T) {
	if InsertTimingReal.String() != "real" || InsertTimingFast.String() != "fast" {
		t.Error("InsertTiming String mismatch")
	}
	if got := InsertTiming(99).String(); got != "timing(99)" {
		t.Errorf("unknown timing String = %q", got)
	}
}

func TestFactory_New(t *testing.T) {
	exp := &canonical.Export{Root: &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "device", Access: canonical.AccessRead,
		Children: canonical.EmptyChildren(),
	}}}
	p := (&Factory{}).New(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, exp)
	if p == nil {
		t.Fatal("Factory.New returned nil")
	}
}

func TestPreloadSlot(t *testing.T) {
	s := newTestServer(t)
	// No resolver attached → ErrNoDMLibrary.
	if err := s.PreloadSlot(1, "axon/synapse/RRS18-1601/acp1"); err == nil {
		t.Fatal("PreloadSlot without resolver should error")
	}
	// With a resolver the card installs immediately at present.
	s.SetDMLibrary(&fakeDMResolver{
		schemas: map[string]*devicemodel.Schema{
			"RRS18-1601": schemaWithIdentity(1, "RRS18", "1601"),
		},
	})
	if err := s.PreloadSlot(1, "axon/synapse/RRS18-1601/acp1"); err != nil {
		t.Fatalf("PreloadSlot: %v", err)
	}
	if got := readSlotStatus(t, s, 1); got != 2 {
		t.Fatalf("after preload slot status = %d, want present (2)", got)
	}
	if e := readEntry(t, s, 1, codec.GroupIdentity, 0); e == nil {
		t.Fatal("identity[0] missing after preload")
	}
}
