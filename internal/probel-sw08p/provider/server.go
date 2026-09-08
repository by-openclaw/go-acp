package probelsw08p

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/export/canonical"
	"dhs/internal/plugin"
	"dhs/internal/probel-sw08p/codec"
	"dhs/internal/provider"
)

// Server is the exported alias for the concrete Probel provider. Mirrors
// the acp1 / acp2 provider convention so cmd/acp-provider can reach
// protocol-specific helpers (e.g. demo hooks added by follow-up PRs)
// via a type assertion.
type Server = server

// server is the concrete provider.Provider for Probel SW-P-08 over TCP.
// One listener accepts many client sessions; each session runs in its
// own goroutine reading framed commands and dispatching them to per-CMD
// handlers (added per-command PRs).
type server struct {
	// Base owns the listener, the live-session set, the stop sequence and
	// the metrics connector — everything a TCP provider keeps that is not
	// about SW-P-08.
	provider.Base[*session]

	logger *slog.Logger
	tree   *tree

	// mu guards sessionIdle + keepaliveInterval; the session set is Base's.
	mu sync.Mutex

	// sessionIdle, when > 0, reaps a client session that has sent nothing
	// for that long. Guarded by mu; 0 = disabled (the default).
	sessionIdle time.Duration

	// profile aggregates wire-tolerance events observed across every
	// session since the server started. See compliance_events.go.
	profile *compliance.Profile

	// keepaliveInterval is the per-session ping cadence. 0 disables.
	// Set via SetKeepaliveInterval before Serve.
	keepaliveInterval time.Duration
}

// ComplianceProfile returns the provider-scoped compliance profile —
// always non-nil once newServer has run. Safe to read from any
// goroutine; compliance.Profile is internally synchronized.
func (s *server) ComplianceProfile() *compliance.Profile {
	return s.profile
}

func newServer(deps plugin.Deps, exp *canonical.Export) *server {
	deps = deps.WithDefaults()
	logger := deps.Logger
	t, err := newTree(exp)
	if err != nil {
		logger.Error("probel provider: tree build failed", slog.String("err", err.Error()))
		t = &tree{matrices: map[matrixKey]*matrixState{}}
	}
	srv := &server{logger: logger, tree: t, profile: &compliance.Profile{}}
	srv.Init(deps)
	for _, id := range codec.CommandIDs() {
		srv.Metrics().RegisterCmd(uint8(id), codec.CommandName(id))
	}
	return srv
}

// listenHook is a test-only seam. In production it is nil and Serve
// binds a real TCP listener via net.ListenConfig. A test installs it to
// supply a fake listener whose Accept returns a non-ErrClosed,
// non-Canceled error so Serve's final `return err` arm (otherwise
// unreachable, since a real listener only ever yields net.ErrClosed on
// shutdown or ctx.Err() on cancel) is exercised. Kept transparent (nil
// in prod) per the probel-sw02p fake-listener idiom; no Serve logic is
// weakened.
var listenHook func(ctx context.Context, addr string) (net.Listener, error)

// Serve binds addr and accepts client sessions until ctx is cancelled.
func (s *server) Serve(ctx context.Context, addr string) error {
	var ln net.Listener
	var err error
	if listenHook != nil {
		ln, err = listenHook(ctx, addr)
	} else {
		ln, err = s.Listen(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("probel provider: listen %q: %w", addr, err)
	}

	s.logger.Info("probel provider listening",
		slog.String("addr", ln.Addr().String()),
		slog.Int("matrices", s.tree.Size()),
	)

	// Base.Stop closes the listener (unblocking Accept) and drains sessions;
	// idempotent, so a later explicit Stop is a no-op.
	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()

	return s.AcceptLoop(ctx, ln, func(conn net.Conn) *session {
		return newSession(s, conn)
	})
}

// SetValue mutates the served tree from the API path (acp-srv / tests).
// Path format for Probel: "<matrix>.<level>.<dst>" — all decimal.
// The value must be a source index (int, int64, uint64, string
// convertible). Per-command PRs may broaden this (name updates, salvos).
func (s *server) SetValue(_ context.Context, path string, val any) (any, error) {
	m, l, dst, err := parseCrosspointPath(path)
	if err != nil {
		return nil, err
	}
	src, err := coerceSource(val)
	if err != nil {
		return nil, err
	}
	if err := s.tree.applyConnect(m, l, dst, src); err != nil {
		return nil, err
	}
	s.logger.Info("probel set crosspoint",
		slog.Int("matrix", int(m)),
		slog.Int("level", int(l)),
		slog.Int("dst", int(dst)),
		slog.Int("src", int(src)),
	)
	return map[string]uint16{"src": src}, nil
}

// kaInterval is the configured per-session keepalive cadence (0 = off).
func (s *server) kaInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.keepaliveInterval
}

// parseCrosspointPath parses "matrix.level.dst" into uint8/uint8/uint16.
func parseCrosspointPath(path string) (uint8, uint8, uint16, error) {
	var m, l int
	var dst int
	if _, err := fmt.Sscanf(path, "%d.%d.%d", &m, &l, &dst); err != nil {
		return 0, 0, 0, fmt.Errorf("probel: path %q must be \"matrix.level.dst\"", path)
	}
	if m < 0 || m > 255 || l < 0 || l > 255 || dst < 0 || dst > 0xFFFF {
		return 0, 0, 0, fmt.Errorf("probel: path %q has out-of-range component", path)
	}
	return uint8(m), uint8(l), uint16(dst), nil
}

// fanOutTally broadcasts f to every session except the originator.
// Used by per-command handlers that emit TxCrosspointTally /
// TxProtectTally / TxSalvoGroupTally after a successful state change.
// The originating session receives its own confirm reply via the
// handlerResult.reply path — it does not need the tally too.
func (s *server) fanOutTally(origin *session, f codec.Frame) {
	raw := codec.Pack(f)
	var sessions []*session
	for _, sess := range s.Conns() {
		if sess != origin {
			sessions = append(sessions, sess)
		}
	}
	// Per docs/logging.md: skip announce logs entirely. Tally
	// fan-out runs on every connect and fires N-1 times per session,
	// so an Info+HexDump here is ~N² work per connect at scale. Keep
	// a Debug breadcrumb for diagnostics.
	debug := s.logger.Enabled(context.Background(), slog.LevelDebug)
	for _, sess := range sessions {
		if debug {
			s.logger.Debug("probel tally fan-out",
				slog.String("remote", sess.remoteAddr()),
				slog.Int("cmd", int(f.ID)),
				slog.Int("wire_len", len(raw)),
				slog.String("hex", codec.HexDump(raw)),
			)
		}
		if err := sess.write(raw); err != nil {
			s.logger.Warn("probel tally send",
				slog.String("remote", sess.remoteAddr()),
				slog.String("err", err.Error()),
			)
			s.profile.Note(TallyBroadcastFailed)
		}
	}
}

func coerceSource(val any) (uint16, error) {
	switch v := val.(type) {
	case int:
		return uint16(v), nil
	case int32:
		return uint16(v), nil
	case int64:
		return uint16(v), nil
	case uint16:
		return v, nil
	case uint32:
		return uint16(v), nil
	case uint64:
		return uint16(v), nil
	case float64:
		return uint16(v), nil
	}
	return 0, fmt.Errorf("probel: cannot coerce %T to source index", val)
}

// DefaultSessionIdleTimeout is the reaper window a caller gets by asking for
// the default. It is 3x the provider's own keep-alive cadence, so a client
// that answers our pings is never reaped on a single missed round.
//
// It is NOT applied unless SetSessionIdleTimeout is called: SW-P-08 mandates
// no keep-alive (§2), so on a link with no heartbeat silence carries no
// liveness information and reaping by default would disconnect healthy,
// idle controllers.
const DefaultSessionIdleTimeout = 3 * DefaultKeepaliveInterval

// SetSessionIdleTimeout arms (d > 0) or disables (d <= 0) reaping of silent
// client sessions. Applies to sessions accepted after this call.
//
// Enable it when something guarantees inbound traffic — the provider's own
// keep-alive is on, or the controller polls (VSM interrogates on a timer).
// Without such a guarantee, leave it off.
func (s *server) SetSessionIdleTimeout(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionIdle = d
}

// idleTimeout reports the configured reaper window (0 = disabled).
func (s *server) idleTimeout() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionIdle
}
