//go:build integration

// Loopback integration test: our OSC 1.0 provider pushes to our OSC 1.0
// consumer via localhost UDP. No external peer required; CI-safe.
//
// Run with:
//
//	go test -tags integration ./internal/osc/integration/...
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

func TestV10_UDP_Loopback_Message(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cp := consumer.NewPluginV10(logger)
	defer func() { _ = cp.Disconnect() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cp.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	got := make(chan codec.Message, 4)
	if err := cp.SubscribePattern("/mixer/*", func(ev consumer.PacketEvent) { got <- ev.Msg }); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	sp := provider.NewServerV10(logger)
	defer func() { _ = sp.Stop() }()
	if err := sp.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	dest := cp.BoundAddr()
	if err := sp.AddDestination(dest.IP.String(), dest.Port); err != nil {
		t.Fatalf("addDest: %v", err)
	}

	sent := codec.Message{
		Address: "/mixer/fader1",
		Args: []codec.Arg{
			codec.Float32(0.75),
			codec.String("PGM"),
			codec.Int32(42),
		},
	}
	if err := sp.SendMessage(sent); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case m := <-got:
		if m.Address != "/mixer/fader1" {
			t.Errorf("address = %q", m.Address)
		}
		if len(m.Args) != 3 {
			t.Fatalf("args len=%d, want 3", len(m.Args))
		}
		if m.Args[0].Float32 != 0.75 {
			t.Errorf("arg0=%v", m.Args[0].Float32)
		}
		if m.Args[1].String != "PGM" {
			t.Errorf("arg1=%q", m.Args[1].String)
		}
		if m.Args[2].Int32 != 42 {
			t.Errorf("arg2=%d", m.Args[2].Int32)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func TestV10_UDP_Loopback_Bundle_FansToSubscribers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cp := consumer.NewPluginV10(logger)
	defer func() { _ = cp.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = cp.Connect(ctx, "127.0.0.1", 0)

	got := make(chan string, 8)
	_ = cp.SubscribePattern("", func(ev consumer.PacketEvent) { got <- ev.Msg.Address })

	sp := provider.NewServerV10(logger)
	defer func() { _ = sp.Stop() }()
	_ = sp.Bind("127.0.0.1:0")
	dest := cp.BoundAddr()
	_ = sp.AddDestination(dest.IP.String(), dest.Port)

	bundle := codec.Bundle{
		Timetag: 1,
		Elements: []codec.Packet{
			codec.Message{Address: "/ch/1/mute", Args: []codec.Arg{codec.True()}},
			codec.Message{Address: "/ch/2/mute", Args: []codec.Arg{codec.False()}},
			codec.Message{Address: "/ch/3/label", Args: []codec.Arg{codec.String("Host Mic")}},
		},
	}
	if err := sp.SendBundle(bundle); err != nil {
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
	for _, want := range []string{"/ch/1/mute", "/ch/2/mute", "/ch/3/label"} {
		if !seen[want] {
			t.Errorf("missing %s", want)
		}
	}
}

func TestV10_UDP_MultiInstance_SamePort(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cp1 := consumer.NewPluginV10(logger)
	defer func() { _ = cp1.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cp1.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("cp1: %v", err)
	}
	sharedPort := cp1.BoundAddr().Port

	cp2 := consumer.NewPluginV10(logger)
	defer func() { _ = cp2.Disconnect() }()
	if err := cp2.Connect(ctx, "127.0.0.1", sharedPort); err != nil {
		t.Fatalf("cp2 shared port: %v", err)
	}
	if cp2.BoundAddr().Port != sharedPort {
		t.Fatalf("cp2 port mismatch")
	}

	got1 := make(chan string, 2)
	got2 := make(chan string, 2)
	_ = cp1.SubscribePattern("", func(ev consumer.PacketEvent) { got1 <- ev.Msg.Address })
	_ = cp2.SubscribePattern("", func(ev consumer.PacketEvent) { got2 <- ev.Msg.Address })

	sp := provider.NewServerV10(logger)
	defer func() { _ = sp.Stop() }()
	_ = sp.Bind("127.0.0.1:0")
	_ = sp.AddDestination("127.0.0.1", sharedPort)
	_ = sp.SendMessage(codec.Message{Address: "/multi", Args: []codec.Arg{codec.Int32(1)}})

	// Accept partial reception on platforms where SO_REUSEPORT isn't
	// active (TSL-multiinstance pattern).
	recv := 0
	deadline := time.After(2 * time.Second)
	for recv < 2 {
		select {
		case <-got1:
			recv++
		case <-got2:
			recv++
		case <-deadline:
			if recv == 0 {
				t.Fatal("no listener received — SO_REUSEADDR wiring broken")
			}
			return
		}
	}
}
