package acp2

// The acp2 half-open regression.
//
// keepAliveWatchdog deliberately does not close the socket, on the documented
// assumption that "a real socket break is detected by the read loop
// independently". That is only true once the read loop has a deadline: a
// blackholed peer (NAT/firewall drop with no RST) produces no read error at
// all, so before the idle deadline the reader parked in the kernel forever and
// the warm-restart reconnect never fired.

import (
	"net"
	"testing"
	"time"
)

// A peer that accepts the connection and then says nothing must be detected,
// and the session's done channel must close so reconnect.go can act.
func TestSessionIdleDeadlineClosesSilentPeer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		accepted <- c // hold it open and stay completely silent
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	s := NewSession(nil, testLogger())
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	s.SetIdleTimeout(150 * time.Millisecond)

	go s.readLoop(conn)

	select {
	case <-s.done:
		// readLoop exited: the silence was detected.
	case <-time.After(10 * time.Second):
		t.Fatal("readLoop never returned on a silent peer — the reader is hung, " +
			"which is the half-open bug the deadline exists to close")
	}

	select {
	case c := <-accepted:
		_ = c.Close()
	default:
	}
}

// A zero/negative timeout disables the deadline (opt-out must really opt out).
func TestSetIdleTimeoutDisable(t *testing.T) {
	s := NewSession(nil, testLogger())
	s.SetIdleTimeout(5 * time.Second)
	if got := s.IdleTimeout(); got != 5*time.Second {
		t.Fatalf("IdleTimeout = %v, want 5s", got)
	}
	s.SetIdleTimeout(0)
	if got := s.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v after disable, want 0", got)
	}
	s.SetIdleTimeout(-1)
	if got := s.IdleTimeout(); got != 0 {
		t.Fatalf("IdleTimeout = %v for negative input, want 0", got)
	}
}

// SetIdleTimeout on a session with no connection must not panic — it is
// called from startKeepAlive, which can run before/after a socket exists.
func TestSetIdleTimeoutNilConnSafe(t *testing.T) {
	s := NewSession(nil, testLogger())
	s.SetIdleTimeout(time.Second)
}
