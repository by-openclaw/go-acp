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

// TestSendExtendedInterrogateRoundTrip drives SendExtendedInterrogate
// through a net.Pipe pair; the fake matrix replies with tx 67 carrying
// a routed source and both flag bits.
func TestSendExtendedInterrogateRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	// rx 65 = SOM + cmd + 2 payload + checksum = 5 bytes.
	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		buf := make([]byte, 5)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil || req.ID != codec.RxExtendedInterrogate {
			t.Errorf("fake matrix unpack got=%+v err=%v", req, err)
			return
		}
		ip, _ := codec.DecodeExtendedInterrogate(req)
		reply := codec.EncodeExtendedTally(codec.ExtendedTallyParams{
			Destination: ip.Destination,
			Source:      9999,
			BadSource:   true,
		})
		if _, werr := b.Write(codec.Pack(reply)); werr != nil {
			t.Errorf("fake matrix write: %v", werr)
		}
	}()

	p := &Plugin{logger: logger}
	p.client = client

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tally, err := p.SendExtendedInterrogate(ctx, 5000)
	if err != nil {
		t.Fatalf("SendExtendedInterrogate: %v", err)
	}
	if tally.Destination != 5000 || tally.Source != 9999 || !tally.BadSource {
		t.Errorf("tally = %+v; want dst=5000 src=9999 BadSource=true", tally)
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendExtendedConnectRoundTrip drives SendExtendedConnect through
// a net.Pipe pair with a fake matrix that echoes rx 66 as tx 68.
func TestSendExtendedConnectRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	// rx 66 = SOM + cmd + 4 payload + checksum = 7 bytes.
	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		buf := make([]byte, 7)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil || req.ID != codec.RxExtendedConnect {
			t.Errorf("fake matrix unpack got=%+v err=%v", req, err)
			return
		}
		cp, _ := codec.DecodeExtendedConnect(req)
		reply := codec.EncodeExtendedConnected(codec.ExtendedConnectedParams{
			Destination: cp.Destination,
			Source:      cp.Source,
		})
		if _, werr := b.Write(codec.Pack(reply)); werr != nil {
			t.Errorf("fake matrix write: %v", werr)
		}
	}()

	p := &Plugin{logger: logger}
	p.client = client

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cp, err := p.SendExtendedConnect(ctx, 5000, 10000)
	if err != nil {
		t.Fatalf("SendExtendedConnect: %v", err)
	}
	if cp.Destination != 5000 || cp.Source != 10000 {
		t.Errorf("tx 68 = (dst=%d src=%d); want (5000, 10000)", cp.Destination, cp.Source)
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendExtendedNotConnected verifies error paths for both new helpers.
func TestSendExtendedNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := p.SendExtendedInterrogate(context.Background(), 5000); err == nil {
		t.Error("SendExtendedInterrogate on unconnected plugin returned nil")
	}
	if _, err := p.SendExtendedConnect(context.Background(), 5000, 10000); err == nil {
		t.Error("SendExtendedConnect on unconnected plugin returned nil")
	}
}
