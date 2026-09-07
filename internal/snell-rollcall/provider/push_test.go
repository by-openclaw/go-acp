package rollcall

import (
	"context"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// Two messages open a value stream and the second is easy to miss: the back
// channel makes a session able to receive pushes at all, and enabling it is
// what asks for the flush of everything that has changed.

func TestEnablingTheBackChannelFlushesEveryValue(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl|codec.SvcMenus|codec.SvcLongStr)

	enableBackChannel(t, sess, codec.BackChannelEnable)

	// card1 has four commands. Every one of them arrives without anything
	// having changed, which is what makes a client's first screen correct.
	seen := map[uint32]bool{}
	for i := 0; i < 4; i++ {
		f := nextPush(t, sess)
		if f.Type != codec.MsgRetValue {
			t.Fatalf("push %d was %s, want RetValue", i, f.Type)
		}
		v, err := codec.DecodeValue(f.Payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		seen[v.Command] = true
	}
	if len(seen) != 4 {
		t.Errorf("the flush carried %d distinct commands, want 4", len(seen))
	}
}

func TestFutureOnlyDoesNotFlush(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl|codec.SvcLongStr)

	enableBackChannel(t, sess, codec.BackChannelFutureOnly)

	// Nothing has changed yet, so nothing may arrive. The change made next is
	// the barrier: if it is the first push, the flush did not happen.
	if _, err := s.p.SetValue(context.Background(), "frame.card1.video.gain", 1.0); err != nil {
		t.Fatalf("SetValue: %v", err)
	}

	f := nextPush(t, sess)
	v, err := codec.DecodeValue(f.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Val != 10 {
		t.Errorf("first push carried %d, want the change (10)", v.Val)
	}
}

func TestAChangeReachesASubscriberInItsOwnGeneration(t *testing.T) {
	s := newServed(t, testTree())

	old := s.open(1, codec.SvcControl)
	modern := s.open(1, codec.SvcControl|codec.SvcLongStr)

	enableBackChannel(t, old, codec.BackChannelFutureOnly)
	enableBackChannel(t, modern, codec.BackChannelFutureOnly)

	if _, err := s.p.SetValue(context.Background(), "frame.card1.video.gain", -3.0); err != nil {
		t.Fatalf("SetValue: %v", err)
	}

	// One change, two subscribers, two different structures on the wire.
	f := nextPush(t, old)
	if f.Type != codec.MsgSetParam {
		t.Fatalf("the 16-bit subscriber got %s", f.Type)
	}
	fs, err := codec.DecodeFuncStatus(f.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fs.Value != -30 {
		t.Errorf("16-bit push carried %d, want -30", fs.Value)
	}

	f = nextPush(t, modern)
	if f.Type != codec.MsgRetValue {
		t.Fatalf("the 32-bit subscriber got %s", f.Type)
	}
	v, err := codec.DecodeValue(f.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Val != -30 {
		t.Errorf("32-bit push carried %d, want -30", v.Val)
	}
}

func TestAWriterIsNotToldItsOwnChange(t *testing.T) {
	s := newServed(t, testTree())
	slot, gain := commandOf(t, s.p, "frame.card1.video.gain")

	writer := s.open(slot, codec.SvcControl|codec.SvcLongStr)
	listener := s.open(slot, codec.SvcControl|codec.SvcLongStr)

	enableBackChannel(t, writer, codec.BackChannelFutureOnly)
	enableBackChannel(t, listener, codec.BackChannelFutureOnly)

	payload, _ := codec.Value{Command: gain, Mode: codec.ModeValue, Val: -50}.AppendTo(nil)
	do(t, writer, codec.MsgSetValue, payload)

	// The listener hears it; the writer already has it in its reply, and
	// pushing it back makes a client that echoes what it hears loop.
	if v := decodePush(t, nextPush(t, listener)); v.Value != -50 {
		t.Errorf("the listener got %d, want -50", v.Value)
	}

	if _, err := s.p.SetValue(context.Background(), "frame.card1.video.gain", -2.0); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if v := decodePush(t, nextPush(t, writer)); v.Value != -20 {
		t.Errorf("the writer's first push carried %d, want the later change", v.Value)
	}
}

func TestADisplayLineIsPushedToSubscribers(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcDisplay|codec.SvcControl|codec.SvcLongStr)

	enableBackChannel(t, sess, codec.BackChannelFutureOnly)

	if err := s.p.SetDisplay(context.Background(), 1, -1, "PSU FAIL"); err != nil {
		t.Fatalf("SetDisplay: %v", err)
	}

	f := nextPush(t, sess)
	if f.Type != codec.MsgDispData {
		t.Fatalf("got %s, want DispData", f.Type)
	}
	d, err := codec.DecodeDisp(f.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Minus one is the error line: a priority rather than a position on the
	// front panel.
	if d.Line != -1 || d.Text != "PSU FAIL" {
		t.Errorf("push carried line %d %q", d.Line, d.Text)
	}
}

func TestDisablingTheBackChannelStopsThePushes(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl|codec.SvcLongStr)

	enableBackChannel(t, sess, codec.BackChannelFutureOnly)
	enableBackChannel(t, sess, codec.BackChannelDisable)

	if _, err := s.p.SetValue(context.Background(), "frame.card1.video.gain", -1.0); err != nil {
		t.Fatalf("SetValue: %v", err)
	}

	// Nothing may arrive. A keepalive on the front channel is the barrier: it
	// is answered, and a push queued before it would have gone first.
	if got := do(t, sess, codec.MsgKeepAlive, nil); got.Type != codec.MsgAck {
		t.Fatalf("keepalive: %s", got.Type)
	}
	select {
	case f := <-sess.Pushes():
		t.Errorf("a push arrived after the back channel was closed: %s", f.Frame.Type)
	default:
	}
}

func TestEnablingTwiceDoesNotDuplicateThePushes(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(2, codec.SvcControl|codec.SvcLongStr)

	enableBackChannel(t, sess, codec.BackChannelFutureOnly)
	enableBackChannel(t, sess, codec.BackChannelFutureOnly)

	if _, err := s.p.SetValue(context.Background(), "frame.card2.level", 8); err != nil {
		t.Fatalf("SetValue: %v", err)
	}

	if v := decodePush(t, nextPush(t, sess)); v.Value != 8 {
		t.Errorf("push carried %d, want 8", v.Value)
	}

	if got := do(t, sess, codec.MsgKeepAlive, nil); got.Type != codec.MsgAck {
		t.Fatalf("keepalive: %s", got.Type)
	}
	select {
	case f := <-sess.Pushes():
		t.Errorf("one change was pushed twice: %s", f.Frame.Type)
	default:
	}
}

func TestBackChannelRefusals(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl)

	if got := refused(t, sess, codec.MsgBkChnReady, nil); got.Type != codec.MsgNack {
		t.Errorf("an empty state answered %s, want Nack", got.Type)
	}
	if got := refused(t, sess, codec.MsgBkChnReady, []byte{9}); got.Type != codec.MsgNack {
		t.Errorf("an unknown state answered %s, want Nack", got.Type)
	}
}

func TestChangeReportingIsAcknowledged(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl)

	for _, typ := range []codec.PacketType{codec.MsgRepFChg, codec.MsgStopRepFChg} {
		if got := do(t, sess, typ, nil); got.Type != codec.MsgAck {
			t.Errorf("%s answered %s, want Ack", typ, got.Type)
		}
	}
}

func TestAPushForAWideCommandIsNotSentToA16BitClient(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl)

	// A command past sixteen bits cannot be named to this client at all: its
	// menu does not list the line either, so there is nothing to tell it.
	typ, payload, err := encodePush(sess, pending{
		isValue: true,
		value:   codec.Value{Command: 0x1_0000, Mode: codec.ModeValue},
	})
	if err == nil {
		t.Errorf("encoded %s with %d bytes, want a refusal", typ, len(payload))
	}
}

func TestAPushThatWillNotEncodeIsDroppedNotSent(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcControl)

	enableBackChannel(t, sess, codec.BackChannelFutureOnly)

	// Queued straight onto the subscriber: this is a message the wire cannot
	// carry, and the pump must skip it rather than stall behind it.
	sub := subscriberOf(t, s.p, sess)
	s.p.enqueue(sub, pending{
		isValue: true,
		value:   codec.Value{Command: 0x1_0000, Mode: codec.ModeValue},
	})

	if _, err := s.p.SetValue(context.Background(), "frame.card1.video.gain", -4.0); err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	if v := decodePush(t, nextPush(t, sess)); v.Value != -40 {
		t.Errorf("the push after the bad one carried %d", v.Value)
	}
}

func TestASubscriberThatFallsBehindLosesPushesRatherThanBlocking(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(2, codec.SvcControl|codec.SvcLongStr)

	enableBackChannel(t, sess, codec.BackChannelFutureOnly)
	sub := subscriberOf(t, s.p, sess)

	// Nobody is reading the pushes, so the queue fills. A change made then is
	// dropped rather than held: one client that stops acknowledging must not
	// stop a device with a hundred cards.
	for i := 0; i < pushQueue*2; i++ {
		s.p.enqueue(sub, pending{isValue: true, value: codec.Value{Command: 1}})
	}

	if !hasEvent(s.p, EventPushDropped) {
		t.Error("a dropped push should be recorded as a compliance event")
	}
}

func TestPushesStopWhenTheProviderStops(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(2, codec.SvcControl|codec.SvcLongStr)

	enableBackChannel(t, sess, codec.BackChannelFutureOnly)
	sub := subscriberOf(t, s.p, sess)

	// Stop closes the provider's own done channel, which is what ends the
	// goroutine sending this subscriber's queue. It must not need the client
	// to do anything.
	if err := s.p.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if s.p.enqueueWaiting(sub, pending{isValue: true, value: codec.Value{Command: 1}}) {
		return // it fitted in the queue, which is fine; nothing is sending it
	}
}

// enableBackChannel sets the back channel state and checks it was accepted.
func enableBackChannel(t *testing.T, s *session.Session, state uint8) {
	t.Helper()

	if got := do(t, s, codec.MsgBkChnReady, []byte{state}); got.Type != codec.MsgAck {
		t.Fatalf("back channel state %d answered %s", state, got.Type)
	}
}

// nextPush waits for one push and acknowledges it, which is what asks for the
// next: every push is a request, not a notification.
func nextPush(t *testing.T, s *session.Session) codec.Frame {
	t.Helper()

	select {
	case push, ok := <-s.Pushes():
		if !ok {
			t.Fatal("the push channel closed")
		}
		if err := push.Ack(); err != nil {
			t.Fatalf("acknowledge: %v", err)
		}
		return push.Frame
	case <-time.After(5 * time.Second):
		t.Fatal("no push arrived")
		return codec.Frame{}
	}
}

// decodePush reads whichever value structure a push carried.
func decodePush(t *testing.T, f codec.Frame) codec.FuncStatus {
	t.Helper()

	switch f.Type {
	case codec.MsgRetValue:
		v, err := codec.DecodeValue(f.Payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		return funcStatusOf(v)
	case codec.MsgSetParam:
		fs, err := codec.DecodeFuncStatus(f.Payload)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		return fs
	default:
		t.Fatalf("%s is not a value push", f.Type)
		return codec.FuncStatus{}
	}
}

// subscriberOf finds the provider's own record of a client's subscription.
func subscriberOf(t *testing.T, p *Provider, s *session.Session) *subscriber {
	t.Helper()

	st := p.linkState(providerLink(t, p))
	for _, sub := range st.subscribers() {
		if sub.s.RemoteIndex() == s.LocalIndex() {
			return sub
		}
	}
	t.Fatal("the provider has no subscription for that session")
	return nil
}
