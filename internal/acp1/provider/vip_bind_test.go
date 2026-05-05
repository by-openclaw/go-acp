package acp1

import (
	"context"
	"net"
	"testing"
	"time"
)

// startServerOnAddr launches s.Serve on the given addr and returns
// when the listener is reachable.
func startServerOnAddr(t *testing.T, s *server, addr string) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- s.Serve(ctx, addr)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("udp4", addr, 50*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Errorf("server did not return within 2s")
		}
	})
	return cancel
}

// TestVIPBind_LoopbackBroadcastSourcePinned verifies the broadcast
// socket's LocalAddr matches the listener's bind IP. We bind to
// 127.0.0.1 (a real loopback IP, distinct from 0.0.0.0) and check
// that s.bcast.LocalAddr() reflects 127.0.0.1.
//
// The "real" VIP scenario (multiple non-loopback IPs on different
// ifaces) is exercised in integration tests on the LXC rig — unit
// tests can only assert the bind logic here.
func TestVIPBind_LoopbackBroadcastSourcePinned(t *testing.T) {
	s := newTestServer(t)

	// Pick a kernel-assigned port to avoid collisions when tests run
	// in parallel.
	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	_ = probe.Close()

	addr := net.JoinHostPort("127.0.0.1", itoa(port))
	startServerOnAddr(t, s, addr)
	time.Sleep(100 * time.Millisecond)

	s.mu.Lock()
	bc := s.bcast
	s.mu.Unlock()
	if bc == nil {
		// Some systems refuse the broadcast dial from a loopback IP
		// (no route for 255.255.255.255). That's documented behaviour;
		// skip rather than fail.
		t.Skip("loopback broadcast dial unsupported on this host — skip")
	}
	local := bc.LocalAddr().(*net.UDPAddr)
	if !local.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("broadcast LocalAddr = %v, want 127.0.0.1", local.IP)
	}
}

func TestVIPBind_UnspecifiedHostLeavesKernelChoice(t *testing.T) {
	// When --bind is not set (or = 0.0.0.0), we deliberately do NOT
	// pin the broadcast source so the kernel's default-route logic
	// applies (preserves legacy behaviour).
	s := newTestServer(t)

	probe, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		t.Fatalf("probe listen: %v", err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	_ = probe.Close()

	addr := net.JoinHostPort("0.0.0.0", itoa(port))
	startServerOnAddr(t, s, addr)
	time.Sleep(100 * time.Millisecond)

	s.mu.Lock()
	bc := s.bcast
	s.mu.Unlock()
	if bc == nil {
		t.Skip("broadcast dial unsupported on this host — skip")
	}
	local := bc.LocalAddr().(*net.UDPAddr)
	// With unspecified bind, the kernel chooses; we can't predict
	// which IP. Just ensure the broadcast socket exists and has a
	// concrete (not zero) source after dial.
	if local.IP.IsUnspecified() {
		t.Fatalf("broadcast source = unspecified, kernel should have picked something")
	}
}

// itoa is a tiny stdlib-free integer-to-string helper for ports.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
