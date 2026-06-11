package acp1

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/devicemodel"
	"dhs/internal/export"
	"dhs/internal/export/canonical"
)

func TestDeriveAccess(t *testing.T) {
	cases := map[string]uint8{
		canonical.AccessRead:      codec.AccessRead,
		canonical.AccessWrite:     codec.AccessWrite | codec.AccessSetDef,
		canonical.AccessReadWrite: codec.AccessRead | codec.AccessWrite | codec.AccessSetDef,
		"bogus":                   0,
	}
	for in, want := range cases {
		if got := deriveAccess(in); got != want {
			t.Errorf("deriveAccess(%q) = %08b, want %08b", in, got, want)
		}
	}
}

func TestAccessByteToString(t *testing.T) {
	cases := map[uint8]string{
		0x01: canonical.AccessRead,
		0x02: canonical.AccessWrite,
		0x03: canonical.AccessReadWrite,
		0x00: canonical.AccessNone,
	}
	for in, want := range cases {
		if got := accessByteToString(in); got != want {
			t.Errorf("accessByteToString(%#x) = %q, want %q", in, got, want)
		}
	}
}

func TestPickSnapshot(t *testing.T) {
	if pickSnapshot(nil, 1) != nil {
		t.Error("nil schema should return nil")
	}
	multi := &devicemodel.Schema{Slots: map[int]*export.Snapshot{
		1: {}, 2: {},
	}}
	if pickSnapshot(multi, 2) == nil {
		t.Error("exact slot match should return snapshot")
	}
	if pickSnapshot(multi, 9) != nil {
		t.Error("multi-slot miss should return nil")
	}
	single := &devicemodel.Schema{Slots: map[int]*export.Snapshot{7: {}}}
	if pickSnapshot(single, 1) == nil {
		t.Error("single-entry schema should fall back regardless of slot")
	}
}

func TestFuzzableType(t *testing.T) {
	yes := []codec.ObjectType{codec.TypeInteger, codec.TypeLong, codec.TypeByte, codec.TypeFloat, codec.TypeIPAddr, codec.TypeEnum}
	no := []codec.ObjectType{codec.TypeString, codec.TypeAlarm, codec.TypeFile, codec.TypeFrame, codec.TypeRoot}
	for _, ty := range yes {
		if !fuzzableType(ty) {
			t.Errorf("type %d should be fuzzable", ty)
		}
	}
	for _, ty := range no {
		if fuzzableType(ty) {
			t.Errorf("type %d should not be fuzzable", ty)
		}
	}
}

func TestReadIntBounds_Variants(t *testing.T) {
	// int64 / int / float64 bounds.
	min, max := readIntBounds(&entry{param: &canonical.Parameter{Minimum: int64(-5), Maximum: int64(5)}})
	if min != -5 || max != 5 {
		t.Errorf("int64 bounds = %d,%d", min, max)
	}
	min, max = readIntBounds(&entry{param: &canonical.Parameter{Minimum: int(1), Maximum: int(9)}})
	if min != 1 || max != 9 {
		t.Errorf("int bounds = %d,%d", min, max)
	}
	min, max = readIntBounds(&entry{param: &canonical.Parameter{Minimum: float64(2), Maximum: float64(8)}})
	if min != 2 || max != 8 {
		t.Errorf("float bounds = %d,%d", min, max)
	}
	// No range → defensive [0,1].
	min, max = readIntBounds(&entry{param: &canonical.Parameter{}})
	if min != 0 || max != 1 {
		t.Errorf("defensive bounds = %d,%d want 0,1", min, max)
	}
}

func TestRandomBytesFor_Types(t *testing.T) {
	s := discardServer()
	r := rand.New(rand.NewSource(4))
	mk := func(ty codec.ObjectType, p *canonical.Parameter) *entry { return &entry{acpType: ty, param: p} }

	if b, ok := s.randomBytesFor(mk(codec.TypeLong, &canonical.Parameter{Minimum: int64(0), Maximum: int64(1000), Value: int64(10)}), r, 10, false); !ok || len(b) != 4 {
		t.Errorf("long walk = %v,%v", b, ok)
	}
	if b, ok := s.randomBytesFor(mk(codec.TypeByte, &canonical.Parameter{Minimum: int64(0), Maximum: int64(40), Value: int64(5)}), r, 5, true); !ok || len(b) != 1 {
		t.Errorf("byte uniform = %v,%v", b, ok)
	}
	if b, ok := s.randomBytesFor(mk(codec.TypeIPAddr, &canonical.Parameter{}), r, 0, false); !ok || len(b) != 4 {
		t.Errorf("ipaddr = %v,%v", b, ok)
	}
	// Enum with a map.
	if b, ok := s.randomBytesFor(mk(codec.TypeEnum, &canonical.Parameter{EnumMap: []canonical.EnumEntry{{Key: "a", Value: 0}, {Key: "b", Value: 1}}}), r, 0, false); !ok || len(b) != 1 {
		t.Errorf("enum = %v,%v", b, ok)
	}
	// Enum with no map → not oscillatable.
	if _, ok := s.randomBytesFor(mk(codec.TypeEnum, &canonical.Parameter{}), r, 0, false); ok {
		t.Error("enum with no map should return ok=false")
	}
	// Unsupported type.
	if _, ok := s.randomBytesFor(mk(codec.TypeString, &canonical.Parameter{}), r, 0, false); ok {
		t.Error("string should return ok=false")
	}
}

func TestBroadcastsEnabled(t *testing.T) {
	mkTree := func(p *canonical.Parameter) *tree {
		tr := &tree{entries: map[objectKey]*entry{}, slots: map[uint8]*slotCounts{}}
		if p != nil {
			tr.entries[objectKey{slot: 0, group: codec.GroupControl, id: 4}] = &entry{acpType: codec.TypeEnum, param: p}
		}
		return tr
	}
	// Absent gate → permissive.
	if !mkTree(nil).broadcastsEnabled() {
		t.Error("absent Broadcasts gate should default to enabled")
	}
	// Enum map, value points to "On".
	on := mkTree(&canonical.Parameter{Value: int64(1),
		EnumMap: []canonical.EnumEntry{{Key: "Off", Value: 0}, {Key: "On", Value: 1}}})
	if !on.broadcastsEnabled() {
		t.Error("enum=On should enable broadcasts")
	}
	off := mkTree(&canonical.Parameter{Value: int64(0),
		EnumMap: []canonical.EnumEntry{{Key: "Off", Value: 0}, {Key: "On", Value: 1}}})
	if off.broadcastsEnabled() {
		t.Error("enum=Off should disable broadcasts")
	}
	// Bare numeric, no map: 0=off, non-zero=on.
	if !mkTree(&canonical.Parameter{Value: int64(1)}).broadcastsEnabled() {
		t.Error("bare numeric 1 should enable")
	}
	if mkTree(&canonical.Parameter{Value: int64(0)}).broadcastsEnabled() {
		t.Error("bare numeric 0 should disable")
	}
}

// TestCascadeInsert_FullAndPreempt drives the full no_card→present cascade and
// the pre-emption path (a second insert cancels the first; an extract cancels
// an in-flight insert).
func TestCascadeInsert_FullAndPreempt(t *testing.T) {
	s := newTestServer(t)
	s.SetInsertTiming(InsertTimingFast)
	_ = s.setSlotStatus(1, 0) // reset to no_card

	s.CascadeInsert(context.Background(), 1)
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if readSlotStatus(t, s, 1) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if readSlotStatus(t, s, 1) != 2 {
		t.Fatalf("cascade did not reach present: %d", readSlotStatus(t, s, 1))
	}

	// Pre-empt: start an insert then immediately a second one — trackCascade
	// must cancel the first goroutine and run the second. Wait for the
	// surviving cascade to settle at present before extracting so the assert
	// is deterministic (a cancelled goroutine still sets powerup once before
	// its select observes the cancel).
	_ = s.setSlotStatus(1, 0)
	s.CascadeInsert(context.Background(), 1)
	s.CascadeInsert(context.Background(), 1)
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if readSlotStatus(t, s, 1) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if readSlotStatus(t, s, 1) != 2 {
		t.Fatalf("pre-empted cascade did not reach present: %d", readSlotStatus(t, s, 1))
	}
	// Now extract cleanly (no in-flight goroutine racing).
	s.CascadeExtract(1)
	if got := readSlotStatus(t, s, 1); got != 0 {
		t.Fatalf("after extract: state = %d, want no_card", got)
	}
}

// TestSession_Root_EdgeCases covers handleRoot's unknown-slot and illegal-method
// branches.
func TestSession_Root_EdgeCases(t *testing.T) {
	s := newTestServer(t)
	// Root on a slot with no counts → instance error.
	rep, _ := s.handleRequest(&codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 9,
		MCode: byte(codec.MethodGetValue), ObjGroup: codec.GroupRoot, ObjID: 0,
	})
	if rep == nil || rep.MType != codec.MTypeError {
		t.Fatalf("unknown-slot Root should error, got %+v", rep)
	}
	// Illegal method on Root (setValue) → illegal-method error.
	rep, _ = s.handleRequest(&codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodSetValue), ObjGroup: codec.GroupRoot, ObjID: 0,
	})
	if rep == nil || rep.MType != codec.MTypeError {
		t.Fatalf("illegal Root method should error, got %+v", rep)
	}
}

func TestAdmin_ArgTypeErrors(t *testing.T) {
	s := newTestServer(t)
	c := startAdminServer(t, s, "test-argtypes")
	// slot.state with slot as a string → readIntArg error.
	resp, err := c.Call(context.Background(), &AdminRequest{
		Verb: "slot.state", Args: map[string]any{"slot": "notnum", "state": "error"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "error" {
		t.Fatalf("string slot should error, got %q", resp.Status)
	}
	// slot.state with state as a number → readStringArg error.
	resp, err = c.Call(context.Background(), &AdminRequest{
		Verb: "slot.state", Args: map[string]any{"slot": 1, "state": 5},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Status != "error" {
		t.Fatalf("numeric state should error, got %q", resp.Status)
	}
}
