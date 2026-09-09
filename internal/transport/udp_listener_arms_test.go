package transport

import (
	"context"
	"errors"
	"testing"
	"time"
)

// setReuseAddr reports a socket-option failure instead of hiding it: a
// descriptor that is not a socket cannot take SO_REUSEADDR on any OS.
func TestSetReuseAddrReportsSocketOptionFailure(t *testing.T) {
	if err := setReuseAddr(^uintptr(0)); err == nil {
		t.Error("SO_REUSEADDR on a bogus descriptor must fail")
	}
}

// A listener Receive whose context is already cancelled reports the
// cancellation — through the cancel watcher closing the socket — rather
// than a bare read error, on every OS.
func TestUDPListenerReceiveReportsCancelledContext(t *testing.T) {
	l, err := listenUDPEphemeral(t)
	if err != nil {
		t.Skipf("cannot bind a UDP listener here: %v", err)
	}
	defer func() { _ = l.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = l.Receive(ctx, 64)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Receive with a cancelled ctx = %v, want context.Canceled", err)
	}

	// And a cancellation that lands while the read is blocked.
	l2, err := listenUDPEphemeral(t)
	if err != nil {
		t.Skipf("cannot bind a UDP listener here: %v", err)
	}
	defer func() { _ = l2.Close() }()
	ctx2, cancel2 := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, _, err := l2.Receive(ctx2, 64); done <- err }()
	time.AfterFunc(20*time.Millisecond, cancel2)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Receive cancelled mid-read = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Receive did not return after cancel")
	}
}
