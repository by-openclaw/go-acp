package dnssd

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// openLoopbackConn binds a throwaway unicast UDP socket on 127.0.0.1:0.
// Handed back from a faked listenMulticastUDP so openMulticastConns can
// run its per-interface success block without joining the real
// 224.0.0.251 multicast group (which unit tests must avoid).
func openLoopbackConn(t *testing.T) *net.UDPConn {
	t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	return c
}

// ipv4Addrs / ipv6Addrs are canned interfaceAddrs replies: one with an
// IPv4 IPNet (hasIPv4 -> true), one with only IPv6 (hasIPv4 -> false).
func ipv4Addrs() ([]net.Addr, error) {
	return []net.Addr{&net.IPNet{IP: net.IPv4(127, 0, 0, 1), Mask: net.CIDRMask(8, 32)}}, nil
}

func ipv6Addrs() ([]net.Addr, error) {
	return []net.Addr{&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)}}, nil
}

// TestOpenMulticastConns_InterfacesError drives the netInterfaces seam to
// force the enumerate-interfaces error return.
func TestOpenMulticastConns_InterfacesError(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()
	netInterfaces = func() ([]net.Interface, error) { return nil, errors.New("boom") }
	if _, err := openMulticastConns(nil); err == nil {
		t.Fatal("openMulticastConns should surface the netInterfaces error")
	}
}

// TestOpenMulticastConns_PerInterface drives every per-interface arm of
// the loop through the netInterfaces / interfaceAddrs / listenMulticastUDP
// seams: a filtered interface (loopback flag), a non-IPv4 interface, a
// bind failure, a bind that succeeds but whose multicast-loopback setopt
// fails (closed conn), and a fully successful interface. The result has
// at least one conn so the len(conns)==0 fallback is skipped.
func TestOpenMulticastConns_PerInterface(t *testing.T) {
	origIf, origAddrs, origListen := netInterfaces, interfaceAddrs, listenMulticastUDP
	defer func() {
		netInterfaces, interfaceAddrs, listenMulticastUDP = origIf, origAddrs, origListen
	}()

	netInterfaces = func() ([]net.Interface, error) {
		return []net.Interface{
			{Index: 1, Name: "skip", Flags: net.FlagUp | net.FlagMulticast | net.FlagLoopback},
			{Index: 2, Name: "noipv4", Flags: net.FlagUp | net.FlagMulticast},
			{Index: 3, Name: "bindfail", Flags: net.FlagUp | net.FlagMulticast},
			{Index: 4, Name: "looperr", Flags: net.FlagUp | net.FlagMulticast},
			{Index: 5, Name: "good", Flags: net.FlagUp | net.FlagMulticast},
		}, nil
	}
	interfaceAddrs = func(ifi net.Interface) ([]net.Addr, error) {
		if ifi.Name == "noipv4" {
			return ipv6Addrs()
		}
		return ipv4Addrs()
	}
	// A pre-closed conn so setMulticastLoopback fails for "looperr".
	closed := openLoopbackConn(t)
	_ = closed.Close()
	listenMulticastUDP = func(network string, ifi *net.Interface, gaddr *net.UDPAddr) (*net.UDPConn, error) {
		switch ifi.Name {
		case "bindfail":
			return nil, errors.New("bind refused")
		case "looperr":
			return closed, nil
		default:
			return openLoopbackConn(t), nil
		}
	}

	conns, err := openMulticastConns(discardLogger())
	if err != nil {
		t.Fatalf("openMulticastConns: %v", err)
	}
	if len(conns) != 2 { // looperr (closed) + good
		t.Fatalf("want 2 conns, got %d", len(conns))
	}
	_ = closeConns(conns)
}

// TestOpenMulticastConns_FallbackSuccess drives the len(conns)==0
// nil-interface fallback: an empty interface list means no per-interface
// conn is bound, so the fallback listenMulticastUDP(nil) branch runs.
func TestOpenMulticastConns_FallbackSuccess(t *testing.T) {
	origIf, origListen := netInterfaces, listenMulticastUDP
	defer func() { netInterfaces, listenMulticastUDP = origIf, origListen }()

	netInterfaces = func() ([]net.Interface, error) { return nil, nil }
	listenMulticastUDP = func(network string, ifi *net.Interface, gaddr *net.UDPAddr) (*net.UDPConn, error) {
		return openLoopbackConn(t), nil
	}

	conns, err := openMulticastConns(discardLogger())
	if err != nil {
		t.Fatalf("openMulticastConns fallback: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("want 1 fallback conn, got %d", len(conns))
	}
	_ = closeConns(conns)
}

// TestOpenMulticastConns_FallbackError drives the fallback's error return:
// no usable interface and the nil-interface bind fails too.
func TestOpenMulticastConns_FallbackError(t *testing.T) {
	origIf, origListen := netInterfaces, listenMulticastUDP
	defer func() { netInterfaces, listenMulticastUDP = origIf, origListen }()

	netInterfaces = func() ([]net.Interface, error) { return nil, nil }
	listenMulticastUDP = func(network string, ifi *net.Interface, gaddr *net.UDPAddr) (*net.UDPConn, error) {
		return nil, errors.New("no multicast")
	}
	if _, err := openMulticastConns(discardLogger()); err == nil {
		t.Fatal("openMulticastConns should surface the fallback bind error")
	}
}

// TestHasIPv4_AddrsErrorSeam forces hasIPv4's Addrs() error return
// deterministically through the interfaceAddrs seam.
func TestHasIPv4_AddrsErrorSeam(t *testing.T) {
	orig := interfaceAddrs
	defer func() { interfaceAddrs = orig }()
	interfaceAddrs = func(ifi net.Interface) ([]net.Addr, error) {
		return nil, errors.New("addrs failed")
	}
	if hasIPv4(&net.Interface{Name: "x"}) {
		t.Error("hasIPv4 should be false when interfaceAddrs errors")
	}
}

// withFallbackNet points netInterfaces at an empty list and
// listenMulticastUDP at loopback sockets so the constructors' stdlib path
// succeeds without touching the real multicast group. Returns a restore.
func withFallbackNet(t *testing.T) func() {
	t.Helper()
	origIf, origListen := netInterfaces, listenMulticastUDP
	netInterfaces = func() ([]net.Interface, error) { return nil, nil }
	listenMulticastUDP = func(network string, ifi *net.Interface, gaddr *net.UDPAddr) (*net.UDPConn, error) {
		return openLoopbackConn(t), nil
	}
	return func() { netInterfaces, listenMulticastUDP = origIf, origListen }
}

// TestConstructors_Success drives newStdlibBrowser / newStdlibResponder /
// NewBrowser / NewResponder down the successful stdlib path, including the
// logger==nil default arm of NewBrowser / NewResponder.
func TestConstructors_Success(t *testing.T) {
	defer withFallbackNet(t)()

	b, err := newStdlibBrowser(discardLogger())
	if err != nil {
		t.Fatalf("newStdlibBrowser: %v", err)
	}
	_ = b.Close()

	r, err := newStdlibResponder(discardLogger())
	if err != nil {
		t.Fatalf("newStdlibResponder: %v", err)
	}
	_ = r.Close()

	nb, err := NewBrowser(discardLogger())
	if err != nil {
		t.Fatalf("NewBrowser: %v", err)
	}
	_ = nb.Close()

	nbNil, err := NewBrowser(nil) // logger==nil -> slog.Default()
	if err != nil {
		t.Fatalf("NewBrowser(nil): %v", err)
	}
	_ = nbNil.Close()

	nr, err := NewResponder(discardLogger())
	if err != nil {
		t.Fatalf("NewResponder: %v", err)
	}
	_ = nr.Close()

	nrNil, err := NewResponder(nil) // logger==nil -> slog.Default()
	if err != nil {
		t.Fatalf("NewResponder(nil): %v", err)
	}
	_ = nrNil.Close()
}

// TestConstructors_Error drives the openMulticastConns error return up
// through every constructor.
func TestConstructors_Error(t *testing.T) {
	orig := netInterfaces
	defer func() { netInterfaces = orig }()
	netInterfaces = func() ([]net.Interface, error) { return nil, errors.New("boom") }

	// This is about the stdlib path, so say so: on a host that actually
	// runs avahi-daemon the constructors take the daemon path and never
	// reach openMulticastConns at all. GitHub's runners have no avahi
	// and stayed quiet about it; the Linux tools host does, and did not.
	noDaemon(t)

	if _, err := newStdlibBrowser(discardLogger()); err == nil {
		t.Error("newStdlibBrowser should error")
	}
	if _, err := newStdlibResponder(discardLogger()); err == nil {
		t.Error("newStdlibResponder should error")
	}
	if _, err := NewBrowser(discardLogger()); err == nil {
		t.Error("NewBrowser should error")
	}
	if _, err := NewResponder(discardLogger()); err == nil {
		t.Error("NewResponder should error")
	}
}

// TestSendQueries_TickerFires covers the `case <-t.C: send()` re-arm arm
// by shrinking QueryInterval to ~1 ms so the ticker fires at least once
// before the context is cancelled. QueryInterval is restored via defer.
func TestSendQueries_TickerFires(t *testing.T) {
	orig := QueryInterval
	QueryInterval = time.Millisecond
	defer func() { QueryInterval = orig }()

	b := &stdlibBrowser{logger: discardLogger(), conns: []*net.UDPConn{localUDPConn(t)}}
	defer func() { _ = closeConns(b.conns) }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		b.sendQueries(ctx, "_nmos-register._tcp")
		close(done)
	}()
	// Let several 1 ms ticks fire so the ticker case runs send() again.
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendQueries did not return after ticker test")
	}
}
