//go:build integration

package tsl_integration

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"acp/internal/tsl/codec"
	consumer "acp/internal/tsl/consumer"
	provider "acp/internal/tsl/provider"
)

// TestV50_UDPLoopback_ASCII drives a single-DMSG ASCII v5.0 packet via
// localhost UDP through our provider → consumer stack.
func TestV50_UDPLoopback_ASCII(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cp := consumer.NewPluginV50(logger)
	defer func() { _ = cp.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cp.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("consumer Connect: %v", err)
	}

	got := make(chan codec.V50Packet, 4)
	if err := cp.SubscribeV50(func(ev consumer.FrameV50Event) { got <- ev.Frame }); err != nil {
		t.Fatalf("SubscribeV50: %v", err)
	}

	sp := provider.NewServerV50(logger)
	defer func() { _ = sp.Stop() }()
	if err := sp.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	dest := cp.BoundAddr()
	if err := sp.AddDestination(dest.IP.String(), dest.Port); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}

	sent := codec.V50Packet{
		Screen: 1,
		DMSGs: []codec.DMSG{
			{
				Index:      11,
				LH:         codec.TallyRed,
				TextTally:  codec.TallyGreen,
				RH:         codec.TallyAmber,
				Brightness: codec.BrightnessFull,
				Text:       "CAM 1",
			},
		},
	}
	if err := sp.SendV50(sent); err != nil {
		t.Fatalf("SendV50: %v", err)
	}

	select {
	case p := <-got:
		if p.Screen != 1 || p.UTF16LE {
			t.Errorf("envelope: %+v", p)
		}
		if len(p.DMSGs) != 1 {
			t.Fatalf("DMSG count %d, want 1", len(p.DMSGs))
		}
		d := p.DMSGs[0]
		if d.Index != 11 || d.LH != codec.TallyRed || d.TextTally != codec.TallyGreen || d.RH != codec.TallyAmber {
			t.Errorf("DMSG mismatch: %+v", d)
		}
		if d.Text != "CAM 1" {
			t.Errorf("Text=%q", d.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

// TestV50_UDPLoopback_MultiDMSG_UTF16 drives a multi-DMSG UTF-16LE
// packet (SCREEN=0 meaning unused + broadcast-safe).
func TestV50_UDPLoopback_MultiDMSG_UTF16(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cp := consumer.NewPluginV50(logger)
	defer func() { _ = cp.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = cp.Connect(ctx, "127.0.0.1", 0)

	got := make(chan codec.V50Packet, 1)
	_ = cp.SubscribeV50(func(ev consumer.FrameV50Event) { got <- ev.Frame })

	sp := provider.NewServerV50(logger)
	defer func() { _ = sp.Stop() }()
	_ = sp.Bind("127.0.0.1:0")
	dest := cp.BoundAddr()
	_ = sp.AddDestination(dest.IP.String(), dest.Port)

	sent := codec.V50Packet{
		UTF16LE: true,
		DMSGs: []codec.DMSG{
			{Index: 0, LH: codec.TallyRed, Text: "CAMÉRA"},
			{Index: 1, LH: codec.TallyGreen, Text: "日本語"},
			{Index: 2, LH: codec.TallyAmber, Text: "HOLA"},
		},
	}
	if err := sp.SendV50(sent); err != nil {
		t.Fatalf("SendV50: %v", err)
	}

	select {
	case p := <-got:
		if !p.UTF16LE {
			t.Errorf("UTF16LE flag not set")
		}
		if len(p.DMSGs) != 3 {
			t.Fatalf("DMSG count %d, want 3", len(p.DMSGs))
		}
		want := []string{"CAMÉRA", "日本語", "HOLA"}
		for i, w := range want {
			if p.DMSGs[i].Text != w {
				t.Errorf("DMSG[%d].Text=%q, want %q", i, p.DMSGs[i].Text, w)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

// TestV50_TCPLoopback drives a v5.0 packet through the DLE/STX wrapper:
// provider dials out to consumer's TCP listener; consumer un-stuffs and
// dispatches. Stuffing is exercised by a SCREEN value that contains 0xFE.
func TestV50_TCPLoopback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cp := consumer.NewPluginV50(logger)
	defer func() { _ = cp.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cp.ConnectV50TCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectV50TCP: %v", err)
	}
	addr := cp.BoundTCPAddr()
	if addr == nil {
		t.Fatal("BoundTCPAddr is nil")
	}

	got := make(chan codec.V50Packet, 4)
	if err := cp.SubscribeV50(func(ev consumer.FrameV50Event) { got <- ev.Frame }); err != nil {
		t.Fatalf("SubscribeV50: %v", err)
	}

	sp := provider.NewServerV50(logger)
	defer func() { _ = sp.Stop() }()

	// SCREEN = 0xFEFE exercises byte-stuffing (0xFE → 0xFE 0xFE twice).
	sent := codec.V50Packet{
		Screen: 0xFEFE,
		DMSGs: []codec.DMSG{
			{Index: 0xFE01, LH: codec.TallyRed, Brightness: codec.BrightnessFull, Text: "STUFF"},
		},
	}
	if err := sp.SendV50TCP("127.0.0.1", addr.Port, sent); err != nil {
		t.Fatalf("SendV50TCP: %v", err)
	}

	select {
	case p := <-got:
		if p.Screen != 0xFEFE {
			t.Errorf("Screen=0x%04x, want 0xFEFE", p.Screen)
		}
		if len(p.DMSGs) != 1 || p.DMSGs[0].Index != 0xFE01 {
			t.Errorf("DMSG mismatch: %+v", p.DMSGs)
		}
		if p.DMSGs[0].Text != "STUFF" {
			t.Errorf("Text=%q", p.DMSGs[0].Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

// TestV50_TCP_StreamedMultipleFrames verifies the stream decoder can
// parse back-to-back frames on the same connection.
func TestV50_TCP_StreamedMultipleFrames(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cp := consumer.NewPluginV50(logger)
	defer func() { _ = cp.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = cp.ConnectV50TCP(ctx, "127.0.0.1", 0)
	addr := cp.BoundTCPAddr()

	got := make(chan codec.V50Packet, 8)
	_ = cp.SubscribeV50(func(ev consumer.FrameV50Event) { got <- ev.Frame })

	sp := provider.NewServerV50(logger)
	defer func() { _ = sp.Stop() }()

	for i := uint16(0); i < 4; i++ {
		sent := codec.V50Packet{
			Screen: i,
			DMSGs: []codec.DMSG{
				{Index: i, TextTally: codec.TallyRed, Text: "A"},
			},
		}
		if err := sp.SendV50TCP("127.0.0.1", addr.Port, sent); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	seen := map[uint16]bool{}
	deadline := time.After(3 * time.Second)
	for len(seen) < 4 {
		select {
		case p := <-got:
			seen[p.Screen] = true
		case <-deadline:
			t.Fatalf("only %d/4 frames received, seen=%v", len(seen), seen)
		}
	}
	for i := uint16(0); i < 4; i++ {
		if !seen[i] {
			t.Errorf("missing screen %d", i)
		}
	}
}
