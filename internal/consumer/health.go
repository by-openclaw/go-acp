package consumer

import (
	"context"
	"time"
)

// SessionHealth is the 3-layer protocol-agnostic health snapshot every
// plugin can produce on demand. The bits are orthogonal: a session can
// be Connected without being Live (device hung but TCP up) or Reachable
// without being Connected (network OK, session dropped — reconnect).
//
// Layer mapping:
//
//   - Reachable: network path exists. Verified via ICMP / ARP / TCP SYN-ACK
//     on demand; not constantly pinged.
//
//   - Connected: an active session is open. For TCP: socket ESTABLISHED
//     and any application-level handshake (AN2 negotiated, S101
//     keepalive, ...) complete. For UDP: the dialed remote has produced
//     at least one reply since open.
//
//   - Live: the device is responding now. Set when (now - LastRx) is
//     within StaleAfter; cleared when traffic falls silent past that
//     window. The threshold is per-protocol (Probel 90s, TSL 5s,
//     Ember+ 30s, ACP1 90s, ...).
//
// Aggregating across protocols on one device:
//
//	Device.IsOnline = ANY session.Live
//
// Each session keeps its own (Reachable, Connected, Live). UI surfaces
// them as a per-protocol matrix on the device card; collapsing them into
// one bool loses the diagnostic.
type SessionHealth struct {
	Reachable bool
	Connected bool
	Live      bool

	// LastRx is the timestamp of the last frame received from the peer
	// in this session. Zero if no frame has been received yet.
	LastRx time.Time

	// LastTx is the timestamp of the last frame sent to the peer in this
	// session. Zero if nothing sent yet.
	LastTx time.Time

	// StaleAfter is the rolling window the plugin uses to decide Live:
	// if (now - LastRx) > StaleAfter the device is judged not Live.
	// Plugins set this to their per-protocol convention (e.g. ACP1 = 90s).
	StaleAfter time.Duration
}

// IsLiveAt is a pure helper that recomputes Live for a given evaluation
// time. Useful in tests and for UI render-time evaluation. Does not
// mutate the receiver.
func (h SessionHealth) IsLiveAt(now time.Time) bool {
	if h.LastRx.IsZero() || h.StaleAfter <= 0 {
		return false
	}
	return now.Sub(h.LastRx) <= h.StaleAfter
}

// HealthChecker is the optional interface a Protocol implementation can
// satisfy to expose the 3-layer session health snapshot. Cross-protocol
// callers (CLI verbs, REST API, UI) type-assert to this interface
// rather than calling protocol-specific methods.
//
// Plugins MAY return early with a snapshot reflecting their best
// current state without performing fresh probes; ctx is provided so
// implementations that DO probe (e.g. Reachable via TCP SYN test) can
// honour cancellation.
type HealthChecker interface {
	SessionHealth(ctx context.Context) SessionHealth
}
