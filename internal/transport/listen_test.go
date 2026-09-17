package transport

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// liveTCPConn returns an open client-side *net.TCPConn, with the listener
// and both ends closed when the test ends.
func liveTCPConn(t *testing.T) net.Conn {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// A non-TCP connection must be left alone rather than rejected: callers
// apply the options unconditionally, and net.Pipe is what most of the
// loopback tests in this repo run on.
func TestApplyTCPKeepaliveIgnoresNonTCP(t *testing.T) {
	c, other := net.Pipe()
	defer func() { _ = c.Close(); _ = other.Close() }()

	for _, period := range []time.Duration{0, time.Second, DisableKeepalive} {
		if err := ApplyTCPKeepalive(c, period); err != nil {
			t.Errorf("ApplyTCPKeepalive(pipe, %s) = %v, want nil", period, err)
		}
	}
}

func TestApplyTCPKeepaliveOnLiveSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	srv, err := ln.Accept()
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	defer func() { _ = srv.Close() }()

	// Default, explicit and disabled all succeed on a healthy socket.
	for _, period := range []time.Duration{0, 5 * time.Second, DisableKeepalive} {
		if err := ApplyTCPKeepalive(c, period); err != nil {
			t.Errorf("ApplyTCPKeepalive(%s) = %v, want nil", period, err)
		}
	}
}

// On a closed socket every setsockopt fails — the arm that returns the
// typed ErrListenFailed rather than swallowing the error.
func TestApplyTCPKeepaliveOnClosedSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = c.Close()

	for _, period := range []time.Duration{time.Second, DisableKeepalive} {
		err := ApplyTCPKeepalive(c, period)
		if err == nil {
			t.Errorf("ApplyTCPKeepalive(closed, %s) = nil, want an error", period)
			continue
		}
		if !errors.Is(err, ErrListenFailed) {
			t.Errorf("err = %v, want it to wrap ErrListenFailed", err)
		}
	}
}

func TestApplySocketOptionsOnClosedSocket(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = c.Close()

	// Keepalive is the first option applied, so it is the arm that fails.
	if err := ApplySocketOptions(c, SocketOptions{}); !errors.Is(err, ErrListenFailed) {
		t.Errorf("err = %v, want ErrListenFailed", err)
	}
}

// The SetKeepAlivePeriod failure arm, reached through the seam: SetKeepAlive
// succeeds, the period call does not. Unreachable on a real socket — any fd
// state that fails the second also fails the first.
func TestApplyTCPKeepalivePeriodError(t *testing.T) {
	origAlive, origPeriod := tcpSetKeepAlive, tcpSetKeepAlivePeriod
	defer func() { tcpSetKeepAlive, tcpSetKeepAlivePeriod = origAlive, origPeriod }()
	tcpSetKeepAlive = func(*net.TCPConn, bool) error { return nil }
	tcpSetKeepAlivePeriod = func(*net.TCPConn, time.Duration) error {
		return errors.New("boom")
	}

	c := liveTCPConn(t)
	err := ApplyTCPKeepalive(c, time.Second)
	if err == nil {
		t.Fatal("ApplyTCPKeepalive returned nil with a failing SetKeepAlivePeriod")
	}
	if !errors.Is(err, ErrListenFailed) || !strings.Contains(err.Error(), "keepalive period") {
		t.Errorf("err = %v, want ErrListenFailed naming the period", err)
	}
}

// The NoDelay failure arm, reached through the seam. On a real socket it is
// unreachable: any fd state that fails SetNoDelay also fails SetKeepAlive,
// and ApplyTCPKeepalive returns first.
func TestApplySocketOptionsNoDelayError(t *testing.T) {
	orig := tcpSetNoDelay
	defer func() { tcpSetNoDelay = orig }()
	tcpSetNoDelay = func(*net.TCPConn, bool) error { return errors.New("boom") }

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	err = ApplySocketOptions(c, SocketOptions{NoDelay: true})
	if err == nil {
		t.Fatal("ApplySocketOptions returned nil with a failing SetNoDelay")
	}
	if !errors.Is(err, ErrListenFailed) || !strings.Contains(err.Error(), "nodelay") {
		t.Errorf("err = %v, want ErrListenFailed naming nodelay", err)
	}
}

func TestApplySocketOptionsNoDelayOnPipe(t *testing.T) {
	c, other := net.Pipe()
	defer func() { _ = c.Close(); _ = other.Close() }()

	if err := ApplySocketOptions(c, SocketOptions{NoDelay: true}); err != nil {
		t.Errorf("ApplySocketOptions(pipe) = %v, want nil", err)
	}
}

// The point of the type: a caller keeps its own accept loop and still gets
// the socket policy, without writing the setsockopt calls itself.
func TestListenTCPAppliesOptionsOnAccept(t *testing.T) {
	ln, err := ListenTCP(context.Background(), "tcp", "127.0.0.1:0",
		SocketOptions{KeepalivePeriod: 5 * time.Second, NoDelay: true})
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	defer func() { _ = ln.Close() }()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	srv, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer func() { _ = srv.Close() }()
	if _, ok := srv.(*net.TCPConn); !ok {
		t.Fatalf("Accept returned %T, want *net.TCPConn", srv)
	}
}

// A failed setsockopt must not fail the Accept — refusing the session
// because the kernel declined a keepalive tweak would trade a detectable
// problem for an outright outage.
func TestListenTCPAcceptSurvivesOptionFailure(t *testing.T) {
	orig := applySocketOptions
	defer func() { applySocketOptions = orig }()
	applySocketOptions = func(net.Conn, SocketOptions) error {
		return errors.New("boom")
	}

	ln, err := ListenTCP(context.Background(), "tcp", "127.0.0.1:0", SocketOptions{})
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	defer func() { _ = ln.Close() }()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = c.Close() }()

	srv, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept returned an error after an option failure: %v", err)
	}
	_ = srv.Close()
}

func TestListenTCPAcceptPropagatesAcceptError(t *testing.T) {
	ln, err := ListenTCP(context.Background(), "tcp", "127.0.0.1:0", SocketOptions{})
	if err != nil {
		t.Fatalf("ListenTCP: %v", err)
	}
	_ = ln.Close()

	if _, err := ln.Accept(); err == nil {
		t.Fatal("Accept on a closed listener returned nil error")
	}
}

func TestListenTCPBindError(t *testing.T) {
	// Port 1 on a non-local address cannot be bound.
	_, err := ListenTCP(context.Background(), "tcp", "198.51.100.1:1", SocketOptions{})
	if err == nil {
		t.Fatal("ListenTCP succeeded on an unbindable address")
	}
	if !errors.Is(err, ErrListenFailed) {
		t.Errorf("err = %v, want it to wrap ErrListenFailed", err)
	}
}

func TestListenTCPRawGivesTheConcreteListener(t *testing.T) {
	ln, err := ListenTCPRaw(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	// The point of this function: AcceptTCP, which net.Listener does not have.
	if ln.Addr() == nil {
		t.Error("bound listener has no address")
	}
}

func TestListenTCPRawRejectsBadAddress(t *testing.T) {
	if _, err := ListenTCPRaw(context.Background(), "tcp4", "not-an-address"); err == nil {
		t.Error("a malformed address must be reported")
	} else if !errors.Is(err, ErrListenFailed) {
		t.Errorf("err = %v, want it to wrap ErrListenFailed", err)
	}
}

// A "tcp*" network always yields a *net.TCPListener, so this arm needs the
// seam — and it is here, once, rather than in every caller that needs the
// concrete type.
func TestListenTCPRawUnexpectedListenerType(t *testing.T) {
	orig := listenTCPRawAssert
	listenTCPRawAssert = func(net.Listener) (*net.TCPListener, bool) { return nil, false }
	defer func() { listenTCPRawAssert = orig }()

	_, err := ListenTCPRaw(context.Background(), "tcp4", "127.0.0.1:0")
	if err == nil {
		t.Fatal("an unexpected listener type must fail the bind")
	}
	if !errors.Is(err, ErrListenFailed) {
		t.Errorf("err = %v, want it to wrap ErrListenFailed", err)
	}
}
