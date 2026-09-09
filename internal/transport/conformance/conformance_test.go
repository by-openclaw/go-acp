package conformance

// The battery run against transports built to misbehave in exactly one
// way each. A suite that passes everything is worse than no suite —
// this package's own argument about silent skips — so every case here
// is shown catching the thing it exists to catch.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------
// the recorder
// ---------------------------------------------------------------

// recorder stands in for *testing.T so a failing case can be observed
// rather than failing the test that drove it.
type recorder struct {
	host *testing.T

	mu     sync.Mutex
	cases  []string
	failed map[string][]string
	name   string
}

// fatal is what a recorder's Fatalf panics with, so a case stops where
// a real one would.
type fatal struct{ msg string }

func newRecorder(t *testing.T) *recorder {
	return &recorder{host: t, failed: map[string][]string{}}
}

func (r *recorder) Helper() {}

func (r *recorder) Error(a ...any) { r.note(fmt.Sprint(a...)) }

func (r *recorder) Errorf(f string, a ...any) { r.note(fmt.Sprintf(f, a...)) }

func (r *recorder) Fatal(a ...any) {
	r.note(fmt.Sprint(a...))
	panic(fatal{fmt.Sprint(a...)})
}

func (r *recorder) Fatalf(f string, a ...any) {
	r.note(fmt.Sprintf(f, a...))
	panic(fatal{fmt.Sprintf(f, a...)})
}

func (r *recorder) hostT() *testing.T { return r.host }

func (r *recorder) note(msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed[r.name] = append(r.failed[r.name], msg)
}

func (r *recorder) run(name string, f func(reporter)) {
	sub := &recorder{host: r.host, failed: r.failed, name: name}
	r.mu.Lock()
	r.cases = append(r.cases, name)
	r.mu.Unlock()
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(fatal); !ok {
				panic(p)
			}
		}
	}()
	f(sub)
}

// failures returns every complaint recorded, flattened.
func (r *recorder) failures() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, msgs := range r.failed {
		out = append(out, msgs...)
	}
	return out
}

// failedCase reports whether the named case complained.
func (r *recorder) failedCase(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.failed[name]) > 0
}

// ---------------------------------------------------------------
// a transport that behaves
// ---------------------------------------------------------------

// echoConn is an in-memory conn that returns what it is given, and can
// be told to misbehave in one specific way.
type echoConn struct {
	mu     sync.Mutex
	queue  [][]byte
	closed bool

	// the ways a transport can be wrong, one per case
	acceptEmpty    bool // Send takes a zero-length payload
	ignoreCancel   bool // Receive waits out the deadline whatever the ctx says
	acceptAnyMax   bool // Receive allocates on a non-positive max
	closeTwice     error
	failFirstClose bool // Close refuses the first time too
	workAfterClose bool
	dropSends      bool // Send succeeds and nothing comes back
	leak           bool // every dial starts a goroutine that outlives it
	corrupt        bool // Receive returns something else
	failSend       bool // Send always refuses
	answerAlways   bool // Receive answers even with nothing sent
	slowDeadline   bool // Receive errors, but long after the deadline
}

func (c *echoConn) Send(ctx context.Context, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed && !c.workAfterClose {
		return errors.New("send on a closed conn")
	}
	if len(payload) == 0 && !c.acceptEmpty {
		return errors.New("empty payload")
	}
	if c.failSend {
		return errors.New("send refused")
	}
	if c.dropSends {
		return nil
	}
	c.queue = append(c.queue, append([]byte(nil), payload...))
	return nil
}

func (c *echoConn) Receive(ctx context.Context, max int) ([]byte, error) {
	if max <= 0 {
		if !c.acceptAnyMax {
			return nil, errors.New("non-positive max")
		}
		// The bug this case exists for: a transport that allocates on
		// a non-positive max answers rather than refusing.
		return make([]byte, 0), nil
	}
	c.mu.Lock()
	if c.closed && !c.workAfterClose {
		c.mu.Unlock()
		return nil, errors.New("receive on a closed conn")
	}
	if len(c.queue) > 0 {
		out := c.queue[0]
		c.queue = c.queue[1:]
		c.mu.Unlock()
		if c.corrupt {
			return []byte("not what was sent"), nil
		}
		return out, nil
	}
	c.mu.Unlock()

	if c.slowDeadline {
		// The bug this case exists for: the deadline is honoured
		// eventually, which for an operator is the same as not at all.
		<-time.After(4 * time.Second)
		return nil, errors.New("deadline, late")
	}
	if c.answerAlways {
		// The bug this case exists for: a transport that answers a
		// read nobody wrote to, so a deadline never expires and a
		// caller reads a message that was never sent.
		return []byte("unsolicited"), nil
	}
	if c.ignoreCancel {
		// The bug this case exists for: a read deadline handles a
		// timeout and leaves a cancelled read blocked until it
		// arrives.
		// Long enough to exceed what the case tolerates, short enough
		// not to dominate the suite.
		<-time.After(6 * time.Second)
		return nil, errors.New("deadline")
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *echoConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return c.closeTwice
	}
	c.closed = true
	if c.failFirstClose {
		return errors.New("close refused")
	}
	return nil
}

// fakeTransport builds a battery entry over echoConn, with the given
// misbehaviour applied to every conn it hands out.
func fakeTransport(break_ func(*echoConn)) Transport {
	return Transport{
		Caps: Caps{Name: "fake", Client: true, Server: true, ServerReplies: true, Ordered: true},
		StartEcho: func(t *testing.T) (string, func()) {
			return "in-memory", func() {}
		},
		Dial: func(ctx context.Context, addr string) (Conn, error) {
			c := &echoConn{}
			if break_ != nil {
				break_(c)
			}
			if c.leak {
				stop := make(chan struct{})
				go func() { <-stop }()
			}
			return c, nil
		},
	}
}

// The battery passes a transport that keeps every promise, and runs
// every case while doing it.
func TestBatteryPassesAConformingTransport(t *testing.T) {
	r := newRecorder(t)
	run(r, fakeTransport(nil))

	if got := r.failures(); len(got) != 0 {
		t.Fatalf("a conforming transport failed: %v", got)
	}
	if len(r.cases) != 9 {
		t.Errorf("cases run = %v, want the whole battery", r.cases)
	}
}

// Each case catches the one thing it exists to catch. A suite whose
// assertions do not fire is a green wall that means nothing.
//
// Only the case under examination is run, not the whole battery: the
// deliberate waits in the others — a deadline that has to expire, a
// drain that has to give up — would otherwise be paid once per
// misbehaviour.
func TestEachCaseCatchesItsOwnFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		run    func(reporter, Transport)
		break_ func(*echoConn)
	}{
		{"a transport that does not echo", testEcho,
			func(c *echoConn) { c.corrupt = true }},
		{"a transport whose Send refuses", testEcho,
			func(c *echoConn) { c.failSend = true }},
		{"a transport that accepts an empty payload", testEmptyPayload,
			func(c *echoConn) { c.acceptEmpty = true }},
		{"a transport that answers a read nobody wrote to", testReceiveTimeout,
			func(c *echoConn) { c.answerAlways = true }},
		{"a transport that honours a deadline far too late", testReceiveTimeout,
			func(c *echoConn) { c.slowDeadline = true }},
		{"a transport that ignores a cancel", testReceiveCancel,
			func(c *echoConn) { c.ignoreCancel = true }},
		{"a transport that answers instead of noticing a cancel", testReceiveCancel,
			func(c *echoConn) { c.answerAlways = true }},
		{"a transport that allocates on a non-positive max", testInvalidMax,
			func(c *echoConn) { c.acceptAnyMax = true }},
		{"a transport whose first Close errors", testCloseIdempotent,
			func(c *echoConn) { c.failFirstClose = true }},
		{"a transport whose second Close errors", testCloseIdempotent,
			func(c *echoConn) { c.closeTwice = errors.New("already closed") }},
		{"a transport that works after close", testUseAfterClose,
			func(c *echoConn) { c.workAfterClose = true }},
		{"an ordered transport that drops replies", testConcurrent,
			func(c *echoConn) { c.dropSends = true }},
		{"a transport whose Send refuses under many senders", testConcurrent,
			func(c *echoConn) { c.failSend = true }},
		{"a transport that leaks a goroutine per connection", testNoGoroutineLeak,
			func(c *echoConn) { c.leak = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRecorder(t)
			r.name = tc.name
			runCase(r, func(rep reporter) { tc.run(rep, fakeTransport(tc.break_)) })
			if !r.failedCase(tc.name) {
				t.Fatalf("the case passed %s", tc.name)
			}
		})
	}
}

// runCase invokes one case and absorbs the panic a recorded Fatalf
// raises, the way the battery's own runner does.
func runCase(r *recorder, f func(reporter)) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(fatal); !ok {
				panic(p)
			}
		}
	}()
	f(r)
}

// The public entry point is what every transport in the fleet calls,
// so it is exercised as such — against one that keeps every promise,
// because a failure here has nowhere to be recorded.
func TestPublicRunAgainstAConformingTransport(t *testing.T) {
	Run(t, fakeTransport(nil))
}

// An adapter that supplies half a transport is a programming error,
// and the battery refuses to run rather than reporting a green suite
// it never executed.
func TestBatteryRefusesAnIncompleteAdapter(t *testing.T) {
	for _, tr := range []Transport{
		{Caps: Caps{Name: "no-dial"}, StartEcho: func(*testing.T) (string, func()) { return "", func() {} }},
		{Caps: Caps{Name: "no-echo"}, Dial: func(context.Context, string) (Conn, error) { return nil, nil }},
	} {
		r := newRecorder(t)
		func() {
			defer func() {
				if p := recover(); p != nil {
					if _, ok := p.(fatal); !ok {
						panic(p)
					}
				}
			}()
			run(r, tr)
		}()
		if got := r.failures(); len(got) == 0 ||
			!strings.Contains(got[0], "StartEcho and Dial") {
			t.Errorf("%s: = %v, want the missing half named", tr.Caps.Name, got)
		}
		if len(r.cases) != 0 {
			t.Errorf("%s ran %v, want nothing", tr.Caps.Name, r.cases)
		}
	}
}

// An adapter whose Dial fails is reported where it happens, rather
// than as a confusing failure in whichever case dialled first.
func TestBatteryReportsADialItCannotMake(t *testing.T) {
	tr := fakeTransport(nil)
	tr.Dial = func(context.Context, string) (Conn, error) {
		return nil, errors.New("no route")
	}

	r := newRecorder(t)
	run(r, tr)

	if got := r.failures(); len(got) == 0 {
		t.Fatal("a transport that cannot be dialled must be reported")
	}
}

// The payload floor is a protocol rule living inside a transport type
// — transport.TCPConn's Send prepends an ACP1 header its Receive then
// rejects below 8 bytes — so the battery pads rather than tripping
// over it.
func TestPayloadRespectsTheFloor(t *testing.T) {
	if got := payload(Caps{MinPayload: 8}, "hi"); len(got) != 8 {
		t.Errorf("= %q (%d bytes), want it padded to the floor", got, len(got))
	}
	if got := payload(Caps{}, "hi"); string(got) != "hi" {
		t.Errorf("= %q, want the seed untouched where there is no floor", got)
	}
	if got := payload(Caps{MinPayload: 2}, "hello"); string(got) != "hello" {
		t.Errorf("= %q, want a seed already over the floor untouched", got)
	}
}

// An unordered transport owes no particular reply, so the concurrent
// case must not fail one for dropping datagrams — asserting delivery
// on UDP would make the battery wrong about the transport rather than
// the other way round.
func TestConcurrentCaseForgivesAnUnorderedTransport(t *testing.T) {
	tr := fakeTransport(func(c *echoConn) { c.dropSends = true })
	tr.Caps.Ordered = false

	r := newRecorder(t)
	run(r, tr)

	if r.failedCase("concurrent senders") {
		t.Errorf("a datagram transport was failed for dropping: %v", r.failures())
	}
}

// The skip sentinel exists so an adapter that cannot build a case for
// a capability it declared says so, rather than passing silently.
func TestErrSkipped(t *testing.T) {
	wrapped := fmt.Errorf("tls case: %w", ErrSkipped)
	if !errors.Is(wrapped, ErrSkipped) {
		t.Error("the sentinel must survive wrapping")
	}
	if !strings.Contains(ErrSkipped.Error(), "not applicable") {
		t.Errorf("= %q", ErrSkipped.Error())
	}
}
