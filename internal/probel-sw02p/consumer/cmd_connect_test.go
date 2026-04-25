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

// TestSendConnectRoundTrip drives SendConnect through a net.Pipe pair:
// side B plays a matrix that receives rx 02 and echoes tx 04 CONNECTED
// with the same dst / src.
func TestSendConnectRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	// rx 02 = SOM + cmd + 3 payload + checksum = 6 bytes.
	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		buf := make([]byte, 6)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil {
			t.Errorf("fake matrix unpack: %v", err)
			return
		}
		if req.ID != codec.RxConnect {
			t.Errorf("fake matrix got cmd %#x; want RxConnect", req.ID)
			return
		}
		cp, derr := codec.DecodeConnect(req)
		if derr != nil {
			t.Errorf("fake matrix decode rx 02: %v", derr)
			return
		}
		reply := codec.EncodeConnected(codec.ConnectedParams{
			Destination: cp.Destination,
			Source:      cp.Source,
		})
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
	cp, err := p.SendConnect(ctx, 42, 17, false)
	if err != nil {
		t.Fatalf("SendConnect: %v", err)
	}
	if cp.Destination != 42 || cp.Source != 17 {
		t.Errorf("tx 04 = (dst=%d src=%d); want (42, 17)", cp.Destination, cp.Source)
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendConnectNotConnected verifies the error contract.
func TestSendConnectNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := p.SendConnect(context.Background(), 1, 2, false)
	if err == nil {
		t.Fatal("SendConnect on unconnected plugin returned nil; want ErrNotConnected")
	}
}
