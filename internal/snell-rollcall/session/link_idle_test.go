package session

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

// TestNewIdleLink_ReadsNothingUntilStarted pins the two halves of NewLink.
//
// A pipe write completes only when the other end reads, so a frame written to
// an idle link stays pending — Start is what takes it, and the frame lands on
// the unsolicited queue once it does.
func TestNewIdleLink_ReadsNothingUntilStarted(t *testing.T) {
	ours, theirs := net.Pipe()
	l := NewIdleLink(ours, Config{}, testDeps(nil))
	t.Cleanup(func() { _ = l.Close() })

	written := make(chan error, 1)
	go func() {
		f := codec.Frame{Dst: codec.Broadcast(), Src: peerAddr, Type: codec.MsgIam}
		buf, _ := f.AppendTo(nil)
		_, err := theirs.Write(buf)
		written <- err
	}()

	select {
	case err := <-written:
		t.Fatalf("the idle link read before Start (write returned %v)", err)
	case <-time.After(50 * time.Millisecond):
	}

	l.Start()
	select {
	case err := <-written:
		if err != nil {
			t.Fatalf("write after Start: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the started link did not read the pending frame")
	}
	select {
	case f := <-l.Unsolicited():
		if f.Type != codec.MsgIam {
			t.Fatalf("got %s, want Iam", f.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the frame read after Start was not dispatched")
	}
}

// countingConn counts the reads made on it.
type countingConn struct {
	net.Conn
	reads atomic.Int32
}

func (c *countingConn) Read(p []byte) (int, error) {
	c.reads.Add(1)
	return c.Conn.Read(p)
}

// A link closed before it is started never starts reading. A server registers
// an idle link and starts it a moment later; if its Stop closes the link in
// between, Start used to launch a reader on a link its owner had already
// waited out.
func TestIdleLink_ClosedBeforeStartNeverReads(t *testing.T) {
	ours, theirs := net.Pipe()
	t.Cleanup(func() { _ = theirs.Close() })
	conn := &countingConn{Conn: ours}
	l := NewIdleLink(conn, Config{}, testDeps(nil))

	_ = l.Close()
	l.Start()
	time.Sleep(50 * time.Millisecond)

	if n := conn.reads.Load(); n != 0 {
		t.Errorf("a link closed before Start read from its connection %d time(s)", n)
	}
}

// Start and Close at once, from two goroutines: what a server's accept path
// and its Stop do. Whichever wins, Close returns only when no reader is left,
// and the race detector sees no Add meeting a Wait.
func TestIdleLink_StartAndCloseAtOnce(t *testing.T) {
	for i := 0; i < 200; i++ {
		ours, theirs := net.Pipe()
		conn := &countingConn{Conn: ours}
		l := NewIdleLink(conn, Config{}, testDeps(nil))

		var both sync.WaitGroup
		both.Add(2)
		go func() { defer both.Done(); l.Start() }()
		go func() { defer both.Done(); _ = l.Close() }()
		both.Wait()
		_ = l.Close() // a reader that won the race is waited out here at the latest

		before := conn.reads.Load()
		time.Sleep(time.Millisecond)
		if after := conn.reads.Load(); after != before {
			t.Fatalf("round %d: the link still read after Close returned", i)
		}
		_ = theirs.Close()
	}
}
