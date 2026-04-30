package probelsw02p

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"acp/internal/export/canonical"
	"acp/internal/metrics"
	"acp/internal/probel-sw02p/codec"
	"acp/internal/protocol/compliance"
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

	mu       sync.Mutex
	listener net.Listener
	sessions map[*session]struct{}
	closed   bool
	stopped  chan struct{}

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
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("probel-sw02p provider: listen %q: %w", addr, err)
	}
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
