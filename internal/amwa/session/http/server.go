package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"runtime/debug"
	"sync"
	"time"
)

// HandlerFunc is the per-route signature. Returning an error becomes
// HTTP 500 with the spec-compatible body shape; returning a value
// is JSON-encoded with status 200 (or status if explicitly returned).
type HandlerFunc func(ctx context.Context, r *stdhttp.Request) (status int, body any, err error)

// Server is a thin route table over net/http.Server. Routes are an
// exact-match `(method, path) -> HandlerFunc`; no wildcards yet (IS-09
// has zero parameterised paths).
type Server struct {
	Logger *slog.Logger

	mu     sync.RWMutex
	routes map[routeKey]HandlerFunc
	mux    *stdhttp.ServeMux
	srv    *stdhttp.Server
}

type routeKey struct {
	method string
	path   string
}

// ErrorBody mirrors the IS-04 §4.4 / IS-09 RAML error envelope so
// peers see a uniform shape on 4xx / 5xx.
type ErrorBody struct {
	Code  int    `json:"code"`
	Error string `json:"error"`
	Debug string `json:"debug,omitempty"`
}

// NewServer constructs an empty Server with a default logger if nil.
func NewServer(logger *slog.Logger) *Server {
	return &Server{
		Logger: logger,
		routes: make(map[routeKey]HandlerFunc),
	}
}

// Handle registers an exact-match (method, path) handler. Re-registering
// the same key panics — wiring bugs should fail loudly.
func (s *Server) Handle(method, path string, fn HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := routeKey{method: method, path: path}
	if _, dup := s.routes[k]; dup {
		panic(fmt.Sprintf("nmos/http: duplicate route %s %s", method, path))
	}
	s.routes[k] = fn
}

// Serve binds to addr and serves until ctx is cancelled. Returns the
// first non-graceful shutdown error.
func (s *Server) Serve(ctx context.Context, addr string) error {
	s.mu.Lock()
	if s.mux != nil {
		s.mu.Unlock()
		return fmt.Errorf("nmos/http: server already started")
	}
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/", s.dispatch)
	s.mux = mux
	s.srv = &stdhttp.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	srv := s.srv
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// dispatch is the single net/http handler that walks the route table
// and emits the response — or a spec-shaped 404 / 405 / 500.
func (s *Server) dispatch(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			if s.Logger != nil {
				s.Logger.Error("nmos/http: handler panic",
					"method", r.Method, "path", r.URL.Path, "panic", rec,
					"stack", string(debug.Stack()))
			}
			writeErrorJSON(w, stdhttp.StatusInternalServerError, "Internal Server Error", "panic recovered")
		}
	}()

	s.mu.RLock()
	fn, ok := s.routes[routeKey{method: r.Method, path: r.URL.Path}]
	if !ok {
		// Try the other commonly-paired method to choose 404 vs 405.
		methodNotAllowed := false
		for k := range s.routes {
			if k.path == r.URL.Path && k.method != r.Method {
				methodNotAllowed = true
				break
			}
		}
		s.mu.RUnlock()
		if methodNotAllowed {
			writeErrorJSON(w, stdhttp.StatusMethodNotAllowed, "Method Not Allowed", r.Method+" "+r.URL.Path)
		} else {
			writeErrorJSON(w, stdhttp.StatusNotFound, "Not Found", r.URL.Path)
		}
		return
	}
	s.mu.RUnlock()

	status, body, err := fn(r.Context(), r)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("nmos/http: handler error",
				"method", r.Method, "path", r.URL.Path, "err", err)
		}
		writeErrorJSON(w, stdhttp.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	if status == 0 {
		status = stdhttp.StatusOK
	}
	writeJSON(w, status, body)
}

// writeJSON serialises body as JSON with the spec-mandated header set.
func writeJSON(w stdhttp.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		// Headers already flushed; nothing to do but log via panic
		// recovery in the caller. We don't have a slog handle here.
		_ = err
	}
}

// writeErrorJSON emits the IS-04 §4.4 error envelope.
func writeErrorJSON(w stdhttp.ResponseWriter, status int, errStr, debug string) {
	writeJSON(w, status, ErrorBody{
		Code:  status,
		Error: errStr,
		Debug: debug,
	})
}
