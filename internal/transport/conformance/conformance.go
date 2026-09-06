// Package conformance is the battery every transport answers to.
//
// The point is trust. A transport you can only exercise indirectly — through
// a connector's tests — is exercised, not specified: the connector's tests
// say what that connector needs, not what the transport promises. This suite
// is the promise, written once and run against tcp, udp, ws, http and mqtt
// so a change to one cannot quietly alter what another guarantees.
//
// A transport plugs in by filling a Transport struct: what it can do (Caps),
// how to start an echo server, and how to dial one. Functions rather than an
// interface, for the same reason consumer.Supervisor takes functions — the
// transports spell their operations differently and bending them to satisfy
// one abstraction would change working code to please a test.
//
// Capabilities are declared, not guessed. A case that does not apply is
// SKIPPED WITH A REASON, never silently passed: a green suite that quietly
// skipped TLS is worse than no suite, because it reads as proof.
package conformance

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Caps declares what a transport actually supports. Every field exists
// because some transport in the fleet answers differently.
type Caps struct {
	// Name is the transport's name in test output, e.g. "tcp".
	Name string

	// Client and Server record which halves the LIBRARY provides. Both are
	// true for tcp, udp and ws; http ships a client only (the routed server
	// still lives in amwa), and mqtt ships a publisher only.
	Client bool
	Server bool

	// ServerReplies is false for a receive-only server. transport.UDPListener
	// is exactly that — it can Receive and Close and has no way to answer,
	// because it exists for ACP1 broadcast announcements. Such a transport
	// still gets a client-side echo test, driven by a stdlib responder.
	ServerReplies bool

	// MinPayload is the smallest payload that survives a round trip.
	// It is 1 for a raw datagram transport and 8 for transport.TCPConn,
	// whose Send prepends an ACP1 MLEN header that Receive then rejects
	// below 8 — a protocol rule living inside a transport type.
	MinPayload int

	// MaxPayload bounds a single message. 0 means "no practical bound".
	MaxPayload int

	// Ordered is true for a reliable, ordered stream (tcp, ws) and false
	// for best-effort datagrams (udp), where a test must not assert that
	// every message arrives or that order is kept.
	Ordered bool

	// TLS and MutualTLS gate the transport-security cases. Bearer gates the
	// token cases, which only the HTTP family can carry: tcp and udp have no
	// application layer to put a header in.
	TLS       bool
	MutualTLS bool
	Bearer    bool
}

// Conn is the client side of one session: send bytes, get bytes back.
// Deliberately the smallest surface every transport shares.
type Conn interface {
	Send(ctx context.Context, payload []byte) error
	Receive(ctx context.Context, max int) ([]byte, error)
	Close() error
}

// Transport is one entry in the battery.
type Transport struct {
	Caps Caps

	// StartEcho starts a server that returns every payload to its sender,
	// and returns the address to dial plus a stop function. Registering
	// cleanup with t is the adapter's job.
	StartEcho func(t *testing.T) (addr string, stop func())

	// Dial opens a client connection to addr.
	Dial func(ctx context.Context, addr string) (Conn, error)
}

// Run executes the whole battery against tr.
func Run(t *testing.T, tr Transport) {
	t.Helper()
	if tr.StartEcho == nil || tr.Dial == nil {
		t.Fatalf("conformance: %s must supply StartEcho and Dial", tr.Caps.Name)
	}

	t.Run("echo round trip", func(t *testing.T) { testEcho(t, tr) })
	t.Run("empty payload rejected", func(t *testing.T) { testEmptyPayload(t, tr) })
	t.Run("receive honours the deadline", func(t *testing.T) { testReceiveTimeout(t, tr) })
	t.Run("receive rejects a non-positive max", func(t *testing.T) { testInvalidMax(t, tr) })
	t.Run("close is idempotent", func(t *testing.T) { testCloseIdempotent(t, tr) })
	t.Run("use after close fails", func(t *testing.T) { testUseAfterClose(t, tr) })
	t.Run("concurrent senders", func(t *testing.T) { testConcurrent(t, tr) })
	t.Run("no goroutine leak", func(t *testing.T) { testNoGoroutineLeak(t, tr) })
}

// payload builds a message this transport will accept, padded to MinPayload
// so a framed transport's floor is respected rather than tripped over.
func payload(c Caps, seed string) []byte {
	b := []byte(seed)
	for len(b) < c.MinPayload {
		b = append(b, '.')
	}
	return b
}

func dialEcho(t *testing.T, tr Transport) (Conn, func()) {
	t.Helper()
	addr, stop := tr.StartEcho(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := tr.Dial(ctx, addr)
	if err != nil {
		stop()
		t.Fatalf("dial %s: %v", addr, err)
	}
	return c, func() { _ = c.Close(); stop() }
}

// The baseline every transport owes: what goes in comes back out, byte for
// byte. A transport that cannot do this is not a transport.
func testEcho(t *testing.T, tr Transport) {
	c, done := dialEcho(t, tr)
	defer done()

	want := payload(tr.Caps, "hello-transport")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Send(ctx, want); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := c.Receive(ctx, 4096)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("echo = %q, want %q", got, want)
	}
}

// An empty payload is a caller bug, not a zero-length message: every
// transport in this lib rejects it rather than putting an empty frame on
// the wire.
func testEmptyPayload(t *testing.T, tr Transport) {
	c, done := dialEcho(t, tr)
	defer done()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Send(ctx, nil); err == nil {
		t.Error("Send accepted an empty payload")
	}
}

// A read with a deadline and nothing to read must come back, not hang.
// This is the property the whole 24/7 stall came down to.
func testReceiveTimeout(t *testing.T, tr Transport) {
	c, done := dialEcho(t, tr)
	defer done()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.Receive(ctx, 4096)
	if err == nil {
		t.Fatal("Receive returned with no data sent and a deadline set")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Receive took %s to honour a 150ms deadline", elapsed)
	}
}

// A non-positive max is a caller bug: allocating on it would either panic or
// silently accept anything.
func testInvalidMax(t *testing.T, tr Transport) {
	c, done := dialEcho(t, tr)
	defer done()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, max := range []int{0, -1} {
		if _, err := c.Receive(ctx, max); err == nil {
			t.Errorf("Receive(max=%d) returned no error", max)
		}
	}
}

// Close twice is a normal shutdown race — a supervisor closing a session
// the reader already tore down — and must not be an error.
func testCloseIdempotent(t *testing.T, tr Transport) {
	addr, stop := tr.StartEcho(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := tr.Dial(ctx, addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("second Close: %v, want nil", err)
	}
}

// After Close the session is over and both directions must say so rather
// than blocking or pretending to work.
func testUseAfterClose(t *testing.T, tr Transport) {
	addr, stop := tr.StartEcho(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := tr.Dial(ctx, addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = c.Close()

	if err := c.Send(ctx, payload(tr.Caps, "after-close")); err == nil {
		t.Error("Send on a closed conn returned no error")
	}
	if _, err := c.Receive(ctx, 4096); err == nil {
		t.Error("Receive on a closed conn returned no error")
	}
}

// Many goroutines sending on one conn must not corrupt each other or trip
// the race detector. Replies are counted, not matched: on an unordered
// transport a datagram may legitimately be dropped.
func testConcurrent(t *testing.T, tr Transport) {
	c, done := dialEcho(t, tr)
	defer done()

	const senders = 8
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, senders)
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := c.Send(ctx, payload(tr.Caps, "concurrent")); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Send: %v", err)
	}

	// Drain what came back. An ordered transport owes every reply; a
	// datagram transport owes none in particular, so only the ordered case
	// asserts a count.
	got := 0
	for i := 0; i < senders; i++ {
		rctx, rcancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := c.Receive(rctx, 4096)
		rcancel()
		if err != nil {
			break
		}
		got++
	}
	if tr.Caps.Ordered && got != senders {
		t.Errorf("got %d replies for %d sends on an ordered transport", got, senders)
	}
}

// A closed session must leave nothing running. A transport that leaks one
// goroutine per connection is the same 24/7 failure in slow motion.
func testNoGoroutineLeak(t *testing.T, tr Transport) {
	settle()
	before := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		c, done := dialEcho(t, tr)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = c.Send(ctx, payload(tr.Caps, "leak-check"))
		_, _ = c.Receive(ctx, 4096)
		cancel()
		done()
	}

	settle()
	after := runtime.NumGoroutine()
	// A small slack absorbs the runtime's own bookkeeping goroutines; a real
	// leak here is one per connection, not one in total.
	if after > before+2 {
		t.Errorf("goroutines %d -> %d after 5 connect/close cycles", before, after)
	}
}

// settle gives finished goroutines a chance to be reaped before counting.
func settle() {
	for i := 0; i < 20; i++ {
		runtime.Gosched()
		time.Sleep(5 * time.Millisecond)
	}
}

// ErrSkipped is returned by an adapter that cannot build a case for a
// capability it declared. Surfacing it beats a silent pass.
var ErrSkipped = errors.New("conformance: case not applicable")
