package acp2

import (
	"context"
	"dhs/internal/plugin"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
	"dhs/internal/provider"
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
	// Base owns the listener, the live-session set, the stop sequence and
	// the AN2-Type-attributed metrics connector — everything a TCP provider
	// keeps that is not about ACP2. The session type parameter keeps the
	// fan-out in broadcastAnnounce typed.
	provider.Base[*session]

	logger *slog.Logger
	tree   *tree

	// slotProtos overrides the GetSlotInfo proto advertisement per slot
	// (manifest slot "protos"; see SetSlotProtos). Guarded by tree.mu —
	// slotInfo reads it under the same lock as perSlot.
	slotProtos map[uint8][]uint8

	// mu guards sessionIdle only; the session set moved to Base.
	mu sync.Mutex

	// sessionIdle, when > 0, reaps a client session that has sent nothing
	// for that long. Guarded by mu; 0 = disabled (the default).
	sessionIdle time.Duration
}

func newServer(deps plugin.Deps, exp *canonical.Export) *server {
	deps = deps.WithDefaults()
	logger := deps.Logger
	s := &server{logger: logger}
	s.Init(deps)
	for _, t := range []codec.AN2Type{
		codec.AN2TypeRequest, codec.AN2TypeReply, codec.AN2TypeEvent,
		codec.AN2TypeError, codec.AN2TypeData,
	} {
		s.Metrics().RegisterCmd(uint8(t), t.String())
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

// Serve binds addr (e.g. "0.0.0.0:2072") and blocks until ctx is
// cancelled or a fatal listen error occurs.
func (s *server) Serve(ctx context.Context, addr string) error {
	// tcp4 preserved: acp2 binds IPv4-only. The bind goes through Base's
	// injected transport; Base.AcceptLoop applies the socket policy per
	// connection, so a listener injected by a test gets it too.
	ln, err := s.Listen(ctx, "tcp4", addr)
	if err != nil {
		return fmt.Errorf("acp2 provider: listen %q: %w", addr, err)
	}

	s.logger.Info("acp2 provider listening",
		slog.String("addr", ln.Addr().String()),
		slog.Int("objects", s.tree.count()),
	)

	// Close the listener (and drain sessions) when ctx goes away; unblocks
	// Accept. Base.Stop is idempotent, so a later explicit Stop is a no-op.
	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()

	return s.AcceptLoop(ctx, ln, func(conn net.Conn) *session {
		return newSession(s, conn)
	})
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
	targets := s.Conns()
	totalSessions := len(targets)
	subscribed := 0
	for _, sess := range targets {
		if sess.enabled[codec.AN2ProtoACP2] {
			subscribed++
		}
	}

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
