package probelsw08p

// Provider-side reaping of silent clients.
//
// Symmetric with the consumer fix: a provider without this holds a goroutine,
// a socket and a session entry for every controller that vanished without an
// RST. On a consumer that costs one hung watch; on a provider it accumulates
// one leak per lost client for the life of the process, and nothing errors.

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func testServer(t *testing.T) *server {
	t.Helper()
	return newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
}

func TestServerSessionIdleTimeoutDefaultsOff(t *testing.T) {
	srv := testServer(t)
	if got := srv.idleTimeout(); got != 0 {
		t.Fatalf("idleTimeout = %v by default, want 0 — SW-P-08 mandates no "+
			"keep-alive, so a quiet controller must not be disconnected", got)
	}
}

func TestServerSetSessionIdleTimeout(t *testing.T) {
	srv := testServer(t)
	srv.SetSessionIdleTimeout(DefaultSessionIdleTimeout)
	if got := srv.idleTimeout(); got != DefaultSessionIdleTimeout {
		t.Fatalf("idleTimeout = %v, want %v", got, DefaultSessionIdleTimeout)
	}
	srv.SetSessionIdleTimeout(-1)
	if got := srv.idleTimeout(); got != -1 {
		t.Fatalf("idleTimeout = %v, want the disable sentinel preserved", got)
	}
}

// The window must leave room for several missed keep-alives.
func TestDefaultSessionIdleTimeoutAllowsMissedKeepalives(t *testing.T) {
	if DefaultSessionIdleTimeout < 3*DefaultKeepaliveInterval {
		t.Fatalf("DefaultSessionIdleTimeout (%v) must allow at least 3 keep-alives of %v",
			DefaultSessionIdleTimeout, DefaultKeepaliveInterval)
	}
}

// A client that connects and then says nothing is reaped once the window is
// armed — the session goroutine returns instead of blocking forever.
func TestSessionReapsSilentClient(t *testing.T) {
	srv := testServer(t)
	srv.SetSessionIdleTimeout(120 * time.Millisecond)

	a, b := net.Pipe()
	defer func() { _ = b.Close() }()

	sess := newSession(srv, a)
	done := make(chan struct{})
	go func() { sess.run(context.Background()); close(done) }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("session.run never returned on a silent client — the provider is " +
			"holding a goroutine and socket for a peer that stopped talking")
	}
}

// With reaping OFF (the default) a silent client is NOT disconnected: a quiet
// controller on a protocol with no heartbeat is healthy, and killing it would
// be worse than leaking.
func TestSessionKeepsSilentClientWhenDisabled(t *testing.T) {
	srv := testServer(t) // idle disabled
	a, b := net.Pipe()
	defer func() { _ = a.Close(); _ = b.Close() }()

	sess := newSession(srv, a)
	done := make(chan struct{})
	go func() { sess.run(context.Background()); close(done) }()

	select {
	case <-done:
		t.Fatal("a silent client was disconnected with reaping disabled")
	case <-time.After(400 * time.Millisecond):
		// Still connected, as intended.
	}
}

// Arming the reaper can itself fail — on a socket that is already closed.
// The session must return rather than proceeding to read with no deadline.
func TestSessionRunReturnsWhenArmFails(t *testing.T) {
	srv := testServer(t)
	srv.SetSessionIdleTimeout(time.Second)

	a, b := net.Pipe()
	_ = b.Close()
	_ = a.Close() // SetReadDeadline on a closed pipe errors

	sess := newSession(srv, a)
	done := make(chan struct{})
	go func() { sess.run(context.Background()); close(done) }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("session.run did not return when the deadline could not be armed")
	}
}
