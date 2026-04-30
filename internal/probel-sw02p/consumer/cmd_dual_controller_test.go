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

// TestSendDualControllerStatusRequestRoundTrip drives SendDualController
// StatusRequest through a net.Pipe pair with a fake matrix reporting
// "Slave active, Idle faulty".
func TestSendDualControllerStatusRequestRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	// rx 050 = 3 bytes (zero-length MESSAGE).
	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		buf := make([]byte, 3)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil || req.ID != codec.RxDualControllerStatusRequest {
			t.Errorf("fake matrix unpack got=%+v err=%v", req, err)
			return
		}
		reply := codec.EncodeDualControllerStatusResponse(codec.DualControllerStatusResponseParams{
			Active:     codec.ActiveControllerSlave,
			IdleStatus: codec.IdleControllerFaulty,
		})
		if _, werr := b.Write(codec.Pack(reply)); werr != nil {
			t.Errorf("fake matrix write: %v", werr)
		}
	}()

	p := &Plugin{logger: logger}
	p.client = client

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := p.SendDualControllerStatusRequest(ctx)
	if err != nil {
		t.Fatalf("SendDualControllerStatusRequest: %v", err)
	}
	if resp.Active != codec.ActiveControllerSlave || resp.IdleStatus != codec.IdleControllerFaulty {
		t.Errorf("resp = %+v; want slave/faulty", resp)
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendDualControllerStatusRequestNotConnected verifies the error
// path when Plugin has no underlying Client.
func TestSendDualControllerStatusRequestNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := p.SendDualControllerStatusRequest(context.Background()); err == nil {
		t.Error("SendDualControllerStatusRequest on unconnected plugin returned nil")
	}
}
