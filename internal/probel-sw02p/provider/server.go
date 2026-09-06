package probelsw02p

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
	"dhs/internal/probel-sw02p/codec"
	"dhs/internal/transport"
)

// Server is the exported alias for the concrete SW-P-02 provider.
// Mirrors the probel-sw08p provider convention so cmd/dhs can reach
// protocol-specific helpers via a type assertion.
type Server = server

// server is the concrete provider.Provider for SW-P-02 over TCP. One
// listener accepts many client sessions; each session runs in its own
// goroutine reading framed commands. Per-command handlers land in
// follow-up commits; the scaffold's dispatcher is a no-op and simply
// notes UnsupportedCommand for every well-formed inbound frame.
type server struct {
	logger *slog.Logger
	tree   *tree

	mu sync.Mutex

	// sessionIdle, when > 0, reaps a client session that has sent nothing
	// for that long. Guarded by mu; 0 = disabled (the default).
	sessionIdle time.Duration
	listener    net.Listener
	sessions    map[*session]struct{}
	closed      bool
	stopped     chan struct{}

	// profile aggregates wire-tolerance events observed across every
	// session since the server started.
	profile *compliance.Profile

	// metrics aggregates rx/tx counters + error counters + handler
	// latency buckets across every session. Always non-nil after
	// newServer.
	metrics *metrics.Connector

	// selfDeviceNumber + selfDeviceName configure how this matrix
	// responds to rx 103 PROTECT DEVICE NAME REQUEST (§3.2.67) — a
	// controller asking "what's your device name?" gets back tx 099
	// with these values. Defaults chosen in newServer match the
	// project "DHS" branding; callers can override via SetSelfDevice.
	selfDeviceNumber uint16
	selfDeviceName   string
}

// DefaultSelfDeviceName is the tx 099 device name emitted by an
// un-configured SW-P-02 provider. 8-char ASCII per §3.2.63 width.
const DefaultSelfDeviceName = "DHS-SW02"

// DefaultSelfDeviceNumber is the tx 099 Device Number emitted by an
// un-configured SW-P-02 provider. 0 mirrors "no specific address /
// anonymous device" — controllers that require non-zero identity
// should call SetSelfDevice to override.
const DefaultSelfDeviceNumber uint16 = 0

// SetSelfDevice configures the (Device Number, 8-char ASCII name)
// pair this matrix reports when answering rx 103 PROTECT DEVICE
// NAME REQUEST. Name is coerced to 8 characters on the wire
// (space-padded / truncated) by the codec.
func (s *server) SetSelfDevice(num uint16, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selfDeviceNumber = num
	s.selfDeviceName = name
}

// Metrics returns the server-wide connector metrics. Always non-nil.
func (s *server) Metrics() *metrics.Connector { return s.metrics }

// ComplianceProfile returns the provider-scoped compliance profile —
// always non-nil once newServer has run.
func (s *server) ComplianceProfile() *compliance.Profile {
	return s.profile
}

func newServer(logger *slog.Logger, exp *canonical.Export) *server {
	if logger == nil {
		logger = slog.Default()
	}
	t, err := newTree(exp)
	if treeBuildErrHook != nil {
		err = treeBuildErrHook()
	}
	if err != nil {
		logger.Error("probel-sw02p provider: tree build failed", slog.String("err", err.Error()))
		t = &tree{matrices: map[matrixKey]*matrixState{}}
	}
	met := metrics.NewConnector()
	for _, id := range codec.CommandIDs() {
		met.RegisterCmd(uint8(id), codec.CommandName(id))
	}
	return &server{
		logger:           logger,
		tree:             t,
		sessions:         map[*session]struct{}{},
		stopped:          make(chan struct{}),
		profile:          &compliance.Profile{},
		metrics:          met,
		selfDeviceNumber: DefaultSelfDeviceNumber,
		selfDeviceName:   DefaultSelfDeviceName,
	}
}

// Serve binds addr and accepts client sessions until ctx is cancelled.
func (s *server) Serve(ctx context.Context, addr string) error {
	ln, err := transport.ListenTCP(ctx, "tcp", addr, transport.SocketOptions{})
	if err != nil {
		return fmt.Errorf("probel-sw02p provider: listen %q: %w", addr, err)
	}
	// The embedded listener, not the wrapper: serveListener's accept loop
	// applies the socket policy itself, so a listener handed to
	// ServeListener gets it too. Passing the wrapper would apply it twice.
	return s.serveListener(ctx, ln.Listener)
}

// ServeListener accepts client sessions on a pre-bound listener until
// ctx is cancelled. Exported on the concrete type (not part of the
// neutral provider.Provider interface) so external-package tests can
// bind "127.0.0.1:0" themselves and skip the close-then-rebind window
// of the addr-based path — in a parallel test sweep another process
// can steal the port between the probe listener's Close and Serve's
// re-listen (issue #694 flake class).
func (s *server) ServeListener(ctx context.Context, ln net.Listener) error {
	return s.serveListener(ctx, ln)
}

// serveListener accepts client sessions on a pre-bound listener until
// ctx is cancelled. Used by Serve once the listener is open and by
// in-process tests that want to skip the close-then-rebind race
// window of the addr-based path.
func (s *server) serveListener(ctx context.Context, ln net.Listener) error {
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	s.logger.Info("probel-sw02p provider listening",
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

	err := s.acceptLoop(ctx, ln)
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

// SetValue mutates the served tree from the API path. Path format for
// SW-P-02: "<matrix>.<level>.<dst>" — all decimal. Value must be a
// source index (int, int64, uint64, string convertible).
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
	s.logger.Info("probel-sw02p set crosspoint",
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
		// OS-level dead-peer probe. Without it a half-open client session
		// (a NAT or firewall drop with no RST) holds a goroutine and a
		// socket here for ever. Applied in the accept loop rather than at
		// bind time so ServeListener's injected listener gets it too.
		_ = transport.ApplySocketOptions(conn, transport.SocketOptions{})
		sess := newSession(s, conn)
		s.mu.Lock()
		s.sessions[sess] = struct{}{}
		s.mu.Unlock()
		go func() {
			sessCtx, cancel := context.WithCancel(ctx)
			defer cancel()
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
		return 0, 0, 0, fmt.Errorf("probel-sw02p: path %q must be \"matrix.level.dst\"", path)
	}
	if m < 0 || m > 255 || l < 0 || l > 255 || dst < 0 || dst > 0xFFFF {
		return 0, 0, 0, fmt.Errorf("probel-sw02p: path %q has out-of-range component", path)
	}
	return uint8(m), uint8(l), uint16(dst), nil
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
	return 0, fmt.Errorf("probel-sw02p: cannot coerce %T to source index", val)
}

// SetSessionIdleTimeout arms (d > 0) or disables (d <= 0) reaping of silent
// client sessions. Applies to sessions accepted after this call.
//
// Off by default. SW-P-02 defines no keep-alive command, so an idle link is
// indistinguishable from a dead one; enable this only where something
// guarantees inbound traffic (a controller that polls, as VSM does with a
// rotating rx 01).
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
