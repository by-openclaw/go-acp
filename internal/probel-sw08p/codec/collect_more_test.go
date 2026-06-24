package codec

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

// collectPeer wires two Clients over a net.Pipe; clientB runs `reply` (in its
// own goroutine) when it sees a Crosspoint Tally Dump Request. The returned
// clientA is the system under test.
func collectPeer(t *testing.T, reply func(b *Client)) (*Client, func()) {
	t.Helper()
	a, b := net.Pipe()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	cfg := ClientConfig{WireHexLog: &disable}
	ca := NewClientFromConn(a, logger, cfg)
	cb := NewClientFromConn(b, logger, cfg)
	cb.Subscribe(func(f Frame) {
		if f.ID != RxCrosspointTallyDumpRequest {
			return
		}
		go reply(cb)
	})
	return ca, func() { _ = ca.Close(); _ = cb.Close() }
}

func wordFrame(i int) Frame {
	return Frame{ID: TxCrosspointTallyDumpWord, Payload: []byte{byte(i)}}
}

// TestSendCollect_NilCollectZeroGap covers the two default-application
// branches: collect==nil → falls back to match, and idleGap<=0 → 750ms.
func TestSendCollect_NilCollectZeroGap(t *testing.T) {
	ca, cleanup := collectPeer(t, func(b *Client) {
		_, _ = b.Send(context.Background(), wordFrame(1), nil)
	})
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := Frame{ID: RxCrosspointTallyDumpRequest, Payload: []byte{0}}
	match := func(f Frame) bool { return f.ID == TxCrosspointTallyDumpWord }
	frames, err := ca.SendCollect(ctx, req, match, nil, 0, nil)
	if err != nil {
		t.Fatalf("SendCollect: %v", err)
	}
	if len(frames) < 1 {
		t.Fatalf("collected %d frames; want >=1", len(frames))
	}
}

// TestSendCollect_DropsRejectedFrame covers the collector's reject path: a
// frame that arrives but fails collect() is dropped, not gathered.
func TestSendCollect_DropsRejectedFrame(t *testing.T) {
	ca, cleanup := collectPeer(t, func(b *Client) {
		_, _ = b.Send(context.Background(), wordFrame(1), nil)
		_, _ = b.Send(context.Background(), Frame{ID: TxCrosspointTallyDumpByte, Payload: []byte{2}}, nil)
	})
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := Frame{ID: RxCrosspointTallyDumpRequest, Payload: []byte{0}}
	match := func(f Frame) bool { return f.ID == TxCrosspointTallyDumpWord }
	collect := func(f Frame) bool { return f.ID == TxCrosspointTallyDumpWord }
	frames, err := ca.SendCollect(ctx, req, match, collect, 150*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("SendCollect: %v", err)
	}
	for _, f := range frames {
		if f.ID != TxCrosspointTallyDumpWord {
			t.Errorf("rejected frame leaked into result: %#x", f.ID)
		}
	}
}

// TestSendCollect_SendError covers the early return when the request Send
// itself fails (here: a closed client).
func TestSendCollect_SendError(t *testing.T) {
	a, b := net.Pipe()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	cfg := ClientConfig{WireHexLog: &disable}
	ca := NewClientFromConn(a, logger, cfg)
	cb := NewClientFromConn(b, logger, cfg)
	_ = ca.Close()
	_ = cb.Close()

	req := Frame{ID: RxCrosspointTallyDumpRequest, Payload: []byte{0}}
	match := func(f Frame) bool { return true }
	if _, err := ca.SendCollect(context.Background(), req, match, match, 100*time.Millisecond, nil); err == nil {
		t.Fatal("SendCollect on closed client returned nil; want send error")
	}
}

// TestSendCollect_DoneTrueOnFirstFrame covers the pre-loop done() short
// circuit: the done predicate is already satisfied by the Send-claimed first
// frame, so SendCollect returns before entering the collect loop.
func TestSendCollect_DoneTrueOnFirstFrame(t *testing.T) {
	ca, cleanup := collectPeer(t, func(b *Client) {
		_, _ = b.Send(context.Background(), wordFrame(1), nil)
	})
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := Frame{ID: RxCrosspointTallyDumpRequest, Payload: []byte{0}}
	match := func(f Frame) bool { return f.ID == TxCrosspointTallyDumpWord }
	done := func(fs []Frame) bool { return len(fs) >= 1 }
	frames, err := ca.SendCollect(ctx, req, match, match, 2*time.Second, done)
	if err != nil {
		t.Fatalf("SendCollect: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("done@1 should stop at the first frame; got %d", len(frames))
	}
}

// TestSendCollect_CtxCancel covers the ctx.Done() arm: the context deadline
// elapses while the collector waits (idle gap far in the future, no done).
func TestSendCollect_CtxCancel(t *testing.T) {
	ca, cleanup := collectPeer(t, func(b *Client) {
		_, _ = b.Send(context.Background(), wordFrame(1), nil)
		// then stay idle so only the ctx deadline can end the collect
	})
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	req := Frame{ID: RxCrosspointTallyDumpRequest, Payload: []byte{0}}
	match := func(f Frame) bool { return f.ID == TxCrosspointTallyDumpWord }
	if _, err := ca.SendCollect(ctx, req, match, match, 10*time.Second, nil); err == nil {
		t.Fatal("expected ctx deadline error from SendCollect")
	}
}

// TestSendCollect_TimerResetOnBump covers the bump arm's idle-window restart:
// a steady stream of matching frames keeps resetting the idle timer (done nil
// so no early stop), and collection ends only once the frames stop and the
// gap elapses. Exercises timer.Reset on the bump path across several frames.
func TestSendCollect_TimerResetOnBump(t *testing.T) {
	ca, cleanup := collectPeer(t, func(b *Client) {
		for i := 0; i < 5; i++ {
			time.Sleep(5 * time.Millisecond)
			_, _ = b.Send(context.Background(), wordFrame(i), nil)
		}
	})
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := Frame{ID: RxCrosspointTallyDumpRequest, Payload: []byte{0}}
	match := func(f Frame) bool { return f.ID == TxCrosspointTallyDumpWord }
	frames, err := ca.SendCollect(ctx, req, match, match, 50*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("SendCollect: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("collected no frames")
	}
}
