package emberplus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"acp/internal/export/canonical"
)

// server is the provider runtime. One listener, many sessions, a shared
// tree, and a per-OID subscription table.
type server struct {
	logger *slog.Logger
	tree   *tree
	funcs  *functionRegistry
	salvos *salvoStore
	locks  *lockStore

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
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	s.logger.Info("listening",
		slog.String("addr", ln.Addr().String()),
		slog.Int("tree_size", len(s.tree.byOID)),
	)

	// Unsolicited stream fan-out — runs if the tree has any Parameters
	// with a streamIdentifier, exits on ctx cancel or Stop().
	go s.runStreamer(ctx, 100*time.Millisecond)

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
func (s *server) broadcastParam(oid string, p *canonical.Parameter) {
	e, ok := s.tree.lookupOID(oid)
	if !ok {
		return
	}
	payload := s.encodeParamAnnouncement(e, p)

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
