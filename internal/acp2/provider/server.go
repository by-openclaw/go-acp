package acp2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
	"dhs/internal/metrics"
	"dhs/internal/transport"
)

// Server is the exported alias for the concrete provider — lets
// `cmd/acp-provider/main.go` reach the ACP2-specific helpers
// (e.g. RunAnnounceDemo) via a type assertion without widening the
// cross-protocol provider.Provider interface.
type Server = server

// server is the concrete provider.Provider for ACP2 over AN2/TCP.
//
// Concurrency model:
//   - Serve runs the TCP accept loop on one goroutine.
//   - Each accepted connection is handled by a session goroutine that
//     owns the conn and serialises all writes — no per-write lock needed.
//   - Announcements fan out across sessions under s.mu.
//
// Tree mutation (via Provider.SetValue or incoming set_property) takes
// tree.mu's write lock; reads take RLock. Consistent with the emberplus
// + acp1 providers.
type server struct {
	logger *slog.Logger
	tree   *tree

	// slotProtos overrides the GetSlotInfo proto advertisement per slot
	// (manifest slot "protos"; see SetSlotProtos). Guarded by tree.mu —
	// slotInfo reads it under the same lock as perSlot.
	slotProtos map[uint8][]uint8

	mu sync.Mutex

	// sessionIdle, when > 0, reaps a client session that has sent nothing
	// for that long. Guarded by mu; 0 = disabled (the default).
	sessionIdle time.Duration
	listener    net.Listener
	sessions    map[*session]struct{}
	closed      bool
	stopped     chan struct{}

	// metrics is the server-wide connector snapshot exposed via
	// Metrics() so `producer acp2 serve --metrics-addr` scrapes it
	// (#969). Frames are attributed by AN2 Type (request/reply/event/
	// error/data) — the natural command axis for AN2. Always non-nil.
	metrics *metrics.Connector
}

func newServer(logger *slog.Logger, exp *canonical.Export) *server {
	if logger == nil {
		logger = slog.Default()
	}
	met := metrics.NewConnector()
	for _, t := range []codec.AN2Type{
		codec.AN2TypeRequest, codec.AN2TypeReply, codec.AN2TypeEvent,
		codec.AN2TypeError, codec.AN2TypeData,
	} {
		met.RegisterCmd(uint8(t), t.String())
	}
	s := &server{
		logger:   logger,
		sessions: map[*session]struct{}{},
		stopped:  make(chan struct{}),
		metrics:  met,
	}
	t, err := newTree(exp)
	if err != nil {
		logger.Error("acp2 provider: tree build failed", slog.String("err", err.Error()))
		s.tree = emptyTree()
		return s
	}
	s.tree = t
	if t.labelDeviations > 0 {
		// Absorb-and-surface: the emulated device's labels violate the
		// spec charset (real Neuron uses underscores). Served VERBATIM —
		// controllers bind by exact labels — and reported here.
		logger.Warn("acp2 provider: labels outside the spec charset served verbatim",
			slog.Int("count", t.labelDeviations))
	}
	return s
}

// Metrics returns the server-wide connector metrics — satisfies the
// cmd/dhs metricsExposer optional interface so --metrics-addr scrapes
// the acp2 provider (#969). Always non-nil.
func (s *server) Metrics() *metrics.Connector { return s.metrics }

// Serve binds addr (e.g. "0.0.0.0:2072") and blocks until ctx is
// cancelled or a fatal listen error occurs.
func (s *server) Serve(ctx context.Context, addr string) error {
	// tcp4 preserved: acp2 binds IPv4-only.
	//
	// The bind goes through transport; the socket policy is applied by the
	// accept loop below, which is why the embedded listener is used rather
	// than the wrapper — the wrapper would apply it a second time, and the
	// accept loop is the arm that also covers a listener injected by a test.
	tln, err := transport.ListenTCP(ctx, "tcp4", addr, transport.SocketOptions{})
	if err != nil {
		return fmt.Errorf("acp2 provider: listen %q: %w", addr, err)
	}
	ln := tln.Listener

	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	s.logger.Info("acp2 provider listening",
		slog.String("addr", ln.Addr().String()),
		slog.Int("objects", s.tree.count()),
	)

	// Close listener when ctx goes away; unblocks Accept.
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		if !s.closed {
			s.closed = true
			_ = ln.Close()
		}
		s.mu.Unlock()
	}()

	return s.acceptLoop(ln)
}

// acceptLoop runs the blocking accept/spawn loop against ln. Split out of
// Serve so the accept-error arms can be exercised with an injected
// listener; Serve's only caller path is unchanged. A clean shutdown
// (listener closed via ctx or Stop) returns nil; any other Accept error
// is surfaced to the caller.
func (s *server) acceptLoop(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
				close(s.stopped)
				return nil
			}
			close(s.stopped)
			return err
		}
		// OS-level dead-peer probe. Without it a half-open client session
		// (a NAT or firewall drop with no RST) holds a goroutine and a
		// socket here for ever — the inbound twin of the consumer-side
		// stall. Applied here rather than at bind time so an injected
		// listener gets it too.
		_ = transport.ApplySocketOptions(conn, transport.SocketOptions{})
		sess := newSession(s, conn)
		s.registerSession(sess)
		go func() {
			sess.run()
			s.unregisterSession(sess)
		}()
	}
}

// Stop closes the listener and all active sessions. Safe to call
// multiple times.
func (s *server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.listener != nil {
		err = s.listener.Close()
	}
	for sess := range s.sessions {
		_ = sess.conn.Close()
	}
	return err
}

// SetValue mutates the served tree via the API path and fans the
// change out to every session that has EnableProtocolEvents([2])
// subscribed. Ships in Step 2e; this commit leaves it unimplemented.
func (s *server) SetValue(_ context.Context, path string, val any) (any, error) {
	_, _ = path, val
	return nil, errors.New("acp2 provider: SetValue ships in Step 2e")
}

// broadcastAnnounce wraps the announce ACP2 message in an AN2 data
// frame and sends it to every session that has EnableProtocolEvents
// ([ACP2]) subscribed — spec §"ACP2 Announces" p.88. Sessions that
// haven't subscribed are silently skipped (matching how a real Axon
// device ignores unregistered listeners).
func (s *server) broadcastAnnounce(slot uint8, ann *codec.ACP2Message) {
	// Bypass EncodeACP2Message (which is request-shaped for the four
	// ACP2 funcs) and build the reply/announce frame manually. See
	// replyACP2 for the same rationale.
	raw := make([]byte, 4+len(ann.Body))
	raw[0] = byte(ann.Type)
	raw[1] = ann.MTID
	raw[2] = byte(ann.Func)
	raw[3] = ann.PID
	copy(raw[4:], ann.Body)
	frame := &codec.AN2Frame{
		Proto:   codec.AN2ProtoACP2,
		Slot:    slot,
		MTID:    0,
		Type:    codec.AN2TypeData,
		Payload: raw,
	}

	// Deliver to EVERY session, not only those that sent AN2
	// EnableProtocolEvents([2]). The spec gates announces on the enable
	// (§3.3.4), but the shipping ecosystem contradicts it: Lawo VSM's
	// gadgetserver never sends EnableProtocolEvents on its acp2 session
	// and still expects value announces (live-verified against staging
	// 2026-08-20 — VSM connected, no enable, values froze), so the real
	// Neuron necessarily announces regardless of the gate. Same
	// documented-exception pattern as SW-P-08 salvo cmd-04 (root
	// CLAUDE.md "Spec-strict, no-workaround posture"): follow the wire
	// reality every field controller depends on, keep both counts in
	// the log so the deviation stays observable per fanout.
	s.mu.Lock()
	totalSessions := len(s.sessions)
	subscribed := 0
	targets := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
		if sess.enabled[codec.AN2ProtoACP2] {
			subscribed++
		}
		targets = append(targets, sess)
	}
	s.mu.Unlock()

	s.logger.Info("acp2 announce fanout",
		slog.Int("slot", int(slot)),
		slog.Int("sessions_total", totalSessions),
		slog.Int("sessions_subscribed", subscribed),
		slog.Int("frame_bytes", len(raw)+8),
	)

	for _, sess := range targets {
		if err := sess.write(frame); err != nil {
			s.logger.Warn("acp2 announce send failed",
				slog.String("err", err.Error()),
			)
		}
	}
}

func (s *server) registerSession(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess] = struct{}{}
}

func (s *server) unregisterSession(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sess)
}

// SetSessionIdleTimeout arms (d > 0) or disables (d <= 0) reaping of silent
// client sessions. Applies to sessions accepted after this call.
//
// Off by default. ACP2 announces are event-driven, so a consumer that has
// subscribed and is simply waiting for something to change is healthy and
// silent; enable this only where the consumer keeps the link warm (the acp2
// consumer's own keep-alive prober does, at 5s).
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
