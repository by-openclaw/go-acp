package codec

import (
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

// ReaderDone is the death signal a supervisor blocks on to drive
// reconnection — it must close when the reader goroutine exits.
func TestClientReaderDone(t *testing.T) {
	a, b := net.Pipe()
	defer func() { _ = b.Close() }()

	c := NewClientFromConn(a, slog.New(slog.NewTextHandler(io.Discard, nil)), ClientConfig{})
	select {
	case <-c.ReaderDone():
		t.Fatal("ReaderDone closed while the session was still live")
	default:
	}

	_ = c.Close()
	select {
	case <-c.ReaderDone():
	case <-time.After(10 * time.Second):
		t.Fatal("ReaderDone never closed after the session ended")
	}
}
