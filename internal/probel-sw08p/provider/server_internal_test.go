package probelsw08p

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/probel-sw08p/codec"
)

// TestNewServerNilLogger: passing a nil logger falls back to the default
// without panicking, and the server is fully initialised.
func TestNewServerNilLogger(t *testing.T) {
	srv := newServer(nil, emptyExport())
	if srv.logger == nil {
		t.Fatal("logger not defaulted")
	}
	if srv.metrics == nil || srv.profile == nil || srv.tree == nil {
		t.Fatal("server not fully initialised")
	}
}

// TestServeListenError: an invalid bind address makes Serve return a
// wrapped listen error rather than nil.
func TestServeListenError(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	err := srv.Serve(context.Background(), "256.256.256.256:99999")
	if err == nil {
		t.Fatal("Serve(bad addr): want error, got nil")
	}
}

// TestServeReturnsNilOnContextCancel: a clean ctx cancel collapses
// net.ErrClosed / context.Canceled into a nil return (the success arm of
// Serve's final errors.Is check, line 116).
func TestServeReturnsNilOnContextCancel(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, "127.0.0.1:0") }()
	_ = waitBound(t, srv)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve after cancel = %v; want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
}

// TestStopClosesListenerAndSessions: Stop closes the listener, drops
// active sessions, and is idempotent on a second call.
func TestStopClosesListenerAndSessions(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ctx, "127.0.0.1:0") }()
	addr := waitBound(t, srv)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Wait until the accept loop registered the session.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		n := len(srv.sessions)
		srv.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Idempotent — second Stop is a no-op nil.
	if err := srv.Stop(); err != nil {
		t.Errorf("second Stop = %v; want nil", err)
	}

	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after Stop")
	}
}

// TestStopNoListener: Stop before Serve has bound (listener nil) returns
// nil and still flips closed.
func TestStopNoListener(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	if err := srv.Stop(); err != nil {
		t.Errorf("Stop with no listener = %v; want nil", err)
	}
}

// TestSetValueErrors covers SetValue's three failure arms: bad path,
// uncoercible value, and a tree-rejected connect.
func TestSetValueErrors(t *testing.T) {
	exp := demoMatrixExport(16, 16)
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), exp)
	ctx := context.Background()

	if _, err := srv.SetValue(ctx, "not-a-path", 1); err == nil {
		t.Error("bad path: want error")
	}
	if _, err := srv.SetValue(ctx, "0.0.1", "not-a-number"); err == nil {
		t.Error("uncoercible value: want error")
	}
	if _, err := srv.SetValue(ctx, "0.0.999", 1); err == nil {
		t.Error("out-of-range dst: want error")
	}
	// Happy path returns the source map.
	got, err := srv.SetValue(ctx, "0.0.1", 5)
	if err != nil {
		t.Fatalf("valid SetValue: %v", err)
	}
	m, ok := got.(map[string]uint16)
	if !ok || m["src"] != 5 {
		t.Errorf("SetValue result = %#v; want map src=5", got)
	}
}

// TestParseCrosspointPath covers the parse-error and out-of-range arms.
func TestParseCrosspointPath(t *testing.T) {
	if _, _, _, err := parseCrosspointPath("garbage"); err == nil {
		t.Error("garbage path: want error")
	}
	if _, _, _, err := parseCrosspointPath("999.0.0"); err == nil {
		t.Error("matrix out of range: want error")
	}
	if _, _, _, err := parseCrosspointPath("0.999.0"); err == nil {
		t.Error("level out of range: want error")
	}
	if _, _, _, err := parseCrosspointPath("0.0.99999999"); err == nil {
		t.Error("dst out of range: want error")
	}
	m, l, d, err := parseCrosspointPath("3.2.1000")
	if err != nil {
		t.Fatalf("valid path: %v", err)
	}
	if m != 3 || l != 2 || d != 1000 {
		t.Errorf("got (%d,%d,%d); want (3,2,1000)", m, l, d)
	}
}

// TestCoerceSource covers every accepted numeric type plus the
// unsupported-type error.
func TestCoerceSource(t *testing.T) {
	cases := []struct {
		in   any
		want uint16
	}{
		{int(1), 1},
		{int32(2), 2},
		{int64(3), 3},
		{uint16(4), 4},
		{uint32(5), 5},
		{uint64(6), 6},
		{float64(7), 7},
	}
	for _, tc := range cases {
		got, err := coerceSource(tc.in)
		if err != nil {
			t.Errorf("coerceSource(%T): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("coerceSource(%T) = %d; want %d", tc.in, got, tc.want)
		}
	}
	if _, err := coerceSource("nope"); err == nil {
		t.Error("string: want coerce error")
	}
}

// TestFanOutTallyWriteFailNotes: a session whose write fails (closed
// conn) causes fanOutTally to log + note TallyBroadcastFailed and keep
// going. Uses a fake session with a closed net.Pipe end.
func TestFanOutTallyWriteFail(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())

	c1, c2 := net.Pipe()
	_ = c2.Close()
	sess := newSession(srv, c1)
	sess.closed = true // force write() to return net.ErrClosed
	srv.mu.Lock()
	srv.sessions[sess] = struct{}{}
	srv.mu.Unlock()

	before := srv.profile.Snapshot()[TallyBroadcastFailed]
	srv.fanOutTally(nil, codec.EncodeCrosspointTally(codec.CrosspointTallyParams{
		MatrixID: 0, LevelID: 0, DestinationID: 1, SourceID: 2,
	}))
	after := srv.profile.Snapshot()[TallyBroadcastFailed]
	if after != before+1 {
		t.Errorf("TallyBroadcastFailed %d -> %d; want +1", before, after)
	}
	_ = c1.Close()
}

// TestFanOutTallySkipsOrigin: the originating session is excluded from
// the fan-out (the `sess == origin` continue arm).
func TestFanOutTallySkipsOrigin(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()
	origin := newSession(srv, c1)
	srv.mu.Lock()
	srv.sessions[origin] = struct{}{}
	srv.mu.Unlock()
	// With only the origin registered, fan-out must write nothing — if it
	// tried to write to c1 with no reader this would block; the test
	// completing proves the origin was skipped.
	done := make(chan struct{})
	go func() {
		srv.fanOutTally(origin, codec.EncodeCrosspointTally(codec.CrosspointTallyParams{}))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fanOutTally blocked; origin was not skipped")
	}
}

// TestFanOutTallyDebugLog: with a Debug logger and a real readable peer,
// fanOutTally executes the gated Debug hex-dump branch and writes the
// tally to the non-origin session (server.go ~228).
func TestFanOutTallyDebugLog(t *testing.T) {
	h := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})
	srv := newServer(slog.New(h), emptyExport())

	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()
	// Drain the peer end so the session write doesn't block.
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := c2.Read(buf); err != nil {
				return
			}
		}
	}()
	sess := newSession(srv, c1)
	srv.mu.Lock()
	srv.sessions[sess] = struct{}{}
	srv.mu.Unlock()

	srv.fanOutTally(nil, codec.EncodeCrosspointTally(codec.CrosspointTallyParams{
		MatrixID: 0, LevelID: 0, DestinationID: 1, SourceID: 2,
	}))
}

// errListener is a fake net.Listener whose Accept returns a fixed
// non-shutdown error, used to drive Serve's final error-return arm.
type errListener struct{ err error }

func (e errListener) Accept() (net.Conn, error) { return nil, e.err }
func (e errListener) Close() error              { return nil }
func (e errListener) Addr() net.Addr            { return fakeAddr{} }

// TestServeReturnsAcceptError: when acceptLoop surfaces an error that is
// neither net.ErrClosed nor context.Canceled, Serve returns it verbatim
// (server.go final return). Driven via the listenHook fake-listener seam.
func TestServeReturnsAcceptError(t *testing.T) {
	sentinel := errors.New("probel test: accept exploded")
	listenHook = func(context.Context, string) (net.Listener, error) {
		return errListener{err: sentinel}, nil
	}
	defer func() { listenHook = nil }()

	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // unblock Serve's ctx.Done watcher goroutine
	err := srv.Serve(ctx, "127.0.0.1:0")
	if !errors.Is(err, sentinel) {
		t.Errorf("Serve = %v; want the accept sentinel error", err)
	}
}

// TestServeListenHookError: a listenHook that fails to bind makes Serve
// return the wrapped listen error.
func TestServeListenHookError(t *testing.T) {
	sentinel := errors.New("probel test: bind refused")
	listenHook = func(context.Context, string) (net.Listener, error) {
		return nil, sentinel
	}
	defer func() { listenHook = nil }()

	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	if err := srv.Serve(context.Background(), "x"); !errors.Is(err, sentinel) {
		t.Errorf("Serve = %v; want wrapped bind error", err)
	}
}

// TestAcceptLoopContextCancelled: acceptLoop returns ctx.Err immediately
// when the context is already cancelled (the early ctx.Err arm).
func TestAcceptLoopContextCancelled(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := srv.acceptLoop(ctx, ln); !errors.Is(err, context.Canceled) {
		t.Errorf("acceptLoop = %v; want context.Canceled", err)
	}
}
