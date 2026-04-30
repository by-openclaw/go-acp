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

// TestSendExtendedConnectOnGoRoundTrip drives SendExtendedConnectOnGo
// through a net.Pipe with a fake matrix that echoes rx 069 back as a
// tx 070 ack.
func TestSendExtendedConnectOnGoRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	// rx 069 = SOM + cmd + 4 payload + checksum = 7 bytes.
	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		buf := make([]byte, 7)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil || req.ID != codec.RxExtendedConnectOnGo {
			t.Errorf("fake matrix unpack got=%+v err=%v", req, err)
			return
		}
		p, _ := codec.DecodeExtendedConnectOnGo(req)
		reply := codec.EncodeExtendedConnectOnGoAck(codec.ExtendedConnectOnGoAckParams(p))
		if _, werr := b.Write(codec.Pack(reply)); werr != nil {
			t.Errorf("fake matrix write: %v", werr)
		}
	}()

	p := &Plugin{logger: logger}
	p.client = client

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := p.SendExtendedConnectOnGo(ctx, 5000, 10000)
	if err != nil {
		t.Fatalf("SendExtendedConnectOnGo: %v", err)
	}
	if ack.Destination != 5000 || ack.Source != 10000 {
		t.Errorf("ack = (%d,%d); want (5000,10000)", ack.Destination, ack.Source)
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendExtendedConnectOnGoNotConnected verifies the error path.
func TestSendExtendedConnectOnGoNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := p.SendExtendedConnectOnGo(context.Background(), 1, 2); err == nil {
		t.Error("SendExtendedConnectOnGo on unconnected plugin returned nil")
	}
}
