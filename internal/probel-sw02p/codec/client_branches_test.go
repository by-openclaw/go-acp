package codec

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// TestMinHelper pins both arms of the min() helper used by readLoop's
// decode-error HexDump clamp: a < b returns a, a >= b returns b
// (including the equal case).
func TestMinHelper(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{1, 2, 1},  // a < b
		{5, 3, 3},  // a > b
		{4, 4, 4},  // a == b
		{64, 64, 64},
		{100, 64, 64},
	}
	for _, tc := range cases {
		if got := min(tc.a, tc.b); got != tc.want {
			t.Errorf("min(%d,%d) = %d; want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestSendMatchedReply drives the successful reply-delivery arm of both
// Send (the select case receiving on waiter.reply) and dispatch (the
// matched-frame reply-send `return`). Client B emits a frame that
// Client A's in-flight Send matcher accepts.
func TestSendMatchedReply(t *testing.T) {
	a, b := net.Pipe()
	logger := quietLogger()
	cfg := ClientConfig{WireHexLog: boolPtr(false)}
	clientA := NewClientFromConn(a, logger, cfg)
	clientB := NewClientFromConn(b, logger, cfg)
	defer func() { _ = clientA.Close() }()
	defer func() { _ = clientB.Close() }()

	// A sends a request and waits for a TxConnectOnGoAck reply.
	replyCh := make(chan Frame, 1)
	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		f, err := clientA.Send(ctx,
			Frame{ID: RxConnectOnGo, Payload: []byte{0x00, 0x01, 0x02}},
			func(f Frame) bool { return f.ID == TxConnectOnGoAck })
		if err != nil {
			errCh <- err
			return
		}
		replyCh <- f
	}()

	// B reads A's request, then sends the matching ack back.
	var wg sync.WaitGroup
	wg.Add(1)
	clientB.Subscribe(func(req Frame) {
		defer wg.Done()
		if req.ID != RxConnectOnGo {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		ack := EncodeConnectOnGoAck(ConnectOnGoAckParams{Destination: 1, Source: 2})
		_, _ = clientB.Send(ctx, ack, nil)
	})

	select {
	case f := <-replyCh:
		if f.ID != TxConnectOnGoAck {
			t.Errorf("matched reply ID = %#x; want TxConnectOnGoAck", f.ID)
		}
	case err := <-errCh:
		t.Fatalf("Send returned error: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("Send never received its matched reply")
	}
}

// TestReadLoopPartialFrameAccumulates covers readLoop's
// io.ErrUnexpectedEOF break: a frame split across two writes must not
// dispatch until the whole frame has arrived, exercising the
// "accumulate more bytes" branch.
func TestReadLoopPartialFrameAccumulates(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})
	defer func() { _ = cli.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	cli.Subscribe(func(f Frame) {
		if f.ID == RxConnectOnGo {
			wg.Done()
		}
	})

	frame := Pack(Frame{ID: RxConnectOnGo, Payload: []byte{0x00, 0x01, 0x02}})
	// Split so the first write delivers SOM+cmd+1 byte (>=3, so the
	// scan loop runs) but the full frame has not arrived → Unpack
	// returns io.ErrUnexpectedEOF and readLoop breaks to read more.
	go func() {
		_, _ = b.Write(frame[:3])
		time.Sleep(30 * time.Millisecond)
		_, _ = b.Write(frame[3:])
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("split frame never reassembled + dispatched")
	}
}

// TestReadLoopDecodeErrorShortBuffer feeds a corrupted-checksum frame
// SHORTER than 64 bytes so readLoop's decode-error HexDump clamp calls
// min() on the (len < 64) side, complementing
// TestReadLoopDecodeErrorResync which exercises the (len > 64) side.
func TestReadLoopDecodeErrorShortBuffer(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})
	defer func() { _ = cli.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	cli.Subscribe(func(f Frame) {
		if f.ID == RxConnectOnGo && len(f.Payload) == 3 && f.Payload[0] == 0x07 {
			wg.Done()
		}
	})

	bad := Pack(Frame{ID: RxConnectOnGo, Payload: []byte{0x00, 0x01, 0x02}})
	bad[len(bad)-1] ^= 0x7F // corrupt checksum (< 64-byte buffer)
	good := Pack(Frame{ID: RxConnectOnGo, Payload: []byte{0x07, 0x08, 0x09}})
	go func() {
		_, _ = b.Write(bad)
		_, _ = b.Write(good)
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("valid frame after short-buffer decode error never dispatched")
	}
}

// TestFailPendingReplySlotFull covers the default arm of failPending:
// when the reader exits but the in-flight waiter's reply channel is
// already full (buffered cap 1, slot taken), the error-send must fall
// through to default rather than block. We install a pending waiter with
// a pre-filled reply channel, then close the peer so the reader exits
// and failPending runs.
func TestFailPendingReplySlotFull(t *testing.T) {
	a, b := net.Pipe()
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})
	defer func() { _ = cli.Close() }()

	waiter := &pendingWaiter{
		match: func(Frame) bool { return true },
		reply: make(chan replyResult, 1),
	}
	waiter.reply <- replyResult{} // pre-fill so failPending hits default
	cli.mu.Lock()
	cli.pending = waiter
	cli.mu.Unlock()

	// Peer close → reader Read returns io.EOF → readLoop calls
	// failPending(err); the reply slot is full so the select default arm
	// runs without blocking.
	_ = b.Close()

	select {
	case <-cli.readerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit; failPending may have blocked")
	}
	// The pre-filled value must still be the one in the channel (the EOF
	// send was dropped via default).
	select {
	case r := <-waiter.reply:
		if r.err != nil {
			t.Errorf("reply slot held err %v; want the pre-filled zero value", r.err)
		}
	default:
		t.Error("reply channel unexpectedly empty")
	}
}

// TestReadLoopExitOnEOF confirms the reader goroutine exits cleanly
// (closing readerDone) when the peer closes, exercising the read-error
// return arm of readLoop.
func TestReadLoopExitOnEOF(t *testing.T) {
	a, b := net.Pipe()
	cli := NewClientFromConn(a, quietLogger(), ClientConfig{WireHexLog: boolPtr(false)})
	_ = b.Close() // peer close → Read returns io.EOF → reader exits

	select {
	case <-cli.readerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit after peer close")
	}
	if err := cli.Close(); err != nil && err != io.EOF {
		t.Errorf("Close after reader exit: %v", err)
	}
}
