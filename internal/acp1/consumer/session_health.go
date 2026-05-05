package acp1

import (
	"context"
	"net"
	"sync/atomic"
	"time"

	"dhs/internal/protocol"
)

// acp1StaleAfter is the rolling-window threshold past which the device
// is judged not Live. Per spec p.20 + design discussion: 90 seconds
// matches the slow announce cadence Synapse rack controllers use.
const acp1StaleAfter = 90 * time.Second

// timestampSink stores the most recent rx/tx wall-clock timestamps as
// UnixNano values. Lock-free reads via atomic.Int64; writers update
// once per wire event.
type timestampSink struct {
	lastRxNS atomic.Int64
	lastTxNS atomic.Int64
}

// recordRx must be called from the transport read path.
func (t *timestampSink) recordRx() { t.lastRxNS.Store(time.Now().UnixNano()) }

// recordTx must be called from the transport write path.
func (t *timestampSink) recordTx() { t.lastTxNS.Store(time.Now().UnixNano()) }

func (t *timestampSink) lastRx() time.Time {
	ns := t.lastRxNS.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (t *timestampSink) lastTx() time.Time {
	ns := t.lastTxNS.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// timestampingTransport wraps a Transport and updates a timestampSink on
// every Send/Receive. Cheap (one atomic store per wire event).
type timestampingTransport struct {
	inner interface {
		Send(ctx context.Context, payload []byte) error
		Receive(ctx context.Context, maxSize int) ([]byte, error)
		Close() error
	}
	sink *timestampSink
}

func (t *timestampingTransport) Send(ctx context.Context, payload []byte) error {
	t.sink.recordTx()
	return t.inner.Send(ctx, payload)
}

func (t *timestampingTransport) Receive(ctx context.Context, maxSize int) ([]byte, error) {
	data, err := t.inner.Receive(ctx, maxSize)
	if err == nil && len(data) > 0 {
		t.sink.recordRx()
	}
	return data, err
}

func (t *timestampingTransport) Close() error { return t.inner.Close() }

// SessionHealth implements the cross-protocol HealthChecker (#248).
// Maps the 3 layers (Reachable / Connected / Live) onto ACP1's
// transport mix per the design table.
func (p *Plugin) SessionHealth(ctx context.Context) protocol.SessionHealth {
	p.mu.Lock()
	c := p.client
	host := p.host
	port := p.port
	tk := p.transport
	sink := p.tsSink
	p.mu.Unlock()

	out := protocol.SessionHealth{
		StaleAfter: acp1StaleAfter,
	}
	if sink != nil {
		out.LastRx = sink.lastRx()
		out.LastTx = sink.lastTx()
	}
	out.Connected = c != nil
	out.Live = !out.LastRx.IsZero() && time.Since(out.LastRx) <= acp1StaleAfter

	// Reachable: do a quick on-demand probe. For both UDP and TCP we
	// dial TCP to host:port — it's the cheapest cross-platform check
	// that doesn't require ICMP privileges. Honour the caller's ctx
	// deadline; default 500ms cap so SessionHealth never blocks for
	// long.
	if host != "" && port > 0 {
		out.Reachable = probeReachable(ctx, host, port)
	}
	_ = tk
	return out
}

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
	addr := net.JoinHostPort(host, itoaPort(port))
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func itoaPort(p int) string {
	if p == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for p > 0 {
		i--
		buf[i] = byte('0' + p%10)
		p /= 10
	}
	return string(buf[i:])
}
