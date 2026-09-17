package acp1

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// TestListener_Loopback binds a real UDP listener, registers matching and
// non-matching subscriptions, sends one announcement datagram, and asserts the
// receive loop decodes + dispatches it to exactly the matching subscribers.
func TestListener_Loopback(t *testing.T) {
	l, err := NewListener(slog.Default(), 0) // ephemeral port
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}
	defer l.Stop()
	l.Start(context.Background())

	got := make(chan int, 8)
	hWild := l.Subscribe(-1, "", -1, func(*codec.Message) { got <- 1 }) // wildcard → match
	l.Subscribe(0, "frame", 0, func(*codec.Message) { got <- 2 })       // exact → match
	l.Subscribe(5, "frame", 0, func(*codec.Message) { got <- 99 })      // slot mismatch
	l.Subscribe(0, "control", 0, func(*codec.Message) { got <- 99 })    // group mismatch
	l.Subscribe(0, "frame", 9, func(*codec.Message) { got <- 99 })      // id mismatch

	la, ok := l.conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("listener addr type %T", l.conn.LocalAddr())
	}
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: la.Port})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// MTID=0 announcement on slot 0 / frame / 0.
	ann := buildReply(t, 0, codec.MTypeAnnounce, 0, codec.GroupFrame, 0, []byte{2, 2})
	if _, err := conn.Write(ann); err != nil {
		t.Fatalf("write announce: %v", err)
	}

	// Expect exactly the two matching subscribers to fire.
	deadline := time.After(2 * time.Second)
	fired := 0
	for fired < 2 {
		select {
		case v := <-got:
			if v == 99 {
				t.Fatalf("a non-matching subscription fired")
			}
			fired++
		case <-deadline:
			t.Fatalf("announcement not dispatched: only %d/2 subscribers fired", fired)
		}
	}

	// Unsubscribe (valid handle + stale handle no-op).
	l.Unsubscribe(hWild)
	l.Unsubscribe(SubHandle(9999))

	// A non-announcement (MTID!=0) must be ignored by the loop.
	nonAnn := buildReply(t, 5, codec.MTypeReply, byte(codec.MethodGetValue), codec.GroupFrame, 0, []byte{1})
	_, _ = conn.Write(nonAnn)
	// A malformed datagram must be tolerated.
	_, _ = conn.Write([]byte{0x01})
	time.Sleep(50 * time.Millisecond) // let the loop drain both

	l.Stop() // idempotent with the deferred Stop
}
