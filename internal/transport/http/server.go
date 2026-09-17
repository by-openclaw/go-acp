package http

// The server half of this package. It was lifted out of
// internal/amwa/session/http, where a generic route table, TLS listener,
// CORS policy and panic barrier had grown NMOS-shaped only in two places:
// the error body and the auth gate. Both are now injected, so the machinery
// is reusable and the spec-specific parts stayed with the spec.
//
// What is deliberately NOT here: the IS-04 §4.4 error envelope, the
// BCP-003-02 token rules, and every NMOS route. Those live in
// internal/amwa/session/http, which wires this server and adds them.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	stdhttp "net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
)

// readHeaderTimeout bounds how long a client may take to send its headers.
// Without it a handful of idle connections holds the server open — the
// Slowloris shape.
const readHeaderTimeout = 5 * time.Second

// shutdownGrace is how long in-flight requests get to finish on shutdown.
const shutdownGrace = 5 * time.Second

// HandlerFunc is the per-route signature. Returning an error becomes a 500
// carrying whatever [ErrorEncoder] shapes; returning a value JSON-encodes it
// with the given status, or 200 when status is zero.
type HandlerFunc func(ctx context.Context, r *stdhttp.Request) (status int, body any, err error)

// Gate optionally vets every request before it reaches a route — including
// raw (WebSocket) handlers, so one policy covers both.
//
// It returns a REQUEST rather than a bare identity so a gate can stamp its own
// context values without this package knowing what they mean. ok=false means
// the caller writes status/headers/body and stops.
type Gate interface {
	Check(r *stdhttp.Request) (status int, headers map[string]string, body any, next *stdhttp.Request, ok bool)
}

// GateFunc adapts a function to [Gate].
type GateFunc func(r *stdhttp.Request) (int, map[string]string, any, *stdhttp.Request, bool)

// Check calls f.
func (f GateFunc) Check(r *stdhttp.Request) (int, map[string]string, any, *stdhttp.Request, bool) {
	return f(r)
}

// ErrorEncoder shapes the body of an error response. Protocols disagree about
// this — IS-04 §4.4 wants {code,error,debug}, another API wants RFC 7807 —
// so the server never invents one.
type ErrorEncoder func(status int, err, debug string) any

// defaultError is the fallback shape when no encoder is injected.
type defaultError struct {
	Code  int    `json:"code"`
	Error string `json:"error"`
	Debug string `json:"debug,omitempty"`
}

// CORS is the cross-origin policy applied to every response.
//
// A zero value means NO CORS headers, which is the correct default for a
// server that is not browser-facing. Callers that need them set them; the
// NMOS wrapper does.
type CORS struct {
	AllowOrigin  string
	AllowHeaders string
	MaxAge       string
}

// Server is a route table over net/http. Routes are exact-match
// (method, path) or prefix-match (method, prefix*). Exact wins over prefix;
// longer prefixes win over shorter.
type Server struct {
	Logger *slog.Logger

	// Gate, when non-nil, vets every request (routes AND raw handlers).
	// Set before Serve. OPTIONS preflights bypass it — they are
	// credential-less by browser design.
	Gate Gate

	// Errors shapes error bodies. Nil uses a {code,error,debug} default.
	Errors ErrorEncoder

	// CORS is applied to every response. Zero value emits nothing.
	CORS CORS

	// TLS, when non-nil, makes Serve listen with HTTPS/WSS instead of plain
	// HTTP — one or the other on a listener, never both. Responses then
	// carry Strict-Transport-Security.
	TLS *tls.Config

	mu       sync.RWMutex
	routes   map[routeKey]HandlerFunc
	prefixes []prefixRoute // longer prefixes first
	raw      map[string]stdhttp.Handler
	mux      *stdhttp.ServeMux
	srv      *stdhttp.Server
}

type routeKey struct {
	method string
	path   string
}

type prefixRoute struct {
	method string
	prefix string
	fn     HandlerFunc
}

// altSlashForm returns the other spelling of a path: with a trailing slash if
// it had none, without if it had one.
func altSlashForm(p string) string {
	if strings.HasSuffix(p, "/") {
		return strings.TrimSuffix(p, "/")
	}
	return p + "/"
}

// NewServer constructs an empty Server.
func NewServer(logger *slog.Logger) *Server {
	return &Server{Logger: logger, routes: make(map[routeKey]HandlerFunc)}
}

// Handle registers an exact-match (method, path) handler. Re-registering the
// same key panics — a wiring bug should fail loudly at start, not serve one
// of two handlers at random.
func (s *Server) Handle(method, path string, fn HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := routeKey{method: method, path: path}
	if _, dup := s.routes[k]; dup {
		panic(fmt.Sprintf("transport/http: duplicate route %s %s", method, path))
	}
	s.routes[k] = fn
}

// HandlePrefix registers a prefix-match (method, prefix*) handler, for paths
// whose tail is a parameter. Longer prefixes win over shorter ones.
func (s *Server) HandlePrefix(prefix, method string, fn HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pr := prefixRoute{method: method, prefix: prefix, fn: fn}
	idx := 0
	for idx < len(s.prefixes) && len(s.prefixes[idx].prefix) >= len(prefix) {
		idx++
	}
	s.prefixes = append(s.prefixes, prefixRoute{})
	copy(s.prefixes[idx+1:], s.prefixes[idx:])
	s.prefixes[idx] = pr
}

// HandleRaw registers a plain http.Handler at an exact path, bypassing the
// (status, body, error) route table.
//
// Needed for WebSocket upgrades and nothing else: the normal signature returns
// a value the server then serialises, so the framework owns the
// ResponseWriter — but an upgrade must HIJACK the connection and keep it.
func (s *Server) HandleRaw(path string, h stdhttp.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.raw == nil {
		s.raw = map[string]stdhttp.Handler{}
	}
	if _, dup := s.raw[path]; dup {
		panic("transport/http: duplicate raw route " + path)
	}
	s.raw[path] = h
}

// MuxHandler returns the route-table dispatcher as a plain handler, for
// callers composing their own net/http.Server. Safe for concurrent use.
func (s *Server) MuxHandler() stdhttp.Handler {
	return stdhttp.HandlerFunc(s.dispatch)
}

// Serve binds to addr and serves until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, addr string) error {
	s.mu.Lock()
	if s.mux != nil {
		s.mu.Unlock()
		return fmt.Errorf("transport/http: server already started")
	}
	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/", s.dispatch)
	s.mux = mux
	s.srv = &stdhttp.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
		TLSConfig:         s.TLS,
	}
	srv := s.srv
	secure := s.TLS != nil
	s.mu.Unlock()

	errCh := make(chan error, 1)
	go func() {
		var err error
		if secure {
			// Certificates come from TLSConfig's GetCertificate hook so they
			// stay hot-reloadable on renewal — hence the empty paths.
			err = srv.ListenAndServeTLS("", "")
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}

// errorBody shapes an error through the injected encoder, or the default.
func (s *Server) errorBody(status int, errStr, debug string) any {
	if s.Errors != nil {
		return s.Errors(status, errStr, debug)
	}
	return defaultError{Code: status, Error: errStr, Debug: debug}
}

func (s *Server) writeError(w stdhttp.ResponseWriter, status int, errStr, debug string) {
	s.writeJSON(w, status, s.errorBody(status, errStr, debug))
}

// dispatch walks the route table and emits the response — or a 404/405/500.
func (s *Server) dispatch(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	// The panic barrier is the outermost layer on purpose: a handler that
	// panics must still produce a response, or the client sees a dropped
	// connection and retries into the same panic.
	defer func() {
		if rec := recover(); rec != nil {
			if s.Logger != nil {
				s.Logger.Error("transport/http: handler panic",
					"method", r.Method, "path", r.URL.Path, "panic", rec,
					"stack", string(debug.Stack()))
			}
			s.writeError(w, stdhttp.StatusInternalServerError, "Internal Server Error", "panic recovered")
		}
	}()

	// A TLS server declares it only speaks TLS (12-month max-age).
	if r.TLS != nil {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}

	// CORS preflight, before the gate: preflights are credential-less by
	// browser design, so gating them makes every cross-origin call fail.
	// 200 with an empty JSON object rather than 204, so the Content-Type
	// stays consistent with every other response.
	if r.Method == stdhttp.MethodOptions {
		s.setCORS(w, s.methodsForPath(r.URL.Path))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte("{}"))
		return
	}

	// The gate runs BEFORE the raw handlers, so a WebSocket upgrade is held
	// to the same policy as a plain route.
	if s.Gate != nil {
		status, hdrs, body, next, ok := s.Gate.Check(r)
		if !ok {
			for k, v := range hdrs {
				w.Header().Set(k, v)
			}
			s.writeJSON(w, status, body)
			return
		}
		if next != nil {
			r = next
		}
	}

	// Raw handlers next, before the CORS/JSON machinery has touched the
	// ResponseWriter — a hijack must happen on a connection nothing wrote to.
	s.mu.RLock()
	rawH, isRaw := s.raw[r.URL.Path]
	if !isRaw {
		rawH, isRaw = s.raw[altSlashForm(r.URL.Path)]
	}
	s.mu.RUnlock()
	if isRaw {
		rawH.ServeHTTP(w, r)
		return
	}

	s.mu.RLock()
	fn, ok := s.routes[routeKey{method: r.Method, path: r.URL.Path}]
	if !ok {
		// A trailing slash is not a different resource. Clients send both
		// forms freely, and answering 404 for the other spelling is
		// technically defensible and practically useless. Tried only after
		// the exact match, so a route registered for one form still wins.
		fn, ok = s.routes[routeKey{method: r.Method, path: altSlashForm(r.URL.Path)}]
	}
	if !ok {
		for _, pr := range s.prefixes { // longest first
			if pr.method == r.Method && strings.HasPrefix(r.URL.Path, pr.prefix) {
				fn, ok = pr.fn, true
				break
			}
		}
	}
	if !ok {
		// 404 vs 405: does ANY route match this path under another method?
		// Answering 404 for a wrong method tells a client the endpoint does
		// not exist. Both slash forms, for the reason above.
		methodNotAllowed := false
		alt := altSlashForm(r.URL.Path)
		for k := range s.routes {
			if (k.path == r.URL.Path || k.path == alt) && k.method != r.Method {
				methodNotAllowed = true
				break
			}
		}
		if !methodNotAllowed {
			for _, pr := range s.prefixes {
				if pr.method != r.Method && strings.HasPrefix(r.URL.Path, pr.prefix) {
					methodNotAllowed = true
					break
				}
			}
		}
		s.mu.RUnlock()
		if methodNotAllowed {
			s.writeError(w, stdhttp.StatusMethodNotAllowed, "Method Not Allowed", r.Method+" "+r.URL.Path)
		} else {
			s.writeError(w, stdhttp.StatusNotFound, "Not Found", r.URL.Path)
		}
		return
	}
	s.mu.RUnlock()

	status, body, err := fn(r.Context(), r)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("transport/http: handler error",
				"method", r.Method, "path", r.URL.Path, "err", err)
		}
		s.writeError(w, stdhttp.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	if status == 0 {
		status = stdhttp.StatusOK
	}
	s.writeJSON(w, status, body)
}

// RawBody lets a handler return a non-JSON response — SDP text, a certificate,
// anything the spec requires not be JSON. Emitted verbatim with CORS attached.
type RawBody struct {
	ContentType string
	Body        []byte
}

// WithHeaders lets a handler attach extra response headers without changing
// the HandlerFunc signature. Body is encoded as usual and may itself be a
// *RawBody, to combine non-JSON content with custom headers.
type WithHeaders struct {
	Body    any
	Headers map[string]string
}

// writeJSON serialises body, honouring *WithHeaders and *RawBody.
func (s *Server) writeJSON(w stdhttp.ResponseWriter, status int, body any) {
	if wh, ok := body.(*WithHeaders); ok {
		for k, v := range wh.Headers {
			w.Header().Set(k, v)
		}
		s.writeJSON(w, status, wh.Body)
		return
	}
	if rb, ok := body.(*RawBody); ok {
		ct := rb.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		s.setCORS(w, "")
		w.WriteHeader(status)
		_, _ = w.Write(rb.Body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	s.setCORS(w, "")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil && s.Logger != nil {
		// The status line is already flushed, so this cannot become a 500.
		// Logging it is the only honest thing left: the client will see a
		// truncated body and would otherwise have nothing to correlate.
		s.Logger.Error("transport/http: response encode failed after headers were sent",
			"status", status, "err", err)
	}
}

// setCORS applies the configured policy. allowMethods, when non-empty, also
// sets Access-Control-Allow-Methods and Allow — used on preflights.
func (s *Server) setCORS(w stdhttp.ResponseWriter, allowMethods string) {
	h := w.Header()
	if s.CORS.AllowOrigin != "" {
		h.Set("Access-Control-Allow-Origin", s.CORS.AllowOrigin)
	}
	if s.CORS.AllowHeaders != "" {
		h.Set("Access-Control-Allow-Headers", s.CORS.AllowHeaders)
	}
	if s.CORS.MaxAge != "" {
		h.Set("Access-Control-Max-Age", s.CORS.MaxAge)
	}
	if allowMethods != "" {
		h.Set("Access-Control-Allow-Methods", allowMethods)
		h.Set("Allow", allowMethods)
	}
}

// methodsForPath returns the comma-separated methods the route table accepts
// at path, always including OPTIONS, so a preflight tells the peer which verbs
// are real.
func (s *Server) methodsForPath(path string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]struct{}{stdhttp.MethodOptions: {}}
	// Both slash forms, for the same reason dispatch accepts both: a
	// preflight for `.../senders` must advertise the POST registered at
	// `.../senders/`, or the browser refuses the request that would work.
	alt := altSlashForm(path)
	for k := range s.routes {
		if k.path == path || k.path == alt {
			seen[k.method] = struct{}{}
		}
	}
	for _, pr := range s.prefixes {
		if strings.HasPrefix(path, pr.prefix) {
			seen[pr.method] = struct{}{}
		}
	}
	order := []string{
		stdhttp.MethodOptions, stdhttp.MethodGet, stdhttp.MethodHead,
		stdhttp.MethodPost, stdhttp.MethodPut, stdhttp.MethodPatch, stdhttp.MethodDelete,
	}
	out := make([]string, 0, len(seen))
	for _, m := range order {
		if _, ok := seen[m]; ok {
			out = append(out, m)
		}
	}
	return strings.Join(out, ", ")
}
