package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/snell-rollcall/codec"
)

// TestOneMessageInFlight is the rule the whole protocol rests on: a request
// occupies the channel until it is answered, and nothing else may be sent
// meanwhile (spec 4).
//
// A peer matches replies head-of-queue, so two requests on the wire at once do
// not merely arrive out of order, they are answered to the wrong caller.
func TestOneMessageInFlight(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	firstDone := make(chan codec.Frame, 1)
	go func() {
		f, err := s.Do(context.Background(), codec.MsgGetFStat, []byte{0, 1})
		if err != nil {
			t.Errorf("first request: %v", err)
		}
		firstDone <- f
	}()
	first := h.peer.recvType(codec.MsgGetFStat)

	// A second request must not reach the wire while the first is unanswered.
	secondDone := make(chan codec.Frame, 1)
	go func() {
		f, err := s.Do(context.Background(), codec.MsgGetID, nil)
		if err != nil {
			t.Errorf("second request: %v", err)
		}
		secondDone <- f
	}()
	h.peer.quiet()

	select {
	case <-secondDone:
		t.Fatal("the second request completed while the first was in flight")
	default:
	}

	// Answering the first releases the slot, and only then does the second go
	// out.
	h.peer.send(first, codec.MsgRetFStat, nil)
	if f := <-firstDone; f.Type != codec.MsgRetFStat {
		t.Errorf("first reply = %s", f.Type)
	}

	second := h.peer.recvType(codec.MsgGetID)
	h.peer.send(second, codec.MsgRetID, nil)
	if f := <-secondDone; f.Type != codec.MsgRetID {
		t.Errorf("second reply = %s", f.Type)
	}
}

// TestReplyTimeout covers the three-second active-message deadline, driven by
// advancing the clock rather than by waiting.
func TestReplyTimeout(t *testing.T) {
	h := newHarness(t, Config{ReplyTimeout: 3 * time.Second})
	s := h.callSession(t, codec.SvcControl)

	errCh := make(chan error, 1)
	go func() {
		_, err := s.Do(context.Background(), codec.MsgGetStat, nil)
		errCh <- err
	}()
	h.peer.recvType(codec.MsgGetStat) // deliberately unanswered

	h.fire(t, 1, 3*time.Second)

	err := <-errCh
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	// The message that timed out is named, because "timeout" alone in a log
	// says nothing about what was lost.
	if got := err.Error(); !contains(got, "GETSTAT") {
		t.Errorf("err = %q, want it to name the request", got)
	}

	// The slot is released, so the session is still usable.
	go func() { _, _ = s.Do(context.Background(), codec.MsgGetID, nil) }()
	h.peer.recvType(codec.MsgGetID)
}

// TestWaitExtendsTheDeadline covers the message the vendor library never
// implements. A server that needs longer says so, and the deadline restarts.
// Answering Wait with a Nack, as the vendor does, abandons the operation and
// leaves the peer's session behind.
func TestWaitExtendsTheDeadline(t *testing.T) {
	h := newHarness(t, Config{ReplyTimeout: 3 * time.Second})
	s := h.callSession(t, codec.SvcControl)

	type result struct {
		f   codec.Frame
		err error
	}
	done := make(chan result, 1)
	go func() {
		f, err := s.Do(context.Background(), codec.MsgGetFunc, nil)
		done <- result{f, err}
	}()
	req := h.peer.recvType(codec.MsgGetFunc)

	// Two seconds in, the peer asks for thirty more.
	h.fire(t, 1, 2*time.Second)

	wait, err := codec.Wait{Seconds: 30}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	h.peer.send(req, codec.MsgWait, wait)
	h.barrier(t)

	// The original deadline would have expired here. The extension means it
	// does not.
	h.fire(t, 1, 5*time.Second)

	select {
	case r := <-done:
		t.Fatalf("the request ended early: %+v", r)
	case <-time.After(50 * time.Millisecond):
	}

	h.peer.send(req, codec.MsgRetFunc, nil)
	r := <-done
	if r.err != nil {
		t.Fatalf("Do: %v", r.err)
	}
	if r.f.Type != codec.MsgRetFunc {
		t.Errorf("reply = %s, want RETFUNC", r.f.Type)
	}
}

// TestWaitWithoutATimeRestartsTheDefault covers a peer that sends Wait with no
// duration, or one whose payload will not decode. It still means "I am working
// on it", so the deadline restarts rather than the message being discarded.
func TestWaitWithoutATimeRestartsTheDefault(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{"zero seconds", mustWait(t, 0)},
		{"truncated payload", []byte{0x00}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, Config{ReplyTimeout: 3 * time.Second})
			s := h.callSession(t, codec.SvcControl)

			errCh := make(chan error, 1)
			go func() {
				_, err := s.Do(context.Background(), codec.MsgGetStat, nil)
				errCh <- err
			}()
			req := h.peer.recvType(codec.MsgGetStat)

			h.fire(t, 1, 2*time.Second)
			h.peer.send(req, codec.MsgWait, tc.payload)
			h.barrier(t)

			// The deadline restarted, so two more seconds is not enough.
			h.fire(t, 1, 2*time.Second)
			select {
			case err := <-errCh:
				t.Fatalf("the deadline did not restart: %v", err)
			case <-time.After(50 * time.Millisecond):
			}

			// A further three seconds does expire it.
			h.fire(t, 1, 3*time.Second)
			if err := <-errCh; !errors.Is(err, ErrTimeout) {
				t.Errorf("err = %v, want ErrTimeout", err)
			}
		})
	}
}

func mustWait(t *testing.T, secs uint16) []byte {
	t.Helper()
	b, err := codec.Wait{Seconds: secs}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	return b
}

// TestStrikesEndTheSession pins the five-failure rule. The vendor then drops
// the session without sending Term, which is how units run out of sessions;
// this reports it so the caller can reconnect deliberately.
func TestStrikesEndTheSession(t *testing.T) {
	h := newHarness(t, Config{ReplyTimeout: time.Second, MaxStrikes: 3})
	s := h.callSession(t, codec.SvcControl)

	for i := range 3 {
		errCh := make(chan error, 1)
		go func() {
			_, err := s.Do(context.Background(), codec.MsgGetStat, nil)
			errCh <- err
		}()
		h.peer.recvType(codec.MsgGetStat)
		h.fire(t, 1, time.Second)

		err := <-errCh
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("attempt %d: err = %v, want ErrTimeout", i+1, err)
		}
	}

	// The third failure reached the limit, so the channel is finished.
	_, err := s.Do(context.Background(), codec.MsgGetStat, nil)
	if !errors.Is(err, ErrSessionDead) {
		t.Fatalf("err = %v, want ErrSessionDead", err)
	}
	h.peer.quiet()
}

// TestStrikesResetOnSuccess covers the "consecutive" in consecutive failures.
// A session that fails once an hour and works in between is healthy, and
// counting failures without resetting would kill it after five hours.
func TestStrikesResetOnSuccess(t *testing.T) {
	h := newHarness(t, Config{ReplyTimeout: time.Second, MaxStrikes: 2})
	s := h.callSession(t, codec.SvcControl)

	// One failure.
	errCh := make(chan error, 1)
	go func() {
		_, err := s.Do(context.Background(), codec.MsgGetStat, nil)
		errCh <- err
	}()
	h.peer.recvType(codec.MsgGetStat)
	h.fire(t, 1, time.Second)
	if err := <-errCh; !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}

	// Then a success, which clears the count.
	go func() { _, _ = s.Do(context.Background(), codec.MsgGetID, nil) }()
	req := h.peer.recvType(codec.MsgGetID)
	h.peer.send(req, codec.MsgRetID, nil)

	// One more failure must not be the second strike.
	go func() {
		_, err := s.Do(context.Background(), codec.MsgGetStat, nil)
		errCh <- err
	}()
	h.peer.recvType(codec.MsgGetStat)
	h.fire(t, 1, time.Second)
	if err := <-errCh; !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}

	// Still alive.
	go func() { _, _ = s.Do(context.Background(), codec.MsgGetID, nil) }()
	h.peer.recvType(codec.MsgGetID)
}

// TestCancellationReleasesTheSlot covers a caller giving up. The slot has to
// come back, or the session is wedged for every later request.
func TestCancellationReleasesTheSlot(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := s.Do(ctx, codec.MsgGetStat, nil)
		errCh <- err
	}()
	h.peer.recvType(codec.MsgGetStat)

	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}

	go func() { _, _ = s.Do(context.Background(), codec.MsgGetID, nil) }()
	h.peer.recvType(codec.MsgGetID)
}

// TestCancellationWhileQueued covers a caller giving up before it ever reaches
// the wire, which is the common case when a burst is cancelled.
func TestCancellationWhileQueued(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	go func() { _, _ = s.Do(context.Background(), codec.MsgGetFStat, []byte{0, 1}) }()
	h.peer.recvType(codec.MsgGetFStat)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := s.Do(ctx, codec.MsgGetID, nil)
		errCh <- err
	}()

	// It is queued behind the first request, so nothing is on the wire.
	h.peer.quiet()
	cancel()

	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	h.peer.quiet()
}

// TestUnexpectedRepliesDoNotStall covers a peer sending more replies than it
// was asked for. They must not block the read loop, because that would stop
// every other session on the link.
func TestUnexpectedRepliesDoNotStall(t *testing.T) {
	h := newHarness(t, Config{})
	s := h.callSession(t, codec.SvcControl)

	done := make(chan struct{})
	go func() {
		_, _ = s.Do(context.Background(), codec.MsgGetStat, nil)
		close(done)
	}()
	req := h.peer.recvType(codec.MsgGetStat)

	// Ten replies to one request. The first completes it; the rest have
	// nowhere to go and must be dropped rather than block anything.
	for range 10 {
		h.peer.send(req, codec.MsgRetStat, make([]byte, 4))
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the request never completed")
	}

	// The link is still working.
	go func() { _, _ = s.Do(context.Background(), codec.MsgGetID, nil) }()
	h.peer.recvType(codec.MsgGetID)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestChannel_Closed covers the shutdown handshake directly, because the
// interesting part is what happens to callers already waiting for the slot.
//
// The token is passed on rather than the channel closed, so each waiter in
// turn wakes, learns the reason, and hands the token to the next. Nobody is
// left blocked and nobody sees a different error from anybody else.
func TestChannel_Closed(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	c := newChannel("test", clk, time.Second, 5)

	// One caller holds the slot.
	held, err := c.acquire(context.Background(), codec.MsgGetStat)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !c.busy() {
		t.Error("the channel should report itself busy")
	}

	// Two more queue behind it.
	waiting := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := c.acquire(context.Background(), codec.MsgGetID)
			waiting <- err
		}()
	}

	boom := errors.New("link went away")
	c.closeWith(boom)

	for i := range 2 {
		select {
		case err := <-waiting:
			if !errors.Is(err, boom) {
				t.Errorf("waiter %d got %v, want the close reason", i, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("waiter %d was left blocked after the channel closed", i)
		}
	}

	// The caller that held the slot is woken through its frame buffer.
	if _, err := c.await(context.Background(), held); !errors.Is(err, boom) {
		t.Errorf("the holder got %v, want the close reason", err)
	}

	// A caller arriving afterwards gets the same answer rather than blocking.
	if _, err := c.acquire(context.Background(), codec.MsgGetID); !errors.Is(err, boom) {
		t.Errorf("a late caller got %v, want the close reason", err)
	}

	// Closing twice keeps the first reason: it is the one that explains what
	// happened, and the second is a consequence of it.
	c.closeWith(errors.New("something else"))
	if !errors.Is(c.closedErr(), boom) {
		t.Errorf("the reason changed to %v", c.closedErr())
	}
}

// TestChannel_DeliverRefusesAFullBuffer covers a peer sending more than it was
// asked for. The buffer is bounded, and a full one must be refused rather than
// block: the caller is the link's read loop, and stalling it would stop every
// session on the link.
func TestChannel_DeliverRefusesAFullBuffer(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	c := newChannel("test", clk, time.Second, 5)

	if c.deliver(codec.Frame{Type: codec.MsgAck}) {
		t.Error("an idle channel has nothing to deliver to")
	}

	if _, err := c.acquire(context.Background(), codec.MsgGetStat); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Nothing is reading, so the buffer fills.
	n := 0
	for c.deliver(codec.Frame{Type: codec.MsgWait}) {
		n++
		if n > 100 {
			t.Fatal("the buffer is unbounded")
		}
	}
	if n == 0 {
		t.Fatal("nothing could be delivered at all")
	}
	t.Logf("buffered %d frames before refusing", n)

	// And a closed channel accepts nothing.
	c.closeWith(errors.New("done"))
	if c.deliver(codec.Frame{Type: codec.MsgAck}) {
		t.Error("a closed channel accepted a delivery")
	}
}

// TestWaitDuration covers the extension the peer asks for, and the two ways it
// can decline to say: a zero time, and a payload that will not decode. Both
// still mean "I am working on it".
func TestWaitDuration(t *testing.T) {
	const fallback = 3 * time.Second

	thirty, err := codec.Wait{Seconds: 30}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}
	one, err := codec.Wait{Seconds: 1}.AppendTo(nil)
	if err != nil {
		t.Fatalf("AppendTo: %v", err)
	}

	tests := []struct {
		name string
		in   []byte
		want time.Duration
	}{
		{"thirty seconds", thirty, 30 * time.Second},
		{"one second", one, time.Second},
		{"zero seconds", mustWait(t, 0), fallback},
		{"truncated", []byte{0x00}, fallback},
		{"empty", nil, fallback},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := waitDuration(codec.Frame{Type: codec.MsgWait, Payload: tc.in}, fallback)
			if got != tc.want {
				t.Errorf("= %s, want %s", got, tc.want)
			}
		})
	}
}
