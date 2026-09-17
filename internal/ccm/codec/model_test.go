package codec

// Fixtures are real captures from the lab EVS Neuron (BRIDGE 6.7.4) —
// committed decoder oracles, the same precedent as the ACP2 CONVERT
// Hybrid DM. Expected values are read from the captured JSON, not from
// the decoder's own output.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return b
}

func TestDecodeSelf(t *testing.T) {
	d, err := DecodeSelf(read(t, "self.json"))
	if err != nil {
		t.Fatal(err)
	}
	if d.ProductName != "BRIDGE" || d.ProductVersion != "6.7.4" {
		t.Errorf("self = %s %s, want BRIDGE 6.7.4", d.ProductName, d.ProductVersion)
	}
	if d.Streams == nil {
		t.Error("Streams map must be initialised")
	}
}

func TestDecodeStreamsUUIDKeyed(t *testing.T) {
	snd, skipped, err := DecodeStreams(read(t, "senders-video.json"), KindSender, EssenceVideo)
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 0 {
		t.Errorf("real senders should all carry a uuid, skipped: %v", skipped)
	}
	if len(snd) == 0 {
		t.Fatal("no senders decoded")
	}
	// Every sender is UUID-identified and stamped with kind + essence.
	for _, s := range snd {
		if s.UUID == "" {
			t.Fatalf("sender %q has empty UUID", s.Name)
		}
		if s.Kind != KindSender || s.Essence != EssenceVideo {
			t.Errorf("sender %s not stamped: kind=%s essence=%s", s.UUID, s.Kind, s.Essence)
		}
	}
	// The first sender's legs carry per-path stream ids and a 2110 SDP.
	first := snd[0]
	if len(first.Legs) >= 2 {
		if first.Legs[0].IP == "" || first.Legs[0].StreamID == "" {
			t.Errorf("leg[0] incomplete: %+v", first.Legs[0])
		}
		if first.Legs[0].IP == first.Legs[1].IP {
			t.Errorf("ST 2022-7 legs should target distinct addresses: %s == %s", first.Legs[0].IP, first.Legs[1].IP)
		}
	}

	rcv, _, err := DecodeStreams(read(t, "receivers-video.json"), KindReceiver, EssenceVideo)
	if err != nil {
		t.Fatal(err)
	}

	// Assemble a Device and address a stream by UUID — the point of
	// the lib.
	d, _ := DecodeSelf(read(t, "self.json"))
	for _, s := range append(snd, rcv...) {
		d.Streams[s.UUID] = s
	}
	got, ok := d.Stream(first.UUID)
	if !ok || got.Name != first.Name {
		t.Errorf("lookup by UUID %s failed", first.UUID)
	}
	if n := len(d.StreamsByKind(KindSender)); n != len(snd) {
		t.Errorf("StreamsByKind(sender) = %d, want %d", n, len(snd))
	}
}

// A /self body the decoder cannot read is refused rather than
// answered with an empty identity: a Device with no product name is
// indistinguishable from one nobody asked about.
func TestDecodeSelfRefusesWhatItCannotRead(t *testing.T) {
	if _, err := DecodeSelf([]byte(`{`)); err == nil {
		t.Fatal("a body that is not JSON must be refused")
	}
}

// A stream tree that is not an array of streams is refused, and a
// stream with no UUID is skipped with its reason attached — a device
// reporting one is describing a resource nothing else in the plant can
// refer to.
func TestDecodeStreamsRefusalsAndSkips(t *testing.T) {
	if _, _, err := DecodeStreams([]byte(`{"not":"an array"}`), KindSender, EssenceVideo); err == nil {
		t.Fatal("a body that is not a stream array must be refused")
	}

	streams, skipped, err := DecodeStreams([]byte(`[
		{"name":"nameless"},
		{"uuid":"11111111-1111-1111-1111-111111111111","name":"cam-1"}
	]`), KindSender, EssenceVideo)
	if err != nil {
		t.Fatal(err)
	}
	if len(streams) != 1 || streams[0].Name != "cam-1" {
		t.Fatalf("streams = %+v, want only the one that can be keyed", streams)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "no uuid") {
		t.Fatalf("skipped = %v, want the nameless stream reported by name", skipped)
	}
	if !strings.Contains(skipped[0], "nameless") {
		t.Errorf("the reason must name what was skipped: %q", skipped[0])
	}
}
