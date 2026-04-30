package probelsw02p

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"acp/internal/probel-sw02p/codec"
)

// TestSendExtendedProtectTallyDumpRequestWritesFrame drives
// SendExtendedProtectTallyDumpRequest through a net.Pipe and
// confirms the expected rx 105 frame reaches the peer. Uses
// SubscribeExtendedProtectTallyDump on side A to verify the
// fire-and-forget helper still lets callers collect the multi-
// frame tx 100 reply.
func TestSendExtendedProtectTallyDumpRequestWritesFrame(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		// rx 105 = 6 bytes (SOM+CMD+3 payload+checksum).
		buf := make([]byte, 6)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil || req.ID != codec.RxExtendedProtectTallyDumpRequest {
			t.Errorf("fake matrix unpack got=%+v err=%v", req, err)
			return
		}
		p, _ := codec.DecodeExtendedProtectTallyDumpRequest(req)
		if p.Count != 32 || p.StartDestination != 100 {
			t.Errorf("request fields = %+v; want Count=32 StartDestination=100", p)
			return
		}
		// Reply with a single tx 100 carrying 1 entry.
		reply := codec.EncodeExtendedProtectTallyDump(codec.ExtendedProtectTallyDumpParams{
			Entries: []codec.ExtendedProtectTallyDumpEntry{
				{Destination: 100, Device: 7, Protect: codec.ProtectProBel},
			},
		})
		_, _ = b.Write(codec.Pack(reply))
	}()

	p := &Plugin{logger: logger}
	p.client = client

	got := make(chan codec.ExtendedProtectTallyDumpParams, 1)
	_ = p.SubscribeExtendedProtectTallyDump(func(params codec.ExtendedProtectTallyDumpParams) {
		got <- params
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := p.SendExtendedProtectTallyDumpRequest(ctx, 100, 32); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case params := <-got:
		if len(params.Entries) != 1 || params.Entries[0].Destination != 100 {
			t.Errorf("listener got %+v; want 1 entry dst=100", params)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not fire on tx 100")
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendExtendedProtectTallyDumpRequestNotConnected verifies error
// path.
func TestSendExtendedProtectTallyDumpRequestNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := p.SendExtendedProtectTallyDumpRequest(context.Background(), 0, 1); err == nil {
		t.Error("SendExtendedProtectTallyDumpRequest on unconnected plugin returned nil")
	}
}
