package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// TestKeepalive_ProbesAnIdleLink covers why the probe exists at all.
//
// Measured against the live stack during the audit: an idle connection
// receives exactly zero unsolicited frames over ninety seconds, and the
// session survives that silence intact. So silence carries no information, and
// a quiet link is indistinguishable from a dead one until something is sent.
func TestKeepalive_ProbesAnIdleLink(t *testing.T) {
	h := newHarness(t, Config{KeepaliveInterval: 15 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k := NewKeepalive(ctx, h.link, nil)

	// Nothing is sent until the interval elapses.
	h.peer.quiet()

	h.fire(t, 1, 15*time.Second)

	probe := h.peer.recvType(codec.MsgKeepAlive)
	if len(probe.Payload) != 0 {
		t.Errorf("payload = %x, want none; the probe is cheap by design", probe.Payload)
	}
	if probe.Dst.Index != codec.IndexUnknown {
		t.Errorf("index = %d, want %d before a session exists",
			probe.Dst.Index, codec.IndexUnknown)
	}

	h.peer.send(probe, codec.MsgAck, nil)

	deadline := time.Now().Add(2 * time.Second)
	for k.Misses() != 0 || k.Probes() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("after an answered probe: %d probes, %d misses", k.Probes(), k.Misses())
		}
		time.Sleep(time.Millisecond)
	}
}

// TestKeepalive_ProbesTheSession covers the difference the audit calls out. A
// probe on the unconnected index proves the peer's stack is alive; one on a
// session also proves the session still exists, and the session is what
// actually breaks.
func TestKeepalive_ProbesTheSession(t *testing.T) {
	h := newHarness(t, Config{KeepaliveInterval: 15 * time.Second})
	s := h.callSession(t, codec.SvcControl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	NewKeepalive(ctx, h.link, s)

	h.fire(t, 1, 15*time.Second)

	probe := h.peer.recvType(codec.MsgKeepAlive)
	if probe.Dst.Index != s.RemoteIndex() {
		t.Errorf("probe index = %d, want the session's %d", probe.Dst.Index, s.RemoteIndex())
	}
	if probe.Src.Index != s.LocalIndex() {
		t.Errorf("probe source index = %d, want ours %d", probe.Src.Index, s.LocalIndex())
	}
	h.peer.send(probe, codec.MsgAck, nil)
}

// TestKeepalive_ThreeMissesEndTheLink covers a peer that stops answering.
func TestKeepalive_ThreeMissesEndTheLink(t *testing.T) {
	h := newHarness(t, Config{
		KeepaliveInterval:  15 * time.Second,
		ReplyTimeout:       3 * time.Second,
		MaxKeepaliveMisses: 3,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	NewKeepalive(ctx, h.link, nil)

	for i := range 3 {
		// The ticker fires, a probe goes out, and its own deadline is the
		// second armed timer.
		h.fire(t, 1, 15*time.Second)
		h.peer.recvType(codec.MsgKeepAlive)
		h.fire(t, 2, 3*time.Second)

		if i < 2 {
			select {
			case <-h.link.Done():
				t.Fatalf("the link died after %d misses, want 3", i+1)
			case <-time.After(50 * time.Millisecond):
			}
		}
	}

	select {
	case <-h.link.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("three unanswered probes should have ended the link")
	}
	if !errors.Is(h.link.Err(), ErrLinkDead) {
		t.Errorf("link error = %v, want ErrLinkDead", h.link.Err())
	}
}

// TestKeepalive_MissesResetOnAnAnswer covers a link that drops one probe and
// recovers. Counting misses without resetting would kill a healthy link after
// three lost packets spread over an hour.
func TestKeepalive_MissesResetOnAnAnswer(t *testing.T) {
	h := newHarness(t, Config{
		KeepaliveInterval:  15 * time.Second,
		ReplyTimeout:       3 * time.Second,
		MaxKeepaliveMisses: 2,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k := NewKeepalive(ctx, h.link, nil)

	// One missed probe.
	h.fire(t, 1, 15*time.Second)
	h.peer.recvType(codec.MsgKeepAlive)
	h.fire(t, 2, 3*time.Second)

	// One answered.
	h.fire(t, 1, 15*time.Second)
	probe := h.peer.recvType(codec.MsgKeepAlive)
	h.peer.send(probe, codec.MsgAck, nil)

	deadline := time.Now().Add(2 * time.Second)
	for k.Misses() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("misses = %d after an answered probe, want 0", k.Misses())
		}
		time.Sleep(time.Millisecond)
	}

	select {
	case <-h.link.Done():
		t.Fatal("the link died despite recovering")
	default:
	}
}

// TestKeepalive_IsAnActiveMessage pins the rule that makes the probe safe. It
// occupies the one-in-flight slot like any other request. Injecting it beside
// a pending request would have it matched head-of-queue against that request's
// reply, which is a correctness bug rather than a shortcut.
func TestKeepalive_IsAnActiveMessage(t *testing.T) {
	// The reply timeout is set well above the probe interval so the pending
	// request is still outstanding when the probe comes due. With the
	// production values the request would have timed out first, which would
	// test nothing.
	h := newHarness(t, Config{KeepaliveInterval: time.Second, ReplyTimeout: time.Minute})
	s := h.callSession(t, codec.SvcControl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	NewKeepalive(ctx, h.link, s)

	// A request is in flight.
	go func() { _, _ = s.Do(context.Background(), codec.MsgGetFStat, []byte{0, 1}) }()
	req := h.peer.recvType(codec.MsgGetFStat)

	// The probe comes due, and must wait rather than jump the queue.
	h.fire(t, 2, time.Second)
	h.peer.quiet()

	// Answering the request lets the probe out.
	h.peer.send(req, codec.MsgRetFStat, nil)
	probe := h.peer.recvType(codec.MsgKeepAlive)
	h.peer.send(probe, codec.MsgAck, nil)
}

// TestKeepalive_RefusalCountsAsAMiss covers a peer that answers the probe with
// something other than an acknowledgement. It is alive, but not agreeing, and
// treating that as success would hide a session the peer has already dropped.
func TestKeepalive_RefusalCountsAsAMiss(t *testing.T) {
	h := newHarness(t, Config{KeepaliveInterval: time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k := NewKeepalive(ctx, h.link, nil)

	h.fire(t, 1, time.Second)
	probe := h.peer.recvType(codec.MsgKeepAlive)
	h.peer.send(probe, codec.MsgInvSess, nil)

	deadline := time.Now().Add(2 * time.Second)
	for k.Misses() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("a refused probe was counted as a success")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestKeepalive_Disabled covers a link whose peer pushes continuously, where
// probing is wasted traffic.
func TestKeepalive_Disabled(t *testing.T) {
	h := newHarness(t, Config{KeepaliveInterval: -1})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	k := NewKeepalive(ctx, h.link, nil)

	if h.clk.Waiters() != 0 {
		t.Errorf("%d timers armed, want none when probing is disabled", h.clk.Waiters())
	}
	if k.Probes() != 0 {
		t.Errorf("%d probes sent, want none", k.Probes())
	}
}

// TestKeepalive_StopsWithTheContext covers shutdown. A leaked ticker keeps a
// link alive in the runtime after the caller has finished with it.
func TestKeepalive_StopsWithTheContext(t *testing.T) {
	h := newHarness(t, Config{KeepaliveInterval: 15 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	NewKeepalive(ctx, h.link, nil)
	h.armed(t, 1)

	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for h.clk.Waiters() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d timers still armed after cancellation", h.clk.Waiters())
		}
		time.Sleep(time.Millisecond)
	}
}

// TestKeepalive_StopsWithTheLink covers the other way it ends.
func TestKeepalive_StopsWithTheLink(t *testing.T) {
	h := newHarness(t, Config{KeepaliveInterval: 15 * time.Second})

	NewKeepalive(context.Background(), h.link, nil)
	h.armed(t, 1)

	_ = h.link.Close()

	deadline := time.Now().Add(2 * time.Second)
	for h.clk.Waiters() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d timers still armed after the link closed", h.clk.Waiters())
		}
		time.Sleep(time.Millisecond)
	}
}

// TestKeepalive_ProbeDirectly covers the exported single probe, which a caller
// uses to check a link on demand rather than on a schedule.
func TestKeepalive_ProbeDirectly(t *testing.T) {
	h := newHarness(t, Config{KeepaliveInterval: -1})
	k := NewKeepalive(context.Background(), h.link, nil)

	done := make(chan error, 1)
	go func() { done <- k.Probe(context.Background()) }()

	probe := h.peer.recvType(codec.MsgKeepAlive)
	h.peer.send(probe, codec.MsgAck, nil)

	if err := <-done; err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if k.Probes() != 1 {
		t.Errorf("Probes = %d, want 1", k.Probes())
	}
}
