package transport

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// The seam is only worth having if the stdlib type satisfies it with no
// adapter — that is what makes injecting it a no-op for existing callers.
func TestNetDialerSatisfiesDialer(t *testing.T) {
	var _ Dialer = &net.Dialer{}
	var _ Dialer = TCPDialer{}
}

func TestTCPDialerDialsAndAppliesOptions(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	d := TCPDialer{
		Timeout: 5 * time.Second,
		Options: SocketOptions{KeepalivePeriod: 5 * time.Second, NoDelay: true},
	}
	conn, err := d.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, ok := conn.(*net.TCPConn); !ok {
		t.Fatalf("DialContext returned %T, want *net.TCPConn", conn)
	}
}

func TestTCPDialerReportsDialFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing is listening now

	_, err = TCPDialer{}.DialContext(context.Background(), "tcp", addr)
	if err == nil {
		t.Fatal("DialContext to a closed port returned nil")
	}
}

// A failed setsockopt must not fail the dial, mirroring Accept.
func TestTCPDialerSurvivesOptionFailure(t *testing.T) {
	orig := applySocketOptions
	defer func() { applySocketOptions = orig }()
	applySocketOptions = func(net.Conn, SocketOptions) error {
		return errors.New("boom")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	conn, err := TCPDialer{}.DialContext(context.Background(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialContext failed after an option failure: %v", err)
	}
	_ = conn.Close()
}
