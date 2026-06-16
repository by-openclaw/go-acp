package probelsw02p

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/probel-sw02p/codec"
)

// dialServed spins up a server on a loopback listener via serveListener
// and returns a connected client socket plus a cleanup func. The
// returned context cancel + server stop run on cleanup.
func dialServed(t *testing.T) (*server, net.Conn, func()) {
	t.Helper()
	srv := newTestServer(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.serveListener(ctx, ln) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		cancel()
		t.Fatalf("dial: %v", err)
	}
	cleanup := func() {
		_ = conn.Close()
		cancel()
		select {
		case <-serveDone:
		case <-time.After(2 * time.Second):
			t.Error("serveListener did not return after cancel")
		}
	}
	return srv, conn, cleanup
}

// readFrame reads exactly one SW-P-02 frame from conn with a deadline.
func readFrame(t *testing.T, conn net.Conn) (codec.Frame, bool) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 0, 64)
	tmp := make([]byte, 64)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if f, _, derr := codec.Unpack(buf); derr == nil {
				return f, true
			}
		}
		if err != nil {
			return codec.Frame{}, false
		}
	}
}

// TestSessionRoundTripStatusRequest drives a real rx 07 STATUS REQUEST
// over a live socket and reads back the tx 09 reply — exercising
// session.run's read → Unpack → dispatch → write reply path end to end.
func TestSessionRoundTripStatusRequest(t *testing.T) {
	_, conn, cleanup := dialServed(t)
	defer cleanup()

	in := codec.Pack(codec.EncodeStatusRequest(codec.StatusRequestParams{Controller: codec.ControllerLH}))
	if _, err := conn.Write(in); err != nil {
		t.Fatalf("write rx 07: %v", err)
	}
	f, ok := readFrame(t, conn)
	if !ok {
		t.Fatal("no reply frame")
	}
	if f.ID != codec.TxStatusResponse2 {
		t.Errorf("reply ID = %#x; want TxStatusResponse2", f.ID)
	}
}

// TestSessionBroadcastReachesSender drives an rx 02 CONNECT, which
// produces a tx 04 broadcast (no point-to-point reply). The sending
// session is also a fan-out target (§3.2.6 "all ports"), so it must
// receive the tx 04 — exercising session.run's broadcast loop + fanOut.
func TestSessionBroadcastReachesSender(t *testing.T) {
	_, conn, cleanup := dialServed(t)
	defer cleanup()

	in := codec.Pack(codec.EncodeConnect(codec.ConnectParams{Destination: 1, Source: 2}))
	if _, err := conn.Write(in); err != nil {
		t.Fatalf("write rx 02: %v", err)
	}
	f, ok := readFrame(t, conn)
	if !ok {
		t.Fatal("no broadcast frame")
	}
	if f.ID != codec.TxCrosspointConnected {
		t.Errorf("broadcast ID = %#x; want TxCrosspointConnected", f.ID)
	}
}

// TestSessionDesyncDropsLeadingGarbage feeds a leading non-SOM byte
// before a valid rx 07 frame. session.run must drop the garbage byte
// (desync path) and still process the trailing frame.
func TestSessionDesyncDropsLeadingGarbage(t *testing.T) {
	_, conn, cleanup := dialServed(t)
	defer cleanup()

	in := codec.Pack(codec.EncodeStatusRequest(codec.StatusRequestParams{Controller: codec.ControllerLH}))
	// Prepend three junk (non-0xFF) bytes.
	garbage := append([]byte{0x00, 0x12, 0x34}, in...)
	if _, err := conn.Write(garbage); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, ok := readFrame(t, conn)
	if !ok {
		t.Fatal("no reply after desync recovery")
	}
	if f.ID != codec.TxStatusResponse2 {
		t.Errorf("reply ID = %#x; want TxStatusResponse2", f.ID)
	}
}

// TestSessionBadChecksumAbsorbed feeds a frame with a corrupted
// checksum followed by a valid frame. The bad frame fires the
// InboundFrameDecodeFailed compliance event and the session resyncs to
// the valid one — exercising session.run's derr != nil arm.
func TestSessionBadChecksumAbsorbed(t *testing.T) {
	srv, conn, cleanup := dialServed(t)
	defer cleanup()

	bad := codec.Pack(codec.EncodeStatusRequest(codec.StatusRequestParams{Controller: codec.ControllerLH}))
	bad[len(bad)-1] ^= 0x01 // corrupt checksum (still 7-bit; flips low bit)
	good := codec.Pack(codec.EncodeStatusRequest(codec.StatusRequestParams{Controller: codec.ControllerRH}))
	if _, err := conn.Write(append(bad, good...)); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, ok := readFrame(t, conn)
	if !ok {
		t.Fatal("no reply after bad-checksum resync")
	}
	if f.ID != codec.TxStatusResponse2 {
		t.Errorf("reply ID = %#x; want TxStatusResponse2", f.ID)
	}
	// The corrupted frame must have been noted.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if srv.profile.Snapshot()[InboundFrameDecodeFailed] >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := srv.profile.Snapshot()[InboundFrameDecodeFailed]; got < 1 {
		t.Errorf("InboundFrameDecodeFailed = %d; want >= 1", got)
	}
}

// TestServeBindsAndStops covers Serve (the addr-based path) plus Stop:
// Serve binds a real port, Stop closes the listener, and Serve returns
// nil (net.ErrClosed is swallowed).
func TestServeBindsAndStops(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.Serve(ctx, "127.0.0.1:0") }()

	// Wait until the listener is bound.
	deadline := time.Now().Add(2 * time.Second)
	var addr string
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		if srv.listener != nil {
			addr = srv.listener.Addr().String()
		}
		srv.mu.Unlock()
		if addr != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("Serve never bound a listener")
	}

	// Open a session so Stop has a session to drop.
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if err := srv.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	// Second Stop is idempotent (closed already).
	if err := srv.Stop(); err != nil {
		t.Errorf("Stop (2nd) = %v; want nil (idempotent)", err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Errorf("Serve returned %v; want nil after Stop", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Serve did not return after Stop")
	}
}

// TestServeListenError covers Serve's listen-failure arm: an invalid
// address yields a wrapped error.
func TestServeListenError(t *testing.T) {
	srv := newTestServer(t)
	err := srv.Serve(context.Background(), "256.256.256.256:99999")
	if err == nil {
		t.Fatal("Serve(bad addr) = nil; want error")
	}
}

// TestStopWithoutListener covers Stop's nil-listener arm: a server that
// never served has listener == nil, so Stop returns nil.
func TestStopWithoutListener(t *testing.T) {
	srv := newTestServer(t)
	if err := srv.Stop(); err != nil {
		t.Errorf("Stop(no listener) = %v; want nil", err)
	}
}

// TestNewServerNilLogger covers the logger == nil arm of newServer:
// a nil logger is replaced with slog.Default() and the server still
// builds.
func TestNewServerNilLogger(t *testing.T) {
	srv := newServer(nil, nil)
	if srv == nil || srv.logger == nil {
		t.Fatal("newServer(nil,nil) produced nil server/logger")
	}
	if srv.tree == nil {
		t.Error("tree = nil; want non-nil even for nil export")
	}
}

// TestSetValueAppliesAndCoerces covers SetValue end to end (the API
// path): a valid path + int value applies a crosspoint and returns the
// coerced source.
func TestSetValueAppliesAndCoerces(t *testing.T) {
	srv := newTestServer(t) // matrix 0 level 0, counts 4/4
	out, err := srv.SetValue(context.Background(), "0.0.1", 2)
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	m, ok := out.(map[string]uint16)
	if !ok || m["src"] != 2 {
		t.Errorf("SetValue out = %v; want map[src:2]", out)
	}
	if got, ok := srv.tree.matrices[matrixKey{matrix: 0, level: 0}].sources[1]; !ok || got != 2 {
		t.Errorf("tree[1] = (%d, %v); want (2, true)", got, ok)
	}
}

// TestSetValueBadPath covers SetValue's parse-error arm.
func TestSetValueBadPath(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.SetValue(context.Background(), "not-a-path", 1); err == nil {
		t.Error("SetValue(bad path) = nil; want error")
	}
}

// TestSetValueBadValue covers SetValue's coerce-error arm.
func TestSetValueBadValue(t *testing.T) {
	srv := newTestServer(t)
	if _, err := srv.SetValue(context.Background(), "0.0.1", struct{}{}); err == nil {
		t.Error("SetValue(bad value) = nil; want error")
	}
}

// TestSetValueApplyError covers SetValue's applyConnect-error arm: a
// valid path/value whose dst exceeds the declared count fails.
func TestSetValueApplyError(t *testing.T) {
	srv := newTestServer(t) // counts 4/4
	if _, err := srv.SetValue(context.Background(), "0.0.99", 1); err == nil {
		t.Error("SetValue(dst oob) = nil; want error")
	}
}

// TestParseCrosspointPath covers parseCrosspointPath success + both
// failure arms (malformed + out-of-range).
func TestParseCrosspointPath(t *testing.T) {
	m, l, d, err := parseCrosspointPath("2.3.4")
	if err != nil || m != 2 || l != 3 || d != 4 {
		t.Errorf("parse(2.3.4) = (%d,%d,%d,%v); want (2,3,4,nil)", m, l, d, err)
	}
	if _, _, _, err := parseCrosspointPath("bad"); err == nil {
		t.Error("parse(bad) = nil; want error")
	}
	if _, _, _, err := parseCrosspointPath("999.0.0"); err == nil {
		t.Error("parse(matrix oob) = nil; want error")
	}
	if _, _, _, err := parseCrosspointPath("0.999.0"); err == nil {
		t.Error("parse(level oob) = nil; want error")
	}
	if _, _, _, err := parseCrosspointPath("0.0.99999999"); err == nil {
		t.Error("parse(dst oob) = nil; want error")
	}
}

// TestCoerceSource covers every type arm of coerceSource plus the
// default error arm.
func TestCoerceSource(t *testing.T) {
	cases := []struct {
		in   any
		want uint16
	}{
		{int(5), 5},
		{int32(6), 6},
		{int64(7), 7},
		{uint16(8), 8},
		{uint32(9), 9},
		{uint64(10), 10},
		{float64(11), 11},
	}
	for _, c := range cases {
		got, err := coerceSource(c.in)
		if err != nil || got != c.want {
			t.Errorf("coerceSource(%T %v) = (%d, %v); want (%d, nil)", c.in, c.in, got, err, c.want)
		}
	}
	if _, err := coerceSource("nope"); err == nil {
		t.Error("coerceSource(string) = nil; want error")
	}
}

// TestRemoteAddrNilConn covers the remoteAddr nil-conn guard.
func TestRemoteAddrNilConn(t *testing.T) {
	s := &session{}
	if got := s.remoteAddr(); got != "" {
		t.Errorf("remoteAddr(nil conn) = %q; want empty", got)
	}
}

// TestSessionCloseIdempotent covers session.close: closing once shuts
// the socket, closing again is a no-op (closed guard).
func TestSessionCloseIdempotent(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	s := &session{conn: a}
	s.close()
	if !s.closed {
		t.Error("session not marked closed after close()")
	}
	s.close() // second call hits the early-return guard
}

// TestFanOutWriteFailureNotesEventAndContinues covers fanOut's
// write-error arm: a session whose socket is already closed makes the
// write fail, which fires OutboundWriteFailed and continues to the next
// session rather than aborting.
func TestFanOutWriteFailureNotesEventAndContinues(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := newServer(logger, nil)

	// A dead session: pipe with the far end already closed so Write
	// errors deterministically.
	a, b := net.Pipe()
	_ = a.Close()
	_ = b.Close()
	dead := &session{conn: a}

	srv.mu.Lock()
	srv.sessions[dead] = struct{}{}
	srv.mu.Unlock()

	before := srv.profile.Snapshot()[OutboundWriteFailed]
	frame := codec.Pack(codec.EncodeStatusRequest(codec.StatusRequestParams{}))
	srv.fanOut(frame, codec.RxStatusRequest)

	if got := srv.profile.Snapshot()[OutboundWriteFailed]; got != before+1 {
		t.Errorf("OutboundWriteFailed %d -> %d; want +1", before, got)
	}
}

// TestSessionRunReturnsOnCancelledContext covers the ctx.Err() guard
// at the top of session.run's read loop: a context that is already
// cancelled makes run return before the first Read.
func TestSessionRunReturnsOnCancelledContext(t *testing.T) {
	srv := newTestServer(t)
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	sess := newSession(srv, a)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	done := make(chan struct{})
	go func() {
		sess.run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session.run did not return for cancelled ctx")
	}
}

// TestSessionRunReadErrorDebugBranch covers session.run's read-error
// arm for a non-EOF / non-ErrClosed error: a read deadline that has
// already elapsed makes conn.Read return a timeout error, which hits
// the debug-log branch and ends the session.
func TestSessionRunReadErrorDebugBranch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := newServer(logger, nil)

	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	// Force Read to fail immediately with a (non-EOF) deadline error.
	_ = a.SetReadDeadline(time.Now().Add(-time.Second))
	sess := newSession(srv, a)

	done := make(chan struct{})
	go func() {
		sess.run(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session.run did not return on read error")
	}
}

// TestSessionRunPartialFrameWaitsThenCompletes covers the
// io.ErrUnexpectedEOF "break and read more" arm of session.run: a frame
// delivered in two writes (header first, then the rest) must still be
// processed once complete.
func TestSessionRunPartialFrameWaitsThenCompletes(t *testing.T) {
	_, conn, cleanup := dialServed(t)
	defer cleanup()

	in := codec.Pack(codec.EncodeStatusRequest(codec.StatusRequestParams{Controller: codec.ControllerLH}))
	// Send all but the final (checksum) byte, pause, then the rest.
	if _, err := conn.Write(in[:len(in)-1]); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := conn.Write(in[len(in)-1:]); err != nil {
		t.Fatalf("write tail: %v", err)
	}
	f, ok := readFrame(t, conn)
	if !ok {
		t.Fatal("no reply after partial-frame reassembly")
	}
	if f.ID != codec.TxStatusResponse2 {
		t.Errorf("reply ID = %#x; want TxStatusResponse2", f.ID)
	}
}

// TestSessionRunDebugLoggingPath covers the
// logger.Enabled(LevelDebug) rx-logging block of session.run by running
// a session with a debug-level logger and sending a valid frame.
func TestSessionRunDebugLoggingPath(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := newServer(logger, nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.serveListener(ctx, ln) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	in := codec.Pack(codec.EncodeStatusRequest(codec.StatusRequestParams{}))
	if _, err := conn.Write(in); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, ok := readFrame(t, conn)
	if !ok || f.ID != codec.TxStatusResponse2 {
		t.Fatalf("debug-path round trip failed: ok=%v frame=%+v", ok, f)
	}
	cancel()
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Error("serveListener did not return")
	}
}

// TestSessionRunUnsupportedCommandContinues covers session.run's
// "reply == nil && broadcast empty" arm: a well-framed but unsupported
// command id is absorbed (UnsupportedCommand event) and the session
// keeps reading, answering a following supported frame.
func TestSessionRunUnsupportedCommandContinues(t *testing.T) {
	srv, conn, cleanup := dialServed(t)
	defer cleanup()

	// 0x6E (110) is a registered codec command in the catalogue? No —
	// use a Tx-only command that dispatch has no handler for but which
	// the codec PayloadLen knows, so Unpack succeeds and dispatch
	// returns an empty result. tx 04 CONNECTED (TxCrosspointConnected)
	// has a fixed 3-byte payload and no Rx handler.
	unsupported := codec.Pack(codec.EncodeConnected(codec.ConnectedParams{Destination: 0, Source: 0}))
	if _, err := conn.Write(unsupported); err != nil {
		t.Fatalf("write unsupported: %v", err)
	}
	// Follow with a supported rx 07 to prove the session is still alive.
	good := codec.Pack(codec.EncodeStatusRequest(codec.StatusRequestParams{}))
	if _, err := conn.Write(good); err != nil {
		t.Fatalf("write good: %v", err)
	}
	f, ok := readFrame(t, conn)
	if !ok || f.ID != codec.TxStatusResponse2 {
		t.Fatalf("session did not survive unsupported cmd: ok=%v frame=%+v", ok, f)
	}
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if srv.profile.Snapshot()[UnsupportedCommand] >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := srv.profile.Snapshot()[UnsupportedCommand]; got < 1 {
		t.Errorf("UnsupportedCommand = %d; want >= 1", got)
	}
}

// errListener is a net.Listener whose Accept returns a fixed non-
// ErrClosed error, used to drive serveListener's "return err" arm
// (the path that does NOT swallow net.ErrClosed / context.Canceled).
type errListener struct{ err error }

func (e errListener) Accept() (net.Conn, error) { return nil, e.err }
func (e errListener) Close() error              { return nil }
func (e errListener) Addr() net.Addr            { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

// TestServeListenerReturnsAcceptError covers serveListener's non-
// swallowed error return (line "return err"): an Accept that fails with
// an error other than net.ErrClosed / context.Canceled propagates out.
func TestServeListenerReturnsAcceptError(t *testing.T) {
	srv := newTestServer(t)
	sentinel := errors.New("synthetic accept failure")
	err := srv.serveListener(context.Background(), errListener{err: sentinel})
	if !errors.Is(err, sentinel) {
		t.Errorf("serveListener err = %v; want %v", err, sentinel)
	}
}

// TestAcceptLoopReturnsOnCancelledContext covers acceptLoop's ctx.Err()
// guard at the top of the loop: a pre-cancelled context returns
// context.Canceled before Accept is called.
func TestAcceptLoopReturnsOnCancelledContext(t *testing.T) {
	srv := newTestServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := srv.acceptLoop(ctx, ln); !errors.Is(err, context.Canceled) {
		t.Errorf("acceptLoop(cancelled) = %v; want context.Canceled", err)
	}
}

// TestSessionWriteFailureClosesSession covers session.run's
// write-failure return arm: a handler reply written to a peer whose
// socket has gone away makes session.write fail, firing
// OutboundWriteFailed and ending the session loop.
func TestSessionWriteFailureClosesSession(t *testing.T) {
	srv := newTestServer(t)

	// net.Pipe gives a synchronous in-memory conn; closing the far end
	// makes the provider's Write fail once it tries to send a reply.
	provConn, peerConn := net.Pipe()
	sess := newSession(srv, provConn)
	srv.mu.Lock()
	srv.sessions[sess] = struct{}{}
	srv.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		sess.run(ctx)
		close(done)
	}()

	// Send a rx 07 STATUS REQUEST; the handler will try to write tx 09.
	in := codec.Pack(codec.EncodeStatusRequest(codec.StatusRequestParams{}))
	go func() {
		_, _ = peerConn.Write(in)
		// Close the peer so the provider's reply Write fails.
		_ = peerConn.Close()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("session.run did not exit after write failure")
	}
}
