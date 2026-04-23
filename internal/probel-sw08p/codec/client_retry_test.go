package codec

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// discardLogger returns an slog.Logger that throws output away.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakePeer runs a read loop on conn, decodes full SW-P-08 frames, and
// invokes onFrame for each one. Replies (ACK / NAK / data frames) are
// written back to conn under the test's control via the returned Peer.
// Caller closes with Peer.Close.
type fakePeer struct {
	conn    net.Conn
	done    chan struct{}
	onFrame func(*fakePeer, Frame)
}

func newFakePeer(conn net.Conn, onFrame func(*fakePeer, Frame)) *fakePeer {
	p := &fakePeer{
		conn:    conn,
		done:    make(chan struct{}),
		onFrame: onFrame,
	}
	go p.loop()
	return p
}

func (p *fakePeer) loop() {
	defer close(p.done)
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 1024)
	for {
		n, err := p.conn.Read(tmp)
		if err != nil {
			return
		}
		buf = append(buf, tmp[:n]...)
		for len(buf) >= 2 {
			if IsACK(buf) || IsNAK(buf) {
				buf = buf[2:]
				continue
			}
			if buf[0] != DLE {
				buf = buf[1:]
				continue
			}
			f, consumed, perr := Unpack(buf)
			if errors.Is(perr, io.ErrUnexpectedEOF) {
				break
			}
			if perr != nil {
				_, _ = p.conn.Write(PackNAK())
				buf = buf[2:]
				continue
			}
			buf = buf[consumed:]
			if p.onFrame != nil {
				p.onFrame(p, f)
			}
		}
	}
}

func (p *fakePeer) writeACK() { _, _ = p.conn.Write(PackACK()) }
func (p *fakePeer) writeNAK() { _, _ = p.conn.Write(PackNAK()) }
func (p *fakePeer) writeFrame(f Frame) {
	_, _ = p.conn.Write(Pack(f))
}
func (p *fakePeer) Close() error { return p.conn.Close() }

// TestSendACKThenReply: peer ACKs, then sends matching reply. Send
// returns the reply frame.
func TestSendACKThenReply(t *testing.T) {
	a, b := net.Pipe()
	disable := false
	client := NewClientFromConn(a, discardLogger(), ClientConfig{WireHexLog: &disable})
	defer func() { _ = client.Close() }()

	peer := newFakePeer(b, func(p *fakePeer, f Frame) {
		p.writeACK()
		p.writeFrame(Frame{ID: TxCrosspointTally, Payload: []byte{0x00, 0x00, 0x07}})
	})
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req := Frame{ID: RxCrosspointInterrogate, Payload: []byte{0x00, 0x00, 0x00, 0x00}}
	reply, err := client.Send(ctx, req, func(f Frame) bool { return f.ID == TxCrosspointTally })
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply.ID != TxCrosspointTally {
		t.Errorf("reply.ID = %#x; want %#x", reply.ID, TxCrosspointTally)
	}
}

// TestSendNAKRetriesThenACK: peer NAKs first 2 attempts, ACKs the 3rd
// and replies. Send returns success + hits onNAK twice, onRetry twice.
func TestSendNAKRetriesThenACK(t *testing.T) {
	a, b := net.Pipe()
	disable := false

	var nakCount, retryCount atomic.Int32
	client := NewClientFromConn(a, discardLogger(), ClientConfig{
		WireHexLog:  &disable,
		ACKTimeout:  500 * time.Millisecond,
		MaxAttempts: 5,
		OnNAK:       func() { nakCount.Add(1) },
		OnRetry:     func(int) { retryCount.Add(1) },
	})
	defer func() { _ = client.Close() }()

	var attempts atomic.Int32
	peer := newFakePeer(b, func(p *fakePeer, f Frame) {
		n := attempts.Add(1)
		if n < 3 {
			p.writeNAK()
			return
		}
		p.writeACK()
		p.writeFrame(Frame{ID: TxDualControllerStatusResponse, Payload: []byte{0x01}})
	})
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req := Frame{ID: RxDualControllerStatusRequest}
	reply, err := client.Send(ctx, req, func(f Frame) bool { return f.ID == TxDualControllerStatusResponse })
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply.ID != TxDualControllerStatusResponse {
		t.Errorf("reply.ID = %#x", reply.ID)
	}
	if attempts.Load() != 3 {
		t.Errorf("peer saw %d frames; want 3", attempts.Load())
	}
	if nakCount.Load() != 2 {
		t.Errorf("OnNAK fired %d times; want 2", nakCount.Load())
	}
	if retryCount.Load() != 2 {
		t.Errorf("OnRetry fired %d times; want 2", retryCount.Load())
	}
}

// TestSendNAKExhausts: peer NAKs every attempt. Send returns
// ErrMaxAttempts.
func TestSendNAKExhausts(t *testing.T) {
	a, b := net.Pipe()
	disable := false

	client := NewClientFromConn(a, discardLogger(), ClientConfig{
		WireHexLog:  &disable,
		ACKTimeout:  200 * time.Millisecond,
		MaxAttempts: 3,
	})
	defer func() { _ = client.Close() }()

	peer := newFakePeer(b, func(p *fakePeer, f Frame) {
		p.writeNAK()
	})
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.Send(ctx, Frame{ID: RxMaintenance, Payload: []byte{0x00}}, nil)
	if !errors.Is(err, ErrMaxAttempts) {
		t.Fatalf("Send err = %v; want ErrMaxAttempts", err)
	}
}

// TestSendTimeoutRetries: peer stays silent for 2 attempts, then ACKs
// on attempt 3. Verifies ack-timeout path uses retry budget.
func TestSendTimeoutRetries(t *testing.T) {
	a, b := net.Pipe()
	disable := false

	var timeoutCount atomic.Int32
	client := NewClientFromConn(a, discardLogger(), ClientConfig{
		WireHexLog:  &disable,
		ACKTimeout:  150 * time.Millisecond,
		MaxAttempts: 5,
		OnTimeout:   func() { timeoutCount.Add(1) },
	})
	defer func() { _ = client.Close() }()

	var attempts atomic.Int32
	peer := newFakePeer(b, func(p *fakePeer, f Frame) {
		n := attempts.Add(1)
		if n < 3 {
			return // silence — force ack-timeout
		}
		p.writeACK()
	})
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.Send(ctx, Frame{ID: RxMaintenance, Payload: []byte{0x00}}, nil)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if timeoutCount.Load() != 2 {
		t.Errorf("OnTimeout fired %d times; want 2", timeoutCount.Load())
	}
}

// TestSendDataTooLarge: payload > 255 bytes returns ErrDataFieldTooLarge
// without writing anything to the peer.
func TestSendDataTooLarge(t *testing.T) {
	a, b := net.Pipe()
	disable := false
	client := NewClientFromConn(a, discardLogger(), ClientConfig{WireHexLog: &disable})
	defer func() { _ = client.Close() }()
	defer func() { _ = b.Close() }()

	big := make([]byte, 260) // ID + 260 > DataCapHard
	_, err := client.Send(context.Background(), Frame{ID: RxMaintenance, Payload: big}, nil)
	if !errors.Is(err, ErrDataFieldTooLarge) {
		t.Fatalf("err = %v; want ErrDataFieldTooLarge", err)
	}
}

// TestSendCapSoftFires: payload between 128 and 255 bytes fires
// OnCapSoft but the frame goes out.
func TestSendCapSoftFires(t *testing.T) {
	a, b := net.Pipe()
	disable := false

	var capCount atomic.Int32
	var capLen atomic.Int32
	client := NewClientFromConn(a, discardLogger(), ClientConfig{
		WireHexLog: &disable,
		OnCapSoft: func(n int) {
			capCount.Add(1)
			capLen.Store(int32(n))
		},
	})
	defer func() { _ = client.Close() }()

	peer := newFakePeer(b, func(p *fakePeer, f Frame) { p.writeACK() })
	defer func() { _ = peer.Close() }()

	payload := make([]byte, 150) // ID (1) + 150 = 151 bytes, between soft (128) and hard (255)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.Send(ctx, Frame{ID: RxMaintenance, Payload: payload}, nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if capCount.Load() != 1 {
		t.Errorf("OnCapSoft fired %d; want 1", capCount.Load())
	}
	if got := capLen.Load(); got != 151 {
		t.Errorf("OnCapSoft saw len=%d; want 151", got)
	}
}

// TestSendSendInFlight: a second Send while one is already awaiting
// ACK returns ErrSendInFlight.
func TestSendSendInFlight(t *testing.T) {
	a, b := net.Pipe()
	disable := false
	client := NewClientFromConn(a, discardLogger(), ClientConfig{
		WireHexLog:  &disable,
		ACKTimeout:  3 * time.Second,
		MaxAttempts: 1,
	})
	defer func() { _ = client.Close() }()

	// Peer stays silent so first Send blocks in ack-wait.
	drain := make(chan struct{})
	go func() {
		defer close(drain)
		io.Copy(io.Discard, b) //nolint:errcheck
	}()
	defer func() { _ = b.Close(); <-drain }()

	firstDone := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = client.Send(ctx, Frame{ID: RxMaintenance, Payload: []byte{0x00}}, nil)
		close(firstDone)
	}()
	// Give the first Send time to register.
	time.Sleep(50 * time.Millisecond)

	_, err := client.Send(context.Background(), Frame{ID: RxMaintenance, Payload: []byte{0x01}}, nil)
	if !errors.Is(err, ErrSendInFlight) {
		t.Fatalf("second Send err = %v; want ErrSendInFlight", err)
	}
	_ = client.Close()
	<-firstDone
}
