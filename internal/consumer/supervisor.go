package consumer

// Supervisor — keeps a session alive across connection loss, for any protocol.
//
// Detection was solved per connector: a read deadline fires, a keep-alive
// probe notices a broken socket, and the session's death channel closes. That
// alone does not keep a 24/7 watcher running — something has to act on it,
// and until now only cerebrum-nb had that something. acp1, probel-sw02p and
// probel-sw08p detected death and then sat there, connected to nothing.
//
// The cycle is the same for every protocol:
//
//	dial + login  →  setup (baseline + subscribe + handlers)  →  wait
//	     ↑                                                        │
//	     └────────── backoff ←── session died (typed reason) ─────┘
//
// Re-running Setup on every reconnect is the point: a subscription that lives
// on the SERVER dies with the socket, so reconnecting without re-subscribing
// yields a connected watcher that still shows nothing — the original bug
// wearing a different hat.
//
// It is generic over the session type and takes the session's lifecycle as
// FUNCTIONS rather than demanding an interface. That is deliberate: the six
// connectors spell the death signal two different ways (Done on the acp2 and
// cerebrum-nb sessions, ReaderDone on the acp1 and probel clients), and
// renaming a method across four already-approved packages to satisfy a new
// abstraction is the tail wagging the dog. A closure per call site costs three
// lines and changes nothing that already works.

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
// Max. 1 s → 30 s matches the acp2 warm-restart schedule, so the fleet behaves
// the same way whichever connector lost its link.
type Backoff struct {
	Initial time.Duration
	Max     time.Duration
	// MaxAttempts caps consecutive failed attempts. 0 = never give up, which
	// is the right default for a watcher that must survive a weekend outage
	// with no operator present.
	MaxAttempts int
}

// Defaults for a 24/7 watcher.
const (
	DefaultBackoffInitial = 1 * time.Second
	DefaultBackoffMax     = 30 * time.Second
)

func (b Backoff) withDefaults() Backoff {
	if b.Initial <= 0 {
		b.Initial = DefaultBackoffInitial
	}
	if b.Max <= 0 {
		b.Max = DefaultBackoffMax
	}
	if b.Max < b.Initial {
		b.Max = b.Initial
	}
	return b
}

// next doubles d, clamped to Max. Pure, so the schedule is unit-tested without
// waiting in real time.
func (b Backoff) next(d time.Duration) time.Duration {
	d *= 2
	if d > b.Max {
		return b.Max
	}
	return d
}

// Supervisor keeps one logical session connected. Every collaborator is
// injected — no globals, and a test substitutes each one directly.
//
// S is the connector's own session or client type; nothing here needs to know
// what it is.
type Supervisor[S any] struct {
	// Dial opens a session and completes login. Required.
	Dial func(context.Context) (S, error)

	// Setup re-establishes everything that lives on the SERVER or in the
	// caller's rendering state: event handlers, the baseline read, the
	// subscriptions. Called after every successful Dial, the first included.
	// Required.
	Setup func(context.Context, S) error

	// Done returns the channel that closes when the session dies. Required —
	// it is the whole reason this type exists.
	Done func(S) <-chan struct{}

	// Err returns the typed reason the session died. Optional: connectors
	// whose client does not record one leave it nil and the reason is
	// reported as unknown.
	Err func(S) error

	// Close tears the session down. Optional; a connector whose session
	// cleans itself up on death leaves it nil.
	Close func(S)

	Logger  *slog.Logger
	Clock   clock.Clock
	Backoff Backoff

	// LoggerFn, when set, is consulted at call time instead of Logger. A CLI
	// that builds its logger during the FIRST dial would otherwise capture a
	// nil one; resolving lazily lets reconnect messages reach the sink the
	// operator configured.
	LoggerFn func() *slog.Logger

	// OnLost, when set, is called with the typed reason as soon as the link
	// dies — before any backoff — so a gap in the output is explained at the
	// moment it starts.
	OnLost func(error)

	// OnReconnected, when set, is called after a successful re-establish
	// (never for the initial connect), so the operator is told the gap has
	// closed and data is flowing again.
	OnReconnected func(attempt int, downtime time.Duration)
}

// Run connects, then keeps the session alive until ctx is cancelled.
//
// Returns nil on ctx cancellation (the operator stopped it — an orderly end,
// not a failure). Returns a typed error when the initial connect fails or the
// attempt budget is exhausted, so a CLI exits with the contract's runtime code
// rather than pretending it worked.
func (s *Supervisor[S]) Run(ctx context.Context) error {
	if s.Dial == nil || s.Setup == nil || s.Done == nil {
		return errors.New("supervisor: Dial, Setup and Done are required")
	}
	clk := s.Clock
	if clk == nil {
		clk = clock.System()
	}
	bo := s.Backoff.withDefaults()

	// The initial connect uses the caller's context directly: a failure here
	// is the operator's problem to see immediately (bad host, bad password),
	// not something to retry silently behind a backoff.
	sess, err := s.connect(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			s.closeSession(sess)
			return nil
		case <-s.Done(sess):
		}

		// Distinguish "the operator stopped us" from "the link died".
		// Closing a session marks it lost too, so without this check a clean
		// Ctrl-C would be reported as a connection failure.
		if ctx.Err() != nil {
			s.closeSession(sess)
			return nil
		}

		lost := s.reason(sess)
		s.log().Warn("session lost — reconnecting",
			slog.String("reason", errText(lost)))
		if s.OnLost != nil {
			s.OnLost(lost)
		}
		s.closeSession(sess)

		lostAt := clk.Now()
		newSess, ok, rerr := s.reconnect(ctx, clk, bo, lostAt)
		if rerr != nil {
			return rerr
		}
		if !ok { // ctx cancelled while backing off
			return nil
		}
		sess = newSess
	}
}

// reconnect retries dial+setup on the backoff schedule until it succeeds, ctx
// ends (ok=false), or the attempt budget is spent (error).
func (s *Supervisor[S]) reconnect(ctx context.Context, clk clock.Clock,
	bo Backoff, lostAt time.Time) (S, bool, error) {

	var zero S
	delay := bo.Initial
	for attempt := 1; ; attempt++ {
		if err := clk.Sleep(ctx, delay); err != nil {
			return zero, false, nil // ctx cancelled during backoff — orderly stop
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
			return sess, true, nil
		}
		if ctx.Err() != nil {
			return zero, false, nil
		}

		s.log().Warn("reconnect failed",
			slog.Int("attempt", attempt),
			slog.String("err", err.Error()))

		if bo.MaxAttempts > 0 && attempt >= bo.MaxAttempts {
			return zero, false, fmt.Errorf("%w: giving up after %d reconnect attempts: %v",
				transport.ErrConnectionLost, attempt, err)
		}
		delay = bo.next(delay)
	}
}

// connect dials and runs Setup. A Setup failure closes the fresh session so a
// half-built one is never handed back or leaked.
func (s *Supervisor[S]) connect(ctx context.Context) (S, error) {
	sess, err := s.Dial(ctx)
	if err != nil {
		var zero S
		return zero, err
	}
	if err := s.Setup(ctx, sess); err != nil {
		s.closeSession(sess)
		var zero S
		return zero, err
	}
	return sess, nil
}

// reason reports why the session died, or nil when the connector does not
// record one.
func (s *Supervisor[S]) reason(sess S) error {
	if s.Err == nil {
		return nil
	}
	return s.Err(sess)
}

// closeSession tears a session down when the connector supplied a closer.
func (s *Supervisor[S]) closeSession(sess S) {
	if s.Close != nil {
		s.Close(sess)
	}
}

// log resolves the logger at call time, preferring LoggerFn.
func (s *Supervisor[S]) log() *slog.Logger {
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
