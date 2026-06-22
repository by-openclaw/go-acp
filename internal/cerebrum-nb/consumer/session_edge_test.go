package cerebrumnb

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dhs/internal/cerebrum-nb/codec"
)

// nackAll answers every client frame with a NACK so roundTrip returns the
// NACK as a transport-level error (the *roundTrip-error* arm, distinct from
// the unexpected-kind arm exercised elsewhere).
func nackAll(errName string, code int) func(*fakeConn) {
	return func(fc *fakeConn) {
		drainClient(fc, func(frame []byte) ([]byte, bool) {
			return nackFor(frame, errName, code), true
		})
	}
}

func TestSubscribeObtainUnsub_RoundTripError(t *testing.T) {
	fs := newFakeCerebrum(t, nackAll("ONE_OR_MORE_EVENTS_INVALID", 9))
	_, sess := dialFake(t, fs)
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := sess.Subscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err == nil {
		t.Fatal("subscribe: want NACK error")
	}
	if err := sess.Obtain(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err == nil {
		t.Fatal("obtain: want NACK error")
	}
	if err := sess.UnsubscribeAll(ctx); err == nil {
		t.Fatal("unsubscribe_all: want NACK error")
	}
	if err := sess.Unsubscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err == nil {
		t.Fatal("unsubscribe: want NACK error")
	}
}

func TestRecordNack_NilGuard(t *testing.T) {
	s := &Session{compliance: &Profile{}}
	s.recordNack(nil) // must not panic; no count recorded
	if len(s.compliance.Counts()) != 0 {
		t.Fatalf("nil nack should record nothing, got %+v", s.compliance.Counts())
	}
}

func TestLogin_UnexpectedReply(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		frame, err := fc.readClientFrame()
		if err != nil {
			return
		}
		// Reply with ACK instead of LOGIN_REPLY → unexpected-kind arm.
		_ = fc.writeText(ackFor(frame))
	})
	p := NewPlugin(nil)
	p.Username, p.Password = "u", "p"
	ctx, cancel := ctx2s(t)
	defer cancel()
	err := p.Connect(ctx, fs.host(), fs.port())
	if err == nil || !strings.Contains(err.Error(), "unexpected reply") {
		t.Fatalf("want login unexpected reply, got %v", err)
	}
}

func TestConnect_TLSPath(t *testing.T) {
	// UseTLS=true drives the wss:// scheme branch in Connect AND the
	// useTLS+insecure TLSConfig branch in newSession. The plain-TCP fake
	// server speaks no TLS, so the dial fails at the TLS handshake — but
	// only after both target branches have executed.
	fs := newFakeCerebrum(t, func(fc *fakeConn) { _, _ = fc.readClientFrame() })
	p := NewPlugin(nil)
	p.UseTLS = true
	p.InsecureSkipVerify = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := p.Connect(ctx, fs.host(), fs.port())
	if err == nil || !strings.Contains(err.Error(), "ws dial") {
		t.Fatalf("want ws dial (tls) error, got %v", err)
	}
}

func TestGetDeviceInfo_PollError(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		// Read the POLL, never reply → Poll times out.
		_, _ = fc.readClientFrame()
		time.Sleep(300 * time.Millisecond)
	})
	p, _ := dialFake(t, fs)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := p.GetDeviceInfo(ctx); err == nil {
		t.Fatal("want GetDeviceInfo poll error")
	}
}

func TestReadLoop_DropsBinary(t *testing.T) {
	// The server waits for the client's SUBSCRIBE (so the test has already
	// registered its OnEvent callback) before pushing a BINARY frame — which
	// the readLoop must drop — followed by a TEXT routing event.
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		frame, err := fc.readClientFrame()
		if err != nil {
			return
		}
		_ = fc.writeText(ackFor(frame))
		_ = fc.writeBinary([]byte("not-xml-binary"))
		_ = fc.writeText([]byte(`<ROUTING_CHANGE TYPE="ROUTE" DEST_ID="1"/>`))
		_, _ = fc.readClientFrame() // hold open until the client disconnects
	})
	_, sess := dialFake(t, fs)
	var seen atomic.Int32
	mu := make(chan struct{}, 1)
	sess.OnEvent(codec.KindRoutingChange, func(*codec.Frame) {
		seen.Add(1)
		select {
		case mu <- struct{}{}:
		default:
		}
	})
	ctx, cancel := ctx2s(t)
	defer cancel()
	if err := sess.Subscribe(ctx, []codec.SubItem{&codec.DeviceChange{Type: "LIST"}}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	select {
	case <-mu:
	case <-time.After(2 * time.Second):
		t.Fatal("routing event after binary not received")
	}
	if seen.Load() != 1 {
		t.Fatalf("seen = %d, want 1 (binary dropped)", seen.Load())
	}
}

// TestReadLoop_StopRXBranch deterministically drives the top-of-loop
// `case <-s.stopRX: return` arm in readLoop: stopRX is closed BEFORE the
// loop runs, so the first select returns immediately without ever touching
// the (nil) conn. This branch is otherwise scheduling-dependent (close()
// normally makes ReadMessage error first), so the explicit drive keeps the
// 100% floor stable.
func TestReadLoop_StopRXBranch(t *testing.T) {
	s := &Session{
		compliance: &Profile{},
		pending:    map[string]chan *codec.Frame{},
		stopRX:     make(chan struct{}),
	}
	close(s.stopRX)
	done := make(chan struct{})
	go func() { s.readLoop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("readLoop did not return on pre-closed stopRX")
	}
}

func TestSplitURLHostPort_ParseError(t *testing.T) {
	// A control character makes url.Parse fail → ("", 0).
	h, p := splitURLHostPort("ws://\x7f\x00bad")
	if h != "" || p != 0 {
		t.Fatalf("parse-error case: got (%q,%d), want empty", h, p)
	}
}

// TestDispatch_ChannelFull drives the defensive "dropped reply (channel
// full)" arm: a pending mtid waiter already has a buffered reply, and a
// second terminal reply for the same mtid arrives before the waiter drains.
func TestDispatch_ChannelFull(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) { _, _ = fc.readClientFrame() })
	_, sess := dialFake(t, fs)

	const mtid = "777"
	ch := make(chan *codec.Frame, 1)
	sess.mu.Lock()
	sess.pending[mtid] = ch
	sess.mu.Unlock()

	first := &codec.Frame{Kind: codec.KindAck, MTID: mtid}
	second := &codec.Frame{Kind: codec.KindAck, MTID: mtid}
	sess.dispatch(first)  // fills the buffered channel
	sess.dispatch(second) // hits the channel-full default arm (must not block)

	select {
	case got := <-ch:
		if got != first {
			t.Fatal("expected the first frame to be the buffered one")
		}
	case <-time.After(time.Second):
		t.Fatal("no frame buffered")
	}
	// Clean up the manual pending entry.
	sess.mu.Lock()
	delete(sess.pending, mtid)
	sess.mu.Unlock()
}

// TestReadLoop_DebugLogOnError exercises the non-EOF read-error debug-log
// branch in readLoop by aborting the TCP connection mid-stream.
func TestReadLoop_DebugLogOnError(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		// Write a partial frame header then RST the socket so the client's
		// next ReadFull returns a non-EOF error.
		_, _ = fc.c.Write([]byte{0x81, 0x05}) // declares 5-byte text, sends none
		if tcp, ok := fc.c.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = fc.c.Close()
	})
	p, _ := dialFake(t, fs)
	// The readLoop will exit on the error; give it a moment. We can't assert
	// the log line directly, but exercising the branch is the goal and the
	// session must remain usable for Disconnect.
	time.Sleep(150 * time.Millisecond)
	_ = p.Disconnect()
}
