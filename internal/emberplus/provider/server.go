package emberplus

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"dhs/internal/emberplus/codec/s101"
	"dhs/internal/export/canonical"
	"dhs/internal/plugin"
	"dhs/internal/provider"
)

// server is the provider runtime. One listener, many sessions, a shared
// tree, and a per-OID subscription table.
type server struct {
	// Base owns the listener, the live-session set, the stop sequence and
	// the metrics connector — the same contract acp2 / probel-sw08p /
	// probel-sw02p embed. The subscription table below stays here under
	// s.mu: Base manages set MEMBERSHIP, this server manages who-watches-
	// what. The two locks are only ever taken sequentially — Conns() first,
	// then s.mu — never nested, so a session dropped between them degrades
	// safely: a write to a closing session errors, an emptied subs sends
	// nothing.
	provider.Base[*session]

	logger *slog.Logger

	tree      *tree
	templates []*canonical.TemplateEntry
	funcs     *functionRegistry
	salvos    *salvoStore
	locks     *lockStore

	// mu guards the subscription table only (the server subs map and every
	// sess.subs). Base owns its own lock for the session set.
	mu sync.Mutex
	// subs: oid -> set of sessions watching it
	subs map[string]map[*session]struct{}
}

func newServer(deps plugin.Deps, exp *canonical.Export) *server {
	deps = deps.WithDefaults()
	t, err := newTree(exp)
	s := &server{
		logger: deps.Logger.With(slog.String("plugin", "emberplus-provider")),
		funcs:  newFunctionRegistry(),
		subs:   map[string]map[*session]struct{}{},
	}
	s.Init(deps)
	s.Metrics().RegisterCmd(s101.CmdEmBER, "ember")
	s.Metrics().RegisterCmd(s101.CmdKeepAliveReq, "keepalive-req")
	s.Metrics().RegisterCmd(s101.CmdKeepAliveResp, "keepalive-resp")
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

// Serve implements provider.Provider. Blocks until ctx is cancelled or
// the listener returns a fatal error.
func (s *server) Serve(ctx context.Context, addr string) error {
	if s.tree == nil {
		return fmt.Errorf("emberplus-provider: tree not loaded")
	}
	ln, err := s.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	return s.serveListener(ctx, ln)
}

// ServeListener serves on a pre-bound listener. Exported on the concrete
// type (not part of the neutral provider.Provider interface) so tests in
// other packages can bind "127.0.0.1:0" themselves and skip the
// close-then-rebind window of the addr-based path — in a parallel test
// sweep another process can steal the freed port (issue #694 flake class).
func (s *server) ServeListener(ctx context.Context, ln net.Listener) error {
	if s.tree == nil {
		_ = ln.Close()
		return fmt.Errorf("emberplus-provider: tree not loaded")
	}
	return s.serveListener(ctx, ln)
}

func (s *server) serveListener(ctx context.Context, ln net.Listener) error {
	s.logger.Info("listening",
		slog.String("addr", ln.Addr().String()),
		slog.Int("tree_size", len(s.tree.byOID)),
	)

	// Unsolicited stream fan-out — runs if the tree has any Parameters
	// with a streamIdentifier, exits on ctx cancel or Base.Stopped().
	go s.runStreamer(ctx, 500*time.Millisecond)

	// Idle-session sweeper — disconnect peers that haven't sent any
	// frame (keepalive included) for idleSessionTTL. Backstop for
	// clients that crash mid-session and don't unsubscribe: without
	// this, their subs accumulate in the server.subs table forever.
	// Spec p.10 keepalive is short; healthy peers always re-stamp
	// lastActive well within the TTL.
	go s.runIdleSweeper(ctx, idleSweepInterval, idleSessionTTL)

	// Base.Stop closes the listener (unblocking Accept) and drains every
	// tracked session; idempotent, so a later explicit Stop is a no-op.
	go func() {
		<-ctx.Done()
		_ = s.Stop()
	}()

	// Base.AcceptLoop applies the socket policy per connection, backs off
	// on transient accept errors, tracks each session and removes it when
	// Run returns. newSession is the protocol's only per-connection work.
	return s.AcceptLoop(ctx, ln, func(conn net.Conn) *session {
		return newSession(s, conn)
	})
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

// dropSession removes a session from Base's set and cleans its entries out
// of the subscription table. The two are separate locks taken in sequence,
// never nested: Base.Remove first, then s.mu for the subs sweep.
func (s *server) dropSession(sess *session) {
	s.Remove(sess)
	s.mu.Lock()
	defer s.mu.Unlock()
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
		case <-s.Stopped():
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
	var idle []*session
	for _, sess := range s.Conns() {
		if sess.lastActive.Load() < cutoff {
			idle = append(idle, sess)
		}
	}
	for _, sess := range idle {
		s.logger.Info("idle session swept",
			slog.String("peer", sess.id),
			slog.Duration("ttl", ttl),
		)
		sess.close()
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

	// Base owns the session set; snapshot it and fan out. A session that
	// closed since the snapshot just errors on send (see session.send).
	for _, sess := range s.Conns() {
		sess.send(payload)
	}
}
