package dnssd

import (
	"context"
	"testing"

	"dhs/internal/amwa/codec/dnssd"
)

// browseSub.deliver and closeOut are the guard against the send-on-closed
// race a shared read loop creates: many readLoops deliver to one sub while
// the ctx-cancel goroutine closes it. This drives all four arms — a normal
// buffered send, the ctx.Done bail-out when the reader has stopped, the
// idempotent close, and a deliver after close — proving deliver never lands
// on a closed channel and closeOut never double-closes.
func TestBrowseSubDeliverAndCloseSerialised(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &browseSub{ctx: ctx, service: "x", out: make(chan dnssd.Instance, 1)}

	// Normal delivery lands in the buffer and is received.
	s.deliver(dnssd.Instance{Name: "a"})
	if got := <-s.out; got.Name != "a" {
		t.Fatalf("deliver dropped the instance: %+v", got)
	}

	// Buffer full + context cancelled: deliver takes the ctx.Done arm rather
	// than blocking the shared read loop on a subscriber that stopped reading.
	s.deliver(dnssd.Instance{Name: "b"}) // fills the one-slot buffer
	cancel()
	s.deliver(dnssd.Instance{Name: "c"}) // would block; ctx is done, so it bails

	// closeOut closes once; a second call is a no-op guard, not a panic.
	s.closeOut()
	s.closeOut()

	// A deliver after close is dropped by the closed flag — never a
	// send-on-closed-channel panic.
	s.deliver(dnssd.Instance{Name: "d"})
}
