package codec

// Fixtures are real captures from the lab EVS Neuron (BRIDGE 6.7.4) —
// committed decoder oracles, the same precedent as the ACP2 CONVERT
// Hybrid DM. Expected values are read from the captured JSON, not from
// the decoder's own output.

import (
	"os"
	"path/filepath"
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
