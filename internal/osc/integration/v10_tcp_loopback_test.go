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

// TestV10_TCP_Loopback drives a v1.0 length-prefix TCP round-trip:
// consumer listens, provider dials + writes framed packets.
func TestV10_TCP_Loopback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cp := consumer.NewPluginV10(logger)
	defer func() { _ = cp.Disconnect() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cp.ConnectTCP(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("ConnectTCP: %v", err)
	}
	addr := cp.BoundTCPAddr()
	if addr == nil {
		t.Fatal("BoundTCPAddr nil")
	}

	got := make(chan codec.Message, 4)
	if err := cp.SubscribePattern("/cue/*", func(ev consumer.PacketEvent) { got <- ev.Msg }); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	sp := provider.NewServerV10(logger)
	defer func() { _ = sp.Stop() }()

	sent := codec.Message{
		Address: "/cue/fire",
		Args: []codec.Arg{
			codec.String("show-start"),
			codec.Float32(1.0),
		},
	}
	if err := sp.SendMessageTCP("127.0.0.1", addr.Port, sent); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case m := <-got:
		if m.Address != "/cue/fire" || m.Args[0].String != "show-start" {
			t.Errorf("round-trip: %+v", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

// TestV10_TCP_MultipleFramesOnOneConnection verifies the length-prefix
// stream decoder parses back-to-back packets correctly.
func TestV10_TCP_MultipleFramesOnOneConnection(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cp := consumer.NewPluginV10(logger)
	defer func() { _ = cp.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = cp.ConnectTCP(ctx, "127.0.0.1", 0)
	addr := cp.BoundTCPAddr()

	got := make(chan string, 8)
	_ = cp.SubscribePattern("", func(ev consumer.PacketEvent) { got <- ev.Msg.Address })

	sp := provider.NewServerV10(logger)
	defer func() { _ = sp.Stop() }()
	for i := 0; i < 4; i++ {
		m := codec.Message{
			Address: "/ch/" + string(rune('0'+i)),
			Args:    []codec.Arg{codec.Int32(int32(i))},
		}
		if err := sp.SendMessageTCP("127.0.0.1", addr.Port, m); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	seen := map[string]bool{}
	deadline := time.After(3 * time.Second)
	for len(seen) < 4 {
		select {
		case a := <-got:
			seen[a] = true
		case <-deadline:
			t.Fatalf("got %d/4: %v", len(seen), seen)
		}
	}
}
