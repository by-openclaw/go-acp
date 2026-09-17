package emberplus

import (
	"net"
	"sync"
	"testing"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/emberplus/codec/s101"
)

// wireSession wires a Session's internals over the server end of a
// net.Pipe the same way Connect does, without dialing. Returns the
// session plus the peer (client) reader/writer for driving frames.
func wireSession(t *testing.T, keepAlive, deadMan time.Duration) (*Session, *s101.Writer, *s101.Reader, func()) {
	t.Helper()
	srvConn, cliConn := net.Pipe()
	s := NewSession(discardLogger())
	s.SetProfile(&compliance.Profile{})
	s.keepAliveInterval = keepAlive
	s.deadManThreshold = deadMan
	s.mu.Lock()
	s.conn = srvConn
	s.reader = s101.NewReader(srvConn)
	s.writer = s101.NewWriter(srvConn)
	s.closed = false
	s.keepAliveDone = make(chan struct{})
	s.deadManDone = make(chan struct{})
	s.mu.Unlock()
	s.touchRX()
	cleanup := func() { _ = s.Disconnect(); _ = cliConn.Close() }
	return s, s101.NewWriter(cliConn), s101.NewReader(cliConn), cleanup
}

// TestReadLoop_FrameKinds drives readLoop over a pipe with a keepalive
// request (→ response branch), a multi-frame EmBER message
// (First/middle/Last reassembly), and a single EmBER frame.
func TestReadLoop_FrameKinds(t *testing.T) {
	s, w, r, cleanup := wireSession(t, time.Hour, time.Hour)
	defer cleanup()

	var mu sync.Mutex
	var elemBatches int
	s.SetOnElement(func([]glow.Element) {
		mu.Lock()
		elemBatches++
		mu.Unlock()
	})

	go s.readLoop()
	// Drain any keepalive responses the read loop emits so writes never block.
	go func() {
		for {
			if _, err := r.ReadFrame(); err != nil {
				return
			}
		}
	}()

	// Keepalive request → readLoop sends a response.
	if err := w.WriteFrame(s101.NewKeepAliveRequest()); err != nil {
		t.Fatalf("write keepalive: %v", err)
	}

	// Multi-frame EmBER message: split a GetDirectory payload across
	// First + Last fragments.
	payload := glow.EncodeGetDirectory()
	half := len(payload) / 2
	if half == 0 {
		half = 1
	}
	writeFrag := func(flags byte, body []byte) {
		if err := w.WriteFrame(&s101.Frame{
			Slot: s101.SlotDefault, MsgType: s101.MsgEmBER, Command: s101.CmdEmBER,
			Version: s101.VersionS101, Flags: flags, DTD: s101.DTDGlow, Payload: body,
		}); err != nil {
			t.Fatalf("write frag: %v", err)
		}
	}
	writeFrag(s101.FlagFirst, payload[:half])
	writeFrag(0, nil) // middle fragment (empty body tolerated)
	writeFrag(s101.FlagLast, payload[half:])

	// Single EmBER frame.
	if err := w.WriteFrame(s101.NewEmBERFrame(payload)); err != nil {
		t.Fatalf("write single: %v", err)
	}

	// Allow the read loop to process.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := elemBatches
		mu.Unlock()
		if n >= 2 { // multi-frame + single
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if elemBatches < 2 {
		t.Errorf("element batches = %d, want ≥2", elemBatches)
	}
}

// TestKeepAliveLoop_Tick drives keepAliveLoop with a tiny interval so the
// ticker branch fires and writes a keepalive request.
func TestKeepAliveLoop_Tick(t *testing.T) {
	s, _, r, cleanup := wireSession(t, 5*time.Millisecond, time.Hour)
	defer cleanup()

	got := make(chan struct{}, 1)
	go func() {
		for {
			f, err := r.ReadFrame()
			if err != nil {
				return
			}
			if f.Command == s101.CmdKeepAliveReq {
				select {
				case got <- struct{}{}:
				default:
				}
			}
		}
	}()
	go s.keepAliveLoop()

	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Error("keepAliveLoop never emitted a keepalive request")
	}
}

// TestKeepAliveLoop_ClosedSession deterministically covers the tick arm's
// closed-session return: the session is marked closed BEFORE the loop
// starts and keepAliveDone stays open, so the first tick is the only
// possible wake-up and must exit through the closed check. (Previously
// this arm was only hit when a tick raced Disconnect inside the
// tick-vs-done window — #694 coverage class, named by the CI floor.)
func TestKeepAliveLoop_ClosedSession(t *testing.T) {
	s, _, _, cleanup := wireSession(t, 5*time.Millisecond, time.Hour)
	defer cleanup()

	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()

	done := make(chan struct{})
	go func() { s.keepAliveLoop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("keepAliveLoop did not exit on the closed-session check")
	}
}

// TestKeepAliveLoop_WriteError deterministically covers the tick arm's
// write-failure return: the underlying pipe is closed before the loop
// starts (session NOT marked closed, keepAliveDone open), so the first
// tick reaches WriteFrame and gets an immediate ErrClosedPipe.
func TestKeepAliveLoop_WriteError(t *testing.T) {
	s, _, _, cleanup := wireSession(t, 5*time.Millisecond, time.Hour)
	defer cleanup()

	s.mu.Lock()
	c := s.conn
	s.mu.Unlock()
	_ = c.Close() // writer now fails instantly; closed flag stays false

	done := make(chan struct{})
	go func() { s.keepAliveLoop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("keepAliveLoop did not exit on the keep-alive write error")
	}
}

// TestDeadManLoop_Fires drives deadManLoop with a tiny threshold and a
// backdated lastRX so it declares the session dead and fires the
// disconnected state change.
func TestDeadManLoop_Fires(t *testing.T) {
	s, _, r, cleanup := wireSession(t, time.Hour, 10*time.Millisecond)
	defer cleanup()
	go func() {
		for {
			if _, err := r.ReadFrame(); err != nil {
				return
			}
		}
	}()

	fired := make(chan string, 1)
	s.SetOnStateChange(func(connected bool, reason string) {
		if !connected {
			select {
			case fired <- reason:
			default:
			}
		}
	})
	// Backdate so age exceeds the threshold immediately.
	s.lastRXMu.Lock()
	s.lastRX = time.Now().Add(-time.Second)
	s.lastRXMu.Unlock()

	go s.deadManLoop()

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Error("deadManLoop never fired a disconnect")
	}
}
