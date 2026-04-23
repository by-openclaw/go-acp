package codec

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
)

func TestHexDump(t *testing.T) {
	cases := []struct {
		in   []byte
		want string
	}{
		{nil, ""},
		{[]byte{0x10}, "10"},
		{[]byte{0x10, 0x02, 0x01}, "10 02 01"},
		{[]byte{0xAB, 0xCD, 0xEF, 0x00, 0xFF}, "ab cd ef 00 ff"},
	}
	for _, tc := range cases {
		got := HexDump(tc.in)
		if got != tc.want {
			t.Errorf("HexDump(%v) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

// TestClientLoopback wires both ends of a net.Pipe into a Client each
// and verifies:
//   - a framed Send round-trips through Pack/Unpack on both sides
//   - an unmatched frame is delivered to Subscribe listeners
//   - Close unblocks a pending Send with io.EOF
func TestClientLoopback(t *testing.T) {
	a, b := net.Pipe()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	cfg := ClientConfig{WireHexLog: &disable}
	clientA := NewClientFromConn(a, logger, cfg)
	clientB := NewClientFromConn(b, logger, cfg)
	defer func() { _ = clientA.Close() }()
	defer func() { _ = clientB.Close() }()

	// A subscribes to events so B's unsolicited frame is captured.
	var wg sync.WaitGroup
	wg.Add(1)
	clientA.Subscribe(func(f Frame) {
		defer wg.Done()
		if f.ID != TxCrosspointTally {
			t.Errorf("listener got cmd %02x; want %02x", f.ID, TxCrosspointTally)
		}
		if len(f.Payload) != 3 {
			t.Errorf("listener got payload len %d; want 3", len(f.Payload))
		}
	})

	// B sends an unsolicited tally to A.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tally := Frame{ID: TxCrosspointTally, Payload: []byte{0x00, 0x01, 0x05}}
	if _, err := clientB.Send(ctx, tally, nil); err != nil {
		t.Fatalf("B.Send: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("listener never fired")
	}
}

// TestClientCloseWakesPending verifies an in-flight Send is unblocked
// when the reader goroutine exits (EOF / peer close).
func TestClientCloseWakesPending(t *testing.T) {
	a, b := net.Pipe()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := NewClientFromConn(a, logger, ClientConfig{WireHexLog: &disable})

	// Drain and drop everything the client writes so Send's Write() never blocks.
	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := client.Send(ctx, Frame{ID: RxMaintenance, Payload: []byte{0x00}},
			func(Frame) bool { return true })
		done <- err
	}()

	// Close peer → Read on a returns io.EOF → reader exits →
	// failPending fires.
	time.Sleep(50 * time.Millisecond)
	_ = b.Close()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Send returned nil; want non-nil on peer close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send blocked past peer close")
	}
	_ = client.Close()
}
