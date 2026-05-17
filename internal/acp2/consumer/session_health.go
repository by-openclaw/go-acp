package acp2

import (
	"context"
	"net"
	"strconv"
	"time"

	"dhs/internal/consumer"
)

// acp2StaleAfter is the rolling-window threshold past which the
// session is judged not Live. Mirrors ACP1's 90 s default — broadcast
// industry baseline for slow announce cadence; per-protocol tuning is
// available via the cross-protocol --keepalive-timeout flag.
const acp2StaleAfter = 90 * time.Second

// SessionHealth implements the cross-protocol HealthChecker (#248)
// for ACP2. The 3 layers map onto AN2/TCP as follows:
//
//   - Reachable: TCP SYN to host:port within 500 ms (cheapest
//     cross-platform check; no ICMP privileges required).
//   - Connected: an AN2 session exists in p.session (handshake done).
//   - Live: a frame was received from the device within StaleAfter.
//     The keep-alive prober (AN2 GetSlotInfo every 5 s) keeps this
//     true even on idle sessions.
//
// Plugin returns the snapshot synchronously without holding a probe
// lock — Reachable is the only field that may take wall-clock time;
// callers honour ctx for cancellation.
func (p *Plugin) SessionHealth(ctx context.Context) consumer.SessionHealth {
	p.mu.Lock()
	s := p.session
	p.mu.Unlock()

	out := consumer.SessionHealth{
		StaleAfter: acp2StaleAfter,
	}
	if s == nil {
		// No session — not Connected, not Live. Reachable can still be
		// probed if we know the host (e.g. between Disconnect and
		// reconnect). When session is nil we don't know the host; skip.
		return out
	}

	out.Connected = true
	out.LastRx = s.LastRx()
	out.Live = !out.LastRx.IsZero() && time.Since(out.LastRx) <= acp2StaleAfter

	host := s.Host()
	port := s.Port()
	if host != "" && port > 0 {
		out.Reachable = probeReachable(ctx, host, port)
	}
	return out
}

// probeReachable performs a short TCP SYN test against host:port.
// 500 ms cap; honours ctx deadline if it's tighter. Pure-Go
// (uses net.Dialer) so it works on Windows + Linux + macOS without
// special privileges (unlike ICMP).
func probeReachable(ctx context.Context, host string, port int) bool {
	d := net.Dialer{Timeout: 500 * time.Millisecond}
	if dl, ok := ctx.Deadline(); ok {
		if time.Until(dl) < d.Timeout {
			d.Timeout = time.Until(dl)
			if d.Timeout <= 0 {
				return false
			}
		}
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
