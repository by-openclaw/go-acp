package acp1

import (
	"io"
	"log/slog"
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
	return newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), exp)
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
