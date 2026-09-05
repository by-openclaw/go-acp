package acp1

import (
	"testing"
)

// ReaderDone is the death signal a supervisor blocks on to drive
// reconnection. It must be non-nil and open on a fresh client.
func TestClientsExposeReaderDone(t *testing.T) {
	tc := &TCPClient{readerDone: make(chan struct{})}
	if tc.ReaderDone() == nil {
		t.Fatal("TCPClient.ReaderDone() returned nil")
	}
	select {
	case <-tc.ReaderDone():
		t.Fatal("TCPClient.ReaderDone closed while live")
	default:
	}
	close(tc.readerDone)
	select {
	case <-tc.ReaderDone():
	default:
		t.Fatal("TCPClient.ReaderDone did not observe the close")
	}

	ac := &AN2Client{readerDone: make(chan struct{})}
	if ac.ReaderDone() == nil {
		t.Fatal("AN2Client.ReaderDone() returned nil")
	}
	close(ac.readerDone)
	select {
	case <-ac.ReaderDone():
	default:
		t.Fatal("AN2Client.ReaderDone did not observe the close")
	}
}
