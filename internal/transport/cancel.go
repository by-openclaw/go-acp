package transport

// Making a blocked read notice a cancelled context.
//
// Every Receive in this package arms a read DEADLINE from ctx.Deadline().
// That handles a timeout and nothing else: a socket has no idea a context
// exists, so cancelling one leaves the read blocked until the deadline it was
// given — which, for a watcher with a 90 s idle window, means Ctrl-C can take
// 90 seconds to be noticed.
//
// It was visible in acp1: Discover's test cancels after 80 ms and the call
// took the full 10 s window, with the test's own comment claiming it exited
// "via the Receive error". It exits via the deadline, every time.
//
// The fix is the standard one — push the read deadline to now, which makes
// the kernel return immediately — done here once rather than in each
// Receive.

import (
	"context"
	"net"
	"time"
)

// deadlineSetter is the one method this needs; every conn type here has it.
type deadlineSetter interface {
	SetReadDeadline(t time.Time) error
}

// watchCancel unblocks a read on c when ctx is cancelled, and returns a stop
// function the caller MUST defer.
//
// A context with no Done channel (context.Background) costs nothing: no
// goroutine is started.
//
// stop WAITS for the watcher to exit, and that is load-bearing rather than
// tidy. Without it a watcher whose Receive has already returned can still be
// scheduled, wake up, and push the deadline into the past AFTER the next
// Receive armed its own — clobbering a live read and losing a message. It
// showed up immediately: the conformance suite's concurrent-senders case
// dropped one reply in eight. Waiting here means a returned Receive
// guarantees its watcher is dead.
//
// After a genuine cancel the deadline is left in the past, which is correct:
// the caller asked for the read to stop, and the next Receive re-arms its own
// deadline on entry.
func watchCancel(ctx context.Context, c deadlineSetter) (stop func()) {
	done := ctx.Done()
	if done == nil {
		return func() {}
	}
	stopped := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		select {
		case <-done:
			// Any time in the past works; time.Now() is the conventional
			// spelling and avoids the zero value, which CLEARS the deadline.
			_ = c.SetReadDeadline(time.Now())
		case <-stopped:
		}
	}()
	return func() {
		close(stopped)
		<-exited
	}
}

// cancelledReadErr maps the error a cancelled read produces into the
// context's own error, so a caller sees context.Canceled rather than a
// timeout it did not ask for.
//
// Order matters: a genuine deadline that expires at the same moment as a
// cancel should still report the cancel, because that is what the caller
// acted on.
func cancelledReadErr(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return ctx.Err()
		}
	}
	return nil
}
