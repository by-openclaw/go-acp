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

// TestSendConnectOnGoRoundTrip drives the consumer's SendConnectOnGo
// through a net.Pipe pair: side A hosts the Plugin's underlying Client,
// side B plays the role of a matrix that echoes the rx 05 back as a
// tx 12 ack. Verifies the full send+await+decode flow.
func TestSendConnectOnGoRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	// Fake matrix — wait for exactly one rx 05 frame on B, send a
	// matching tx 12 ack back. SendConnectOnGo uses dst=42 src=17 →
	// both fit in the narrow addressing range (< 128), so the
	// Multiplier is 0x00 and the payload is {00, 42, 17}.
	//
	// Frame sizes:
	//   rx 05 = SOM + cmd + 3 payload + checksum = 6 bytes
	//   tx 12 = 6 bytes (same shape)
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
		if req.ID != codec.RxConnectOnGo {
			t.Errorf("fake matrix got cmd %#x; want RxConnectOnGo", req.ID)
			return
		}
		// Decode to confirm dst/src, then emit the echo ack.
		p, derr := codec.DecodeConnectOnGo(req)
		if derr != nil {
			t.Errorf("fake matrix decode rx 05: %v", derr)
			return
		}
		ack := codec.EncodeConnectOnGoAck(codec.ConnectOnGoAckParams{
			Destination: p.Destination,
			Source:      p.Source,
		})
		if _, werr := b.Write(codec.Pack(ack)); werr != nil {
			t.Errorf("fake matrix write ack: %v", werr)
		}
	}()

	// Manually wire the Plugin to the test client — avoids the full
	// TCP Dial path while still exercising the send/await pump.
	p := &Plugin{logger: logger}
	p.client = client
	p.host = "test"
	p.port = 0

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := p.SendConnectOnGo(ctx, 42, 17, false)
	if err != nil {
		t.Fatalf("SendConnectOnGo: %v", err)
	}
	if ack.Destination != 42 || ack.Source != 17 {
		t.Errorf("ack = (dst=%d src=%d); want (42, 17)", ack.Destination, ack.Source)
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendConnectOnGoNotConnected verifies the error contract when
// SendConnectOnGo is called on a Plugin that was never Connect()ed.
func TestSendConnectOnGoNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := p.SendConnectOnGo(context.Background(), 1, 2, false)
	if err == nil {
		t.Fatal("SendConnectOnGo on unconnected plugin returned nil; want ErrNotConnected")
	}
}
