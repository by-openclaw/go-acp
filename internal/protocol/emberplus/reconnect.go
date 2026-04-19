package emberplus

import (
	"context"
	"sync"
	"time"

	"acp/internal/protocol/emberplus/glow"
)

// clearTree wipes every in-RAM tree index so a post-reconnect walk
// starts from an empty state. Leaves subscription callbacks alone
// (subs["*"] stays — the user's wildcard watch must survive the
// reconnect) but resets streamIndex since the path → streamId
// mapping may have changed on the provider restart.
//
// Called from refreshAfterReconnect; never from the disconnect
// path (values stay visible on disconnect, cleared only once we
// know a fresh walk is imminent).
func (p *Plugin) clearTree() {
	p.treeMu.Lock()
	p.numIndex = make(map[string]*treeEntry)
	p.pathIndex = make(map[string]*treeEntry)
	p.labelIndex = make(map[string][]*treeEntry)
	p.numPath = make(map[string]string)
	p.treeMu.Unlock()

	p.templatesMu.Lock()
	p.templates = make(map[string]*glow.Template)
	p.templatesMu.Unlock()

	p.subsMu.Lock()
	p.streamIndex = make(map[int64][]string)
	p.subsMu.Unlock()
}

// reconnectPolicy controls the auto-reconnect behaviour the plugin
// starts when the session drops unexpectedly.
//
// Defaults match a reasonable "device came back after a reboot"
// story: try quickly at first, back off to avoid hammering, give
// up only when the operator explicitly Disconnects.
type reconnectPolicy struct {
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	MaxAttempts    int // 0 = unlimited
}

func defaultReconnectPolicy() reconnectPolicy {
	return reconnectPolicy{
		InitialBackoff: 2 * time.Second,
		MaxBackoff:     30 * time.Second,
		MaxAttempts:    0,
	}
}

// reconnectCtrl owns the lifecycle of one reconnect goroutine per
// Plugin. Mutex-guarded: at most one reconnect loop alive at a time.
type reconnectCtrl struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	active bool
}

// start kicks off a reconnect goroutine unless one is already
// running. Caller is expected to have just observed a
// disconnect; no-op otherwise. Returns immediately.
func (c *reconnectCtrl) start(p *Plugin) {
	c.mu.Lock()
	if c.active {
		c.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.active = true
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			c.active = false
			c.cancel = nil
			c.mu.Unlock()
		}()
		p.reconnectLoop(ctx)
	}()
}

// stop cancels any in-flight reconnect loop. Safe to call when
// inactive.
func (c *reconnectCtrl) stop() {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.mu.Unlock()
}

// reconnectLoop is the worker goroutine. Exits when:
//   - ctx.Done (Disconnect() / user quit)
//   - a fresh Connect succeeds (session is live again)
//   - MaxAttempts hit
//
// On success it triggers a fresh walk and stream re-subscription
// so the consumer tree repopulates and `fr` flips live for every
// entry that actually comes back.
func (p *Plugin) reconnectLoop(ctx context.Context) {
	policy := defaultReconnectPolicy()
	backoff := policy.InitialBackoff
	attempts := 0

	ip := p.connIP
	port := p.connPort

	for {
		if err := ctx.Err(); err != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		attempts++
		p.logger.Info("emberplus: reconnect attempt",
			"host", ip, "port", port, "attempt", attempts, "backoff", backoff)

		// Build a fresh session object. The previous one is
		// dead and will be garbage-collected once nothing holds it.
		s := NewSession(p.logger)
		s.SetOnElement(p.handleElements)
		s.SetProfile(p.profile)
		s.SetOnStateChange(p.onSessionStateChange)
		if p.recorder != nil {
			s.SetRecorder(p.recorder)
		}

		dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := s.Connect(dialCtx, ip, port)
		cancel()

		if err == nil {
			p.mu.Lock()
			p.session = s
			p.mu.Unlock()

			// Re-walk in the background so the consumer sees
			// live values flow after the session event fired.
			go p.refreshAfterReconnect(ctx)
			return
		}

		p.logger.Debug("emberplus: reconnect failed",
			"host", ip, "port", port, "err", err)

		if policy.MaxAttempts > 0 && attempts >= policy.MaxAttempts {
			p.logger.Warn("emberplus: reconnect giving up",
				"host", ip, "port", port, "attempts", attempts)
			return
		}

		backoff *= 2
		if backoff > policy.MaxBackoff {
			backoff = policy.MaxBackoff
		}
	}
}

// refreshAfterReconnect performs the post-reconnect housekeeping:
//   - Clear stale tree metadata so the upcoming walk gets the
//     provider's CURRENT truth without being polluted by the
//     pre-disconnect state. A provider restart can flip
//     streamIdentifier (0→N or N→0), rename parameters, or
//     change the entire Parameter layout; the old glowParam
//     values would otherwise cling via merge rules.
//   - Send a fresh GetDirectory so announces start flowing again.
//   - Auto-subscribe every stream-backed Parameter (wildcard case).
//
// Note: the user's cached values shown during the disconnect
// window came from the pre-disconnect tree (freshness=stale).
// Once we clear here, the watch briefly shows nothing until the
// walk refills — that's the correct trade-off: freshness > stickiness
// when the provider may have restarted with different shape.
func (p *Plugin) refreshAfterReconnect(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		return
	}
	p.clearTree()
	if _, err := p.Walk(ctx, 0); err != nil {
		p.logger.Debug("emberplus: reconnect walk failed", "err", err)
		return
	}
	// autoSubscribeStreams walks the refreshed tree. streamSubs
	// was cleared in onSessionStateChange(false); no stale skips.
	p.autoSubscribeStreams()
}
