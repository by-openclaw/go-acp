//go:build integration

package osc_integration

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"acp/internal/osc/codec"
	consumer "acp/internal/osc/consumer"
	provider "acp/internal/osc/provider"
)

// TestV11_SLIP_Loopback_Message drives a v1.1 TCP round-trip with SLIP
// double-END framing and exercises the 1.1-specific payload-less tags
// T / F / N / I plus OSC-blob (which can contain 0xC0 / 0xDB bytes to
// trigger SLIP escape-stuffing).
func TestV11_SLIP_Loopback_Message(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cp := consumer.NewPluginV11(logger)
	defer func() { _ = cp.Disconnect() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cp.ConnectTCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectTCP: %v", err)
	}
	addr := cp.BoundTCPAddr()

	got := make(chan codec.Message, 4)
	_ = cp.SubscribePattern("/q/*", func(ev consumer.PacketEvent) { got <- ev.Msg })

	sp := provider.NewServerV11(logger)
	defer func() { _ = sp.Stop() }()

	// Blob with an END (0xC0) and an ESC (0xDB) byte to verify SLIP
	// escape-stuffing end-to-end.
	stuffBlob := []byte{0x01, codec.SLIPEnd, 0x02, codec.SLIPEsc, 0x03}
	sent := codec.Message{
		Address: "/q/go",
		Args: []codec.Arg{
			codec.True(),
			codec.False(),
			codec.Nil(),
			codec.Infinitum(),
			codec.Blob(stuffBlob),
		},
	}
	if err := sp.SendMessageTCP("127.0.0.1", addr.Port, sent); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case m := <-got:
		if m.Address != "/q/go" || len(m.Args) != 5 {
			t.Fatalf("shape: %+v", m)
		}
		if m.Args[0].Tag != codec.TagTrue {
			t.Errorf("arg0: %c", m.Args[0].Tag)
		}
		if m.Args[1].Tag != codec.TagFalse {
			t.Errorf("arg1: %c", m.Args[1].Tag)
		}
		if m.Args[2].Tag != codec.TagNil {
			t.Errorf("arg2: %c", m.Args[2].Tag)
		}
		if m.Args[3].Tag != codec.TagInfinitum {
			t.Errorf("arg3: %c", m.Args[3].Tag)
		}
		if len(m.Args[4].Blob) != len(stuffBlob) {
			t.Errorf("blob length: %d", len(m.Args[4].Blob))
		}
		for i, b := range stuffBlob {
			if m.Args[4].Blob[i] != b {
				t.Errorf("blob[%d] = 0x%02x, want 0x%02x", i, m.Args[4].Blob[i], b)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

// TestV11_SLIP_Loopback_Bundle verifies that nested bundles survive
// the SLIP round-trip (and that SLIP frames one packet == one bundle).
func TestV11_SLIP_Loopback_Bundle(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cp := consumer.NewPluginV11(logger)
	defer func() { _ = cp.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = cp.ConnectTCP(ctx, "127.0.0.1", 0)
	addr := cp.BoundTCPAddr()

	got := make(chan string, 8)
	_ = cp.SubscribePattern("", func(ev consumer.PacketEvent) { got <- ev.Msg.Address })

	sp := provider.NewServerV11(logger)
	defer func() { _ = sp.Stop() }()

	bundle := codec.Bundle{
		Timetag: 1,
		Elements: []codec.Packet{
			codec.Message{Address: "/scene/1", Args: []codec.Arg{codec.True()}},
			codec.Bundle{
				Timetag: 2,
				Elements: []codec.Packet{
					codec.Message{Address: "/scene/1/go", Args: []codec.Arg{codec.Int32(1)}},
					codec.Message{Address: "/scene/1/note", Args: []codec.Arg{codec.String("auto")}},
				},
			},
		},
	}
	if err := sp.SendBundleTCP("127.0.0.1", addr.Port, bundle); err != nil {
		t.Fatalf("send bundle: %v", err)
	}

	seen := map[string]bool{}
	deadline := time.After(3 * time.Second)
	for len(seen) < 3 {
		select {
		case a := <-got:
			seen[a] = true
		case <-deadline:
			t.Fatalf("got %d/3: %v", len(seen), seen)
		}
	}
	for _, want := range []string{"/scene/1", "/scene/1/go", "/scene/1/note"} {
		if !seen[want] {
			t.Errorf("missing %s", want)
		}
	}
}
