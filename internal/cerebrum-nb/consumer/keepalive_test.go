package cerebrumnb

// Tests for the 24/7 liveness fix.
//
// The regression these guard: readLoop used to call ReadMessage with
// context.Background() and no deadline, so a peer that stopped sending
// without closing the socket parked the reader forever. The watcher kept
// running, kept its process alive, and never logged another line — the
// "no crash but no logs" failure the operator reported.
//
// The idle-timeout paths below are driven by a real (short) deadline rather
// than the fake clock because the deadline lives in the OS socket, not in our
// code; the cadence-driven paths use the injected clock so nothing sleeps.

import (
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
	"dhs/internal/transport"
)

// The headline test: a server that accepts the connection and then goes
// completely silent must be detected, not waited on forever.
func TestSessionIdleTimeoutMarksConnectionLost(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		// Read the client's frames and answer nothing, ever. This is a
		// half-open peer: the socket is open, the server is mute.
		for {
			if _, err := fc.readClientFrame(); err != nil {
				return
			}
		}
	})
	_, sess := dialFake(t, fs)

	// Tighten the window so the test is fast; probing off so the ONLY thing
	// under test is the read deadline.
	sess.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: consumer.DisableInterval,
		Timeout:  150 * time.Millisecond,
	})

	select {
	case <-sess.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("session never reported loss — the reader is hung, which is the original bug")
	}

	err := sess.Err()
	if err == nil {
		t.Fatal("Done fired but Err() is nil; the cause must always be recorded")
	}
	if !errors.Is(err, transport.ErrConnectionLost) {
		t.Errorf("Err() = %v; must wrap transport.ErrConnectionLost so callers can errors.Is it", err)
	}
	if !errors.Is(err, transport.ErrIdleTimeout) {
		t.Errorf("Err() = %v; a silent peer must be reported as transport.ErrIdleTimeout", err)
	}
	if sess.SessionLive() {
		t.Error("SessionLive() must be false after the session is lost")
	}
}

// The counterpart: a server that answers our Pings keeps the session alive
// well past the idle window. Without the per-frame deadline re-arm in the ws
// layer, this test fails — the connection would be declared dead despite
// being demonstrably healthy.
func TestSessionPongsKeepSessionAlive(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		// readClientFrame answers Pings with Pongs inside the fake server's
		// frame reader; just keep reading so the exchange continues.
		for {
			if _, err := fc.readClientFrame(); err != nil {
				return
			}
		}
	})
	_, sess := dialFake(t, fs)

	sess.SetKeepAlive(consumer.KeepAliveConfig{
		Interval: 20 * time.Millisecond,
		Timeout:  400 * time.Millisecond,
	})

	// Well past the idle window: if pongs did not re-arm the deadline this
	// would have fired several times over.
	select {
	case <-sess.Done():
		t.Fatalf("session was declared lost while the peer was answering: %v", sess.Err())
	case <-time.After(1 * time.Second):
	}
}

// Closing the session must publish a cause on Done — a supervisor blocked
// there has to wake up on an orderly shutdown too, not just on failure.
func TestSessionCloseMarksLost(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		for {
			if _, err := fc.readClientFrame(); err != nil {
				return
			}
		}
	})
	p, sess := dialFake(t, fs)

	if sess.SessionLive() && sess.LastRx().IsZero() {
		t.Error("SessionLive() must be false before any frame has arrived")
	}
	if err := p.Disconnect(); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not fire on local close")
	}
	if err := sess.Err(); !errors.Is(err, transport.ErrConnectionLost) {
		t.Errorf("Err() = %v; want it to wrap transport.ErrConnectionLost", err)
	}
}

// markLost keeps the FIRST cause: the initial failure explains the cascade
// that follows it, so a later generic error must not overwrite a specific one.
func TestMarkLostKeepsFirstCause(t *testing.T) {
	s := &Session{done: make(chan struct{})}
	first := errors.New("first cause")
	s.markLost(first)
	s.markLost(errors.New("second cause"))

	if got := s.Err(); !errors.Is(got, first) {
		t.Fatalf("Err() = %v, want the first cause %v", got, first)
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("markLost must close Done")
	}
}

func TestClassifyReadErr(t *testing.T) {
	s := &Session{host: "10.6.250.5", conn: nil}
	// classifyReadErr reads conn.IdleTimeout() only on the timeout arm, so
	// give that arm a conn and leave the others nil-safe.
	// Every arm is named here rather than reached through a live socket.
	// The io.EOF and net.ErrClosed arms used to be covered only incidentally,
	// by a test that closes a real connection — and what a closed peer
	// produces is platform-dependent: Windows gives io.EOF, Linux gives
	// ECONNRESET, which lands in the default arm instead. That made the
	// package's coverage differ by OS and turned the 100% floor into a
	// coin toss on the CI matrix.
	tests := []struct {
		name    string
		err     error
		wantIs  []error
		wantMsg string
		wantNil bool
	}{
		{
			name:    "nil stays nil",
			err:     nil,
			wantNil: true,
		},
		{
			name:    "EOF is the peer closing the WebSocket",
			err:     io.EOF,
			wantIs:  []error{transport.ErrConnectionLost},
			wantMsg: "peer closed the WebSocket",
		},
		{
			name:    "net.ErrClosed is an orderly local close",
			err:     net.ErrClosed,
			wantIs:  []error{transport.ErrConnectionLost},
			wantMsg: "connection closed locally",
		},
		{
			name:   "os.ErrClosed is not net.ErrClosed — falls through",
			err:    os.ErrClosed,
			wantIs: []error{transport.ErrConnectionLost},
		},
		{
			name:   "unknown error still wraps connection-lost",
			err:    errors.New("boom"),
			wantIs: []error{transport.ErrConnectionLost},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := s.classifyReadErr(tc.err)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("classifyReadErr(nil) = %v, want nil", got)
				}
				return
			}
			for _, w := range tc.wantIs {
				if !errors.Is(got, w) {
					t.Errorf("classifyReadErr(%v) = %v; want errors.Is(..., %v)", tc.err, got, w)
				}
			}
			if tc.wantMsg != "" && !strings.Contains(got.Error(), tc.wantMsg) {
				t.Errorf("classifyReadErr(%v) = %v; want it to say %q",
					tc.err, got, tc.wantMsg)
			}
		})
	}
}

// The sentinel expansion is pure, so it is table-tested directly rather than
// through a live session.
func TestResolveKeepAlive(t *testing.T) {
	tests := []struct {
		name         string
		cfg          consumer.KeepAliveConfig
		wantInterval time.Duration
		wantTimeout  time.Duration
	}{
		{
			name:         "zero values take the plugin defaults",
			cfg:          consumer.KeepAliveConfig{},
			wantInterval: defaultKeepAliveInterval,
			wantTimeout:  3 * defaultKeepAliveInterval,
		},
		{
			name:         "explicit interval derives a 3x timeout",
			cfg:          consumer.KeepAliveConfig{Interval: 10 * time.Second},
			wantInterval: 10 * time.Second,
			wantTimeout:  30 * time.Second,
		},
		{
			name: "both explicit are honoured verbatim",
			cfg: consumer.KeepAliveConfig{
				Interval: 5 * time.Second,
				Timeout:  7 * time.Second,
			},
			wantInterval: 5 * time.Second,
			wantTimeout:  7 * time.Second,
		},
		{
			name:         "DisableInterval stops probing but keeps a dead-man window",
			cfg:          consumer.KeepAliveConfig{Interval: consumer.DisableInterval},
			wantInterval: 0,
			wantTimeout:  defaultKeepAliveTimeout,
		},
		{
			name: "DisableTimeout keeps probing but never declares death",
			cfg: consumer.KeepAliveConfig{
				Interval: 10 * time.Second,
				Timeout:  consumer.DisableTimeout,
			},
			wantInterval: 10 * time.Second,
			wantTimeout:  0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gi, gt := resolveKeepAlive(tc.cfg)
			if gi != tc.wantInterval {
				t.Errorf("interval = %v, want %v", gi, tc.wantInterval)
			}
			if gt != tc.wantTimeout {
				t.Errorf("timeout = %v, want %v", gt, tc.wantTimeout)
			}
		})
	}
}

// A stopped prober must not leave its ticker armed. A leaked ticker is both a
// memory leak in a year-long process and a source of CI flake.
func TestKeepAliveStopReleasesTicker(t *testing.T) {
	fs := newFakeCerebrum(t, func(fc *fakeConn) {
		for {
			if _, err := fc.readClientFrame(); err != nil {
				return
			}
		}
	})
	_, sess := dialFake(t, fs)

	// Replace the default (system-clock) prober with a fake-clock one so the
	// ticker accounting is observable.
	sess.stopKeepAlive()
	fake := clock.NewFake(time.Time{})
	sess.startKeepAlive(30*time.Second, fake)

	// The prober arms its ticker on its own goroutine; wait for it rather
	// than assuming it has been scheduled.
	waitFor(t, 5*time.Second, func() bool { return fake.Waiters() == 1 },
		"prober never armed its ticker")
	sess.stopKeepAlive()
	if n := fake.Waiters(); n != 0 {
		t.Fatalf("armed tickers = %d after stop, want 0 (leaked ticker)", n)
	}
}

// stopKeepAlive must be safe to call repeatedly and on a session that never
// started a prober — Disconnect paths call it unconditionally.
func TestKeepAliveStopIdempotent(t *testing.T) {
	s := &Session{done: make(chan struct{}), stopRX: make(chan struct{})}
	s.stopKeepAlive()
	s.stopKeepAlive()
}
