package cerebrumnb

// Supervisor — keeps a Cerebrum watch alive across connection loss.
//
// The cycle it drives is not Cerebrum-specific, so the implementation now
// lives in internal/consumer as a generic Supervisor shared by every
// connector. What stays here is the Cerebrum-shaped face of it: the exported
// type the CLI already builds, typed on *Session, with the same fields and
// the same behaviour.
//
// Re-running Setup on every reconnect is still the point: a Cerebrum
// subscription lives on the SERVER and dies with the socket, so reconnecting
// without re-subscribing yields a connected watcher that shows nothing.

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
)

// Backoff schedules reconnect attempts — the shared schedule (1 s → 30 s).
type Backoff = consumer.Backoff

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
	// Validated here rather than in the shared Supervisor so the operator
	// still gets the connector's name in the message.
	if s.Dial == nil || s.Setup == nil {
		return errors.New("cerebrum-nb supervisor: Dial and Setup are required")
	}
	sup := &consumer.Supervisor[*Session]{
		Dial:  s.Dial,
		Setup: s.Setup,
		// Cerebrum's Session carries both halves: Done closes on death and
		// Err holds the typed reason classifyReadErr produced.
		Done:  func(sess *Session) <-chan struct{} { return sess.Done() },
		Err:   func(sess *Session) error { return sess.Err() },
		Close: func(sess *Session) { s.closeSession(sess) },

		Logger:        s.Logger,
		Clock:         s.Clock,
		Backoff:       s.Backoff,
		LoggerFn:      s.LoggerFn,
		OnLost:        s.OnLost,
		OnReconnected: s.OnReconnected,
	}
	return sup.Run(ctx)
}

// closeSession tears a session down, tolerating nil and double-close.
func (s *Supervisor) closeSession(sess *Session) {
	if sess == nil {
		return
	}
	_ = sess.close()
}
