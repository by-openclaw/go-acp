// Package admin is the local-socket runtime admin control plane for
// the dhs producer (R25 #490). Per memory project_provider_admin
// (now ADR-0027 aligned) and feedback_admin_web_minimal: local-only,
// never network-exposed; filesystem-permission auth on Unix, named-
// pipe ACL on Windows.
//
// Wire format: line-delimited JSON. One request → one response.
//
//	-> {"feature":"sessions","action":"list"}
//	<- {"ok":true,"data":[{"peer":"10.6.239.113:54321","connected":true,...}]}
//
// Pragmatic v1 ships only the read-only `sessions list` verb; the
// hot-reload matrix in R25 #490 spec (health enable/disable, metrics
// enable, log-level set, streamer-interval set, compliance reset/show)
// rolls in as v1.5 feature-by-feature. Verb dispatch returns
// admin:verb-not-implemented for unknown verbs so the CLI gets a
// stable signal across versions.
package admin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// Request is one admin call. Verb identifier is `<feature>:<action>`
// per R25 spec; example: `sessions:list`, `health:enable`,
// `log-level:set`.
type Request struct {
	Verb   string          `json:"verb"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is the JSON reply shape.
type Response struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

// Handler is the per-verb callback the producer's plugin code
// registers via Server.Register. Returns the JSON payload to embed
// in Response.Data; nil = empty data field. Defined as a type alias
// (not a defined type) so plugin packages can declare matching
// interfaces without importing this package.
type Handler = func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)

// Server is the local-socket admin listener. One per producer.
type Server struct {
	logSink io.Writer // INFO log destination; producer's stderr by default

	mu       sync.RWMutex
	handlers map[string]Handler
	listener net.Listener
	closed   chan struct{}
}

// NewServer constructs an admin Server. logSink may be nil → discards.
func NewServer(logSink io.Writer) *Server {
	if logSink == nil {
		logSink = io.Discard
	}
	return &Server{
		logSink:  logSink,
		handlers: map[string]Handler{},
		closed:   make(chan struct{}),
	}
}

// Register binds verb to handler. Replaces any prior handler for the
// same verb name. Verb format: `<feature>:<action>`.
func (s *Server) Register(verb string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[verb] = h
}

// Serve starts the listener on socketPath (Unix socket on every
// supported OS — Go 1.17+ supports AF_UNIX on Windows). Blocks until
// ctx cancels or the listener errors fatally. The socket is removed
// on shutdown.
func (s *Server) Serve(ctx context.Context, socketPath string) error {
	// Best-effort cleanup of stale socket from a prior crash.
	_ = os.Remove(socketPath)
	lc := net.ListenConfig{}
	ln, err := lc.Listen(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("admin: listen %s: %w", socketPath, err)
	}
	// Unix permissions: 0600 — owner-only. Filesystem auth.
	_ = os.Chmod(socketPath, 0o600)

	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()

	_, _ = fmt.Fprintf(s.logSink, "admin: listening on %s\n", socketPath)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				_ = os.Remove(socketPath)
				return nil
			}
			_, _ = fmt.Fprintf(s.logSink, "admin: accept: %v\n", err)
			continue
		}
		go s.handleConn(ctx, conn)
	}
}

// handleConn services one client connection. Reads line-delimited
// JSON Requests, dispatches to the handler, writes Response. Closes
// on EOF or first error.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	line, err := r.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
	if len(line) == 0 {
		return
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		writeResp(w, Response{OK: false, Error: "admin: bad request JSON"})
		return
	}

	s.mu.RLock()
	h, ok := s.handlers[req.Verb]
	s.mu.RUnlock()
	if !ok {
		writeResp(w, Response{OK: false, Error: "admin:verb-not-implemented: " + req.Verb})
		return
	}
	data, err := h(ctx, req.Params)
	if err != nil {
		writeResp(w, Response{OK: false, Error: err.Error()})
		return
	}
	writeResp(w, Response{OK: true, Data: data})
	_, _ = fmt.Fprintf(s.logSink, "admin: %s\n", req.Verb)
}

func writeResp(w *bufio.Writer, resp Response) {
	buf, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = w.Write(buf)
	_, _ = w.Write([]byte{'\n'})
	_ = w.Flush()
}

// Call is the client-side helper that dials the local admin socket,
// sends one Request, and returns the parsed Response. Used by the
// `dhs producer <proto> admin <verb>` CLI dispatcher.
func Call(ctx context.Context, socketPath string, req Request) (Response, error) {
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return Response{}, err
	}
	defer func() { _ = conn.Close() }()
	buf, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	if _, err := conn.Write(append(buf, '\n')); err != nil {
		return Response{}, err
	}
	respLine, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(respLine, &resp); err != nil {
		return Response{}, fmt.Errorf("admin: bad response JSON: %w", err)
	}
	return resp, nil
}

// DefaultSocketPath returns the conventional socket path for a
// connector instance. Caller passes a unique identifier (e.g.
// `emberplus-9000`) to disambiguate when multiple instances run.
func DefaultSocketPath(connectorTag string) string {
	dir := os.TempDir()
	return fmt.Sprintf("%s/dhs-%s-admin.sock", dir, connectorTag)
}
