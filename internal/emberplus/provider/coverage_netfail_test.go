package emberplus

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/emberplus/codec/ber"
	"dhs/internal/emberplus/codec/glow"
)

// failConn is a net.Conn whose Write fails after failAfter successful
// writes. Reads block until closed. Used to drive the mid-operation
// write-error branches of writeEmBERChunks / writePump deterministically.
type failConn struct {
	failAfter int32
	writes    atomic.Int32
	closed    chan struct{}
	once      sync.Once
}

func newFailConn(failAfter int) *failConn {
	return &failConn{failAfter: int32(failAfter), closed: make(chan struct{})}
}

func (c *failConn) Read(b []byte) (int, error) {
	<-c.closed
	return 0, net.ErrClosed
}

func (c *failConn) Write(b []byte) (int, error) {
	n := c.writes.Add(1)
	if n > c.failAfter {
		return 0, errors.New("failConn: induced write error")
	}
	return len(b), nil
}

func (c *failConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (c *failConn) LocalAddr() net.Addr              { return fakeAddr{} }
func (c *failConn) RemoteAddr() net.Addr             { return fakeAddr{} }
func (c *failConn) SetDeadline(time.Time) error      { return nil }
func (c *failConn) SetReadDeadline(time.Time) error  { return nil }
func (c *failConn) SetWriteDeadline(time.Time) error { return nil }

type fakeAddr struct{}

func (fakeAddr) Network() string { return "fake" }
func (fakeAddr) String() string  { return "fake:0" }

// TestWriteEMBERChunks_MultiChunkError covers the WriteFrame-error return
// inside the multi-packet loop (a chunk after the first fails to write).
func TestWriteEMBERChunks_MultiChunkError(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	// Fail after the first frame so the SECOND chunk's WriteFrame errors.
	fc := newFailConn(1)
	sess := newSession(srv, fc)
	defer sess.close()

	big := make([]byte, maxS101Payload*3) // forces ≥3 chunks
	if err := sess.writeEmBERChunks(big); err == nil {
		t.Error("multi-chunk write should propagate the induced error")
	}
}

// TestWritePump_ClosedSession covers writePump's <-s.closed exit branch.
func TestWritePump_ClosedSession(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	fc := newFailConn(1000)
	sess := newSession(srv, fc)

	done := make(chan struct{})
	go func() { sess.writePump(context.Background()); close(done) }()
	// Close the session → writePump observes s.closed and returns.
	sess.close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("writePump did not exit on session close")
	}
}

// TestWritePump_ChannelClosed covers the `payload, ok := <-s.out; !ok`
// branch by closing s.out directly while the pump is parked on it.
func TestWritePump_ChannelClosed(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	fc := newFailConn(1000)
	sess := newSession(srv, fc)
	defer sess.close()

	done := make(chan struct{})
	go func() { sess.writePump(context.Background()); close(done) }()
	// Give the pump a moment to park on the select, then close out.
	time.Sleep(20 * time.Millisecond)
	close(sess.out)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("writePump did not exit on channel close")
	}
}

// TestRun_CtxDone covers run's <-ctx.Done() early-return branch: a
// cancelled context makes the read loop return before reading a frame.
func TestRun_CtxDone(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	fc := newFailConn(1000)
	sess := newSession(srv, fc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already done
	done := make(chan struct{})
	go func() { sess.run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("run did not return on cancelled ctx")
	}
}

// scriptedListener is a net.Listener that returns a scripted sequence of
// Accept results: first a transient (non-ErrClosed) error to drive the
// accept-error-continue branch, then net.ErrClosed to terminate the loop.
type scriptedListener struct {
	calls atomic.Int32
}

func (l *scriptedListener) Accept() (net.Conn, error) {
	switch l.calls.Add(1) {
	case 1:
		return nil, errors.New("scripted: transient accept error")
	default:
		return nil, net.ErrClosed
	}
}
func (l *scriptedListener) Close() error   { return nil }
func (l *scriptedListener) Addr() net.Addr { return fakeAddr{} }

// TestAcceptLoop_TransientErrorContinue covers the accept-error-continue
// path in acceptLoop: a transient error (ctx live, not ErrClosed) is
// logged and the loop continues; a subsequent ErrClosed ends it.
func TestAcceptLoop_TransientErrorContinue(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	ln := &scriptedListener{}
	done := make(chan error, 1)
	go func() { done <- srv.acceptLoop(context.Background(), ln) }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("acceptLoop returned %v, want nil after ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acceptLoop did not terminate")
	}
	if ln.calls.Load() < 2 {
		t.Errorf("expected ≥2 Accept calls (transient + closed), got %d", ln.calls.Load())
	}
}

// TestAcceptLoop_CtxCancelledError covers the ctx.Err()!=nil branch: a
// transient accept error while the context is already cancelled returns
// nil immediately.
func TestAcceptLoop_CtxCancelledError(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	ln := &scriptedListener{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := srv.acceptLoop(ctx, ln); err != nil {
		t.Errorf("acceptLoop with cancelled ctx returned %v, want nil", err)
	}
}

// TestFindCommandInElements_MatrixChildren covers the e.Matrix recursion
// branch (a Command nested inside a Matrix's children).
func TestFindCommandInElements_MatrixChildren(t *testing.T) {
	cmd := &glow.Command{Number: glow.CmdGetDirectory}
	els := []glow.Element{{
		Matrix: &glow.Matrix{
			Path:     []int32{1, 2},
			Children: []glow.Element{{Command: cmd}},
		},
	}}
	got, path := findCommandInElements(els)
	if got == nil || got.Number != glow.CmdGetDirectory {
		t.Fatalf("command not found in matrix children: %+v", got)
	}
	if len(path) == 0 {
		t.Errorf("expected a base path from the matrix, got %v", path)
	}
}

// TestHandleEmber_UnknownCommand covers handleEmber's command-switch
// fallthrough `return nil` for a command number outside
// {GetDirectory, Subscribe, Unsubscribe, Invoke}.
func TestHandleEmber_UnknownCommand(t *testing.T) {
	srv := newServer(nil, buildRichExport())
	fc := newFailConn(1000)
	sess := newSession(srv, fc)
	defer sess.close()

	// Root → Command(number=99) — a number no case handles.
	payload := ber.EncodeTLV(ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagCommand,
			ber.ContextConstructed(glow.CmdCtxNumber, ber.Integer(99)),
		),
	))
	if err := sess.handleEmber(payload); err != nil {
		t.Errorf("unknown command should be a no-op, got %v", err)
	}
}
