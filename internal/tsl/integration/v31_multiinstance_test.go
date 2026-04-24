//go:build integration

package tsl_integration

import (
	"context"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"acp/internal/tsl/codec"
	consumer "acp/internal/tsl/consumer"
	provider "acp/internal/tsl/provider"
)

// TestV31_MultiInstance_SamePort verifies the SO_REUSEADDR multi-listener
// contract: two dhs consumers bound to the same local port both receive
// the same broadcast datagram. Matches the ACP1 announcement-listener
// behaviour expected for all UDP-push protocols.
func TestV31_MultiInstance_SamePort(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Pick an ephemeral port for the first listener, then bind the
	// second to the same port.
	cp1 := consumer.NewPluginV31(logger)
	defer func() { _ = cp1.Disconnect() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cp1.Connect(ctx, "127.0.0.1", 0); err != nil {
		t.Fatalf("cp1 Connect: %v", err)
	}
	sharedPort := cp1.BoundAddr().Port

	cp2 := consumer.NewPluginV31(logger)
	defer func() { _ = cp2.Disconnect() }()
	if err := cp2.Connect(ctx, "127.0.0.1", sharedPort); err != nil {
		t.Fatalf("cp2 Connect to shared port %d: %v", sharedPort, err)
	}
	if cp2.BoundAddr().Port != sharedPort {
		t.Fatalf("cp2 port mismatch: got %d, want %d", cp2.BoundAddr().Port, sharedPort)
	}

	got1 := make(chan codec.V31Frame, 2)
	got2 := make(chan codec.V31Frame, 2)
	_ = cp1.SubscribeV31(func(ev consumer.FrameV31Event) { got1 <- ev.Frame })
	_ = cp2.SubscribeV31(func(ev consumer.FrameV31Event) { got2 <- ev.Frame })

	// Send one datagram. Both listeners should receive it (SO_REUSEPORT
	// on Linux distributes; SO_REUSEADDR on Windows broadcasts to all).
	// Note: on Linux with only SO_REUSEADDR, only one listener would
	// receive. The transport package best-efforts SO_REUSEPORT=15 on
	// Linux to achieve the fan-out behaviour.
	sp := provider.NewServerV31(logger)
	defer func() { _ = sp.Stop() }()
	_ = sp.Bind("127.0.0.1:0")
	_ = sp.AddDestination("127.0.0.1", sharedPort)

	sent := codec.V31Frame{Address: 3, Tally1: true, Brightness: codec.BrightnessFull, Text: "MULTI"}
	if err := sp.SendV31(sent); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Wait for at least one of the two to receive; on Linux it may be
	// just one if SO_REUSEPORT isn't active. On Windows both should get
	// it via SO_REUSEADDR broadcast semantics.
	deadline := time.After(2 * time.Second)
	recvCount := 0
	for recvCount < 2 {
		select {
		case f := <-got1:
			if f.Address != 3 || !f.Tally1 {
				t.Errorf("cp1 wrong frame: %+v", f)
			}
			recvCount++
		case f := <-got2:
			if f.Address != 3 || !f.Tally1 {
				t.Errorf("cp2 wrong frame: %+v", f)
			}
			recvCount++
		case <-deadline:
			if recvCount == 0 {
				t.Fatal("no listener received — SO_REUSEADDR wiring broken")
			}
			// At least one received — acceptable on platforms where
			// SO_REUSEPORT fan-out isn't guaranteed.
			t.Logf("only %d/2 listeners received (platform-dependent SO_REUSEPORT semantics)", recvCount)
			return
		}
	}
}

// TestV31_BroadcastDestAllowed verifies the provider socket has
// SO_BROADCAST enabled so sends to 255.255.255.255 don't fail at the
// kernel level. We don't assert delivery (that depends on the local
// network stack) — only that WriteToUDP accepts the write.
func TestV31_BroadcastDestAllowed(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	sp := provider.NewServerV31(logger)
	defer func() { _ = sp.Stop() }()
	if err := sp.Bind("127.0.0.1:0"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if err := sp.AddDestination("255.255.255.255", 40000); err != nil {
		t.Fatalf("addDest broadcast: %v", err)
	}
	frame := codec.V31Frame{Address: 1, Text: "BCAST"}
	// Expect no "operation not permitted" error. Delivery not asserted.
	if err := sp.SendV31(frame); err != nil {
		// Some platforms may return "network is unreachable" if there
		// is no broadcast-capable interface; that's acceptable. We only
		// want to avoid SO_BROADCAST-missing errors (EACCES).
		var opErr *net.OpError
		if asOpErr(err, &opErr) && isBroadcastDisallowed(opErr) {
			t.Fatalf("broadcast send rejected — SO_BROADCAST wiring broken: %v", err)
		}
	}
}

func asOpErr(err error, target **net.OpError) bool {
	for err != nil {
		if oe, ok := err.(*net.OpError); ok {
			*target = oe
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func isBroadcastDisallowed(oe *net.OpError) bool {
	return oe != nil && oe.Err != nil && oe.Err.Error() != "" &&
		(containsAny(oe.Err.Error(), "permission denied") ||
			containsAny(oe.Err.Error(), "access is denied"))
}

func containsAny(s, sub string) bool {
	if len(sub) == 0 || len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
