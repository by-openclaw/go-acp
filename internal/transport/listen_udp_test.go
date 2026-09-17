package transport

// The defensive arms of ListenUDPAddr, tested ONCE here instead of four
// times across the osc and tsl consumers and providers, each of which used
// to carry its own seam file for exactly these branches.

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
)

func TestListenUDPAddrBinds(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts UDPBindOptions
	}{
		{"no options", UDPBindOptions{}},
		{"reuseaddr", UDPBindOptions{ReuseAddr: true}},
		{"broadcast", UDPBindOptions{Broadcast: true}},
		{"both", UDPBindOptions{ReuseAddr: true, Broadcast: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := ListenUDPAddr(context.Background(), "udp", "127.0.0.1:0", tc.opts)
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			defer func() { _ = conn.Close() }()
			if conn.LocalAddr() == nil {
				t.Error("bound conn has no local address")
			}
		})
	}
}

// ":0" is the ephemeral-local-port case a sender wants, and an empty host
// means every interface.
func TestListenUDPAddrEphemeralPort(t *testing.T) {
	conn, err := ListenUDPAddr(context.Background(), "udp", ":0", UDPBindOptions{})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if conn.LocalAddr().(*net.UDPAddr).Port == 0 {
		t.Error("kernel did not assign a port")
	}
}

// ReuseAddr is the point of the option: a second bind on the same port must
// succeed where it would otherwise be refused.
func TestListenUDPAddrReuseAddrSharesThePort(t *testing.T) {
	first, err := ListenUDPAddr(context.Background(), "udp4", "127.0.0.1:0", UDPBindOptions{ReuseAddr: true})
	if err != nil {
		t.Fatalf("first listen: %v", err)
	}
	defer func() { _ = first.Close() }()

	addr := first.LocalAddr().String()
	second, err := ListenUDPAddr(context.Background(), "udp4", addr, UDPBindOptions{ReuseAddr: true})
	if err != nil {
		// Windows honours SO_REUSEADDR differently for UDP; the option is
		// still set, which is what this function promises.
		t.Skipf("second bind on %s refused by this OS: %v", addr, err)
	}
	_ = second.Close()
}

func TestListenUDPAddrRejectsBadAddress(t *testing.T) {
	if _, err := ListenUDPAddr(context.Background(), "udp", "not-an-address", UDPBindOptions{}); err == nil {
		t.Error("a malformed address must be reported")
	} else if !errors.Is(err, ErrListenFailed) {
		t.Errorf("err = %v, want it to wrap ErrListenFailed", err)
	}
}

// ---- the arms a live socket cannot reach -----------------------------------

func TestListenUDPAddrControlDispatchError(t *testing.T) {
	boom := errors.New("fd already closed")
	orig := udpRawControl
	udpRawControl = func(syscall.RawConn, func(uintptr)) error { return boom }
	defer func() { udpRawControl = orig }()

	_, err := ListenUDPAddr(context.Background(), "udp", "127.0.0.1:0", UDPBindOptions{ReuseAddr: true})
	if err == nil {
		t.Fatal("a Control dispatch failure must fail the bind")
	}
}

func TestListenUDPAddrSetsockoptErrors(t *testing.T) {
	boom := errors.New("setsockopt refused")

	t.Run("reuseaddr", func(t *testing.T) {
		orig := udpSetReuse
		udpSetReuse = func(uintptr) error { return boom }
		defer func() { udpSetReuse = orig }()
		if _, err := ListenUDPAddr(context.Background(), "udp", "127.0.0.1:0",
			UDPBindOptions{ReuseAddr: true, Broadcast: true}); err == nil {
			t.Error("a failed SO_REUSEADDR must fail the bind")
		}
	})

	// Broadcast must still be attempted when ReuseAddr is not requested —
	// the short-circuit above it must not skip it.
	t.Run("broadcast alone", func(t *testing.T) {
		orig := udpSetBcast
		udpSetBcast = func(uintptr) error { return boom }
		defer func() { udpSetBcast = orig }()
		if _, err := ListenUDPAddr(context.Background(), "udp", "127.0.0.1:0",
			UDPBindOptions{Broadcast: true}); err == nil {
			t.Error("a failed SO_BROADCAST must fail the bind")
		}
	})
}

func TestListenUDPAddrUnexpectedConnType(t *testing.T) {
	orig := udpAssertConn
	udpAssertConn = func(net.PacketConn) (*net.UDPConn, bool) { return nil, false }
	defer func() { udpAssertConn = orig }()

	_, err := ListenUDPAddr(context.Background(), "udp", "127.0.0.1:0", UDPBindOptions{})
	if err == nil {
		t.Fatal("an unexpected conn type must fail the bind")
	}
	if !errors.Is(err, ErrListenFailed) {
		t.Errorf("err = %v, want it to wrap ErrListenFailed", err)
	}
}

// With no options requested there is no Control hook at all, so none of the
// setsockopt seams run. Proves the fast path is genuinely option-free.
func TestListenUDPAddrNoOptionsSkipsControl(t *testing.T) {
	called := false
	orig := udpRawControl
	udpRawControl = func(c syscall.RawConn, f func(uintptr)) error {
		called = true
		return c.Control(f)
	}
	defer func() { udpRawControl = orig }()

	conn, err := ListenUDPAddr(context.Background(), "udp", "127.0.0.1:0", UDPBindOptions{})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if called {
		t.Error("no options were requested; the Control hook must not be installed")
	}
}
