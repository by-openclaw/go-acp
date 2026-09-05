package probelsw08p

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/export/canonical"
	"dhs/internal/metrics"
	"dhs/internal/probel-sw08p/codec"
	"dhs/internal/transport"
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
	logger *slog.Logger
	tree   *tree

	mu       sync.Mutex
	listener net.Listener
	sessions map[*session]struct{}
	closed   bool
	stopped  chan struct{}

	// sessionIdle, when > 0, reaps a client session that has sent nothing
	// for that long. Guarded by mu; 0 = disabled (the default).
	sessionIdle time.Duration

	// profile aggregates wire-tolerance events observed across every
	// session since the server started. See compliance_events.go.
	profile *compliance.Profile

	// metrics aggregates rx/tx counters + error counters + handler
	// latency buckets across every session since Serve started.
	// Always non-nil after newServer.
	metrics *metrics.Connector

	// keepaliveInterval is the per-session ping cadence. 0 disables.
	// Set via SetKeepaliveInterval before Serve.
	keepaliveInterval time.Duration
}

// Metrics returns the server-wide connector metrics. Always non-nil.
func (s *server) Metrics() *metrics.Connector { return s.metrics }

// ComplianceProfile returns the provider-scoped compliance profile —
// always non-nil once newServer has run. Safe to read from any
// goroutine; compliance.Profile is internally synchronized.
func (s *server) ComplianceProfile() *compliance.Profile {
	return s.profile
}

func newServer(logger *slog.Logger, exp *canonical.Export) *server {
	if logger == nil {
		logger = slog.Default()
	}
	t, err := newTree(exp)
	if err != nil {
		logger.Error("probel provider: tree build failed", slog.String("err", err.Error()))
		t = &tree{matrices: map[matrixKey]*matrixState{}}
	}
	met := metrics.NewConnector()
	for _, id := range codec.CommandIDs() {
		met.RegisterCmd(uint8(id), codec.CommandName(id))
	}
	return &server{
		logger:   logger,
		tree:     t,
		sessions: map[*session]struct{}{},
		stopped:  make(chan struct{}),
		profile:  &compliance.Profile{},
		metrics:  met,
	}
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
	listen := func(ctx context.Context, addr string) (net.Listener, error) {
		lc := &net.ListenConfig{}
		return lc.Listen(ctx, "tcp", addr)
	}
	if listenHook != nil {
		listen = listenHook
	}
	ln, err := listen(ctx, addr)
	if err != nil {
		return fmt.Errorf("probel provider: listen %q: %w", addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	s.logger.Info("probel provider listening",
		slog.String("addr", ln.Addr().String()),
		slog.Int("matrices", s.tree.Size()),
	)

	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			_ = ln.Close()
		}
		s.mu.Unlock()
	}()

	err = s.acceptLoop(ctx, ln)
	close(s.stopped)
	if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Stop closes the listener and drops all active sessions.
func (s *server) Stop() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.listener
	sessions := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()
	for _, sess := range sessions {
		sess.close()
	}
	if ln != nil {
		return ln.Close()
	}
	return nil
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

func (s *server) acceptLoop(ctx context.Context, ln net.Listener) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		// OS-level dead-peer probe, in addition to the app-layer cmd 08
		// keepalive below: the two answer different questions, and a peer
		// that never implements cmd 09 still needs a dead-socket detector.
		// Applied in the accept loop rather than at bind time so an
		// injected listener (listenHook) gets it too.
		_ = transport.ApplySocketOptions(conn, transport.SocketOptions{})
		sess := newSession(s, conn)
		s.mu.Lock()
		s.sessions[sess] = struct{}{}
		interval := s.keepaliveInterval
		s.mu.Unlock()
		go func() {
			sessCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			sess.startKeepalive(sessCtx, interval)
			sess.run(sessCtx)
			s.mu.Lock()
			delete(s.sessions, sess)
			s.mu.Unlock()
		}()
	}
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
	s.mu.Lock()
	sessions := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
		if sess == origin {
			continue
		}
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()
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
