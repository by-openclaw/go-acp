package probelsw08p

// Tests for the spec-sanctioned cmd 08 keep-alive poll.
//
// Before this, sw08p's only keep-alive was the PASSIVE 0x11/0x22 responder —
// matrix-initiated, and not in the spec at all. A matrix that never pinged
// left the session with no liveness signal, so the reader's dead-man deadline
// could not safely be armed and half-open links went undetected.

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/clock"
	sw08session "dhs/internal/probel-sw08p/session"
)

func TestIdleWindowForPoll(t *testing.T) {
	tests := []struct {
		name    string
		spacing time.Duration
		want    time.Duration
	}{
		{
			name:    "3x the spacing once past the floor",
			spacing: 30 * time.Second,
			want:    90 * time.Second,
		},
		{
			name:    "floored so scheduling jitter cannot trip it",
			spacing: 1 * time.Second,
			want:    minKeepaliveIdleWindow,
		},
		{
			name:    "the default spacing lands on the floor",
			spacing: DefaultKeepalivePollSpacing,
			want:    minKeepaliveIdleWindow,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := idleWindowForPoll(tc.spacing); got != tc.want {
				t.Errorf("idleWindowForPoll(%v) = %v, want %v", tc.spacing, got, tc.want)
			}
		})
	}
}

// The window must always leave room for several missed polls, or one dropped
// frame tears down a healthy matrix link.
func TestIdleWindowAlwaysAllowsSeveralPolls(t *testing.T) {
	for _, spacing := range []time.Duration{
		time.Second, 5 * time.Second, DefaultKeepalivePollSpacing, time.Minute,
	} {
		if w := idleWindowForPoll(spacing); w < 3*spacing {
			t.Errorf("spacing %v -> window %v, which allows fewer than 3 polls", spacing, w)
		}
	}
}

func TestResolvedKeepalivePollSpacing(t *testing.T) {
	p := &Plugin{}
	if got := p.resolvedKeepalivePollSpacing(); got != DefaultKeepalivePollSpacing {
		t.Fatalf("zero value = %v, want the default %v", got, DefaultKeepalivePollSpacing)
	}
	p.SetKeepalivePollSpacing(3 * time.Second)
	if got := p.resolvedKeepalivePollSpacing(); got != 3*time.Second {
		t.Fatalf("explicit = %v, want 3s", got)
	}
	// Negative disables; startKeepalivePoll must then arm nothing.
	p.SetKeepalivePollSpacing(-1)
	if got := p.resolvedKeepalivePollSpacing(); got != -1 {
		t.Fatalf("negative = %v, want it preserved as the disable sentinel", got)
	}
}

// Disabling the poll must ALSO leave the dead-man deadline disarmed: the two
// are meaningless apart, since without a probe silence proves nothing.
func TestKeepalivePollDisabledArmsNothing(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	fake := clock.NewFake(time.Time{})
	p.startKeepalivePoll(-1, fake)

	if p.kaPoll != nil {
		t.Error("a disabled poll must not start a prober")
	}
	if n := fake.Waiters(); n != 0 {
		t.Errorf("armed timers = %d with the poll disabled, want 0", n)
	}
}

// startKeepalivePoll on a plugin with no client must be a no-op, not a panic —
// Connect wires them in order and tests construct bare plugins.
func TestKeepalivePollNilClientSafe(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p.startKeepalivePoll(time.Second, clock.NewFake(time.Time{}))
	p.stopKeepalivePoll()
}

// stopKeepalivePoll is called unconditionally from Disconnect, so it must
// tolerate never having started.
func TestKeepalivePollStopIdempotent(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	p.stopKeepalivePoll()
	p.stopKeepalivePoll()
}

// A second start must be a no-op — Connect wires it once, and a double-arm
// would leak a prober goroutine per call.
func TestKeepalivePollDoubleStartIsNoop(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cli, cleanup := dialSilentMatrix(t)
	defer cleanup()
	p.client = cli

	fake := clock.NewFake(time.Time{})
	p.startKeepalivePoll(time.Second, fake)
	first := p.kaPoll
	p.startKeepalivePoll(time.Second, fake)
	if p.kaPoll != first {
		t.Fatal("a second startKeepalivePoll replaced the running prober")
	}
	p.stopKeepalivePoll()
}

// A nil clock falls back to the system clock rather than panicking.
func TestKeepalivePollNilClockDefaults(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cli, cleanup := dialSilentMatrix(t)
	defer cleanup()
	p.client = cli

	p.startKeepalivePoll(time.Hour, nil)
	if p.kaPoll == nil {
		t.Fatal("a nil clock must default to the system clock, not skip the prober")
	}
	p.stopKeepalivePoll()
}

// On each tick the prober writes one cmd 08 frame; when the socket dies the
// loop exits instead of spinning on a dead link.
func TestKeepalivePollWritesAndExitsOnWriteError(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	cli, cleanup := dialSilentMatrix(t)
	p.client = cli

	fake := clock.NewFake(time.Time{})
	p.startKeepalivePoll(time.Second, fake)
	ka := p.kaPoll
	if ka == nil {
		t.Fatal("prober did not start")
	}
	// Let the prober arm its ticker, then fire a tick: one cmd 08 goes out.
	waitForTicker(t, fake)
	fake.Advance(time.Second)

	// Kill the socket: the next tick's write fails and the loop returns.
	cleanup()
	done := make(chan struct{})
	go func() { ka.stopped.Wait(); close(done) }()
	for i := 0; i < 50; i++ {
		fake.Advance(time.Second)
		select {
		case <-done:
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("prober did not exit after the socket died")
}

// dialSilentMatrix returns a sw08session.Client wired to a peer that accepts and
// never speaks — enough to exercise the write side.
func dialSilentMatrix(t *testing.T) (*sw08session.Client, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		c, aerr := ln.Accept()
		if aerr == nil {
			go func() { _, _ = io.Copy(io.Discard, c) }()
		}
	}()
	cli, err := sw08session.Dial(context.Background(), nil, ln.Addr().String(),
		slog.New(slog.NewTextHandler(io.Discard, nil)), sw08session.ClientConfig{})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return cli, func() { _ = cli.Close(); _ = ln.Close() }
}

func waitForTicker(t *testing.T, f *clock.Fake) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if f.Waiters() > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("prober never armed its ticker")
}
