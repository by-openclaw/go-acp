package protocol

import "time"

// ReconnectConfig is the cross-protocol auto-reconnect configuration.
// CLI flags --reconnect / --reconnect-cap / --reconnect-max-attempts
// populate it, then connect() hands it to the plugin via SetReconnect
// (optional capability — plugins that don't implement Reconnecter
// silently ignore it).
//
// Zero values mean "use plugin default": Initial=0 → 1 s, Cap=0 → 30 s,
// MaxAttempts=0 → unlimited. Disabled=true skips the reconnect loop
// entirely (current pre-#367 behaviour for backwards-compat scripts
// that prefer hard exit on session loss).
type ReconnectConfig struct {
	// Initial backoff delay before the first reconnect attempt after
	// a session loss. 0 = plugin default (1 s).
	Initial time.Duration

	// Cap caps the exponential backoff. 0 = plugin default (30 s).
	// Backoff doubles on each failed attempt up to this cap.
	Cap time.Duration

	// MaxAttempts limits the number of reconnect attempts before the
	// plugin gives up. 0 = unlimited (the watch verb keeps retrying
	// indefinitely; operator interrupts with Ctrl-C).
	MaxAttempts int

	// Disabled turns the reconnect loop off. Useful for short-lived
	// CLI verbs (info / get / set) that should fail-fast rather than
	// wait for retry. The watch / metrics serve verbs leave it false.
	Disabled bool
}

// Reconnecter is the optional capability a plugin satisfies when it
// supports automatic session re-establishment after loss. The plugin
// owns the reconnect goroutine, exponential backoff, and re-issuing
// any active subscriptions after a successful re-handshake.
//
// Plugins that don't implement Reconnecter are unaffected; the harness
// type-asserts and only calls SetReconnect when satisfied.
type Reconnecter interface {
	SetReconnect(cfg ReconnectConfig)
}
