package emberplus

import (
	"sync"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/glow"
)

// newStreamTestPlugin builds a minimally-initialised Plugin with the
// index maps a stream dispatch needs, plus one stream-backed Parameter
// entry registered under streamID. The entry's glowParam carries the
// supplied StreamDescriptor (nil = native-value passthrough).
func newStreamTestPlugin(t *testing.T, streamID int64, desc *glow.StreamDescription) (*Plugin, *treeEntry) {
	t.Helper()
	p := &Plugin{
		logger:      discardLogger(),
		numIndex:    map[string]*treeEntry{},
		pathIndex:   map[string]*treeEntry{},
		labelIndex:  map[string][]*treeEntry{},
		numPath:     map[string]string{},
		subs:        map[string]consumer.EventFunc{},
		streamSubs:  map[string][]int32{},
		streamIndex: map[int64][]string{},
		profile:     &compliance.Profile{},
	}
	gp := &glow.Parameter{
		Number:              1,
		Path:                []int32{1, 1},
		Identifier:          "meter",
		StreamIdentifier:    streamID,
		HasStreamIdentifier: true,
		StreamDescriptor:    desc,
		Value:               int64(0),
	}
	entry := &treeEntry{
		glowParam:   gp,
		numericPath: []int32{1, 1},
		freshness:   FreshnessLive,
		obj: consumer.Object{
			OID: "1.1", Label: "meter", Path: []string{"root", "meter"},
			Kind: consumer.KindInt, Value: consumer.Value{Kind: consumer.KindInt},
		},
	}
	key := numericKey(entry.numericPath)
	p.numIndex[key] = entry
	p.streamIndex[streamID] = []string{key}
	return p, entry
}

// TestStreamDispatch_NativeValue drives dispatchStreams →
// deliverStreamValue → notifySubscribers for a StreamEntry carrying a
// native (already-typed) value with no StreamDescriptor — the
// resolveStreamValue passthrough branch.
func TestStreamDispatch_NativeValue(t *testing.T) {
	p, _ := newStreamTestPlugin(t, 7, nil)

	var mu sync.Mutex
	var got []consumer.Event
	p.subs["*"] = func(e consumer.Event) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}

	p.handleElements([]glow.Element{{
		Streams: []glow.StreamEntry{{StreamIdentifier: 7, Value: int64(42)}},
	}})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
	if got[0].Value.Int != 42 {
		t.Errorf("delivered value = %d, want 42", got[0].Value.Int)
	}
	// One field-diff change synthesised (0 → 42).
	if len(got[0].Changes) != 1 || got[0].Changes[0].Name != "value" {
		t.Errorf("changes = %+v, want one value change", got[0].Changes)
	}
}

// TestStreamDispatch_DescriptorDecode drives resolveStreamValue's
// octet-string + StreamDescriptor branch: the raw payload is a byte
// slice and the descriptor selects a 16-bit big-endian sample at an
// offset.
func TestStreamDispatch_DescriptorDecode(t *testing.T) {
	desc := &glow.StreamDescription{
		Format: glow.StreamFmtUnsignedInt16BigEndian,
		Offset: 2,
	}
	p, _ := newStreamTestPlugin(t, 9, desc)

	delivered := make(chan consumer.Event, 1)
	p.subs["*"] = func(e consumer.Event) {
		select {
		case delivered <- e:
		default:
		}
	}

	// payload: [pad pad 0x01 0x02] → at offset 2, u16be(0x0102) = 258.
	payload := []byte{0xAA, 0xBB, 0x01, 0x02}
	p.handleElements([]glow.Element{{
		Streams: []glow.StreamEntry{{StreamIdentifier: 9, Value: payload}},
	}})

	select {
	case e := <-delivered:
		if e.Value.Int != 258 {
			t.Errorf("decoded value = %d, want 258", e.Value.Int)
		}
	default:
		t.Fatal("no stream event delivered")
	}
}

// TestStreamDispatch_EdgeCases covers resolveStreamValue's reject
// branches and dispatchStreams' skip branches: unknown streamId, raw
// not a []byte despite a descriptor, and an out-of-range offset.
func TestStreamDispatch_EdgeCases(t *testing.T) {
	// raw value is not []byte but a descriptor is set → passthrough.
	desc := &glow.StreamDescription{Format: glow.StreamFmtUnsignedInt16BigEndian, Offset: 0}
	gp := &glow.Parameter{StreamDescriptor: desc}
	if got := resolveStreamValue(gp, int64(5)); got != int64(5) {
		t.Errorf("non-byte raw with descriptor = %v, want passthrough 5", got)
	}
	// offset past end → passthrough raw.
	descOff := &glow.StreamDescription{Format: glow.StreamFmtUnsignedInt16BigEndian, Offset: 99}
	gp2 := &glow.Parameter{StreamDescriptor: descOff}
	raw := []byte{0x01, 0x02}
	gotOff := resolveStreamValue(gp2, raw)
	if _, isBytes := gotOff.([]byte); !isBytes {
		t.Errorf("out-of-range offset must return raw bytes, got %T", gotOff)
	}
	// negative offset → passthrough.
	descNeg := &glow.StreamDescription{Format: glow.StreamFmtUnsignedInt16BigEndian, Offset: -1}
	gp3 := &glow.Parameter{StreamDescriptor: descNeg}
	gotNeg := resolveStreamValue(gp3, raw)
	if _, isBytes := gotNeg.([]byte); !isBytes {
		t.Errorf("negative offset must return raw bytes, got %T", gotNeg)
	}

	// dispatchStreams: unknown streamId is skipped silently (no panic).
	p, _ := newStreamTestPlugin(t, 1, nil)
	p.dispatchStreams([]glow.StreamEntry{{StreamIdentifier: 999, Value: int64(1)}})

	// dispatchStreams: streamId maps to a path whose entry has a nil
	// glowParam — skipped.
	p.numIndex["1.1"].glowParam = nil
	p.dispatchStreams([]glow.StreamEntry{{StreamIdentifier: 1, Value: int64(1)}})
}

// TestDeliverStreamValue_NilGuard covers the early-return when the
// entry's glowParam is nil.
func TestDeliverStreamValue_NilGuard(t *testing.T) {
	p, entry := newStreamTestPlugin(t, 3, nil)
	entry.glowParam = nil
	// Must not panic; no subscriber needed.
	p.deliverStreamValue(entry, int64(1))
}
