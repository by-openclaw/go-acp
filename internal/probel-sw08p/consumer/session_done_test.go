package probelsw08p

import (
	"net"
	"testing"

	"dhs/internal/probel-sw08p/codec"
)

// Not connected: nil, which blocks forever in a select — the correct
// "this never fires" answer rather than a channel that fires immediately.
func TestSessionDoneNilWhenNotConnected(t *testing.T) {
	if ch := (&Plugin{}).SessionDone(); ch != nil {
		t.Fatal("SessionDone on an unconnected plugin returned a live channel")
	}
}

// Connected: the channel closes when the reader goroutine exits, which is
// the signal a supervisor blocks on to drive reconnection.
func TestSessionDoneClosesWhenTheReaderExits(t *testing.T) {
	ours, theirs := net.Pipe()
	t.Cleanup(func() { _ = theirs.Close() })

	cli := codec.NewClientFromConn(ours, nil, codec.ClientConfig{})
	p := &Plugin{client: cli}

	done := p.SessionDone()
	if done == nil {
		t.Fatal("SessionDone returned nil for a connected plugin")
	}
	select {
	case <-done:
		t.Fatal("SessionDone fired while the session was still alive")
	default:
	}

	_ = cli.Close()
	<-done // closes, or the test times out — which is the failure we want
}