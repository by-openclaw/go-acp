package acp1

import (
	"context"
	"net"
	"testing"
	"time"
)

// TestServeTCP_AcceptWarn injects an accept hook that returns a transient
// (non-closed) error once, then net.ErrClosed to exit — driving the accept
// warn arm.
func TestServeTCP_AcceptWarn(t *testing.T) {
	s := newTestServer(t)
	calls := 0
	s.acceptTCPHook = func(*net.TCPListener) (*net.TCPConn, error) {
		calls++
		if calls == 1 {
			return nil, errAcceptSynthetic // non-closed → warn + continue
		}
		return nil, net.ErrClosed // → return nil
	}
	addr := freeAddr(t)
	_ = s.ServeTCP(contextBg(), addr)
	if calls < 2 {
		t.Fatalf("accept hook calls = %d, want >= 2", calls)
	}
}

// TestServeAN2_AcceptWarn mirrors the accept-warn for AN2.
func TestServeAN2_AcceptWarn(t *testing.T) {
	s := newTestServer(t)
	calls := 0
	s.acceptTCPHook = func(*net.TCPListener) (*net.TCPConn, error) {
		calls++
		if calls == 1 {
			return nil, errAcceptSynthetic
		}
		return nil, net.ErrClosed
	}
	addr := freeAddr(t)
	_ = s.ServeAN2(contextBg(), addr)
	if calls < 2 {
		t.Fatalf("accept hook calls = %d, want >= 2", calls)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	a := ln.Addr().String()
	_ = ln.Close()
	return a
}

func contextBg() context.Context { return context.Background() }

var errAcceptSynthetic = &acceptErr{}

type acceptErr struct{}

func (*acceptErr) Error() string { return "synthetic accept failure" }

// TestServeTCP_ListenError: binding an address already held (no reuse) makes
// ServeTCP's ListenTCP fail.
func TestServeTCP_ListenError(t *testing.T) {
	held, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	defer func() { _ = held.Close() }()
	s := newTestServer(t)
	if err := s.ServeTCP(context.Background(), held.Addr().String()); err == nil {
		t.Error("address in use: want listen error")
	}
}

// TestServeAN2_ListenError mirrors the listen-failure case for AN2.
func TestServeAN2_ListenError(t *testing.T) {
	held, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	defer func() { _ = held.Close() }()
	s := newTestServer(t)
	if err := s.ServeAN2(context.Background(), held.Addr().String()); err == nil {
		t.Error("address in use: want listen error")
	}
}

// TestServeTCP_PerIPCapOverWire opens more than tcpMaxSessionsPerIP
// connections from one IP so ServeTCP's per-ip refusal arm fires.
func TestServeTCP_PerIPCapOverWire(t *testing.T) {
	s := newTestServer(t)
	addr, _ := startTCPServer(t, s)
	var conns []net.Conn
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	// Open cap+2 connections; the server caps live sessions per IP at 32.
	for i := 0; i < tcpMaxSessionsPerIP+2; i++ {
		c, err := net.DialTimeout("tcp4", addr, 2*time.Second)
		if err != nil {
			continue
		}
		conns = append(conns, c)
		time.Sleep(2 * time.Millisecond) // let the server register each
	}
	time.Sleep(100 * time.Millisecond) // let the cap-exceeded path log
}

// TestServeAN2_PerIPCapOverWire mirrors the cap test for the AN2 server.
func TestServeAN2_PerIPCapOverWire(t *testing.T) {
	s := newTestServer(t)
	addr := startAN2Server(t, s)
	var conns []net.Conn
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for i := 0; i < tcpMaxSessionsPerIP+2; i++ {
		c, err := net.DialTimeout("tcp4", addr, 2*time.Second)
		if err != nil {
			continue
		}
		conns = append(conns, c)
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
}
