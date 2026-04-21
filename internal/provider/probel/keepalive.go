package probel

import (
	"context"
	"log/slog"
	"time"

	iprobel "acp/internal/probel"
)

// DefaultKeepaliveInterval matches the TS emulator's heartbeat cadence.
// Providers with KeepaliveInterval = 0 do not send pings — explicit
// opt-in is required; SW-P-08 §2 does not mandate keepalives.
const DefaultKeepaliveInterval = 30 * time.Second

// SetKeepaliveInterval sets the per-session ping cadence for newly
// accepted sessions. 0 disables (default). Must be called before Serve.
// Pings use tx 0x11 APP_KEEPALIVE_REQUEST; controllers are expected to
// answer with rx 0x22 APP_KEEPALIVE_RESPONSE (the session reader already
// accepts those frames silently — no handler wired).
//
// Reference: TS assets/probel/smh-probelsw08p/src/command/application-keep-alive/.
// Not defined in SW-P-08 §3; this is a TS-emulator convention.
func (s *server) SetKeepaliveInterval(d time.Duration) {
	s.mu.Lock()
	s.keepaliveInterval = d
	s.mu.Unlock()
}

// startKeepalive runs a ticker in its own goroutine that periodically
// emits tx 0x11 to the session. Stops when ctx is done or the session
// closes. No-op when interval <= 0.
func (s *session) startKeepalive(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	raw := iprobel.Pack(iprobel.EncodeKeepaliveRequest())
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if s.isClosed() {
					return
				}
				if err := s.write(raw); err != nil {
					s.srv.logger.Debug("probel keepalive write failed",
						slog.String("remote", s.remoteAddr()),
						slog.String("err", err.Error()),
					)
					return
				}
			}
		}
	}()
}
