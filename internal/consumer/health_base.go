package consumer

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	"dhs/internal/metrics"
	"dhs/internal/transport"
)

// RxTxTimes is everything Health needs from a session: when the peer last
// spoke, and when we last spoke to it.
//
// It is an interface rather than two fields because connectors already track
// this in different places — acp2 on its session, acp1 in a transport-wrapping
// sink, everyone else in the injected metrics.Connector. Health does not care
// which, so it asks.
type RxTxTimes interface {
	LastRx() time.Time
	LastTx() time.Time
}

// MetricsTimes adapts an injected *metrics.Connector to RxTxTimes.
//
// This is the zero-wiring path: a connector that already counts frames
// through its Connector gets health without touching its read or write loop.
type MetricsTimes struct{ C *metrics.Connector }

func (m MetricsTimes) LastRx() time.Time {
	if m.C == nil {
		return time.Time{}
	}
	return m.C.Snapshot().LastRxAt
}

func (m MetricsTimes) LastTx() time.Time {
	if m.C == nil {
		return time.Time{}
	}
	return m.C.Snapshot().LastTxAt
}

// Health is the one HealthChecker implementation, embedded by every
// connector instead of reimplemented per protocol.
//
// It existed twice before this — verbatim in acp1 and acp2, nowhere in the
// other nine connectors — which is how `health` came to be a verb that worked
// on two protocols and errored on the rest. It also probed the wrong thing:
// both copies dialled TCP regardless of the session's actual transport, so a
// live UDP ACP1 session against a Synapse rack reported reachable=false while
// it was answering every request.
//
// The layer rules, in one place:
//
//   - Live: the peer sent something within StaleAfter.
//   - Reachable: there is proof of a network path. A frame received inside
//     the stale window IS that proof, and a better one than a SYN-ACK — so
//     when Live, Reachable follows without a probe. Only when the evidence
//     has gone stale does it fall back to dialling, and only on a stream
//     transport, where a connect attempt means something.
//   - Connected: a session is open. Set by Opened, cleared by Closed.
//
// The fallback probe goes through transport.Net like every other socket in
// the process, so health honours the same timeouts and socket policy as the
// session it reports on.
type Health struct {
	net   transport.Net
	stale time.Duration

	mu      sync.Mutex
	times   RxTxTimes
	network string
	host    string
	port    int
	open    bool
}

// probeTimeout caps the fallback dial. Health is called from a CLI verb and
// from the UI's per-device poll; neither may block on an unreachable host.
const probeTimeout = 500 * time.Millisecond

// DefaultStaleAfter is the silence threshold a connector gets without saying
// anything. 90s is the broadcast-industry baseline for announce cadence, and
// what ACP1 and ACP2 both chose independently.
const DefaultStaleAfter = 90 * time.Second

// Configure supplies the two things a connector knows and Health does not:
// the injected transport, and the protocol's own silence threshold
// (ACP1/ACP2 90s, TSL 5s, Ember+ 30s). Called once from the factory.
//
// It is optional by design. Health is embedded BY VALUE, so a connector
// built as a bare struct literal — which is how most of this repo's tests
// build one — has a working Health without a constructor. An embedded
// POINTER would have been nil in every one of those, and the panic would
// have shown up at the first health call rather than at the missing
// assignment.
func (h *Health) Configure(n transport.Net, stale time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.net, h.stale = n, stale
}

// staleWindow is the configured threshold, or the default when Configure was
// never called. Caller holds h.mu.
func (h *Health) staleWindow() time.Duration {
	if h.stale <= 0 {
		return DefaultStaleAfter
	}
	return h.stale
}

// dialer is the configured transport, or a process default. Caller holds
// h.mu.
func (h *Health) dialer() transport.Net {
	if h.net == nil {
		return transport.New(transport.Config{Timeout: probeTimeout})
	}
	return h.net
}

// Opened records that a session is up on network ("tcp", "udp", ...) to
// host:port, taking its timestamps from times. Called from Connect.
func (h *Health) Opened(network, host string, port int, times RxTxTimes) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.network, h.host, h.port, h.times, h.open = network, host, port, times, true
}

// Closed records that the session is gone. The address is kept: a dropped
// session still has a host worth probing, which is the whole point of
// reporting Reachable and Connected separately.
func (h *Health) Closed() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.open = false
}

// SessionHealth satisfies HealthChecker.
func (h *Health) SessionHealth(ctx context.Context) SessionHealth {
	h.mu.Lock()
	times, network, host, port, open := h.times, h.network, h.host, h.port, h.open
	stale, dialer := h.staleWindow(), h.dialer()
	h.mu.Unlock()

	out := SessionHealth{StaleAfter: stale, Connected: open}
	if times != nil {
		out.LastRx = times.LastRx()
		out.LastTx = times.LastTx()
	}
	out.Live = out.IsLiveAt(time.Now())

	switch {
	case out.Live:
		// Traffic inside the window is direct proof of the path.
		out.Reachable = true
	case host != "" && port > 0 && isStream(network):
		out.Reachable = probe(ctx, dialer, host, port)
	}
	return out
}

// isStream reports whether a connect attempt on this network carries
// information. Dialling a datagram socket succeeds without the peer existing,
// so for UDP a failed probe and a healthy silent device are the same result —
// better to report Reachable=false than to report a fiction.
func isStream(network string) bool {
	switch network {
	case "tcp", "tcp4", "tcp6":
		return true
	}
	return false
}

// probe is the fallback: a short connect to host:port, through the injected
// transport, honouring whichever deadline is tighter.
func probe(ctx context.Context, n transport.Net, host string, port int) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	conn, err := n.Dial(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
