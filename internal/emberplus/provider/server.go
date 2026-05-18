package emberplus

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
)

// Server is the exported alias for the concrete Ember+ provider so
// cmd/dhs/cmd_producer.go can reach protocol-specific setters (e.g.
// SetStreamIdleTTL for R9 #472) via a type assertion without exposing
// the rest of the package internals.
type Server = server

// server is the provider runtime. One listener, many sessions, a shared
// tree, and a per-OID subscription table.
type server struct {
	logger    *slog.Logger
	tree      *tree
	templates []*canonical.TemplateEntry
	funcs     *functionRegistry
	salvos    *salvoStore
	locks     *lockStore

	mu       sync.Mutex
	listener net.Listener
	sessions map[*session]struct{}
	// subs: oid -> set of sessions watching it
	subs map[string]map[*session]struct{}

	// profile aggregates wire-tolerance + producer-side compliance
	// events across every session since Serve started. See
	// compliance_events.go.
	profile *compliance.Profile

	// streamIdleTTL is the per-session "no rx for this long → clear
	// the session's subscriptions" budget. 0 disables (default before
	// R9 #472). Set via SetStreamIdleTTL before Serve. The hard
	// idle-session sweep (idleSessionTTL) still backstops dead TCP
	// sockets independently.
	streamIdleTTL time.Duration

	stopOnce sync.Once
	stopped  chan struct{}
}

func newServer(logger *slog.Logger, exp *canonical.Export) *server {
	if logger == nil {
		logger = slog.Default()
	}
	t, err := newTree(exp)
	s := &server{
		logger:   logger.With(slog.String("plugin", "emberplus-provider")),
		funcs:    newFunctionRegistry(),
		sessions: map[*session]struct{}{},
		subs:     map[string]map[*session]struct{}{},
		profile:  &compliance.Profile{},
		stopped:  make(chan struct{}),
	}
	if err != nil {
		// defer until Serve so the factory signature stays clean
		s.logger.Error("tree build failed", slog.String("err", err.Error()))
	} else {
		s.tree = t
		s.templates = exp.Templates
		s.setupBuiltinFunctions()
	}
	return s
}

// SetStreamIdleTTL configures the per-session stream idle-TTL. Must be
// called before Serve. 0 disables the soft sweep (subs cleared only by
// the hard idle-session sweep). Negative values are clamped to 0.
func (s *server) SetStreamIdleTTL(d time.Duration) {
	if d < 0 {
		d = 0
	}
	s.streamIdleTTL = d
}

// ComplianceProfile returns the provider-scoped compliance profile —
// always non-nil after newServer. Safe to read from any goroutine.
func (s *server) ComplianceProfile() *compliance.Profile {
	return s.profile
}

// PeerHealth carries the per-peer health snapshot exposed by the
// provider for #300 (provider-side health) and consumed by the R24
// #489 admin endpoint. Mirrors consumer.SessionHealth shape so a
// future cross-protocol UI can render both sides with one renderer.
type PeerHealth struct {
	Peer       string        // remote address ("10.6.239.113:54321")
	Connected  bool          // socket established and S101 handshake done
	Live       bool          // (now - LastRx) <= StaleAfter
	LastRx     time.Time     // most recent inbound frame
	StaleAfter time.Duration // window used for Live
	SubsOpen   int           // number of subscriptions currently held
}

// PeerHealthSnapshot returns one entry per active consumer session
// (#300 provider side). Safe to call from any goroutine; the slice
// is a fresh allocation so the caller can sort / render freely.
func (s *server) PeerHealthSnapshot() []PeerHealth {
	s.mu.Lock()
	out := make([]PeerHealth, 0, len(s.sessions))
	now := time.Now()
	for sess := range s.sessions {
		lastNano := sess.lastActive.Load()
		var lastRx time.Time
		if lastNano > 0 {
			lastRx = time.Unix(0, lastNano)
		}
		live := lastNano > 0 && now.Sub(lastRx) <= idleSessionTTL
		out = append(out, PeerHealth{
			Peer:       sess.id,
			Connected:  true, // session in s.sessions = socket established + accepted
			Live:       live,
			LastRx:     lastRx,
			StaleAfter: idleSessionTTL,
			SubsOpen:   len(sess.subs),
		})
	}
	s.mu.Unlock()
	return out
}

// Serve implements provider.Provider. Blocks until ctx is cancelled or
// the listener returns a fatal error.
func (s *server) Serve(ctx context.Context, addr string) error {
	if s.tree == nil {
		return fmt.Errorf("emberplus-provider: tree not loaded")
	}
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	s.logger.Info("listening",
		slog.String("addr", ln.Addr().String()),
		slog.Int("tree_size", len(s.tree.byOID)),
	)

	// Unsolicited stream fan-out — runs if the tree has any Parameters
	// with a streamIdentifier, exits on ctx cancel or Stop().
	go s.runStreamer(ctx, 500*time.Millisecond)

	// Idle-session sweeper — disconnect peers that haven't sent any
	// frame (keepalive included) for idleSessionTTL. Backstop for
	// clients that crash mid-session and don't unsubscribe: without
	// this, their subs accumulate in the server.subs table forever.
	// Spec p.10 keepalive is short; healthy peers always re-stamp
	// lastActive well within the TTL.
	go s.runIdleSweeper(ctx, idleSweepInterval, idleSessionTTL)

	// Stream idle-TTL sweeper (R9 #472) — soft variant of the above:
	// clears the session's subscription set when lastActive is older
	// than streamIdleTTL but KEEPS the TCP session open in case the
	// peer resumes keep-alives. Subs cleared via the same path
	// Unsubscribe uses (server.unsubscribe). Disabled when
	// streamIdleTTL == 0.
	if s.streamIdleTTL > 0 {
		interval := s.streamIdleTTL / 3
		if interval < time.Second {
			interval = time.Second
		}
		go s.runStreamIdleSweeper(ctx, interval, s.streamIdleTTL)
	}

	// Close listener on ctx cancel to unblock Accept.
	go func() {
		select {
		case <-ctx.Done():
		case <-s.stopped:
		}
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			if ctx.Err() != nil {
				return nil
			}
			s.logger.Debug("accept", slog.String("err", err.Error()))
			continue
		}
		sess := newSession(s, conn)
		s.registerSession(sess)
		go sess.run(ctx)
	}
}

// Stop implements provider.Provider.
func (s *server) Stop() error {
	s.stopOnce.Do(func() {
		close(s.stopped)
		s.mu.Lock()
		ln := s.listener
		sessions := make([]*session, 0, len(s.sessions))
		for sess := range s.sessions {
			sessions = append(sessions, sess)
		}
		s.mu.Unlock()
		if ln != nil {
			_ = ln.Close()
		}
		for _, sess := range sessions {
			sess.close()
		}
	})
	return nil
}

// SetValue mutates a parameter on the served tree and broadcasts a
// QualifiedParameter announcement to every subscribed consumer.
func (s *server) SetValue(ctx context.Context, path string, val any) (any, error) {
	oid := path
	// Allow dotted identifier paths — resolve to OID via the tree index.
	if e, ok := s.tree.lookupPath(path); ok {
		oid = e.el.Common().OID
	}
	p, err := s.tree.setParamValue(oid, val)
	if err != nil {
		return nil, err
	}
	s.broadcastParam(oid, p)
	return p.Value, nil
}

// --- Session bookkeeping ---

func (s *server) registerSession(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sess] = struct{}{}
}

func (s *server) dropSession(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sess)
	for oid, set := range s.subs {
		delete(set, sess)
		if len(set) == 0 {
			delete(s.subs, oid)
		}
	}
}

func (s *server) subscribe(sess *session, oid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set, ok := s.subs[oid]
	if !ok {
		set = map[*session]struct{}{}
		s.subs[oid] = set
	}
	set[sess] = struct{}{}
	sess.subs[oid] = struct{}{}
}

func (s *server) unsubscribe(sess *session, oid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if set, ok := s.subs[oid]; ok {
		delete(set, sess)
		if len(set) == 0 {
			delete(s.subs, oid)
		}
	}
	delete(sess.subs, oid)
}

// broadcastParam fans out a QualifiedParameter announcement to every
// active consumer session. The Subscribe / Unsubscribe commands in the
// Ember+ spec gate STREAM parameter emission specifically; for plain
// parameters every shipping provider (libember-cpp, TinyEmber+, Lawo
// stacks) pushes value-change announcements to all connected sessions
// regardless of explicit subscription. Most consumers (EmberViewer,
// EmberPlusView, mc² controllers) never send Subscribe for non-stream
// parameters and rely on this fan-out — without it they freeze on the
// initial GetDirectory snapshot. Subscribers that missed the
// send-queue high-water-mark silently drop the frame — see
// session.send. Stream-parameter fan-out stays subscription-gated in
// streamer.go.
// Idle-sweeper tunables — aligned with the Cerebrum-NB convention:
// if a peer stops sending any frame (keepalive included) for 30 s,
// drop the session. S101 keepalive cadence is typically 5-15 s, so a
// healthy peer re-stamps lastActive at least twice within the window;
// a peer that misses 2-3 keepalives is genuinely dead and its subs
// would otherwise sit in server.subs forever (cf. [[project-keepalive-contract]]).
// Sweep every 10 s so the worst-case detection latency is TTL + interval ≈ 40 s.
const (
	idleSweepInterval = 10 * time.Second
	idleSessionTTL    = 30 * time.Second
)

// runIdleSweeper closes sessions whose lastActive timestamp is older
// than ttl. Backstop for clients that crash without unsubscribing — the
// subs they registered would otherwise sit in server.subs forever,
// growing memory + (more importantly) keeping the streamer / parameter
// broadcaster doing per-tick work for a dead consumer.
func (s *server) runIdleSweeper(ctx context.Context, interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopped:
			return
		case <-ticker.C:
			s.sweepIdleSessions(ttl)
		}
	}
}

// sweepIdleSessions collects sessions whose lastActive is older than
// `now - ttl`, then closes them outside the server lock so close() can
// re-acquire it via dropSession without deadlocking.
func (s *server) sweepIdleSessions(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl).UnixNano()
	s.mu.Lock()
	var idle []*session
	for sess := range s.sessions {
		if sess.lastActive.Load() < cutoff {
			idle = append(idle, sess)
		}
	}
	s.mu.Unlock()
	for _, sess := range idle {
		s.logger.Info("idle session swept",
			slog.String("peer", sess.id),
			slog.Duration("ttl", ttl),
		)
		sess.close()
	}
}

// runStreamIdleSweeper is the soft variant of runIdleSweeper (R9 #472):
// fires sweepStreamIdleSubs at `interval` until ctx cancels or Stop()
// closes s.stopped. interval should be ≤ ttl/2 so the worst-case
// detection latency stays bounded; the caller in Serve picks ttl/3.
func (s *server) runStreamIdleSweeper(ctx context.Context, interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopped:
			return
		case <-ticker.C:
			s.sweepStreamIdleSubs(ttl)
		}
	}
}

// sweepStreamIdleSubs clears the subscription set on every session
// whose lastActive is older than `now - ttl` AND which currently holds
// at least one subscription. The TCP session stays open — only the
// subs are released, mirroring an explicit per-OID Unsubscribe from
// the peer. Each cleared session ticks one StreamIdleTTLExpired event
// in the compliance profile and logs INFO with last_rx.
//
// Lock discipline: collect under s.mu, release the lock before calling
// s.unsubscribe (which retakes it) to avoid re-entry on the same
// goroutine. Sessions are pointer-identified so concurrent dropSession
// during the gap is benign (unsubscribe handles already-unregistered
// oids by no-op on the map delete).
func (s *server) sweepStreamIdleSubs(ttl time.Duration) {
	cutoff := time.Now().Add(-ttl).UnixNano()
	type pending struct {
		sess   *session
		oids   []string
		lastRx time.Duration
	}
	s.mu.Lock()
	var todo []pending
	for sess := range s.sessions {
		if len(sess.subs) == 0 {
			continue
		}
		last := sess.lastActive.Load()
		if last >= cutoff {
			continue
		}
		oids := make([]string, 0, len(sess.subs))
		for oid := range sess.subs {
			oids = append(oids, oid)
		}
		todo = append(todo, pending{
			sess:   sess,
			oids:   oids,
			lastRx: time.Since(time.Unix(0, last)),
		})
	}
	s.mu.Unlock()
	for _, p := range todo {
		for _, oid := range p.oids {
			s.unsubscribe(p.sess, oid)
		}
		s.profile.Note(StreamIdleTTLExpired)
		s.logger.Info("stream-ttl expired",
			slog.String("session", p.sess.id),
			slog.Duration("last_rx", p.lastRx),
			slog.Int("subs_cleared", len(p.oids)),
		)
	}
}

func (s *server) broadcastParam(oid string, p *canonical.Parameter) {
	// Encode the announcement while holding the tree read lock so a
	// concurrent SetValue cannot tear p.Value mid-read. canonical.Parameter.Value
	// is `any` (two-word interface header) — without the read lock, the
	// encoder can observe a partially-updated header and either panic on
	// type-assert or emit corrupted bytes. The lock window is tiny
	// (encode is pure-compute, no I/O), so write-side contention is
	// negligible — far cheaper than the alternative deep-copy of every
	// Parameter on every announce.
	s.tree.mu.RLock()
	e, ok := s.tree.byOID[oid]
	if !ok {
		s.tree.mu.RUnlock()
		return
	}
	payload := s.encodeParamAnnouncement(e, p)
	s.tree.mu.RUnlock()

	s.mu.Lock()
	targets := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
		targets = append(targets, sess)
	}
	s.mu.Unlock()

	for _, sess := range targets {
		sess.send(payload)
	}
}
