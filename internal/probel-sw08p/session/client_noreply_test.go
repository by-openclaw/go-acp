package session

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/probel-sw08p/codec"
)

// TestSendACKNoReplyFailsFast: the peer ACKs an rx 100 name request and never
// answers, as a Neuron Shuffle does for 4-char names. Send must fail with
// ErrNoReply after the reply timeout and fire OnNoReply, not hold the caller
// until its own (much longer) context expires.
func TestSendACKNoReplyFailsFast(t *testing.T) {
	a, b := net.Pipe()
	disable := false
	var fired atomic.Int32
	client := NewClientFromConn(a, discardLogger(), ClientConfig{
		WireHexLog:   &disable,
		ReplyTimeout: 100 * time.Millisecond,
		OnNoReply:    func() { fired.Add(1) },
	})
	defer func() { _ = client.Close() }()

	peer := newFakePeer(b, func(p *fakePeer, f codec.Frame) { p.writeACK() })
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := codec.EncodeAllSourceNamesRequest(codec.AllSourceNamesRequestParams{NameLength: codec.NameLen4})
	start := time.Now()
	_, err := client.Send(ctx, req, func(f codec.Frame) bool { return f.ID == codec.TxSourceNamesResponse })
	if !errors.Is(err, ErrNoReply) {
		t.Fatalf("Send err = %v; want ErrNoReply", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Send took %v; want ~100ms (reply timeout), not the caller's 10s context", d)
	}
	if fired.Load() != 1 {
		t.Errorf("OnNoReply fired %d times; want 1", fired.Load())
	}
}

// TestSendACKReplyWithinReplyTimeout: a reply that lands inside the reply
// timeout is returned as before.
func TestSendACKReplyWithinReplyTimeout(t *testing.T) {
	a, b := net.Pipe()
	disable := false
	client := NewClientFromConn(a, discardLogger(), ClientConfig{WireHexLog: &disable, ReplyTimeout: time.Second})
	defer func() { _ = client.Close() }()

	peer := newFakePeer(b, func(p *fakePeer, f codec.Frame) {
		p.writeACK()
		time.Sleep(50 * time.Millisecond)
		p.writeFrame(codec.Frame{ID: codec.TxCrosspointTally, Payload: []byte{0x00, 0x00, 0x07}})
	})
	defer func() { _ = peer.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := codec.Frame{ID: codec.RxCrosspointInterrogate, Payload: []byte{0x00, 0x00, 0x00, 0x00}}
	reply, err := client.Send(ctx, req, func(f codec.Frame) bool { return f.ID == codec.TxCrosspointTally })
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reply.ID != codec.TxCrosspointTally {
		t.Errorf("reply.ID = %#x; want %#x", reply.ID, codec.TxCrosspointTally)
	}
}
