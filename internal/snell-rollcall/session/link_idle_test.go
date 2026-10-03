package session

import (
	"net"
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
