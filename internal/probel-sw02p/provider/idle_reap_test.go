package probelsw02p

// Provider-side reaping of silent clients — symmetric with the consumer fix.
// Without it the provider holds a goroutine and a socket for every client that
// vanished without an RST, accumulating one leak per lost peer.

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func idleTestServer(t *testing.T) *server {
	t.Helper()
	return newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
}

func TestServerSessionIdleTimeoutDefaultsOff(t *testing.T) {
	if got := idleTestServer(t).idleTimeout(); got != 0 {
		t.Fatalf("idleTimeout = %v by default, want 0 — a quiet but healthy "+
			"client must not be disconnected", got)
	}
}

func TestServerSetSessionIdleTimeout(t *testing.T) {
	srv := idleTestServer(t)
	srv.SetSessionIdleTimeout(90 * time.Second)
	if got := srv.idleTimeout(); got != 90*time.Second {
		t.Fatalf("idleTimeout = %v, want 90s", got)
	}
	srv.SetSessionIdleTimeout(-1)
	if got := srv.idleTimeout(); got != -1 {
		t.Fatalf("idleTimeout = %v, want the disable sentinel preserved", got)
	}
}

// Armed: a client that connects and then says nothing is reaped.
func TestSessionReapsSilentClient(t *testing.T) {
	srv := idleTestServer(t)
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

// Disabled (the default): a silent client is NOT disconnected.
func TestSessionKeepsSilentClientWhenDisabled(t *testing.T) {
	srv := idleTestServer(t)
	a, b := net.Pipe()
	defer func() { _ = a.Close(); _ = b.Close() }()

	sess := newSession(srv, a)
	done := make(chan struct{})
	go func() { sess.run(context.Background()); close(done) }()

	select {
	case <-done:
		t.Fatal("a silent client was disconnected with reaping disabled")
	case <-time.After(400 * time.Millisecond):
	}
}

// Arming the reaper can itself fail — on a socket that is already closed.
// The session must return rather than proceeding to read with no deadline.
func TestSessionRunReturnsWhenArmFails(t *testing.T) {
	srv := idleTestServer(t)
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
