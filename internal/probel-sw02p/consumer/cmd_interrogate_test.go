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

// TestSendInterrogateRoundTrip drives SendInterrogate through a net.Pipe
// pair: side A hosts the Plugin's Client, side B plays a matrix that
// receives rx 01 and replies with tx 03 TALLY echoing dst + reporting a
// fixed source. Verifies the full send+await+decode pipeline.
func TestSendInterrogateRoundTrip(t *testing.T) {
	a, b := net.Pipe()
	defer func() {
		_ = a.Close()
		_ = b.Close()
	}()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	disable := false
	client := codec.NewClientFromConn(a, logger, codec.ClientConfig{WireHexLog: &disable})

	// rx 01 = SOM + cmd + 2-byte payload + checksum = 5 bytes.
	// tx 03 = SOM + cmd + 3-byte payload + checksum = 6 bytes.
	matrixDone := make(chan struct{})
	go func() {
		defer close(matrixDone)
		buf := make([]byte, 5)
		if _, err := io.ReadFull(b, buf); err != nil {
			t.Errorf("fake matrix read: %v", err)
			return
		}
		req, _, err := codec.Unpack(buf)
		if err != nil {
			t.Errorf("fake matrix unpack: %v", err)
			return
		}
		if req.ID != codec.RxInterrogate {
			t.Errorf("fake matrix got cmd %#x; want RxInterrogate", req.ID)
			return
		}
		ip, derr := codec.DecodeInterrogate(req)
		if derr != nil {
			t.Errorf("fake matrix decode rx 01: %v", derr)
			return
		}
		// Reply with tx 03 TALLY. src=7 is arbitrary — the client
		// filter only keys on (ID=TxTally, Destination matches).
		reply := codec.EncodeTally(codec.TallyParams{
			Destination: ip.Destination,
			Source:      7,
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
	tally, err := p.SendInterrogate(ctx, 42)
	if err != nil {
		t.Fatalf("SendInterrogate: %v", err)
	}
	if tally.Destination != 42 || tally.Source != 7 {
		t.Errorf("tally = (dst=%d src=%d); want (42, 7)", tally.Destination, tally.Source)
	}

	<-matrixDone
	_ = client.Close()
}

// TestSendInterrogateNotConnected verifies the error contract when
// SendInterrogate is called on a Plugin that was never Connect()ed.
func TestSendInterrogateNotConnected(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := p.SendInterrogate(context.Background(), 1)
	if err == nil {
		t.Fatal("SendInterrogate on unconnected plugin returned nil; want ErrNotConnected")
	}
}
