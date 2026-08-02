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

// gatedConn is a fake net.Conn that feeds a fixed byte stream on Read and
// lets 2-byte writes (DLE ACK / DLE NAK) through instantly while BLOCKING
// any longer write (a handler reply). This deterministically stalls the
// dispatcher goroutine on its reply write without stalling the read
// loop's ACK writes — the only way to build channel backpressure on the
// per-session dispatchCh so the read loop's `case <-ctx.Done()` send arm
// (session.go) becomes reachable.
type gatedConn struct {
	mu        sync.Mutex
	data      []byte // remaining bytes to hand to Read
	released  chan struct{}
	closeOnce sync.Once
}

func (g *gatedConn) Read(p []byte) (int, error) {
	g.mu.Lock()
	if len(g.data) > 0 {
		n := copy(p, g.data)
		g.data = g.data[n:]
		g.mu.Unlock()
		return n, nil
	}
	g.mu.Unlock()
	// No more data — block until released, then report EOF so run() exits
	// cleanly.
	<-g.released
	return 0, io.EOF
}

func (g *gatedConn) Write(p []byte) (int, error) {
	if len(p) <= 2 {
		return len(p), nil // ACK / NAK pass through
	}
	<-g.released // a reply — block until the test releases
	return len(p), nil
}

func (g *gatedConn) Close() error {
	// A real net.Conn.Close is idempotent; run()'s defer and session.close()
	// can both call it, and under -race the old check-then-close select raced
	// (two goroutines hitting default → "close of closed channel"). sync.Once
	// makes it race-free and idempotent.
	g.closeOnce.Do(func() { close(g.released) })
	return nil
}

func (g *gatedConn) LocalAddr() net.Addr                { return fakeAddr{} }
func (g *gatedConn) RemoteAddr() net.Addr               { return fakeAddr{} }
func (g *gatedConn) SetDeadline(time.Time) error        { return nil }
func (g *gatedConn) SetReadDeadline(time.Time) error    { return nil }
func (g *gatedConn) SetWriteDeadline(time.Time) error   { return nil }

type fakeAddr struct{}

func (fakeAddr) Network() string { return "fake" }
func (fakeAddr) String() string  { return "fake:0" }

// TestReadLoopDispatchBackpressureCancel: with the dispatcher stalled on
// its first reply write, the read loop fills dispatchCh (cap 256) and the
// next send blocks; cancelling the ctx then drives the read loop's
// `case <-ctx.Done(): return` arm in run().
func TestReadLoopDispatchBackpressureCancel(t *testing.T) {
	srv := newServer(slog.New(slog.NewTextHandler(io.Discard, nil)), demoMatrixExport(16, 16))

	// Build a stream of many interrogate frames (each yields a >2-byte
	// reply). dispatchChanCap + a comfortable margin guarantees the read
	// loop blocks on the send select while the dispatcher is stalled.
	one := codec.Pack(codec.EncodeCrosspointInterrogate(codec.CrosspointInterrogateParams{
		MatrixID: 0, LevelID: 0, DestinationID: 1,
	}))
	stream := make([]byte, 0, len(one)*(dispatchChanCap+8))
	for i := 0; i < dispatchChanCap+8; i++ {
		stream = append(stream, one...)
	}
	gc := &gatedConn{data: stream, released: make(chan struct{})}
	sess := newSession(srv, gc)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() { sess.run(ctx); close(runDone) }()

	// Give the read loop time to saturate dispatchCh and block on the
	// select send (the dispatcher is stuck writing the first reply).
	time.Sleep(200 * time.Millisecond)
	cancel() // unblock the read loop via the ctx.Done() send arm

	// Release the gated writes so the stalled dispatcher + run() can exit.
	_ = gc.Close()

	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not return after ctx cancel under backpressure")
	}
}
