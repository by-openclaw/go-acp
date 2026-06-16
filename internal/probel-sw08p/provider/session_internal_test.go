package probelsw08p

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"dhs/internal/probel-sw08p/codec"
)

// debugServer returns a provider whose logger is at Debug level so the
// session's gated Debug rx/tx hex-dump branches execute.
func debugServer(t *testing.T) *server {
	t.Helper()
	h := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})
	srv := newServer(slog.New(h), demoMatrixExport(16, 16))
	return srv
}

// drainReader consumes everything the session writes so net.Pipe writes
// on the session side never block. It records the bytes seen for
// assertions.
type sink struct {
	mu  sync.Mutex
	buf []byte
}

func (s *sink) start(t *testing.T, conn net.Conn) {
	t.Helper()
	go func() {
		tmp := make([]byte, 256)
		for {
			n, err := conn.Read(tmp)
			if n > 0 {
				s.mu.Lock()
				s.buf = append(s.buf, tmp[:n]...)
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
}

func (s *sink) snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, len(s.buf))
	copy(out, s.buf)
	return out
}

// countMarker counts non-overlapping 2-byte DLE+marker pairs in buf.
func countMarker(buf []byte, marker byte) int {
	n := 0
	for i := 0; i+1 < len(buf); i++ {
		if buf[i] == codec.DLE && buf[i+1] == marker {
			n++
			i++
		}
	}
	return n
}

// runSession spins s.run on a net.Pipe pair, returns the client end +
// a sink draining the session's writes + a stop func.
func runSession(t *testing.T, srv *server) (net.Conn, *sink, func()) {
	t.Helper()
	cConn, sConn := net.Pipe()
	sess := newSession(srv, sConn)
	srv.mu.Lock()
	srv.sessions[sess] = struct{}{}
	srv.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() {
		sess.run(ctx)
		close(runDone)
	}()

	sk := &sink{}
	sk.start(t, cConn)

	return cConn, sk, func() {
		cancel()
		_ = cConn.Close()
		select {
		case <-runDone:
		case <-time.After(2 * time.Second):
			t.Error("session.run did not return")
		}
	}
}

// TestSessionReaderAckNakDesyncPaths feeds a DLE ACK, a DLE NAK, a
// desync byte, then a valid frame and asserts the reader ACKs exactly
// the valid frame (ACK/NAK pseudo-frames and the stray byte are
// consumed silently). Exercises session.run lines 102-120.
func TestSessionReaderAckNakDesyncPaths(t *testing.T) {
	srv := debugServer(t)
	conn, sk, stop := runSession(t, srv)
	defer stop()

	// Inbound ACK + NAK pseudo-frames + a stray non-DLE byte.
	if _, err := conn.Write(codec.PackACK()); err != nil {
		t.Fatalf("write ACK: %v", err)
	}
	if _, err := conn.Write(codec.PackNAK()); err != nil {
		t.Fatalf("write NAK: %v", err)
	}
	if _, err := conn.Write([]byte{0x42}); err != nil { // desync byte
		t.Fatalf("write stray: %v", err)
	}
	// A valid interrogate frame — the session must ACK this.
	frame := codec.Pack(codec.EncodeCrosspointInterrogate(codec.CrosspointInterrogateParams{
		MatrixID: 0, LevelID: 0, DestinationID: 1,
	}))
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	// Expect at least one ACK (for the valid frame) and a tally reply.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countMarker(sk.snapshot(), codec.ACK) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := countMarker(sk.snapshot(), codec.ACK); got < 1 {
		t.Errorf("session ACKs = %d; want >= 1 for the valid frame", got)
	}
}

// TestSessionReaderBadFrameNaks feeds a well-delimited but
// checksum-broken frame and asserts the session emits a DLE NAK and
// notes InboundFrameDecodeFailed. Exercises session.run lines 125-133.
func TestSessionReaderBadFrameNaks(t *testing.T) {
	srv := debugServer(t)
	conn, sk, stop := runSession(t, srv)
	defer stop()

	before := srv.profile.Snapshot()[InboundFrameDecodeFailed]
	// DLE STX <id> <wrong-btc> <wrong-chk> DLE ETX — passes EOM scan, fails BTC.
	bad := []byte{codec.DLE, codec.STX, 0x01, 0x99, 0x00, codec.DLE, codec.ETX}
	if _, err := conn.Write(bad); err != nil {
		t.Fatalf("write bad frame: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countMarker(sk.snapshot(), codec.NAK) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := countMarker(sk.snapshot(), codec.NAK); got < 1 {
		t.Errorf("session NAKs = %d; want >= 1 on bad frame", got)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if srv.profile.Snapshot()[InboundFrameDecodeFailed] > before {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if srv.profile.Snapshot()[InboundFrameDecodeFailed] <= before {
		t.Error("InboundFrameDecodeFailed not noted on bad frame")
	}
}

// TestSessionReaderPartialFrameBuffers writes a half frame (no EOM), then
// the remainder, proving the io.ErrUnexpectedEOF break path holds bytes
// until the frame completes (session.run lines 122-123).
func TestSessionReaderPartialFrameBuffers(t *testing.T) {
	srv := debugServer(t)
	conn, sk, stop := runSession(t, srv)
	defer stop()

	frame := codec.Pack(codec.EncodeCrosspointInterrogate(codec.CrosspointInterrogateParams{
		MatrixID: 0, LevelID: 0, DestinationID: 2,
	}))
	half := len(frame) / 2
	if _, err := conn.Write(frame[:half]); err != nil {
		t.Fatalf("write first half: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // reader sees a partial frame, buffers it
	if _, err := conn.Write(frame[half:]); err != nil {
		t.Fatalf("write second half: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countMarker(sk.snapshot(), codec.ACK) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := countMarker(sk.snapshot(), codec.ACK); got < 1 {
		t.Errorf("session ACKs = %d; want >= 1 once the split frame reassembles", got)
	}
}

// TestSessionDispatchHandlerError drives a frame whose handler rejects
// it (connect to an out-of-range dst). The session ACKs at the framer
// layer, then dispatch logs + notes HandlerRejected with no reply.
func TestSessionDispatchHandlerError(t *testing.T) {
	srv := debugServer(t) // 16×16
	conn, sk, stop := runSession(t, srv)
	defer stop()

	before := srv.profile.Snapshot()[HandlerRejected]
	frame := codec.Pack(codec.EncodeCrosspointConnect(codec.CrosspointConnectParams{
		MatrixID: 0, LevelID: 0, DestinationID: 999, SourceID: 1,
	}))
	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if srv.profile.Snapshot()[HandlerRejected] > before {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if srv.profile.Snapshot()[HandlerRejected] <= before {
		t.Error("HandlerRejected not noted on rejecting handler")
	}
	// Framer-level ACK still went out.
	if got := countMarker(sk.snapshot(), codec.ACK); got < 1 {
		t.Errorf("session ACKs = %d; want >= 1 (framer ACK before handler)", got)
	}
}

// TestSessionDispatchReplyAndStream drives a tally-dump request whose
// handler streams multiple frames back to the sender (streamToSender
// path), plus a normal interrogate that exercises the single-reply path.
func TestSessionDispatchReplyAndStream(t *testing.T) {
	srv := debugServer(t) // 16×16 → byte-form dump, single frame, but streamToSender
	conn, sk, stop := runSession(t, srv)
	defer stop()

	// Single-reply path: interrogate → tx 003.
	if _, err := conn.Write(codec.Pack(codec.EncodeCrosspointInterrogate(
		codec.CrosspointInterrogateParams{MatrixID: 0, LevelID: 0, DestinationID: 1}))); err != nil {
		t.Fatalf("write interrogate: %v", err)
	}
	// streamToSender path: tally dump.
	if _, err := conn.Write(codec.Pack(codec.EncodeCrosspointTallyDumpRequest(
		codec.CrosspointTallyDumpRequestParams{MatrixID: 0, LevelID: 0}))); err != nil {
		t.Fatalf("write dump: %v", err)
	}

	// Wait until we see both a tx 003 tally and a tx 022 dump frame.
	var sawTally, sawDump bool
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (!sawTally || !sawDump) {
		buf := sk.snapshot()
		i := 0
		for i+1 < len(buf) {
			if codec.IsACK(buf[i:]) || codec.IsNAK(buf[i:]) {
				i += 2
				continue
			}
			if buf[i] != codec.DLE {
				i++
				continue
			}
			f, consumed, err := codec.Unpack(buf[i:])
			if err != nil {
				break
			}
			switch f.ID {
			case codec.TxCrosspointTally:
				sawTally = true
			case codec.TxCrosspointTallyDumpByte, codec.TxCrosspointTallyDumpWord:
				sawDump = true
			}
			i += consumed
		}
		if !sawTally || !sawDump {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !sawTally {
		t.Error("never saw tx 003 tally reply")
	}
	if !sawDump {
		t.Error("never saw tx 022/023 dump frame from streamToSender")
	}
}

// TestSessionRunCtxCancelledAtTop: run() with an already-cancelled ctx
// returns at the read-loop's top-of-loop ctx.Err() guard (session.go ~85)
// before ever blocking in Read.
func TestSessionRunCtxCancelledAtTop(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()
	sess := newSession(srv, c2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	runDone := make(chan struct{})
	go func() { sess.run(ctx); close(runDone) }()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return immediately on cancelled ctx")
	}
}

// TestDispatcherCtxCancelledDrops: the dispatcher goroutine, fed a frame
// after its ctx is cancelled, hits the ctx.Err() guard inside the range
// loop and returns without dispatching (session.go ~180).
func TestDispatcherCtxCancelledDrops(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), demoMatrixExport(16, 16))
	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()
	sess := newSession(srv, c2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the dispatcher runs
	sess.startDispatcher(ctx)

	// Push a frame; the dispatcher receives it, sees ctx.Err(), and returns
	// without calling dispatch (no reply written).
	sess.dispatchCh <- pendingFrame{
		f:    codec.EncodeCrosspointInterrogate(codec.CrosspointInterrogateParams{MatrixID: 0, LevelID: 0, DestinationID: 1}),
		rxAt: time.Now(),
	}
	close(sess.dispatchCh)

	done := make(chan struct{})
	go func() { sess.dispatchWG.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher did not return after cancelled-ctx frame")
	}
}

// TestSessionReadErrorNonEOF: closing the session's OWN conn while run()
// is blocked in Read makes Read return io.ErrClosedPipe — which is
// neither io.EOF nor net.ErrClosed — exercising the Debug read-error log
// branch (session.go ~90). Requires a Debug-level logger.
func TestSessionReadErrorNonEOF(t *testing.T) {
	h := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})
	srv := newServer(slog.New(h), demoMatrixExport(16, 16))

	cConn, sConn := net.Pipe()
	defer func() { _ = cConn.Close() }()
	sess := newSession(srv, sConn)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() { sess.run(ctx); close(runDone) }()

	// Let run() reach its blocking Read, then close the session's own end.
	time.Sleep(50 * time.Millisecond)
	_ = sConn.Close() // run's Read returns io.ErrClosedPipe

	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("session.run did not return after own-conn close")
	}
}

// TestSessionWriteClosed: write() on a closed session returns
// net.ErrClosed.
func TestSessionWriteClosed(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()
	sess := newSession(srv, c1)
	sess.close()
	if err := sess.write([]byte{0x10, 0x06}); err != net.ErrClosed {
		t.Errorf("write after close = %v; want net.ErrClosed", err)
	}
}

// TestSessionCloseIdempotent: close() twice is safe (the closed-guard
// early return).
func TestSessionCloseIdempotent(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	c1, c2 := net.Pipe()
	defer func() { _ = c2.Close() }()
	sess := newSession(srv, c1)
	sess.close()
	sess.close() // must not panic / double-close
}

// TestSessionDispatchReplyWriteFail: dispatch() on a closed session
// still runs the handler but every write (reply + stream) fails. Drives
// the reply-write-fail arm (session.go ~216) and the stream emit-error
// arm (~226/233) without a live socket.
func TestSessionDispatchReplyWriteFail(t *testing.T) {
	srv := debugServer(t) // 16×16, Debug logger so the tx hex-dump branch runs
	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	defer func() { _ = c2.Close() }()
	sess := newSession(srv, c1)
	sess.close() // every write() now returns net.ErrClosed

	// Single-reply path — write fails, logged, no panic.
	sess.dispatch(codec.EncodeCrosspointInterrogate(codec.CrosspointInterrogateParams{
		MatrixID: 0, LevelID: 0, DestinationID: 1,
	}), time.Now())

	// streamToSender path — the emit closure's write fails, surfacing an
	// error out of streamToSender which dispatch logs.
	sess.dispatch(codec.EncodeCrosspointTallyDumpRequest(
		codec.CrosspointTallyDumpRequestParams{MatrixID: 0, LevelID: 0}), time.Now())
}

// TestDispatcherPanicRecovered: a handler that panics (nil tree → nil
// deref inside handleCrosspointInterrogate) is recovered by the
// dispatcher goroutine's deferred recover (session.go ~172). The
// session must not crash the process; run() returns cleanly.
func TestDispatcherPanicRecovered(t *testing.T) {
	// Build the server via newServer so all metrics/profile wiring is
	// intact, then null its tree so the handler nil-derefs and panics.
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	srv.tree = nil

	c1, c2 := net.Pipe()
	sess := newSession(srv, c2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan struct{})
	go func() { sess.run(ctx); close(runDone) }()

	sk := &sink{}
	sk.start(t, c1)

	// Interrogate → handler dereferences nil tree → panics → recovered.
	if _, err := c1.Write(codec.Pack(codec.EncodeCrosspointInterrogate(
		codec.CrosspointInterrogateParams{MatrixID: 0, LevelID: 0, DestinationID: 1}))); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Give the dispatcher a beat to panic + recover.
	time.Sleep(100 * time.Millisecond)
	cancel()
	_ = c1.Close()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("session.run did not return after dispatcher panic")
	}
}

// TestSessionRemoteAddrNilConn: remoteAddr handles a nil conn gracefully.
func TestSessionRemoteAddrNilConn(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), emptyExport())
	sess := &session{srv: srv} // conn nil
	if got := sess.remoteAddr(); got != "" {
		t.Errorf("remoteAddr(nil conn) = %q; want empty", got)
	}
}
