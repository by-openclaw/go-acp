package acp1

import (
	"context"
	"io"
	"net"
	"testing"

	"dhs/internal/acp1/codec"
)

// stubClient satisfies clientIface WITHOUT a reader goroutine — the shape of
// the UDP path, where the socket is connectionless and there is no session to
// lose.
type stubClient struct{}

func (stubClient) Do(context.Context, *codec.Message) (*codec.Message, error) {
	return nil, nil
}
func (stubClient) Close() error { return nil }

// A transport with no reader has no death signal, and nil is the right
// answer: it blocks forever in a select rather than firing immediately.
func TestSessionDoneNilForConnectionlessTransport(t *testing.T) {
	p := &Plugin{client: stubClient{}}
	if ch := p.SessionDone(); ch != nil {
		t.Fatal("SessionDone returned a live channel for a client with no reader")
	}
	// Not connected at all is the same answer.
	if ch := (&Plugin{}).SessionDone(); ch != nil {
		t.Fatal("SessionDone on an unconnected plugin returned a live channel")
	}
}

// AN2 carries announcements on the reader goroutine, so its exit IS the end
// of the session — the signal a supervisor blocks on.
func TestSessionDoneClosesWhenTheAN2ReaderExits(t *testing.T) {
	ours, theirs := net.Pipe()
	go func() { _, _ = io.Copy(io.Discard, theirs) }()
	t.Cleanup(func() { _ = theirs.Close() })

	p := &Plugin{logger: discardLogger(), dialer: &fakeDialer{conn: ours}}
	if err := p.connectAN2(context.Background(), "10.6.239.113", 2072); err != nil {
		t.Fatalf("connectAN2: %v", err)
	}

	done := p.SessionDone()
	if done == nil {
		t.Fatal("SessionDone returned nil for a connected AN2 session")
	}
	select {
	case <-done:
		t.Fatal("SessionDone fired while the session was still alive")
	default:
	}

	_ = p.client.Close()
	<-done // closes, or the test times out — which is the failure we want
}
