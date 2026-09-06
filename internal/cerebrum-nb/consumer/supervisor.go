package cerebrumnb

// Supervisor — keeps a watch alive across connection loss.
//
// keepalive.go made a dead session DETECTABLE: the read deadline fires, the
// prober notices a broken socket, and Session.Done() closes carrying a typed
// transport.ErrConnectionLost. That alone does not keep a 24/7 watcher
// running — something has to act on it. Before this, cerebrum-nb watch
// blocked on <-ctx.Done() and never looked at the session at all, so a lost
// connection meant the process sat there, alive and silent, forever.
//
// Supervisor is that missing half. It owns the dial/setup/observe cycle:
//
//	dial + login  →  setup (baseline + subscribe + handlers)  →  wait
//	     ↑                                                        │
//	     └────────── backoff ←── session died (typed reason) ─────┘
//
// Re-running Setup on every reconnect is the point: a Cerebrum subscription
// lives on the SERVER, so a new socket starts with none. Reconnecting without
// re-subscribing yields a connected watcher that still shows nothing — the
// original bug wearing a different hat.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"dhs/internal/clock"
	"dhs/internal/transport"
)

// Backoff schedules reconnect attempts. Exponential from Initial, doubling to
// Max. Matches the acp2 warm-restart schedule (1s → 30s) so the fleet behaves
// the same way whichever connector lost its link.
type Backoff struct {
	Initial time.Duration
	Max     time.Duration
	// MaxAttempts caps consecutive failed attempts. 0 = never give up,
	// which is the right default for a watcher that must survive a
	// weekend outage without an operator present.
	MaxAttempts int
}

// Defaults for a 24/7 watcher.
const (
	defaultBackoffInitial = 1 * time.Second
	defaultBackoffMax     = 30 * time.Second
)

func (b Backoff) withDefaults() Backoff {
	if b.Initial <= 0 {
		b.Initial = defaultBackoffInitial
	}
	if b.Max <= 0 {
		b.Max = defaultBackoffMax
	}
	if b.Max < b.Initial {
		b.Max = b.Initial
	}
	return b
}

// next doubles d, clamped to Max. Pure, so the schedule is unit-tested
// without waiting in real time.
func (b Backoff) next(d time.Duration) time.Duration {
	d *= 2
	if d > b.Max {
		return b.Max
	}
	return d
}

// Supervisor keeps one logical watch connected. All collaborators are
// injected — no globals, and tests substitute each one directly.
type Supervisor struct {
	// Dial opens a session and completes login. Required.
	Dial func(context.Context) (*Session, error)

	// Setup re-establishes everything that lives on the SERVER or in the
	// caller's rendering state: event handlers, the baseline read, and the
	// subscriptions. Called after every successful Dial, including the
	// first. Required.
	Setup func(context.Context, *Session) error

	Logger  *slog.Logger
	Clock   clock.Clock
	Backoff Backoff

	// LoggerFn, when set, is consulted at call time instead of Logger. The
	// CLI builds its logger during the FIRST dial, so a logger captured
	// before Run starts would still be nil; resolving lazily lets the
	// reconnect messages reach the sink the operator configured.
	LoggerFn func() *slog.Logger

	// OnLost, when set, is called with the typed reason as soon as the link
	// dies — before any backoff — so a gap in the output is explained at the
	// moment it starts.
	OnLost func(error)

	// OnReconnected, when set, is called after a successful re-establish
	// (never for the initial connect). The CLI uses it to tell the operator
	// the gap has closed and rows are flowing again.
	OnReconnected func(attempt int, downtime time.Duration)
}

// Run connects, then keeps the session alive until ctx is cancelled.
//
// Returns nil on ctx cancellation (the operator stopped it — an orderly end,
// not a failure). Returns a typed error when the initial connect fails or the
// attempt budget is exhausted, so the CLI exits with the contract's runtime
// code rather than pretending it worked.
func (s *Supervisor) Run(ctx context.Context) error {
	if s.Dial == nil || s.Setup == nil {
		return errors.New("cerebrum-nb supervisor: Dial and Setup are required")
	}
	clk := s.Clock
	if clk == nil {
		clk = clock.System()
	}
	bo := s.Backoff.withDefaults()

	// Initial connect uses the caller's context directly: a failure here is
	// the operator's problem to see immediately (bad host, bad password),
	// not something to silently retry behind a backoff.
	sess, err := s.connect(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			s.closeSession(sess)
			return nil
		case <-sess.Done():
		}

		// Distinguish "the operator stopped us" from "the link died".
		// Closing the session marks it lost too, so without this check a
		// clean Ctrl-C would be reported as a connection failure.
		if ctx.Err() != nil {
			s.closeSession(sess)
			return nil
		}

		lost := sess.Err()
		s.log().Warn("session lost — reconnecting",
			slog.String("reason", errText(lost)))
		if s.OnLost != nil {
			s.OnLost(lost)
		}
		s.closeSession(sess)

		lostAt := clk.Now()
		newSess, rerr := s.reconnect(ctx, clk, bo, lostAt)
		if rerr != nil {
			return rerr
		}
		if newSess == nil { // ctx cancelled while backing off
			return nil
		}
		sess = newSess
	}
}

// reconnect retries dial+setup on the backoff schedule until it succeeds, ctx
// ends (returns nil, nil), or the attempt budget is spent (returns an error).
func (s *Supervisor) reconnect(ctx context.Context, clk clock.Clock,
	bo Backoff, lostAt time.Time) (*Session, error) {

	delay := bo.Initial
	for attempt := 1; ; attempt++ {
		if err := clk.Sleep(ctx, delay); err != nil {
			return nil, nil // ctx cancelled during backoff — an orderly stop
		}

		s.log().Info("reconnect attempt",
			slog.Int("attempt", attempt),
			slog.Duration("after", delay))

		sess, err := s.connect(ctx)
		if err == nil {
			downtime := clk.Now().Sub(lostAt)
			s.log().Info("reconnected",
				slog.Int("attempt", attempt),
				slog.Duration("downtime", downtime))
			if s.OnReconnected != nil {
				s.OnReconnected(attempt, downtime)
			}
			return sess, nil
		}
		if ctx.Err() != nil {
			return nil, nil
		}

		s.log().Warn("reconnect failed",
			slog.Int("attempt", attempt),
			slog.String("err", err.Error()))

		if bo.MaxAttempts > 0 && attempt >= bo.MaxAttempts {
			return nil, fmt.Errorf("%w: giving up after %d reconnect attempts: %v",
				transport.ErrConnectionLost, attempt, err)
		}
		delay = bo.next(delay)
	}
}

// connect dials and runs Setup. A Setup failure closes the fresh session so a
// half-built one is never handed back or leaked.
func (s *Supervisor) connect(ctx context.Context) (*Session, error) {
	sess, err := s.Dial(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Setup(ctx, sess); err != nil {
		s.closeSession(sess)
		return nil, err
	}
	return sess, nil
}

// closeSession tears a session down, tolerating nil and double-close.
func (s *Supervisor) closeSession(sess *Session) {
	if sess == nil {
		return
	}
	_ = sess.close()
}

// log resolves the logger at call time, preferring LoggerFn.
func (s *Supervisor) log() *slog.Logger {
	if s.LoggerFn != nil {
		if l := s.LoggerFn(); l != nil {
			return l
		}
	}
	if s.Logger != nil {
		return s.Logger
	}
	return slog.New(slog.NewTextHandler(discard{}, nil))
}

func errText(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}

// discard is an io.Writer sink for the nil-logger fallback.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
