package consumer

import "time"

// KeepAliveConfig is the cross-protocol keep-alive configuration. CLI
// flags --keepalive / --keepalive-timeout populate it, then connect()
// hands it to the plugin via SetKeepAlive (optional capability —
// plugins that don't implement KeepAliver silently ignore it).
//
// Zero values mean "use the plugin's default for that knob": Interval=0
// asks the plugin for its native cadence (acp1: 5s, ember+: 10s, ...)
// and Timeout=0 expands to 3*Interval. Pass DisableInterval to turn
// probes off entirely; pass DisableTimeout to keep probing but never
// declare the session dead.
type KeepAliveConfig struct {
	// Interval between keep-alive probes. 0 = plugin default.
	// DisableInterval = no probes.
	Interval time.Duration

	// Timeout (dead-man threshold) — declare session dead after this
	// long without any RX activity. 0 = 3 * Interval.
	// DisableTimeout = never declare dead.
	Timeout time.Duration
}

// DisableInterval / DisableTimeout are sentinel values for the CLI
// path. -1 (a value the user can never type as a duration but the
// flag parser maps to "off") tells the plugin to skip the watchdog
// or the prober without confusing it with the zero=default case.
const (
	DisableInterval time.Duration = -1
	DisableTimeout  time.Duration = -1
)

// KeepAliver is the optional capability a plugin satisfies when it
// supports the cross-protocol keep-alive contract. The plugin owns the
// actual probe goroutine and dead-man watchdog; this interface just
// hands it the cadence + threshold the user picked at the CLI.
//
// Plugins that don't implement KeepAliver are unaffected; the harness
// type-asserts and only calls SetKeepAlive when satisfied. Today:
// acp1 (this PR), emberplus (existing implementation).
type KeepAliver interface {
	SetKeepAlive(cfg KeepAliveConfig)
}

// SessionLiveness is the pull-side accessor the watch verb (and any
// future health-aware caller) uses to drive the freshness column.
//
// True = lastRX within the dead-man threshold. Plugins that don't
// implement SessionLiveAccessor are treated as always-live (no
// freshness downgrade) — matches today's behaviour.
type SessionLiveAccessor interface {
	SessionLive() bool
}

// SessionDoneAccessor is the push-side twin of SessionLiveAccessor: a channel
// that closes when the session ends, whether from a peer close, an I/O error
// or an idle deadline firing.
//
// SessionLive answers "is the data I am showing fresh?" — it is polled.
// SessionDone answers "is this session over?" — it is waited on, and it is
// what a Supervisor blocks in order to drive reconnection. Detection without
// it is a watcher that knows the link is dead and keeps running anyway, which
// is the 24/7 stall the operator reported.
//
// Optional, like every capability in this file. A plugin that does not
// implement it keeps today's behaviour exactly: the watch verb runs without a
// supervisor. Connectionless transports (ACP1 over UDP) have no session to
// lose and correctly do not implement it.
type SessionDoneAccessor interface {
	SessionDone() <-chan struct{}
}
