package emberplus

// Compliance event labels — producer-side named deviations the Ember+
// provider records (does NOT silently work around). Same philosophy as
// the consumer side (internal/emberplus/consumer/compliance_events.go):
// absorb + fire event; never silently swallow a state change.
//
// The generic Profile counter lives in internal/consumer/compliance/.
// Aggregated across every accepted session since Serve started.
//
// Adding a new label is an API change — downstream tooling may
// aggregate by key.
const (
	// StreamIdleTTLExpired fires when the stream idle-TTL sweep
	// (server.sweepStreamIdleSubs, R9 #472) clears the subscription
	// set on a session that has not sent any frame (keepalive
	// included) for streamIdleTTL. The TCP session stays open — only
	// the subs are released, mirroring an explicit per-OID
	// Unsubscribe sequence from the peer. Operators see the deviation
	// in the profile counter; high counts suggest stream consumers
	// that crash without unsubscribing or sit behind a NAT that
	// silently drops their keepalives.
	StreamIdleTTLExpired = "stream_idle_ttl_expired"
)
