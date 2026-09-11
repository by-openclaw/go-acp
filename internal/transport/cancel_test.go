package transport

// Idempotency of the cancellation watcher. The conformance battery covers
// what cancellation DOES; these cover the lifecycle of the helper itself,
// which every Receive in the package now depends on.

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// A context that can never be cancelled starts no goroutine, and stopping
// that watcher must still be safe.
func TestWatchCancelNoDoneChannel(t *testing.T) {
	c := liveTCPConn(t)
	stop := watchCancel(context.Background(), c)
	stop()
	stop() // idempotent even in the no-op case
}

// stop() is shaped like context.CancelFunc and must be safe to call more
// than once — an unguarded close would panic inside a transport.
func TestWatchCancelStopIsIdempotent(t *testing.T) {
	c := liveTCPConn(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stop := watchCancel(ctx, c)
	stop()
	stop()
	stop()
}

// Stopping after the cancel already fired is the race every Receive runs:
// the watcher may already be gone by the time the deferred stop lands.
func TestWatchCancelStopAfterFire(t *testing.T) {
	c := liveTCPConn(t)
	ctx, cancel := context.WithCancel(context.Background())

	stop := watchCancel(ctx, c)
	cancel()
	time.Sleep(20 * time.Millisecond) // let the watcher act
	stop()
	stop()
}

func TestCancelledReadErrOnlyClaimsTimeouts(t *testing.T) {
	live := context.Background()
	dead, cancel := context.WithCancel(context.Background())
	cancel()

	timeout := &net.OpError{Err: &timeoutErr{}}
	other := errors.New("connection reset")

	if got := cancelledReadErr(live, timeout); got != nil {
		t.Errorf("a live context reported %v, want nil", got)
	}
	if got := cancelledReadErr(dead, other); got != nil {
		t.Errorf("a non-timeout error was reported as a cancel: %v", got)
	}
	if got := cancelledReadErr(dead, timeout); !errors.Is(got, context.Canceled) {
		t.Errorf("cancelled + timeout = %v, want context.Canceled", got)
	}
}

// timeoutErr is a net.Error that reports Timeout, which is what a read
// deadline pushed into the past produces.
type timeoutErr struct{}

func (*timeoutErr) Error() string { return "i/o timeout" }
func (*timeoutErr) Timeout() bool { return true }
func (*timeoutErr) Temporary() bool {
	return true
}

// pureDeadline returns a context whose Deadline is d from now but whose
// cancellation does not fire within the test.
//
// It isolates the "socket deadline expired, context still live" arm of every
// Receive, which context.WithTimeout cannot: its own timer and the socket's
// read deadline fire at the same instant and race for which arm reports the
// timeout — both spell context.DeadlineExceeded, so the assertion is stable
// but the arm taken is not. Here the socket's deadline is the only clock.
//
// Done is inherited from a long-lived parent rather than nil, so the cancel
// watcher runs exactly as it does in production.
func pureDeadline(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return deadlineOnly{Context: parent, dl: time.Now().Add(d)}
}

type deadlineOnly struct {
	context.Context
	dl time.Time
}

func (d deadlineOnly) Deadline() (time.Time, bool) { return d.dl, true }
