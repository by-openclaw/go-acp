package transport

// Idle — the shared read-deadline bound for any long-lived session.
//
// Every connector that holds a socket open for months needs the same thing: a
// limit on how long the peer may be silent, re-armed before each read, so a
// half-open connection (a NAT or firewall drop with no RST) surfaces as an
// error instead of parking a goroutine in the kernel forever.
//
// It applies to BOTH roles, and the provider side is not the lesser case:
//   - a consumer without it never notices its device died;
//   - a provider without it accumulates one dead session, goroutine and socket
//     per client that vanished — a slow leak in a process meant to run for a
//     year, invisible because nothing ever errors.
//
// This was hand-written six times before it lived here. Beyond the
// duplication, every copy shared a subtle race, which is the other reason it
// is now one implementation: arming is NOT just "read the value, set the
// deadline". Those two steps interleave with a concurrent Set:
//
//	reader: d := Get()                  -> reads the old, long value
//	setter: store short; SetReadDeadline(short)
//	reader: SetReadDeadline(old)        -> overwrites the short one
//
// The reader then blocks for the long window while the caller believes it
// tightened the bound. That is not theoretical — it turned a deadline test
// green on three CI runners and hung it on a fourth. Arm and Set therefore
// share a mutex here, so no caller can reintroduce it.
//
// IMPORTANT — arm this only where silence is actually meaningful. A deadline
// asserts "no bytes for D means dead", which is only true when something
// guarantees traffic: an application keep-alive, a poll, or a peer that
// refreshes on a timer. Protocols with no heartbeat (TSL, OSC) have
// legitimately silent-but-healthy links; arming there disconnects working
// peers. Leave it disabled and let the operator opt in.
//
// The zero value is valid and disabled.

import (
	"net"
	"sync"
	"time"
)

// Idle carries a read-deadline duration and applies it to a connection.
// Safe for concurrent use.
type Idle struct {
	mu sync.Mutex
	d  time.Duration
}

// Set arms (d > 0) or disables (d <= 0) the bound, WITHOUT touching a socket.
// Use SetOn when a reader may already be blocked.
//
// A negative duration disables rather than arming a deadline in the past —
// callers use -1 as an explicit "off" sentinel, and taking it literally would
// tear the session down instantly.
func (i *Idle) Set(d time.Duration) {
	if d < 0 {
		d = 0
	}
	i.mu.Lock()
	i.d = d
	i.mu.Unlock()
}

// SetOn arms the bound and pushes it onto c immediately, as one step with
// respect to a concurrent Arm.
//
// Immediate application matters when the reader is ALREADY blocked: storing
// the value alone would not reach it, so a tightened window would not take
// effect until the previous (possibly infinite) deadline expired.
func (i *Idle) SetOn(c net.Conn, d time.Duration) error {
	if d < 0 {
		d = 0
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	i.d = d
	return i.applyLocked(c)
}

// Get reports the current bound; 0 means disabled.
func (i *Idle) Get() time.Duration {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.d
}

// Enabled reports whether a bound is armed.
func (i *Idle) Enabled() bool { return i.Get() > 0 }

// Arm applies the bound to c as an absolute read deadline. Call it
// immediately before each read.
//
// When DISABLED this is a no-op — it deliberately does not clear the
// connection's deadline. A read loop calls Arm on every pass, and the socket's
// deadline is not exclusively ours: callers set one for their own reasons (a
// bounded handshake, a test forcing an immediate timeout). Clearing it here
// would silently undo that on the very next read. Turning the bound off for a
// specific connection is what SetOn(c, 0) is for — an explicit request, not a
// side effect of a disabled reaper.
//
// A nil conn is a no-op: control paths may arm before a socket exists.
func (i *Idle) Arm(c net.Conn) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if c == nil || i.d <= 0 {
		return nil
	}
	return c.SetReadDeadline(time.Now().Add(i.d))
}

// applyLocked pushes the current bound onto c. Caller holds i.mu.
func (i *Idle) applyLocked(c net.Conn) error {
	if c == nil {
		return nil
	}
	if i.d > 0 {
		return c.SetReadDeadline(time.Now().Add(i.d))
	}
	return c.SetReadDeadline(time.Time{})
}
