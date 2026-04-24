//go:build integration

// Loopback integration test: our own v3.1 provider pushes to our own
// v3.1 consumer via localhost UDP. Exercises encode → UDP → decode →
// subscribe callback end-to-end. No external peer required; CI-safe.
//
// Run with:
//
//	go test -tags integration ./internal/tsl/integration/...
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

func TestV31_Loopback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cp := consumer.NewPluginV31(logger)
	defer func() { _ = cp.Disconnect() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cp.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("consumer Connect: %v", err)
	}

	got := make(chan codec.V31Frame, 4)
	if err := cp.SubscribeV31(func(ev consumer.FrameV31Event) { got <- ev.Frame }); err != nil {
		t.Fatalf("SubscribeV31: %v", err)
	}

	// Start provider, bind to ephemeral, register the consumer's port
	// as the destination.
	sp := provider.NewServerV31(logger)
	defer func() { _ = sp.Stop() }()
	if err := sp.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("provider Bind: %v", err)
	}
	dest := cp.BoundAddr()
	if err := sp.AddDestination(dest.IP.String(), dest.Port); err != nil {
		t.Fatalf("AddDestination: %v", err)
	}

	// Send a v3.1 frame.
	sent := codec.V31Frame{
		Address:    7,
		Tally1:     true,
		Tally4:     true,
		Brightness: codec.BrightnessFull,
		Text:       "PGM LIVE",
	}
	if err := sp.SendV31(sent); err != nil {
		t.Fatalf("SendV31: %v", err)
	}

	select {
	case f := <-got:
		if f.Address != 7 {
			t.Errorf("Address=%d, want 7", f.Address)
		}
		if !f.Tally1 || f.Tally2 || f.Tally3 || !f.Tally4 {
			t.Errorf("tally mismatch: %+v", f)
		}
		if f.Brightness != codec.BrightnessFull {
			t.Errorf("brightness=%v, want full", f.Brightness)
		}
		if len(f.Notes) != 0 {
			t.Errorf("clean frame should have no notes, got %+v", f.Notes)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for v3.1 frame")
	}
}

func TestV31_Loopback_MultipleFramesInOrder(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cp := consumer.NewPluginV31(logger)
	defer func() { _ = cp.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cp.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("connect: %v", err)
	}

	got := make(chan codec.V31Frame, 16)
	_ = cp.SubscribeV31(func(ev consumer.FrameV31Event) { got <- ev.Frame })

	sp := provider.NewServerV31(logger)
	defer func() { _ = sp.Stop() }()
	_ = sp.Bind("127.0.0.1:0")
	dest := cp.BoundAddr()
	_ = sp.AddDestination(dest.IP.String(), dest.Port)

	// Drive 8 addresses, each with distinct tally combos.
	for addr := uint8(0); addr < 8; addr++ {
		f := codec.V31Frame{
			Address:    addr,
			Tally1:     addr&1 != 0,
			Tally2:     addr&2 != 0,
			Tally3:     addr&4 != 0,
			Brightness: codec.BrightnessFull,
			Text:       "A",
		}
		if err := sp.SendV31(f); err != nil {
			t.Fatalf("addr %d send: %v", addr, err)
		}
	}

	// Collect all 8. UDP ordering over localhost is effectively
	// preserved; we allow minor reordering by checking the set.
	seen := map[uint8]codec.V31Frame{}
	deadline := time.After(3 * time.Second)
	for len(seen) < 8 {
		select {
		case f := <-got:
			seen[f.Address] = f
		case <-deadline:
			t.Fatalf("got %d/8 frames after 3s", len(seen))
		}
	}
	for addr := uint8(0); addr < 8; addr++ {
		f, ok := seen[addr]
		if !ok {
			t.Errorf("missing addr %d", addr)
			continue
		}
		wantT1 := addr&1 != 0
		wantT2 := addr&2 != 0
		wantT3 := addr&4 != 0
		if f.Tally1 != wantT1 || f.Tally2 != wantT2 || f.Tally3 != wantT3 {
			t.Errorf("addr %d tally = (%v,%v,%v), want (%v,%v,%v)",
				addr, f.Tally1, f.Tally2, f.Tally3, wantT1, wantT2, wantT3)
		}
	}
}
