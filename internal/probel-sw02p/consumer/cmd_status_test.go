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

// TestSendStatusRequestRoundTrip drives SendStatusRequest through a
// net.Pipe pair: side B plays a matrix that receives rx 07 and replies
// with tx 09 STATUS RESPONSE - 2 reporting an overheat flag.
func TestSendStatusRequestRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	// rx 07 = SOM + cmd + 1 payload + checksum = 4 bytes.
	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		buf := make([]byte, 4)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil || req.ID != codec.RxStatusRequest {
			t.Errorf("fake matrix unpack got=%+v err=%v", req, err)
			return
		}
		reply := codec.EncodeStatusResponse2(codec.StatusResponse2Params{Overheat: true})
		if _, werr := b.Write(codec.Pack(reply)); werr != nil {
			t.Errorf("fake matrix write: %v", werr)
		}
	}()

	p := &Plugin{logger: logger}
	p.client = client
	p.host = "test"
	p.port = 0

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	sp, err := p.SendStatusRequest(ctx, codec.ControllerLH)
	if err != nil {
		t.Fatalf("SendStatusRequest: %v", err)
	}
	if !sp.Overheat || sp.Idle || sp.BusFault {
		t.Errorf("status = %+v; want Overheat-only", sp)
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendStatusRequestNotConnected verifies the error contract.
func TestSendStatusRequestNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := p.SendStatusRequest(context.Background(), codec.ControllerLH)
	if err == nil {
		t.Fatal("SendStatusRequest on unconnected plugin returned nil; want ErrNotConnected")
	}
}
