package acp1

import (
	"bytes"
	"dhs/internal/plugin"
	"io"
	"log/slog"
	"math/rand"
	"strings"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

func hintp(s string) *string { return &s }

// playTreeServer builds a 2-slot frame whose objects cover every type the
// oscillator must include (Byte, Enum, Integer, IPAddr) and every type it must
// skip (String identity, Frame status). Slot 0 (rack controller) carries
// oscillatable objects too, so the test can prove "slot 0 included".
func playTreeServer(t *testing.T) *server {
	t.Helper()
	r := canonical.AccessRead
	rw := canonical.AccessReadWrite
	param := func(num int, ident, typ, fmtHint string) *canonical.Parameter {
		p := &canonical.Parameter{
			Header: canonical.Header{
				Number: num, Identifier: ident, Access: rw,
				Children: canonical.EmptyChildren(),
			},
			Type: typ,
		}
		if fmtHint != "" {
			p.Format = hintp(fmtHint)
		}
		switch typ {
		case canonical.ParamInteger:
			p.Value = int64(0)
		case canonical.ParamEnum:
			p.Value = int64(0)
		case canonical.ParamString:
			p.Value = ""
		}
		return p
	}
	group := func(num int, ident string, kids ...canonical.Element) *canonical.Node {
		return &canonical.Node{Header: canonical.Header{
			Number: num, Identifier: ident, Access: r, Children: kids,
		}}
	}

	// Slot 0: rack controller — a Byte + Enum (oscillatable), a String
	// identity (skip), and the Frame status object (skip).
	slot0 := &canonical.Node{Header: canonical.Header{
		Number: 0, Identifier: "slot-0", Access: r,
		Children: []canonical.Element{
			group(1, "identity", param(0, "Card name", canonical.ParamString, "")),
			group(2, "control",
				param(0, "NetwPrefix", canonical.ParamInteger, "uint8"), // Byte
				param(1, "Broadcasts", canonical.ParamEnum, ""),         // Enum
			),
			group(6, "frame", &canonical.Parameter{
				Header: canonical.Header{Number: 0, Identifier: "frame-status",
					Access: r, Children: canonical.EmptyChildren()},
				Type:   canonical.ParamOctets,
				Format: hintp("frame"),
				Value:  []any{int64(2), int64(2)},
			}),
		},
	}}

	// Slot 1: a card with an Integer control (oscillatable).
	slot1 := &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "slot-1", Access: r,
		Children: []canonical.Element{
			group(2, "control", param(0, "Level", canonical.ParamInteger, "")),
		},
	}}

	exp := &canonical.Export{Root: &canonical.Node{Header: canonical.Header{
		Number: 1, Identifier: "device", Access: r,
		Children: []canonical.Element{slot0, slot1},
	}}}
	return newServer(plugin.Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, exp)
}

func TestOscillatable(t *testing.T) {
	cases := []struct {
		t    codec.ObjectType
		want bool
	}{
		{codec.TypeInteger, true},
		{codec.TypeLong, true},
		{codec.TypeByte, true},
		{codec.TypeEnum, true},
		{codec.TypeIPAddr, true},
		{codec.TypeString, false},
		{codec.TypeFloat, false},
		{codec.TypeFrame, false},
		{codec.TypeAlarm, false},
		{codec.TypeFile, false},
	}
	for _, c := range cases {
		if got := oscillatable(c.t); got != c.want {
			t.Errorf("oscillatable(%v) = %v, want %v", c.t, got, c.want)
		}
	}
}

// TestOscillatableTargets_SpansAllSlotsInclSlot0 is the heart of the feature:
// `--play all` must drive every oscillatable object on every slot, slot 0
// included, and skip String/Frame objects.
func TestOscillatableTargets_SpansAllSlotsInclSlot0(t *testing.T) {
	s := playTreeServer(t)
	got := s.oscillatableTargets()

	want := []objectKey{
		{slot: 0, group: codec.GroupControl, id: 0}, // slot 0 Byte
		{slot: 0, group: codec.GroupControl, id: 1}, // slot 0 Enum
		{slot: 1, group: codec.GroupControl, id: 0}, // slot 1 Integer
	}
	if len(got) != len(want) {
		t.Fatalf("oscillatableTargets = %v (len %d), want len %d", got, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("target[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	// Explicitly assert slot 0 is represented and the skipped types are not.
	var sawSlot0 bool
	for _, k := range got {
		if k.slot == 0 {
			sawSlot0 = true
		}
		if k.group == codec.GroupIdentity || k.group == codec.GroupFrame {
			t.Errorf("non-oscillatable object leaked into targets: %+v", k)
		}
	}
	if !sawSlot0 {
		t.Error("slot 0 objects were not included — --play all must cover slot 0")
	}
}

func TestUniformInt(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	if got := uniformInt(r, 5, 5); got != 5 {
		t.Errorf("uniformInt(5,5) = %d, want 5 (degenerate range)", got)
	}
	for i := 0; i < 1000; i++ { // inverted bounds are swapped, not panicked
		if v := uniformInt(r, 10, 0); v < 0 || v > 10 {
			t.Fatalf("uniformInt(10,0) = %d, out of [0,10]", v)
		}
	}
	sawMin, sawMax := false, false
	for i := 0; i < 5000; i++ {
		v := uniformInt(r, 0, 4)
		if v < 0 || v > 4 {
			t.Fatalf("uniformInt out of range: %d", v)
		}
		if v == 0 {
			sawMin = true
		}
		if v == 4 {
			sawMax = true
		}
	}
	if !sawMin || !sawMax {
		t.Errorf("uniformInt did not cover the full span (sawMin=%v sawMax=%v)", sawMin, sawMax)
	}
}

// TestRandomBytesFor_FullRangeSpansBounds proves the "force random from min-max"
// mode actually swings across the whole range — unlike the walk mode, which
// only drifts ±1 from nominal and would never reach the far end quickly.
func TestRandomBytesFor_FullRangeSpansBounds(t *testing.T) {
	s := &server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	e := &entry{acpType: codec.TypeByte, param: &canonical.Parameter{
		Minimum: int64(0), Maximum: int64(32), Value: int64(10),
	}}
	r := rand.New(rand.NewSource(2))
	sawFar := false
	for i := 0; i < 2000; i++ {
		raw, ok := s.randomBytesFor(e, r, 10, true) // fullRange=true
		if !ok || len(raw) != 1 {
			t.Fatalf("byte randomBytesFor: ok=%v len=%d", ok, len(raw))
		}
		v := int64(raw[0])
		if v < 0 || v > 32 {
			t.Fatalf("byte value %d out of [0,32]", v)
		}
		if v > 25 { // far from nominal 10 — walk mode would not get here fast
			sawFar = true
		}
	}
	if !sawFar {
		t.Error("full-range mode never produced a far value — it is not spanning the range")
	}
}

// TestFrameStatusTick_SetsValidState: one tick flips a slot to a valid state
// (0..5) and the change is visible in the frame-status array — the dynamic a
// consumer detects on slot 0.
func TestFrameStatusTick_SetsValidState(t *testing.T) {
	s := newTestServer(t) // frame-status [2,2,0,0]
	r := rand.New(rand.NewSource(7))
	slot, state, ok := s.frameStatusTick(r)
	if !ok {
		t.Fatal("frameStatusTick returned ok=false on a server with frame-status")
	}
	if state > 5 {
		t.Errorf("state %d out of 0..5", state)
	}
	if got := readSlotStatus(t, s, slot); got != state {
		t.Errorf("after tick slot %d status = %d, want %d", slot, got, state)
	}
}

func TestFrameStatusTick_NoFrameStatusNoOp(t *testing.T) {
	s := newServerFromSlots(t, cardSlot(1, "slot-1", "GIO-12")) // no frame-status
	if _, _, ok := s.frameStatusTick(rand.New(rand.NewSource(1))); ok {
		t.Error("frameStatusTick should be a no-op when no frame-status object exists")
	}
}

// TestBroadcastAnnounce_SilentDuringShutdown pins the clean-Ctrl+C behaviour:
// once the server is shutting down (closed=true, as the ctx-cancel path sets),
// a racing --play announce must NOT spray a Warn. Without the guard every
// in-flight play tick logged "acp1 announce send" on the closed socket.
func TestBroadcastAnnounce_SilentDuringShutdown(t *testing.T) {
	s := newTestServer(t)
	var buf bytes.Buffer
	s.logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s.closed = true // simulate the shutdown window

	s.broadcastAnnounce(&codec.Message{
		MTID: 0, MType: codec.MTypeAnnounce,
		ObjGroup: codec.GroupFrame, ObjID: 0, Value: []byte{2, 2},
	})

	if strings.Contains(buf.String(), "announce send") {
		t.Errorf("announce Warn leaked during shutdown:\n%s", buf.String())
	}
}
