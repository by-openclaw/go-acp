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
