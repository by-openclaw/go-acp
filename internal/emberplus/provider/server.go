package emberplus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"dhs/internal/export/canonical"
	"dhs/internal/transport"
)

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

	// Close listener on ctx cancel to unblock Accept.
	go func() {
		select {
		case <-ctx.Done():
		case <-s.stopped:
		}
		_ = ln.Close()
	}()

	return s.acceptLoop(ctx, ln)
}

// acceptLoop runs the listener accept loop until the listener is closed
// or the context is cancelled. Split out of Serve as a testability seam:
// a test can drive it with a fake net.Listener that injects a transient
// (non-ErrClosed) accept error to exercise the accept-error-continue
// branch, which a real OS listener will not produce on demand.
func (s *server) acceptLoop(ctx context.Context, ln net.Listener) error {
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
		// OS-level dead-peer probe. Without it a half-open client session
		// (a NAT or firewall drop with no RST) holds a goroutine and a
		// socket here for ever. Applied in the accept loop rather than at
		// bind time so ServeListener's injected listener gets it too.
		_ = transport.ApplySocketOptions(conn, transport.SocketOptions{})
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
