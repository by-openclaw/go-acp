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

// TestSendSourceLockStatusRequestRoundTrip drives SendSourceLockStatus
// Request through a net.Pipe with a fake HD router that reports 8
// sources (4 locked, 4 unlocked).
func TestSendSourceLockStatusRequestRoundTrip(t *testing.T) {
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
		buf := make([]byte, 4)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil || req.ID != codec.RxSourceLockStatusRequest {
			t.Errorf("fake matrix unpack got=%+v err=%v", req, err)
			return
		}
		reply := codec.EncodeSourceLockStatusResponse(codec.SourceLockStatusResponseParams{
			Locked: []bool{true, true, true, true, false, false, false, false},
		})
		if _, werr := b.Write(codec.Pack(reply)); werr != nil {
			t.Errorf("fake matrix write: %v", werr)
		}
	}()

	p := &Plugin{logger: logger}
	p.client = client

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendSourceLockStatusRequest(ctx, codec.ControllerLH)
	if err != nil {
		t.Fatalf("SendSourceLockStatusRequest: %v", err)
	}
	if len(resp.Locked) < 8 {
		t.Fatalf("Locked len = %d; want >= 8", len(resp.Locked))
	}
	for i := 0; i < 4; i++ {
		if !resp.Locked[i] {
			t.Errorf("Locked[%d] = false; want true", i)
		}
	}
	for i := 4; i < 8; i++ {
		if resp.Locked[i] {
			t.Errorf("Locked[%d] = true; want false", i)
		}
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendSourceLockStatusRequestNotConnected verifies the error path.
func TestSendSourceLockStatusRequestNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := p.SendSourceLockStatusRequest(context.Background(), codec.ControllerLH); err == nil {
		t.Error("SendSourceLockStatusRequest on unconnected plugin returned nil")
	}
}
