package acp2

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"dhs/internal/protocol"
)

// Default reconnect cadence (mirrors the design in issue #367).
//
// 1 s initial, doubling up to 30 s cap. Two failed attempts and the
// operator already feels it; eight attempts span ~120 s — enough to
// wait out a controller restart without spamming the link.
const (
	defaultReconnectInitial = 1 * time.Second
	defaultReconnectCap     = 30 * time.Second
)

// reconnectState holds the lifecycle of the reconnect goroutine.
type reconnectState struct {
	cfg protocol.ReconnectConfig

	// done closes when the goroutine should exit (Disconnect path).
	done chan struct{}

	// stopped fires once the goroutine has returned.
	stopped sync.WaitGroup
}

// activeSubscription is what we replay after a reconnect succeeds.
// Captures the (request, callback) pair the operator handed to
// Subscribe so the new session can re-register it without forcing
// the watcher to do anything.
type activeSubscription struct {
	req protocol.ValueRequest
	fn  protocol.EventFunc
}

// SetReconnect applies the operator's --reconnect-* choice. Must be
// called before Connect (the goroutine starts on the first session
// loss). Implements protocol.Reconnecter.
func (p *Plugin) SetReconnect(cfg protocol.ReconnectConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reconnectCfg = cfg
}

// resolvedReconnect normalises p.reconnectCfg into the (initial, cap,
// maxAttempts) tuple the goroutine actually uses. Disabled=true
// returns initial=0 to signal "no reconnect at all".
func (p *Plugin) resolvedReconnect() (initial, cap time.Duration, maxAttempts int) {
	if p.reconnectCfg.Disabled {
		return 0, 0, 0
	}
	initial = p.reconnectCfg.Initial
	if initial <= 0 {
		initial = defaultReconnectInitial
	}
	cap = p.reconnectCfg.Cap
	if cap <= 0 {
		cap = defaultReconnectCap
	}
	if cap < initial {
		cap = initial
	}
	maxAttempts = p.reconnectCfg.MaxAttempts
	return
}

// startReconnect launches the watcher goroutine that re-dials after a
// session loss. Must be called with p.mu held; first invocation per
// Connect() — the goroutine waits on the current session's Done().
func (p *Plugin) startReconnect() {
	initial, _, _ := p.resolvedReconnect()
	if initial == 0 {
		return // disabled
	}
	if p.rc != nil {
		return // already running
	}
	p.rc = &reconnectState{cfg: p.reconnectCfg, done: make(chan struct{})}
	p.rc.stopped.Add(1)
	go p.reconnectLoop()
}

// stopReconnect signals the goroutine to exit and waits for it.
// Idempotent. Must be called with p.mu held.
func (p *Plugin) stopReconnect() {
	if p.rc == nil {
		return
	}
	close(p.rc.done)
	p.mu.Unlock()
	p.rc.stopped.Wait()
	p.mu.Lock()
	p.rc = nil
}

// reconnectLoop watches the current session's Done() channel. When
// the read loop exits (TCP close), it attempts re-dial with
// exponential backoff up to maxAttempts. On success, replays any
// active subscriptions so the watcher transparently resumes.
func (p *Plugin) reconnectLoop() {
	defer p.rc.stopped.Done()

	for {
		// Snapshot what we need under the lock.
		p.mu.Lock()
		s := p.session
		host := p.host
		port := p.port
		done := p.rc.done
		p.mu.Unlock()

		if s == nil {
			// No session — nothing to watch. We're either pre-Connect
			// or post-Disconnect. Either way, exit cleanly.
			return
		}

		// Wait for either the session to die or the operator to
		// signal Disconnect.
		select {
		case <-done:
			return
		case <-s.Done():
			// Session is gone. Move to the reconnect path.
		}

		select {
		case <-done:
			return
		default:
		}

		p.logger.Warn("acp2 reconnect: session lost, retrying",
			slog.String("host", host),
			slog.Int("port", port))

		if err := p.runReconnectBackoff(host, port); err != nil {
			p.logger.Warn("acp2 reconnect: gave up",
				slog.String("err", err.Error()))
			return
		}
		// Loop back: watch the new session's Done().
	}
}

// runReconnectBackoff dials the device with exponential backoff.
// Returns nil on success (a fresh Connect succeeded and subscriptions
// were replayed); returns an error when MaxAttempts is hit or when
// the operator cancels via stopReconnect.
func (p *Plugin) runReconnectBackoff(host string, port int) error {
	initial, capDur, maxAttempts := p.resolvedReconnect()
	delay := initial
	attempt := 0

	for {
		attempt++

		p.mu.Lock()
		done := p.rc.done
		p.mu.Unlock()

		select {
		case <-done:
			return nil
		case <-time.After(delay):
		}

		p.logger.Info("acp2 reconnect: attempting",
			slog.Int("attempt", attempt),
			slog.Duration("delay", delay))

		// Use a fresh context (caller's connect-ctx is long gone).
		// 5 s dial timeout matches connect()'s floor.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := p.reconnectOnce(ctx, host, port)
		cancel()

		if err == nil {
			p.logger.Info("acp2 reconnect: succeeded",
				slog.Int("attempt", attempt))
			return nil
		}

		p.logger.Debug("acp2 reconnect: attempt failed",
			slog.Int("attempt", attempt),
			slog.String("err", err.Error()))

		if maxAttempts > 0 && attempt >= maxAttempts {
			return err
		}

		delay *= 2
		if delay > capDur {
			delay = capDur
		}
	}
}

// reconnectOnce does one dial + handshake + subscription replay.
// Acquires p.mu only for the brief windows where it's needed; the
// long part (Connect's TCP dial + AN2 handshake) runs lock-free.
func (p *Plugin) reconnectOnce(ctx context.Context, host string, port int) error {
	// Snapshot active subscriptions to replay.
	p.mu.Lock()
	subs := make([]activeSubscription, 0, len(p.activeSubs))
	for _, sub := range p.activeSubs {
		subs = append(subs, sub)
	}
	// Stop keepalive cleanly — close ka.done, wait for the prober +
	// watchdog goroutines to exit, then nil p.ka. Without this the
	// stale goroutines keep referencing the old keepAliveState and
	// panic on the next tick after p.session goes nil.
	p.stopKeepAlive()
	// Clear the old session pointer so Connect doesn't refuse with
	// "already connected".
	p.session = nil
	p.walker = nil
	if p.trees != nil {
		p.trees.Clear()
	}
	p.subHandles = nil
	p.mu.Unlock()

	if err := p.Connect(ctx, host, port); err != nil {
		return err
	}

	// Replay subscriptions.
	for _, sub := range subs {
		if err := p.Subscribe(sub.req, sub.fn); err != nil {
			p.logger.Warn("acp2 reconnect: subscription replay failed",
				slog.String("err", err.Error()))
			// Continue replaying others — partial recovery is better
			// than none.
		}
	}
	return nil
}
