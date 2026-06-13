package acp1

import (
	"context"
	"net"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
)

// TestNewListener_NilLogger covers the nil-logger default arm.
func TestNewListener_NilLogger(t *testing.T) {
	l, err := NewListener(nil, 0)
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}
	defer l.Stop()
	if l.logger == nil {
		t.Fatal("nil logger should default to slog.Default()")
	}
}

// TestNewListener_BindError: an out-of-range port makes the underlying UDP
// bind fail, surfacing from NewListener.
func TestNewListener_BindError(t *testing.T) {
	if _, err := NewListener(nil, 999999); err == nil {
		t.Fatal("out-of-range port: want bind error")
	}
}

// TestListener_SubscriberPanicRecovered: a subscriber that panics must not
// kill the loop — the panic-guarded dispatch recovers it (line ~206).
func TestListener_SubscriberPanicRecovered(t *testing.T) {
	l, err := NewListener(nil, 0)
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}
	defer l.Stop()
	l.Start(context.Background())

	ok := make(chan struct{}, 2)
	l.Subscribe(-1, "", -1, func(*codec.Message) { ok <- struct{}{}; panic("boom") })
	l.Subscribe(-1, "", -1, func(*codec.Message) { ok <- struct{}{} })

	la := l.conn.LocalAddr().(*net.UDPAddr)
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: la.Port})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = conn.Write(buildReply(t, 0, codec.MTypeAnnounce, 0, codec.GroupFrame, 0, []byte{2, 2}))

	got := 0
	deadline := time.After(2 * time.Second)
	for got < 1 {
		select {
		case <-ok:
			got++
		case <-deadline:
			t.Fatal("subscriber not invoked")
		}
	}
	// The loop must still be alive: send a second announcement and confirm
	// a callback fires again.
	_, _ = conn.Write(buildReply(t, 0, codec.MTypeAnnounce, 0, codec.GroupFrame, 0, []byte{2, 2}))
	select {
	case <-ok:
	case <-time.After(2 * time.Second):
		t.Fatal("listener died after subscriber panic")
	}
}

// TestListener_CtxCancelBeforeReceive: a context cancelled before Start
// drives the loop's pre-receive ctx.Err() guard.
func TestListener_CtxCancelBeforeReceive(t *testing.T) {
	l, err := NewListener(nil, 0)
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}
	defer l.Stop()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel up-front so the loop's first ctx.Err() check returns
	l.Start(ctx)
	select {
	case <-l.done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit on pre-cancelled ctx")
	}
}

// TestListener_ClosedConnExits: closing the socket under the running loop
// (without cancelling ctx) drives the receive-error handling. Depending on
// the OS this surfaces as net.ErrClosed (immediate return) or a transient
// error (log + short-sleep retry); either way the loop must terminate once
// Stop cancels the context.
func TestListener_ClosedConnExits(t *testing.T) {
	l, err := NewListener(nil, 0)
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}
	l.Start(context.Background())
	time.Sleep(20 * time.Millisecond) // let the loop reach Receive
	// Close the socket directly → Receive returns an error (ErrClosed or
	// a transient OS error), exercising the error-handling branch.
	_ = l.conn.Close()
	time.Sleep(50 * time.Millisecond) // let the loop process the error path
	// Stop cancels the context so the loop terminates regardless of which
	// error branch the OS produced.
	l.cancel()
	select {
	case <-l.done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not exit after cancel")
	}
}
