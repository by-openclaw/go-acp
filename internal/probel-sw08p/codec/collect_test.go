package codec

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

// sendCollectPeers wires two Clients over a net.Pipe and arms B to reply to a
// Crosspoint Tally Dump Request with `n` word-form tally frames, each spaced by
// `gap` so the collector's idle timer and done-predicate behave deterministically.
func sendCollectPeers(t *testing.T, n int, gap time.Duration) (*Client, func()) {
	t.Helper()
	a, b := net.Pipe()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	cfg := ClientConfig{WireHexLog: &disable}
	clientA := NewClientFromConn(a, logger, cfg)
	clientB := NewClientFromConn(b, logger, cfg)

	clientB.Subscribe(func(f Frame) {
		if f.ID != RxCrosspointTallyDumpRequest {
			return
		}
		// Reply from a goroutine so the dispatch callback never blocks.
		go func() {
			for i := 0; i < n; i++ {
				if gap > 0 {
					time.Sleep(gap)
				}
				_, _ = clientB.Send(context.Background(),
					Frame{ID: TxCrosspointTallyDumpWord, Payload: []byte{byte(i)}}, nil)
			}
		}()
	})

	cleanup := func() { _ = clientA.Close(); _ = clientB.Close() }
	return clientA, cleanup
}

// TestSendCollect_CollectsAllFrames: with no done predicate, SendCollect gathers
// every matching frame (Send-claimed first + collector rest) and returns once the
// stream goes idle.
func TestSendCollect_CollectsAllFrames(t *testing.T) {
	clientA, cleanup := sendCollectPeers(t, 3, 20*time.Millisecond)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := Frame{ID: RxCrosspointTallyDumpRequest, Payload: []byte{0}}
	match := func(f Frame) bool { return f.ID == TxCrosspointTallyDumpWord }

	frames, err := clientA.SendCollect(ctx, req, match, match, 200*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("SendCollect: %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("collected %d frames; want 3", len(frames))
	}
	for _, f := range frames {
		if f.ID != TxCrosspointTallyDumpWord {
			t.Errorf("unexpected frame id %#x", f.ID)
		}
	}
}

// TestSendCollect_DoneStopsEarly: a done predicate ends collection deterministically
// once the operator-known count is reached, without waiting for the idle gap or the
// remaining frames. Frames are spaced wider than the evaluation so the stop is crisp.
func TestSendCollect_DoneStopsEarly(t *testing.T) {
	clientA, cleanup := sendCollectPeers(t, 5, 40*time.Millisecond)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := Frame{ID: RxCrosspointTallyDumpRequest, Payload: []byte{0}}
	match := func(f Frame) bool { return f.ID == TxCrosspointTallyDumpWord }
	done := func(fs []Frame) bool { return len(fs) >= 2 }

	frames, err := clientA.SendCollect(ctx, req, match, match, 2*time.Second, done)
	if err != nil {
		t.Fatalf("SendCollect: %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("done should stop at 2; got %d", len(frames))
	}
}
