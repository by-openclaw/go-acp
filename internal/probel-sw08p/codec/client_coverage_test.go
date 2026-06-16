package codec

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This file pins the Client transport arms not reached by the loopback
// and retry tests: dial failure, nil-logger defaulting, the keepalive
// helper's disable / default / setsockopt-error branches, the wire-hex
// log path, the observer callbacks (onTx / onRx), write-after-close,
// reply-phase context cancellation, the reader's ACK/NAK/desync/decode
// routing, and the min helper.

// --- Dial error + nil logger -----------------------------------------------

// TestDialError exercises the dial-failure return (and nil-logger
// default) by dialing an address that cannot connect.
func TestDialError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	// 127.0.0.1:1 is reserved and refuses; nil logger forces the default.
	_, err := Dial(ctx, "127.0.0.1:1", nil, ClientConfig{DialTimeout: 300 * time.Millisecond})
	if err == nil {
		t.Fatal("Dial to closed port returned nil error")
	}
}

// TestNewClientFromConnNilLogger covers the nil-logger default branch of
// NewClientFromConn.
func TestNewClientFromConnNilLogger(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	c := NewClientFromConn(a, nil, ClientConfig{})
	if c.logger == nil {
		t.Error("NewClientFromConn left logger nil")
	}
	_ = c.Close()
}

// --- applyTCPKeepalive branches --------------------------------------------

// TestApplyTCPKeepaliveDisabled covers the period<0 early return.
func TestApplyTCPKeepaliveDisabled(t *testing.T) {
	// Negative period returns before any conn type assertion — a non-TCP
	// conn proves the early return ran (it would otherwise be a no-op too,
	// but the period<0 arm is what we target).
	a, b := net.Pipe()
	defer func() { _ = a.Close(); _ = b.Close() }()
	applyTCPKeepalive(a, -1, discardLogger()) // must not panic
}

// TestApplyTCPKeepaliveNonTCP covers the non-*net.TCPConn early return
// with a zero (defaulting) period.
func TestApplyTCPKeepaliveNonTCP(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close(); _ = b.Close() }()
	applyTCPKeepalive(a, 0, discardLogger()) // net.Pipe is not *net.TCPConn
}

// TestApplyTCPKeepaliveSetKeepAliveError forces the SetKeepAlive error
// arm using a closed *net.TCPConn (setsockopt on a closed socket fails).
func TestApplyTCPKeepaliveSetKeepAliveError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	tc := conn.(*net.TCPConn)
	_ = tc.Close() // close so SetKeepAlive returns an error
	applyTCPKeepalive(tc, 5*time.Second, discardLogger())
}

// TestApplyTCPKeepaliveSetPeriodError forces the SetKeepAlivePeriod
// error arm via the transparent seam: SetKeepAlive succeeds, but
// SetKeepAlivePeriod returns an error. This arm is unreachable on a
// live, open *net.TCPConn (whenever the socket is healthy both calls
// succeed; once SetKeepAlive errors the helper returns early), so the
// seam is the only way to exercise it. Logic is unchanged — both seam
// vars are nil in production.
func TestApplyTCPKeepaliveSetPeriodError(t *testing.T) {
	origEnable, origPeriod := setKeepAlive, setKeepAlivePeriod
	setKeepAlive = func(*net.TCPConn, bool) error { return nil }
	setKeepAlivePeriod = func(*net.TCPConn, time.Duration) error {
		return errors.New("forced setsockopt failure")
	}
	defer func() { setKeepAlive, setKeepAlivePeriod = origEnable, origPeriod }()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	tc := conn.(*net.TCPConn)
	defer func() { _ = tc.Close() }()
	applyTCPKeepalive(tc, 5*time.Second, discardLogger())
}

// --- Close double-call -----------------------------------------------------

// TestCloseIdempotent covers the already-closed early return in Close.
func TestCloseIdempotent(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	c := NewClientFromConn(a, discardLogger(), ClientConfig{})
	if err := c.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// --- Send: closed client ---------------------------------------------------

// TestSendAfterClose covers the closed-conn guard at the top of Send.
func TestSendAfterClose(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	c := NewClientFromConn(a, discardLogger(), ClientConfig{})
	_ = c.Close()
	_, err := c.Send(context.Background(), Frame{ID: RxMaintenance, Payload: []byte{0}}, nil)
	if !errors.Is(err, net.ErrClosed) {
		t.Errorf("Send after Close = %v; want net.ErrClosed", err)
	}
}

// --- Send: write error -----------------------------------------------------

// TestSendWriteError covers the conn.Write error return inside Send: the
// peer end is closed so the write fails.
func TestSendWriteError(t *testing.T) {
	a, b := net.Pipe()
	_ = b.Close() // peer closed → write on a fails
	c := NewClientFromConn(a, discardLogger(), ClientConfig{})
	defer func() { _ = c.Close() }()
	_, err := c.Send(context.Background(), Frame{ID: RxMaintenance, Payload: []byte{0}}, nil)
	if err == nil {
		t.Error("Send with closed peer returned nil; want write error")
	}
}

// --- Send: wire-hex log + onTx callback ------------------------------------

// TestSendWireHexLogAndOnTx exercises the hex-log TX path (WireHexLog
// true is the default) plus the onTx observer. The peer ACKs so Send
// completes the no-match fast path.
func TestSendWireHexLogAndOnTx(t *testing.T) {
	a, b := net.Pipe()
	var txCount atomic.Int32
	// WireHexLog defaults to true (cfg.WireHexLog nil) → hex log path runs.
	c := NewClientFromConn(a, discardLogger(), ClientConfig{
		OnTx: func([]byte) { txCount.Add(1) },
	})
	defer func() { _ = c.Close() }()

	peer := newFakePeer(b, func(p *fakePeer, f Frame) { p.writeACK() })
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Send(ctx, Frame{ID: RxMaintenance, Payload: []byte{0x00}}, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if txCount.Load() == 0 {
		t.Error("onTx never fired")
	}
}

// --- Send: context cancel in the reply phase -------------------------------

// TestSendCtxCancelInReplyPhase drives the ctx.Done() arm in Send's
// reply-wait select: the peer ACKs (so Send advances past ack-wait) but
// never sends the matching reply, then the context is cancelled.
func TestSendCtxCancelInReplyPhase(t *testing.T) {
	a, b := net.Pipe()
	c := NewClientFromConn(a, discardLogger(), ClientConfig{})
	defer func() { _ = c.Close() }()

	peer := newFakePeer(b, func(p *fakePeer, f Frame) {
		p.writeACK() // ACK only; no matching reply ever sent
	})
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := c.Send(ctx, Frame{ID: RxCrosspointInterrogate, Payload: []byte{0, 0, 0, 0}},
		func(f Frame) bool { return f.ID == TxCrosspointTally })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Send reply-phase = %v; want context.DeadlineExceeded", err)
	}
}

// --- Send: ack-timeout single attempt -> ErrMaxAttempts --------------------

// TestSendAckTimeoutSingleAttempt covers the attempt==maxAttempts
// timeout return arm (MaxAttempts 1, peer silent).
func TestSendAckTimeoutSingleAttempt(t *testing.T) {
	a, b := net.Pipe()
	var toCount atomic.Int32
	c := NewClientFromConn(a, discardLogger(), ClientConfig{
		ACKTimeout:  100 * time.Millisecond,
		MaxAttempts: 1,
		OnTimeout:   func() { toCount.Add(1) },
	})
	defer func() { _ = c.Close() }()

	// Drain writes so Send's Write doesn't block; never reply.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()
	defer func() { _ = b.Close() }()

	_, err := c.Send(context.Background(), Frame{ID: RxMaintenance, Payload: []byte{0}}, nil)
	if !errors.Is(err, ErrMaxAttempts) {
		t.Errorf("Send = %v; want ErrMaxAttempts", err)
	}
	if toCount.Load() != 1 {
		t.Errorf("OnTimeout fired %d; want 1", toCount.Load())
	}
}

// --- Send: timeout retry fires OnRetry -------------------------------------

// TestSendTimeoutFiresOnRetry covers the onRetry callback on the
// ack-timeout (non-final) retry path: the peer stays silent for the
// first attempt, then ACKs the second.
func TestSendTimeoutFiresOnRetry(t *testing.T) {
	a, b := net.Pipe()
	var retryCount atomic.Int32
	c := NewClientFromConn(a, discardLogger(), ClientConfig{
		ACKTimeout:  120 * time.Millisecond,
		MaxAttempts: 3,
		OnRetry:     func(int) { retryCount.Add(1) },
	})
	defer func() { _ = c.Close() }()

	var attempts atomic.Int32
	peer := newFakePeer(b, func(p *fakePeer, f Frame) {
		if attempts.Add(1) < 2 {
			return // silence → ack-timeout → retry
		}
		p.writeACK()
	})
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.Send(ctx, Frame{ID: RxMaintenance, Payload: []byte{0}}, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if retryCount.Load() == 0 {
		t.Error("OnRetry never fired on ack-timeout retry")
	}
}

// --- Send: context cancel during the ack-wait phase ------------------------

// TestSendCtxCancelInAckPhase drives the ctx.Done() arm of the first
// (ack-wait) select: the peer never ACKs and the context is cancelled
// before the ack timer fires.
func TestSendCtxCancelInAckPhase(t *testing.T) {
	a, b := net.Pipe()
	c := NewClientFromConn(a, discardLogger(), ClientConfig{
		ACKTimeout:  5 * time.Second, // long, so ctx cancel wins
		MaxAttempts: 1,
	})
	defer func() { _ = c.Close() }()

	// Drain writes; never ACK.
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()
	defer func() { _ = b.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := c.Send(ctx, Frame{ID: RxMaintenance, Payload: []byte{0}}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Send ack-phase = %v; want context.DeadlineExceeded", err)
	}
}

// --- Send: zero maxAttempts hits the post-loop fallback --------------------

// TestSendZeroMaxAttempts white-box-forces maxAttempts to 0 so the retry
// loop body never executes and Send returns the defensive post-loop
// ErrMaxAttempts. The production clamp in newClient keeps maxAttempts >=
// 1, so this fallback is unreachable through the public constructor; the
// test pokes the field directly to keep the guard exercised without
// weakening it.
func TestSendZeroMaxAttempts(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	c := NewClientFromConn(a, discardLogger(), ClientConfig{})
	defer func() { _ = c.Close() }()

	c.mu.Lock()
	c.maxAttempts = 0
	c.mu.Unlock()

	_, err := c.Send(context.Background(), Frame{ID: RxMaintenance, Payload: []byte{0}}, nil)
	if !errors.Is(err, ErrMaxAttempts) {
		t.Errorf("Send with maxAttempts=0 = %v; want ErrMaxAttempts", err)
	}
}

// --- readLoop: partial frame waits for more bytes --------------------------

// TestReadLoopPartialFrame feeds the reader a truncated frame prefix
// (valid SOM but no EOM yet) so Unpack returns io.ErrUnexpectedEOF and
// the reader breaks to wait for more bytes; the rest of the frame is
// then delivered and dispatched.
func TestReadLoopPartialFrame(t *testing.T) {
	a, b := net.Pipe()
	var got atomic.Bool
	c := NewClientFromConn(a, discardLogger(), ClientConfig{
		OnEvent: func(_ *Client, f Frame) {
			if f.ID == TxCrosspointTally {
				got.Store(true)
			}
		},
	})
	defer func() { _ = c.Close() }()

	// Drain the client's auto-ACK so its Write doesn't block.
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()

	full := Pack(Frame{ID: TxCrosspointTally, Payload: []byte{0, 0, 7}})
	// Write the frame in two halves so the reader sees a partial frame
	// first (io.ErrUnexpectedEOF → break → accumulate).
	if _, err := b.Write(full[:3]); err != nil {
		t.Fatalf("write part 1: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := b.Write(full[3:]); err != nil {
		t.Fatalf("write part 2: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for !got.Load() {
		select {
		case <-deadline:
			t.Fatal("partial frame never reassembled + dispatched")
		case <-time.After(10 * time.Millisecond):
		}
	}
	_ = b.Close()
}

// --- Send: reply-before-ACK (OnNoACK) --------------------------------------

// TestSendReplyBeforeACK covers the spec-deviation arm where a matching
// reply arrives before the peer's DLE ACK.
func TestSendReplyBeforeACK(t *testing.T) {
	a, b := net.Pipe()
	var noackCount atomic.Int32
	c := NewClientFromConn(a, discardLogger(), ClientConfig{
		OnNoACK: func() { noackCount.Add(1) },
	})
	defer func() { _ = c.Close() }()

	peer := newFakePeer(b, func(p *fakePeer, f Frame) {
		// Reply directly, no ACK first.
		p.writeFrame(Frame{ID: TxCrosspointTally, Payload: []byte{0, 0, 7}})
	})
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reply, err := c.Send(ctx, Frame{ID: RxCrosspointInterrogate, Payload: []byte{0, 0, 0, 0}},
		func(f Frame) bool { return f.ID == TxCrosspointTally })
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply.ID != TxCrosspointTally {
		t.Errorf("reply.ID = %#x; want TxCrosspointTally", byte(reply.ID))
	}
	if noackCount.Load() != 1 {
		t.Errorf("OnNoACK fired %d; want 1", noackCount.Load())
	}
}

// --- Write: closed + onTx --------------------------------------------------

// TestWriteAfterClose covers Write's closed-conn guard.
func TestWriteAfterClose(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	c := NewClientFromConn(a, discardLogger(), ClientConfig{})
	_ = c.Close()
	if err := c.Write(PackACK()); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Write after Close = %v; want net.ErrClosed", err)
	}
}

// TestWriteFiresOnTx covers Write's onTx callback path.
func TestWriteFiresOnTx(t *testing.T) {
	a, b := net.Pipe()
	var seen atomic.Int32
	c := NewClientFromConn(a, discardLogger(), ClientConfig{
		OnTx: func([]byte) { seen.Add(1) },
	})
	defer func() { _ = c.Close() }()

	// Drain so Write doesn't block.
	go func() {
		buf := make([]byte, 64)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()
	defer func() { _ = b.Close() }()

	if err := c.Write(PackACK()); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if seen.Load() != 1 {
		t.Errorf("onTx fired %d; want 1", seen.Load())
	}
}

// --- readLoop: ACK/NAK onRx, hex RX log, onRx, desync, decode-error NAK -----

// TestReadLoopRoutesACKNAKAndFrames feeds the client's reader a stream
// containing: a desync junk byte, a DLE ACK, a DLE NAK, a well-framed
// data frame (→ onRx + hex log + dispatch + auto-ACK), and a malformed
// frame (→ DLE NAK emission). All inbound observer + routing arms run.
func TestReadLoopRoutesACKNAKAndFrames(t *testing.T) {
	a, b := net.Pipe()
	var rxCount atomic.Int32
	var gotFrame atomic.Bool

	// WireHexLog nil → defaults true → hex RX log path runs.
	c := NewClientFromConn(a, discardLogger(), ClientConfig{
		OnRx: func([]byte) { rxCount.Add(1) },
		OnEvent: func(_ *Client, f Frame) {
			if f.ID == TxCrosspointTally {
				gotFrame.Store(true)
			}
		},
	})
	defer func() { _ = c.Close() }()

	// Collect bytes the client writes back (auto-ACK + DLE NAK).
	var mu sync.Mutex
	var back []byte
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 256)
		for {
			n, err := b.Read(buf)
			if n > 0 {
				mu.Lock()
				back = append(back, buf[:n]...)
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	// Build the inbound stream.
	stream := []byte{0xAA} // desync junk byte (not DLE)
	stream = append(stream, PackACK()...)
	stream = append(stream, PackNAK()...)
	stream = append(stream, Pack(Frame{ID: TxCrosspointTally, Payload: []byte{0, 0, 7}})...)
	// Malformed frame: valid SOM/EOM framing but bad checksum → reader NAKs.
	bad := []byte{0x10, 0x02, 0x07, 0x01, 0x00, 0x10, 0x03} // wrong CHK
	stream = append(stream, bad...)

	if _, err := b.Write(stream); err != nil {
		t.Fatalf("write stream: %v", err)
	}

	// Wait for the data frame to be dispatched.
	deadline := time.After(2 * time.Second)
	for !gotFrame.Load() {
		select {
		case <-deadline:
			t.Fatal("data frame never dispatched")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if rxCount.Load() < 3 { // ACK + NAK + data frame all call onRx
		t.Errorf("onRx fired %d; want >= 3", rxCount.Load())
	}

	_ = c.Close()
	_ = b.Close()
	<-readerDone

	// The reader must have emitted at least one DLE NAK for the bad frame.
	mu.Lock()
	sawNAK := containsNAK(back)
	mu.Unlock()
	if !sawNAK {
		t.Error("reader never emitted DLE NAK for the malformed frame")
	}
}

// containsNAK reports whether buf contains a DLE NAK sequence.
func containsNAK(buf []byte) bool {
	for i := 0; i+1 < len(buf); i++ {
		if buf[i] == DLE && buf[i+1] == NAK {
			return true
		}
	}
	return false
}

// --- dispatch: duplicate reply falls through to listeners ------------------

// TestDispatchDuplicateReplyFallsThrough exercises dispatch's
// "reply slot already filled" default arm: a matcher claims the first
// frame, a second matching frame finds the buffered reply slot full and
// falls through to the listener fan-out.
func TestDispatchDuplicateReplyFallsThrough(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	c := NewClientFromConn(a, discardLogger(), ClientConfig{})
	defer func() { _ = c.Close() }()

	var listenerHits atomic.Int32
	c.Subscribe(func(Frame) { listenerHits.Add(1) })

	// Install a pending waiter whose reply slot we pre-fill, then dispatch
	// a matching frame so the select default arm (slot full) runs and the
	// frame fans out to the listener.
	waiter := &pendingWaiter{
		match: func(f Frame) bool { return f.ID == TxCrosspointTally },
		reply: make(chan replyResult, 1),
	}
	waiter.reply <- replyResult{} // pre-fill the slot
	c.mu.Lock()
	c.pending = waiter
	c.mu.Unlock()

	c.dispatch(Frame{ID: TxCrosspointTally, Payload: []byte{0, 0, 1}})

	if listenerHits.Load() != 1 {
		t.Errorf("listener fired %d; want 1 (fall-through)", listenerHits.Load())
	}
}

// --- failPending: full reply slot default arm ------------------------------

// TestFailPendingFullSlot drives failPending's select default arm: the
// waiter's reply slot is already full, so failPending cannot enqueue and
// falls through without blocking.
func TestFailPendingFullSlot(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	c := NewClientFromConn(a, discardLogger(), ClientConfig{})
	defer func() { _ = c.Close() }()

	waiter := &pendingWaiter{reply: make(chan replyResult, 1)}
	waiter.reply <- replyResult{} // pre-fill
	c.mu.Lock()
	c.pending = waiter
	c.mu.Unlock()

	c.failPending(errors.New("boom")) // must not block / panic
}

// --- min helper ------------------------------------------------------------

// TestMinHelper covers both branches of the package min helper.
func TestMinHelper(t *testing.T) {
	if got := min(2, 5); got != 2 {
		t.Errorf("min(2,5) = %d; want 2", got)
	}
	if got := min(9, 4); got != 4 {
		t.Errorf("min(9,4) = %d; want 4", got)
	}
}
