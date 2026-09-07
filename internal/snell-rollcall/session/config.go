package session

import "time"

// Protocol timers.
//
// The values come from the vendor library and were checked against the live
// stack during the audit; the reasoning for each is in
// internal/snell-rollcall/docs/audit-2026-09-07.md §3.5.
const (
	// DefaultReplyTimeout bounds one active message. The specification sets
	// three seconds, and a server that needs longer says so with Wait rather
	// than staying silent.
	DefaultReplyTimeout = 3 * time.Second

	// DefaultMaxStrikes is how many consecutive failures end a session. The
	// vendor uses five and then drops the session without sending Term,
	// which is how units run out of sessions; we send it.
	DefaultMaxStrikes = 5

	// DefaultKeepaliveInterval is how long a link may sit idle before it is
	// probed. Fifteen seconds matches RollProxy's own KeepAliveInterval and
	// stays well inside the sixty-second map expiry, so we are never aged
	// out of a peer's device map while still connected.
	DefaultKeepaliveInterval = 15 * time.Second

	// DefaultMaxKeepaliveMisses is how many probes may go unanswered before
	// the link is considered dead.
	DefaultMaxKeepaliveMisses = 3

	// DefaultIamInterval is the announcement period. The specification asks
	// for 12.5 to 17.5 seconds, and the vendor spreads units within that
	// window by adding twenty milliseconds per unit address so a large frame
	// does not announce every module in the same instant.
	DefaultIamInterval = 12500 * time.Millisecond

	// IamSpreadPerUnit is that per-unit offset.
	IamSpreadPerUnit = 20 * time.Millisecond

	// DefaultPushQueue is how many back-channel pushes may be held for a
	// slow reader before the link stops acknowledging them. Acknowledgement
	// is what asks for the next push, so holding it is the flow control the
	// protocol gives us.
	DefaultPushQueue = 64
)

// Config tunes a Link. The zero value is usable: every field falls back to
// the constant above it.
type Config struct {
	// ReplyTimeout bounds one active message.
	ReplyTimeout time.Duration

	// MaxStrikes is how many consecutive failures end a session.
	MaxStrikes int

	// KeepaliveInterval is how long the link may be idle before it is
	// probed. Negative disables probing, which is only appropriate for a
	// link whose peer is known to push continuously.
	KeepaliveInterval time.Duration

	// MaxKeepaliveMisses is how many unanswered probes end the link.
	MaxKeepaliveMisses int

	// PushQueue is the depth of the back-channel delivery queue.
	PushQueue int

	// Local is our own address on this link.
	//
	// A TCP client leaves the net, unit and port zero and lets the gateway
	// stamp them, which is what the vendor's IPShare client does; the
	// address it assigns is learned from the first reply and filled in here.
	// A provider sets its own address, because it is the one doing the
	// stamping.
	Local Address
}

// Address mirrors codec.Address without importing it into a configuration
// struct a caller has to fill in by hand.
type Address struct {
	Net  uint16
	Unit uint8
	Port uint8
}

func (c Config) replyTimeout() time.Duration {
	if c.ReplyTimeout > 0 {
		return c.ReplyTimeout
	}
	return DefaultReplyTimeout
}

func (c Config) maxStrikes() int {
	if c.MaxStrikes > 0 {
		return c.MaxStrikes
	}
	return DefaultMaxStrikes
}

func (c Config) keepaliveInterval() time.Duration {
	switch {
	case c.KeepaliveInterval > 0:
		return c.KeepaliveInterval
	case c.KeepaliveInterval < 0:
		return 0 // explicitly disabled
	default:
		return DefaultKeepaliveInterval
	}
}

func (c Config) maxKeepaliveMisses() int {
	if c.MaxKeepaliveMisses > 0 {
		return c.MaxKeepaliveMisses
	}
	return DefaultMaxKeepaliveMisses
}

func (c Config) pushQueue() int {
	if c.PushQueue > 0 {
		return c.PushQueue
	}
	return DefaultPushQueue
}
