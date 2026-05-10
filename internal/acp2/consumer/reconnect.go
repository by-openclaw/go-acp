package acp2

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Backoff schedule for warm-restart reconnection: 1s → 2s → 4s → 8s →
// 16s → 30s (cap), retry forever until the operator hits Ctrl-C or
// calls Disconnect. No flags — the goal is "transparent recovery";
// adjustable knobs would only encourage operators to misconfigure them.
const (
	defaultReconnectInitial = 1 * time.Second
	defaultReconnectCap     = 30 * time.Second
)

// reconnectState owns the goroutine that watches the current session's
// Done() channel and, on session loss, re-dials with exponential
// backoff and replays every active subscription on the new session.
//
// Lifecycle: started inside Connect after a successful handshake;
// stopped inside Disconnect before the socket closes.
type reconnectState struct {
	done    chan struct{}
	stopped sync.WaitGroup
}

// startReconnectLoop launches the warm-restart watcher. Caller must
// hold p.mu. No-op if already running.
func (p *Plugin) startReconnectLoop() {
	if p.rc != nil {
		return
	}
	p.rc = &reconnectState{done: make(chan struct{})}
	p.rc.stopped.Add(1)
	go p.reconnectLoop()
}

// stopReconnectLoop signals the goroutine and waits for it. Releases
// p.mu while waiting so the goroutine can acquire it on its way out
// (mirrors stopKeepAlive's pattern). Caller must hold p.mu.
func (p *Plugin) stopReconnectLoop() {
	if p.rc == nil {
		return
	}
	close(p.rc.done)
	p.mu.Unlock()
	p.rc.stopped.Wait()
	p.mu.Lock()
	p.rc = nil
}

// reconnectLoop is the warm-restart driver. Reads the current session
// + remote endpoint under the lock, blocks on session.Done(), and on
// loss runs the backoff dial. After a successful reconnect, loops back
// to watch the new session's Done().
func (p *Plugin) reconnectLoop() {
	defer p.rc.stopped.Done()

	for {
		p.mu.Lock()
		s := p.session
		host := p.host
		port := p.port
		done := p.rc.done
		p.mu.Unlock()

		if s == nil {
			return // pre-Connect or post-Disconnect
		}

		select {
		case <-done:
			return
		case <-s.Done():
			// session closed — TCP gone, readLoop returned, etc.
		}

		select {
		case <-done:
			return
		default:
		}

		p.logger.Warn("acp2 reconnect: session lost, reconnecting",
			slog.String("host", host),
			slog.Int("port", port))

		p.runReconnectBackoff(host, port)
		// loop back to watch new session.Done()
	}
}

// runReconnectBackoff dials with exponential backoff until the dial
// succeeds OR stopReconnectLoop fires. Each successful dial replays
// every activeSubscription on the fresh session.
func (p *Plugin) runReconnectBackoff(host string, port int) {
	delay := defaultReconnectInitial
	attempt := 0

	for {
		attempt++

		p.mu.Lock()
		done := p.rc.done
		p.mu.Unlock()

		select {
		case <-done:
			return
		case <-time.After(delay):
		}

		p.logger.Info("acp2 reconnect: attempting",
			slog.Int("attempt", attempt),
			slog.Duration("delay", delay))

		// Fresh ctx — caller's Connect ctx is long gone. 5s dial floor
		// matches connect() helper's dialTimeout floor.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		replayed, err := p.reconnectOnce(ctx, host, port)
		cancel()

		if err == nil {
			p.logger.Info("acp2 reconnect: connected",
				slog.Int("attempt", attempt),
				slog.Int("replayed_subscriptions", replayed))
			return
		}

		p.logger.Warn("acp2 reconnect: attempt failed",
			slog.Int("attempt", attempt),
			slog.String("err", err.Error()))

		delay *= 2
		if delay > defaultReconnectCap {
			delay = defaultReconnectCap
		}
	}
}

// reconnectOnce drops every reference to the dead session, runs a
// fresh Connect (full AN2 + ACP2 handshake including
// EnableProtocolEvents), and re-registers every activeSubscription on
// the new session. Returns (nil, N) on full success.
//
// Why we re-Connect rather than just re-dialing the socket: ACP2
// announces only flow after AN2 EnableProtocolEvents on the current
// session. Re-running Connect is the spec-correct way to reach that
// state — the new session also gets a fresh keepalive, walker, and
// compliance profile.
func (p *Plugin) reconnectOnce(ctx context.Context, host string, port int) (int, error) {
	p.mu.Lock()
	// Drop the dead session. stopKeepAlive is idempotent and tolerates
	// session==nil (the prober/watchdog goroutines hold their own ka
	// pointer). Walker holds a stale session ref — drop it too.
	p.stopKeepAlive()
	p.session = nil
	p.walker = nil
	// activeSubs is preserved — that's the whole point.
	p.mu.Unlock()

	if err := p.Connect(ctx, host, port); err != nil {
		return 0, err
	}

	p.mu.Lock()
	s := p.session
	if s == nil {
		p.mu.Unlock()
		return 0, fmt.Errorf("acp2 reconnect: session nil after Connect succeeded")
	}
	replayed := 0
	for _, sub := range p.activeSubs {
		// New session → fresh subscription handle. The closure is
		// rebuilt from the original (req, fn) so the operator's filter
		// is preserved verbatim.
		sub.sessionID = s.SubscribeAnnounces(p.buildAnnounceClosure(sub.req, sub.fn))
		replayed++
	}
	p.mu.Unlock()
	return replayed, nil
}
