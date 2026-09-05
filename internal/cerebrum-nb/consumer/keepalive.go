package cerebrumnb

// Keep-alive + liveness for the Cerebrum NB WebSocket session.
//
// There are TWO keep-alives here, in opposite directions. Confusing them
// cost us a bug once already, so both are written down.
//
//  1. SERVER → US, and it is the one that keeps the session open. Cerebrum
//     sends an RFC 6455 Ping and expects a Pong. ws.Conn answers it inline
//     (see conn.go, OpPing). Early on we did NOT reply, and the server closed
//     every session at ~30 s. Nothing in this file is responsible for that
//     survival — do not "simplify" the inline Pong away.
//
//  2. US → SERVER, which is what this file does. It exists for OUR benefit:
//     to notice a half-open socket, not to satisfy the server.
//
// Note that POLL (spec §2.2, <POLL MTID="n"/> → <POLL_REPLY …>) is the
// APPLICATION-level keep-alive and redundancy probe — docs/keys.md calls it
// "Keep-alive + redundancy probe" — and an earlier version of this comment
// wrongly claimed Cerebrum had no application heartbeat at all. We send it
// once at connect, and deliberately do NOT use it as the liveness probe: it
// is answered by the application layer, so a slow, busy or auth-gated POLL is
// indistinguishable from a dead link, and treating it as fatal would kill a
// session the server is still happily pinging. A WS Ping is answered by the
// RFC 6455 layer of any conformant server, which is exactly the question this
// probe asks: is the socket still carrying frames?
//
// Two independent failures are therefore detected:
//
//   - the Ping write fails            → the socket is gone, immediately
//   - nothing arrives within Timeout  → the peer is silent (half-open flow, a
//     NAT/firewall drop with no RST) → the read deadline fires
//
// Before this, readLoop called ReadMessage with context.Background() and no
// deadline. A half-open connection therefore parked the reader in the kernel
// forever: no error, no EOF, no log line, no crash — the watcher simply went
// quiet and stayed running. That is the bug this file exists to close.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"dhs/internal/clock"
	"dhs/internal/consumer"
	"dhs/internal/transport"
)

// Defaults chosen to match the rest of the fleet: acp1/acp2 both treat 90 s
// without RX as stale, and a 30 s probe gives three chances inside that
// window. Overridable per-run via the cross-protocol --keepalive /
// --keepalive-timeout flags (consumer.KeepAliveConfig).
const (
	defaultKeepAliveInterval = 30 * time.Second
	defaultKeepAliveTimeout  = 90 * time.Second
)

// keepAlive owns the prober goroutine for one session.
type keepAlive struct {
	stop     chan struct{}
	stopped  sync.WaitGroup
	stopOnce sync.Once
}

// SetKeepAlive implements consumer.KeepAliver: the CLI's --keepalive /
// --keepalive-timeout land here. Interval 0 means "plugin default";
// consumer.DisableInterval turns probing off; consumer.DisableTimeout keeps
// probing but never declares the peer dead.
//
// Applying a config restarts the prober so a change takes effect at once.
func (s *Session) SetKeepAlive(cfg consumer.KeepAliveConfig) {
	interval, timeout := resolveKeepAlive(cfg)
	s.stopKeepAlive()
	if s.conn != nil {
		s.conn.SetIdleTimeout(timeout)
	}
	if interval > 0 {
		s.startKeepAlive(interval, clock.System())
	}
}

// resolveKeepAlive expands the zero/sentinel values of a KeepAliveConfig into
// the concrete (interval, timeout) this plugin will use. Pure — unit-tested
// directly rather than through a running session.
func resolveKeepAlive(cfg consumer.KeepAliveConfig) (interval, timeout time.Duration) {
	switch {
	case cfg.Interval == consumer.DisableInterval:
		interval = 0
	case cfg.Interval <= 0:
		interval = defaultKeepAliveInterval
	default:
		interval = cfg.Interval
	}

	switch {
	case cfg.Timeout == consumer.DisableTimeout:
		timeout = 0
	case cfg.Timeout <= 0:
		// Three probes inside the window: a single dropped Pong must not
		// tear down a healthy session.
		if interval > 0 {
			timeout = 3 * interval
		} else {
			timeout = defaultKeepAliveTimeout
		}
	default:
		timeout = cfg.Timeout
	}
	return interval, timeout
}

// startKeepAlive launches the prober. clk is injected so tests drive the
// cadence deterministically instead of sleeping.
func (s *Session) startKeepAlive(interval time.Duration, clk clock.Clock) {
	if interval <= 0 {
		return
	}
	s.mu.Lock()
	if s.ka != nil {
		s.mu.Unlock()
		return
	}
	ka := &keepAlive{stop: make(chan struct{})}
	s.ka = ka
	s.mu.Unlock()

	ka.stopped.Add(1)
	go s.keepAliveLoop(ka, interval, clk)
}

// stopKeepAlive signals the prober and waits for it. Idempotent.
func (s *Session) stopKeepAlive() {
	s.mu.Lock()
	ka := s.ka
	s.ka = nil
	s.mu.Unlock()
	if ka == nil {
		return
	}
	ka.stopOnce.Do(func() { close(ka.stop) })
	ka.stopped.Wait()
}

// keepAliveLoop pings on every tick until the session ends. A failed write is
// terminal: it means the socket is gone, and the watcher must be told now
// rather than at the next idle deadline.
func (s *Session) keepAliveLoop(ka *keepAlive, interval time.Duration, clk clock.Clock) {
	defer ka.stopped.Done()

	t := clk.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ka.stop:
			return
		case <-s.stopRX:
			return
		case <-s.done:
			return
		case <-t.C():
			// A WS Ping, deliberately — NOT a POLL.
			//
			// POLL is the application keep-alive and a redundancy probe, but
			// it is CLIENT-initiated and answered by the application layer.
			// Treating an unanswered POLL as fatal would kill a session the
			// server is still happily pinging — a slow or auth-gated POLL
			// would look identical to a dead link. A Ping is answered by the
			// RFC 6455 layer of any conformant server, so it tests exactly the
			// thing we care about here: is the socket still carrying frames.
			//
			// Session SURVIVAL does not depend on this probe at all: Cerebrum
			// pings US, and ws.Conn answers Pong inline (conn.go). Failing to
			// do that is what closed sessions at 30s before it was fixed.
			ctx, cancel := context.WithTimeout(context.Background(), interval)
			err := s.conn.Ping(ctx, nil)
			cancel()
			if err != nil {
				s.logger.Warn("keepalive ping failed — session lost",
					slog.String("err", err.Error()))
				s.compliance.Event("cerebrum_keepalive_failed")
				s.markLost(fmt.Errorf("%w: keepalive ping: %v",
					transport.ErrConnectionLost, err))
				return
			}
			s.logger.Debug("keepalive ping sent")
		}
	}
}

// SessionLive implements consumer.SessionLiveAccessor: true when a frame
// arrived within the liveness window. A watcher renders this as the freshness
// of the data it is showing.
func (s *Session) SessionLive() bool {
	select {
	case <-s.done:
		return false
	default:
	}
	var window time.Duration
	if s.conn != nil {
		window = s.conn.IdleTimeout()
	}
	if window <= 0 {
		window = defaultKeepAliveTimeout
	}
	last := s.LastRx()
	if last.IsZero() {
		return false
	}
	return time.Since(last) <= window
}

// classifyReadErr turns a raw read failure into the typed sentinel the error
// contract promises (docs/protocols/error-codes.md). Every arm wraps
// transport.ErrConnectionLost so a caller can ask the single question it
// actually cares about — "did I lose an established session?" — with
// errors.Is, while the specific cause stays legible to the operator.
func (s *Session) classifyReadErr(err error) error {
	switch {
	case err == nil:
		return nil

	case errors.Is(err, os.ErrDeadlineExceeded) || isNetTimeout(err):
		// The read deadline fired: the peer went silent. This is the
		// half-open connection that used to hang the reader forever.
		return fmt.Errorf("%w: %w: no frame from %s within %s",
			transport.ErrConnectionLost, transport.ErrIdleTimeout,
			s.host, s.conn.IdleTimeout())

	case errors.Is(err, io.EOF):
		return fmt.Errorf("%w: peer closed the WebSocket", transport.ErrConnectionLost)

	case errors.Is(err, net.ErrClosed):
		// Local Close() — an orderly shutdown, still a lost session for
		// anyone waiting on Done.
		return fmt.Errorf("%w: connection closed locally", transport.ErrConnectionLost)

	default:
		return fmt.Errorf("%w: %v", transport.ErrConnectionLost, err)
	}
}

// isNetTimeout reports whether err is a net.Error marked Timeout.
func isNetTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
