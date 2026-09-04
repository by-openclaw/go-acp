package probelsw08p

// Tests for the spec-sanctioned cmd 08 keep-alive poll.
//
// Before this, sw08p's only keep-alive was the PASSIVE 0x11/0x22 responder —
// matrix-initiated, and not in the spec at all. A matrix that never pinged
// left the session with no liveness signal, so the reader's dead-man deadline
// could not safely be armed and half-open links went undetected.

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/clock"
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
