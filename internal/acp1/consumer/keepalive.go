package acp1

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"dhs/internal/protocol"

	"dhs/internal/acp1/codec"
)

// Default cadence and dead-man threshold for ACP1 keep-alive.
//
// 5s matches the natural cadence we observed on the wire from real
// controllers (Cerebrum + VSM Studio) — both poll
// `getObject slot=0 frame.0` at this rate as their effective
// keep-alive. 15s = 3 × interval gives one transient drop of
// tolerance before the watchdog flips the session to dead.
const (
	defaultKeepAliveInterval = 5 * time.Second
	defaultKeepAliveTimeout  = 15 * time.Second
)

// keepAliveState carries everything the prober + watchdog need. Owned
// by the Plugin under p.mu; methods take the lock themselves where
// they don't already hold it.
type keepAliveState struct {
	cfg protocol.KeepAliveConfig

	// done closes when the loops should exit (Disconnect path).
	done chan struct{}

	// stopped fires (closed) once both loops have returned. Lets
	// Disconnect block until the goroutines are gone — important so a
	// followup Connect doesn't see two probers fighting over the same
	// client.
	stopped sync.WaitGroup
}

// SetKeepAlive applies the operator's --keepalive / --keepalive-timeout
// choice. Must be called before Connect. Default values fill in for
// any zero-valued field; sentinel constants
// (protocol.DisableInterval / DisableTimeout) turn the prober and
// the watchdog off respectively.
//
// Implements protocol.KeepAliver — the cross-protocol contract the
// CLI uses to push the same flags into every plugin uniformly.
func (p *Plugin) SetKeepAlive(cfg protocol.KeepAliveConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.kaCfg = cfg
}

// resolvedKeepAlive normalises p.kaCfg into the (interval, timeout)
// pair the goroutines actually use. interval=0 means disabled (no
// prober), timeout=0 means disabled (no watchdog).
func (p *Plugin) resolvedKeepAlive() (interval, timeout time.Duration) {
	switch p.kaCfg.Interval {
	case 0:
		interval = defaultKeepAliveInterval
	case protocol.DisableInterval:
		interval = 0
	default:
		interval = p.kaCfg.Interval
	}
	switch p.kaCfg.Timeout {
	case 0:
		if interval > 0 {
			timeout = 3 * interval
		} else {
			timeout = defaultKeepAliveTimeout
		}
	case protocol.DisableTimeout:
		timeout = 0
	default:
		timeout = p.kaCfg.Timeout
	}
	return interval, timeout
}

// SessionLive reports whether a frame has been received from the
// device within the keep-alive timeout window. Used by the watch verb
// to drive the freshness column: live = true → freshness=live, false
// → freshness=cache.
//
// Implements protocol.SessionLiveAccessor.
func (p *Plugin) SessionLive() bool {
	p.mu.Lock()
	sink := p.tsSink
	_, timeout := p.resolvedKeepAlive()
	p.mu.Unlock()
	if sink == nil {
		return false
	}
	if timeout == 0 {
		// Watchdog disabled — never declare dead. As long as the
		// transport is open the session is "live" by definition.
		return true
	}
	last := sink.lastRx()
	if last.IsZero() {
		return false
	}
	return time.Since(last) <= timeout
}

// startKeepAlive spawns the prober + watchdog goroutines for the
// freshly-Connected session. Must be called with p.mu held.
func (p *Plugin) startKeepAlive(ctx context.Context) {
	interval, timeout := p.resolvedKeepAlive()
	if interval == 0 && timeout == 0 {
		return
	}
	p.ka = &keepAliveState{cfg: p.kaCfg, done: make(chan struct{})}

	if interval > 0 {
		p.ka.stopped.Add(1)
		go p.keepAliveProber(ctx, interval)
	}
	if timeout > 0 {
		p.ka.stopped.Add(1)
		go p.keepAliveWatchdog(timeout)
	}
}

// stopKeepAlive signals the loops to exit and waits for them. Idempotent.
// Must be called with p.mu held.
func (p *Plugin) stopKeepAlive() {
	if p.ka == nil {
		return
	}
	close(p.ka.done)
	// Release the lock so the running goroutines can take it on their
	// final pass without deadlocking. Restore on return.
	p.mu.Unlock()
	p.ka.stopped.Wait()
	p.mu.Lock()
	p.ka = nil
}

// keepAliveProber fires `getObject slot=0 frame.0` at the configured
// interval. ACP1 has no dedicated keep-alive frame; this is the de
// facto idiom (Cerebrum + VSM Studio both do it on the wire). The
// reply touches lastRX via the timestampingTransport, which feeds
// SessionLive + the watchdog.
//
// Errors are logged at Debug — a transient failure shouldn't trigger
// the dead-man directly; the watchdog is the authoritative judge.
func (p *Plugin) keepAliveProber(ctx context.Context, interval time.Duration) {
	defer p.ka.stopped.Done()
	tk := time.NewTicker(interval)
	defer tk.Stop()
	for {
		select {
		case <-p.ka.done:
			return
		case <-ctx.Done():
			return
		case <-tk.C:
			p.mu.Lock()
			c := p.client
			p.mu.Unlock()
			if c == nil {
				return
			}
			// MTID left as 0 — Client.Do allocates a fresh one per
			// request. We don't need correlation since the reply is
			// thrown away (the timestamp tap on the transport touches
			// lastRX whether we look at the reply or not).
			req := &codec.Message{
				PVER:     1,
				MType:    codec.MTypeRequest,
				MAddr:    0,
				MCode:    byte(codec.MethodGetObject),
				ObjGroup: codec.GroupFrame,
				ObjID:    0,
			}
			rctx, cancel := context.WithTimeout(ctx, interval)
			_, err := c.Do(rctx, req)
			cancel()
			if err != nil {
				p.logger.Debug("acp1 keepalive probe failed",
					slog.String("err", err.Error()))
			}
		}
	}
}

// keepAliveWatchdog watches lastRX vs the timeout. On expiry it logs
// at Warn, but does NOT close the connection — the watch verb reads
// SessionLive() each tick to decide the freshness column. Letting the
// transport stay open through silence avoids needless reconnect storms
// when the producer is briefly slow; a real socket break is detected
// by the read loop independently and bubbles up as Disconnect.
func (p *Plugin) keepAliveWatchdog(timeout time.Duration) {
	defer p.ka.stopped.Done()
	tick := timeout / 3
	if tick < time.Second {
		tick = time.Second
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	wasLive := true
	for {
		select {
		case <-p.ka.done:
			return
		case <-ticker.C:
			live := p.SessionLive()
			if wasLive && !live {
				p.logger.Warn("acp1 keepalive: session went silent",
					slog.Duration("timeout", timeout))
			} else if !wasLive && live {
				p.logger.Info("acp1 keepalive: session live again")
			}
			wasLive = live
		}
	}
}
