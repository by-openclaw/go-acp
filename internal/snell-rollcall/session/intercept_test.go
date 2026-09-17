package session

import (
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
)

func TestAnInterceptTakesAFrameBeforeAnythingElse(t *testing.T) {
	taken := make(chan codec.Frame, 1)
	h := newHarness(t, Config{Intercept: func(_ *Link, f codec.Frame) bool {
		if f.Type == codec.MsgGetID {
			taken <- f
			return true
		}
		return false
	}})

	// A frame the intercept takes goes nowhere else: it never reaches the
	// announcement channel.
	h.peer.write(codec.Frame{
		Dst:  codec.Address{Unit: 0x01, Index: 3},
		Src:  peerAddr,
		Type: codec.MsgGetID,
	})
	select {
	case f := <-taken:
		if f.Dst.Index != 3 {
			t.Errorf("intercepted %s, want the frame to session index 3", f.Dst)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the intercept never saw the frame")
	}

	// One it declines goes on as before.
	h.peer.write(codec.Frame{Dst: codec.Broadcast(), Src: peerAddr, Type: codec.MsgIam})
	select {
	case f := <-h.link.Unsolicited():
		if f.Type != codec.MsgIam {
			t.Errorf("the link delivered %s, want the declined announcement", f.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a frame the intercept declined was not delivered")
	}
	select {
	case f := <-h.link.Unsolicited():
		t.Errorf("the taken frame was also delivered: %s", f.Type)
	default:
	}
}
