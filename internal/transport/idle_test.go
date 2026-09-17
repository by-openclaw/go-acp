package transport

// Tests for the shared idle bound. The race test is the important one: it is
// the bug that six hand-rolled copies each carried, and that turned a CI run
// red on one runner out of six.

import (
	"net"
	"testing"
	"time"
)

func tcpPair(t *testing.T) net.Conn {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestIdleSetGet(t *testing.T) {
	var i Idle
	if i.Get() != 0 || i.Enabled() {
		t.Fatal("zero value must be disabled")
	}
	i.Set(30 * time.Second)
	if got := i.Get(); got != 30*time.Second {
		t.Fatalf("Get = %v, want 30s", got)
	}
	if !i.Enabled() {
		t.Fatal("Enabled must be true once armed")
	}
	i.Set(0)
	if i.Enabled() {
		t.Fatal("Set(0) must disable")
	}
	// Negative disables rather than arming a deadline in the past.
	i.Set(-5 * time.Second)
	if got := i.Get(); got != 0 {
		t.Fatalf("Get = %v after negative Set, want 0", got)
	}
}

func TestIdleArmNilConnIsNoop(t *testing.T) {
	var i Idle
	i.Set(time.Second)
	if err := i.Arm(nil); err != nil {
		t.Fatalf("Arm(nil) = %v, want nil", err)
	}
	if err := i.SetOn(nil, time.Second); err != nil {
		t.Fatalf("SetOn(nil) = %v, want nil", err)
	}
}

// Armed, a silent peer trips the deadline.
func TestIdleArmFiresOnSilentPeer(t *testing.T) {
	c := tcpPair(t)
	var i Idle
	i.Set(100 * time.Millisecond)
	if err := i.Arm(c); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("read returned nil on a silent peer with the bound armed")
	}
}

// Disabled, it clears a deadline a previous setting left behind — otherwise
// "switch it off" would leave the old bound live.
func TestIdleDisableClearsPreviousDeadline(t *testing.T) {
	c := tcpPair(t)
	var i Idle
	if err := i.SetOn(c, 50*time.Millisecond); err != nil {
		t.Fatalf("SetOn: %v", err)
	}
	if err := i.SetOn(c, 0); err != nil { // disable
		t.Fatalf("SetOn(0): %v", err)
	}
	// With the deadline cleared the read blocks; bound it ourselves so the
	// test cannot hang, and assert it did NOT return early from the old 50ms.
	start := time.Now()
	_ = c.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	_, _ = c.Read(make([]byte, 1))
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond {
		t.Fatalf("read returned after %v — the disabled 50ms bound was still live", elapsed)
	}
}

// SetOn reaches a reader that is ALREADY blocked, rather than waiting out the
// previous window.
func TestIdleSetOnReachesBlockedReader(t *testing.T) {
	c := tcpPair(t)
	var i Idle
	// Start with a long bound, as production does.
	if err := i.SetOn(c, time.Hour); err != nil {
		t.Fatalf("SetOn: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := c.Read(make([]byte, 1))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond) // let it block
	if err := i.SetOn(c, 100*time.Millisecond); err != nil {
		t.Fatalf("tighten: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a blocked reader never picked up the tightened bound")
	}
}

// THE regression. Arm and SetOn must be atomic with respect to each other, or
// the reader re-arms with a value it read before the setter stored the new
// one, and blocks for the OLD window while the caller believes it tightened.
func TestIdleArmSetRaceDoesNotClobber(t *testing.T) {
	c := tcpPair(t)
	var i Idle
	i.Set(time.Hour)

	done := make(chan error, 1)
	go func() {
		// Mimic a read loop: arm from the shared bound, then read.
		for n := 0; n < 500; n++ {
			if err := i.Arm(c); err != nil {
				done <- err
				return
			}
			_ = c.SetReadDeadline(timeSoon(i.Get()))
			if _, err := c.Read(make([]byte, 1)); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	for n := 0; n < 200; n++ {
		if err := i.SetOn(c, 50*time.Millisecond); err != nil {
			t.Fatalf("SetOn: %v", err)
		}
		time.Sleep(time.Millisecond)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("reader completed without ever hitting the deadline")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("tightened bound was clobbered by the reader's stale re-arm")
	}
}

// timeSoon mirrors what a caller would compute from Get(); kept tiny so the
// race test above exercises the read-then-arm window realistically.
func timeSoon(d time.Duration) time.Time {
	if d <= 0 {
		return time.Time{}
	}
	return time.Now().Add(d)
}

// A DISABLED bound must leave the connection's deadline alone. The socket is
// not exclusively ours: a caller may have set a deadline for a bounded
// handshake, or a test may have set one in the past to force an immediate
// error. A read loop calls Arm every pass, so clearing there would silently
// undo that — which is exactly the regression an existing provider test
// caught when Arm cleared on disable.
func TestIdleArmDisabledDoesNotClearCallerDeadline(t *testing.T) {
	c := tcpPair(t)
	var i Idle // disabled

	// Caller sets a deadline already in the past: the next read must fail.
	if err := c.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if err := i.Arm(c); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("a disabled Arm cleared the caller's expired deadline — the read blocked instead of failing")
	}
}

// SetOn(c, 0) IS the explicit way to clear, and must still work.
func TestIdleSetOnZeroClearsDeadline(t *testing.T) {
	c := tcpPair(t)
	var i Idle
	if err := c.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	if err := i.SetOn(c, 0); err != nil {
		t.Fatalf("SetOn(0): %v", err)
	}
	// Deadline cleared: bound the read ourselves so the test cannot hang.
	_ = c.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	start := time.Now()
	_, _ = c.Read(make([]byte, 1))
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("SetOn(0) did not clear the expired deadline")
	}
}
