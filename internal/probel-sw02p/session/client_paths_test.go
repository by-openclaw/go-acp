package session

import (
	"context"
	"dhs/internal/probel-sw02p/codec"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
)

// quietLogger returns a logger that discards output so the wire-hex
// INFO logs don't spam test output.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestDialError covers the Dial error path: dialing an address with no
// listener returns a wrapped error and a nil Client. Uses 127.0.0.1
// port 1 (privileged, never listening in CI) with a tiny timeout so the
// test stays fast.
func TestDialError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	cli, err := Dial(ctx, nil, "127.0.0.1:1", quietLogger(), ClientConfig{
		DialTimeout: 200 * time.Millisecond,
	})
	if err == nil {
		_ = cli.Close()
		t.Fatal("Dial to dead port: got nil err; want connection error")
	}
	if cli != nil {
		t.Errorf("Dial error returned non-nil client %v", cli)
	}
}

// TestDialDefaultsAndNilLogger drives Dial with a zero ClientConfig and
// nil logger so the default-DialTimeout, default-ReadBufferSize, and
// nil-logger fallback branches all execute against a real loopback
// listener.
func TestDialDefaultsAndNilLogger(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := l.Accept()
		accepted <- c
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// nil logger + zero cfg → default timeout / buffer branches.
	cli, err := Dial(ctx, nil, l.Addr().String(), nil, ClientConfig{})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = cli.Close() }()
	srv := <-accepted
	if srv != nil {
		defer func() { _ = srv.Close() }()
	}
}

// TestNewClientFromConnNilLoggerDefaultBuffer covers the nil-logger and
// zero-ReadBufferSize default arms of NewClientFromConn (the loopback
// test already covers the non-default path).
func TestNewClientFromConnNilLoggerDefaultBuffer(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	cli := NewClientFromConn(a, nil, ClientConfig{}) // nil logger, zero buffer
	if cli.logger == nil {
		t.Error("nil logger was not replaced with slog.Default()")
	}
	_ = cli.Close()
}

// TestSendInFlight covers the ErrSendInFlight guard: a second Send
// while the first is still awaiting a reply must be rejected. The first
// Send uses a matcher (so it parks on waiter.reply) and never gets a
// reply within the window.
func TestSendInFlight(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	// Drain whatever the client writes so Write() never blocks.
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()

	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})
	defer func() { _ = cli.Close() }()

	// First Send parks waiting for a reply that never comes.
	started := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		close(started)
		_, _ = cli.Send(ctx, codec.Frame{ID: codec.RxConnectOnGo, Payload: []byte{0, 1, 2}},
			func(codec.Frame) bool { return false })
	}()
	<-started
	// Give the first Send time to install c.pending.
	waitFor(t, func() bool {
		cli.mu.Lock()
		defer cli.mu.Unlock()
		return cli.pending != nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := cli.Send(ctx, codec.Frame{ID: codec.RxGo, Payload: []byte{0}}, func(codec.Frame) bool { return true })
	if !errors.Is(err, ErrSendInFlight) {
		t.Errorf("second Send: got %v; want ErrSendInFlight", err)
	}
}

// TestSendOnClosedClient covers the closed/nil-conn guard in Send: a
// Send after Close returns net.ErrClosed.
func TestSendOnClosedClient(t *testing.T) {
	a, b := net.Pipe()
	_ = b.Close()
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})
	_ = cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := cli.Send(ctx, codec.Frame{ID: codec.RxGo, Payload: []byte{0}}, nil)
	if !errors.Is(err, net.ErrClosed) {
		t.Errorf("Send on closed client: got %v; want net.ErrClosed", err)
	}
}

// TestSendWriteError covers the conn.Write error arm of Send: closing
// the peer end mid-flight makes the pipe Write fail.
func TestSendWriteError(t *testing.T) {
	a, b := net.Pipe()
	// Close the peer so any Write on a returns io.ErrClosedPipe.
	_ = b.Close()
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})
	defer func() { _ = cli.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := cli.Send(ctx, codec.Frame{ID: codec.RxGo, Payload: []byte{0}}, nil)
	if err == nil {
		t.Fatal("Send with dead peer: got nil; want write error")
	}
}

// TestSendCtxCancel covers the ctx.Done() arm of Send: a Send with a
// matcher whose reply never arrives returns ctx.Err() when the context
// deadline elapses.
func TestSendCtxCancel(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})
	defer func() { _ = cli.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := cli.Send(ctx, codec.Frame{ID: codec.RxConnectOnGo, Payload: []byte{0, 1, 2}},
		func(codec.Frame) bool { return false })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Send ctx cancel: got %v; want context.DeadlineExceeded", err)
	}
}

// TestSendWireHexLogObserver covers the WireHexLog INFO branch and the
// OnTx observer callback in Send (match == nil fire-and-forget path).
func TestSendWireHexLogObserver(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	go func() {
		buf := make([]byte, 256)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()

	var txSeen [][]byte
	var mu sync.Mutex
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{
		WireHexLog: boolPtr(true), // exercise the hex-log branch
		OnTx:       func(raw []byte) { mu.Lock(); txSeen = append(txSeen, raw); mu.Unlock() },
	})
	defer func() { _ = cli.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := cli.Send(ctx, codec.Frame{ID: codec.RxGo, Payload: []byte{0}}, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(txSeen) != 1 {
		t.Errorf("OnTx fired %d times; want 1", len(txSeen))
	}
}

// TestWrite covers Client.Write: the happy path (OnTx observer fires +
// bytes hit the wire) and the closed-client guard (net.ErrClosed).
func TestWrite(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()

	var got []byte
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 16)
		n, _ := b.Read(buf)
		got = append(got, buf[:n]...)
		close(done)
	}()

	var observed []byte
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{
		WireHexLog: boolPtr(false),
		OnTx:       func(raw []byte) { observed = append(observed, raw...) },
	})

	raw := []byte{codec.SOM, 0x07, 0x79}
	if err := cli.Write(raw); err != nil {
		t.Fatalf("Write: %v", err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("peer never received Write bytes")
	}
	if len(observed) != len(raw) {
		t.Errorf("OnTx observed %d bytes; want %d", len(observed), len(raw))
	}

	// Closed client → net.ErrClosed.
	_ = cli.Close()
	if err := cli.Write(raw); !errors.Is(err, net.ErrClosed) {
		t.Errorf("Write on closed client: got %v; want net.ErrClosed", err)
	}
}

// TestCloseIdempotentAndWakesPending covers Close's pending-waiter
// wakeup arm and the already-closed early return (double Close).
func TestCloseIdempotentAndWakesPending(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()
	// The peer signals after its FIRST successful read: Send registers
	// c.pending BEFORE conn.Write, so pending != nil alone does not
	// prove the write finished — Close racing the in-flight write made
	// the parked Send return a pipe-write error instead of the EOF
	// wakeup (macOS main red, run 32537698497; ADR-0029 determinism
	// rule). net.Pipe delivers one Write to one Read, so the first
	// read = the whole frame is out of Send's hands.
	frameRead := make(chan struct{})
	go func() {
		buf := make([]byte, 256)
		first := true
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
			if first {
				first = false
				close(frameRead)
			}
		}
	}()
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})

	// Park a Send so c.pending is non-nil when Close fires.
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := cli.Send(ctx, codec.Frame{ID: codec.RxConnectOnGo, Payload: []byte{0, 1, 2}},
			func(codec.Frame) bool { return false })
		done <- err
	}()
	waitFor(t, func() bool {
		cli.mu.Lock()
		defer cli.mu.Unlock()
		return cli.pending != nil
	})
	select {
	case <-frameRead:
	case <-time.After(2 * time.Second):
		t.Fatal("peer never read the parked Send's frame")
	}

	if err := cli.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	// The parked Send must be woken with io.EOF from Close's pending arm.
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Errorf("parked Send woke with %v; want io.EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not wake the pending Send")
	}

	// Second Close → already-closed early return (returns nil).
	if err := cli.Close(); err != nil {
		t.Errorf("second Close: got %v; want nil", err)
	}
}

// TestReadLoopDispatchAndHexLog drives the readLoop happy path with
// WireHexLog enabled and an OnRx observer, then sends a desync byte
// followed by a real frame to exercise the resync (drop-one-byte) arm.
func TestReadLoopDispatchAndHexLog(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()

	var rxCount int
	var mu sync.Mutex
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{
		WireHexLog: boolPtr(true), // exercise RX hex-log branch
		OnRx:       func([]byte) { mu.Lock(); rxCount++; mu.Unlock() },
	})
	defer func() { _ = cli.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	cli.Subscribe(func(f codec.Frame) {
		if f.ID == codec.RxConnectOnGo {
			wg.Done()
		}
	})

	// Write a stray non-codec.SOM byte (desync → dropped) followed by a valid
	// frame so the reader resyncs and dispatches it.
	frame := codec.Pack(codec.Frame{ID: codec.RxConnectOnGo, Payload: []byte{0x00, 0x01, 0x02}})
	go func() {
		_, _ = b.Write([]byte{0x00}) // desync byte
		_, _ = b.Write(frame)
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("frame after desync byte never dispatched")
	}
	mu.Lock()
	defer mu.Unlock()
	if rxCount == 0 {
		t.Error("OnRx never fired")
	}
}

// TestReadLoopDecodeErrorResync feeds a frame with a bad checksum so
// codec.Unpack returns ErrBadChecksum; the reader logs a decode error, drops
// one byte, and keeps scanning. A following valid frame must still be
// dispatched. The bad frame is padded > 64 bytes so the min() helper in
// the decode-error codec.HexDump path is exercised.
func TestReadLoopDecodeErrorResync(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()

	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})
	defer func() { _ = cli.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	cli.Subscribe(func(f codec.Frame) {
		if f.ID == codec.RxConnectOnGo {
			wg.Done()
		}
	})

	// Build a long codec.SOM-led buffer that decodes as an unknown/short
	// command so codec.Unpack errors and the reader drops bytes one at a
	// time. Use a known-command frame with a corrupted checksum, padded
	// with trailing junk > 64 bytes so codec.HexDump's min(len,64) clamp runs.
	bad := codec.Pack(codec.Frame{ID: codec.RxConnectOnGo, Payload: []byte{0x00, 0x01, 0x02}})
	bad[len(bad)-1] ^= 0x7F  // corrupt checksum → ErrBadChecksum
	junk := make([]byte, 80) // > 64 so min() picks 64
	good := codec.Pack(codec.Frame{ID: codec.RxConnectOnGo, Payload: []byte{0x03, 0x04, 0x05}})

	go func() {
		_, _ = b.Write(bad)
		_, _ = b.Write(junk)
		_, _ = b.Write(good)
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("valid frame after decode error never dispatched")
	}
}

// TestDispatchReplySlotFilled covers the "reply slot already filled"
// fall-through arm of dispatch: a matched frame whose waiter.reply
// channel is already full falls through to the async listeners instead
// of blocking. We pre-fill the reply channel, then drive a matching
// frame through dispatch and assert a listener still receives it.
func TestDispatchReplySlotFilled(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})
	defer func() { _ = cli.Close() }()

	// Install a pending waiter whose reply channel is already full, with
	// a matcher that matches everything.
	waiter := &pendingWaiter{
		match: func(codec.Frame) bool { return true },
		reply: make(chan replyResult, 1),
	}
	waiter.reply <- replyResult{} // pre-fill so the select hits default
	cli.mu.Lock()
	cli.pending = waiter
	cli.mu.Unlock()

	got := make(chan codec.Frame, 1)
	cli.Subscribe(func(f codec.Frame) { got <- f })

	cli.dispatch(codec.Frame{ID: codec.RxConnectOnGo, Payload: []byte{0, 1, 2}})
	select {
	case f := <-got:
		if f.ID != codec.RxConnectOnGo {
			t.Errorf("listener got %#x; want codec.RxConnectOnGo", f.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("matched-but-full frame did not fall through to listeners")
	}
}

// boolPtr is a tiny helper for the *bool WireHexLog config knob.
func boolPtr(b bool) *bool { return &b }

// waitFor spins until cond() is true or a 2s deadline elapses.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("waitFor: condition never became true")
}
