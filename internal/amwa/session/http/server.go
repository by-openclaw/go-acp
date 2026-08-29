package http

import (
	"context"
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

// HandlerFunc is the per-route signature. Returning an error becomes
// HTTP 500 with the spec-compatible body shape; returning a value
// is JSON-encoded with status 200 (or status if explicitly returned).
type HandlerFunc func(ctx context.Context, r *stdhttp.Request) (status int, body any, err error)

// Server is a thin route table over net/http.Server. Routes are
// either exact-match `(method, path) -> HandlerFunc` or prefix-match
// `(method, prefix*) -> HandlerFunc`. Exact matches win over prefix
// matches; longer prefixes win over shorter prefixes.
type Server struct {
	Logger *slog.Logger

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

// altSlashForm returns the other spelling of a path: with a trailing
// slash if it had none, without if it had one.
func altSlashForm(p string) string {
	if strings.HasSuffix(p, "/") {
		return strings.TrimSuffix(p, "/")
	}
	return p + "/"
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

// HandlePrefix registers a prefix-match (method, prefix*) handler.
// Used by Registry endpoints under /resource/ and /health/nodes/
// where the path tail is a parameter. Longer prefixes win over
// shorter ones.
func (s *Server) HandlePrefix(prefix, method string, fn HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pr := prefixRoute{method: method, prefix: prefix, fn: fn}
	// Insert in length-descending order so dispatch picks the
	// longest match first.
	idx := 0
	for idx < len(s.prefixes) && len(s.prefixes[idx].prefix) >= len(prefix) {
		idx++
	}
	s.prefixes = append(s.prefixes, prefixRoute{})
	copy(s.prefixes[idx+1:], s.prefixes[idx:])
	s.prefixes[idx] = pr
}

// MuxHandler returns a stdhttp.Handler that runs the route-table
// dispatcher. Used by callers (e.g. the Registry) that compose their
// own net/http.Server (typically because they need a sibling raw
// handler for WebSocket upgrades). The returned Handler is safe for
// concurrent use.
func (s *Server) MuxHandler() stdhttp.Handler {
	return stdhttp.HandlerFunc(s.dispatch)
}

// HandleRaw registers a plain http.Handler at an exact path, bypassing
// the (status, body, error) route table.
//
// Needed for WebSocket upgrades and nothing else. The normal handler
// signature returns a value the server then serialises, which means
// the ResponseWriter is written and closed by the framework -- but an
// upgrade has to HIJACK that connection and keep it, so a handler that
// only returns a body can never perform one.
func (s *Server) HandleRaw(path string, h stdhttp.Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.raw == nil {
		s.raw = map[string]stdhttp.Handler{}
	}
	if _, dup := s.raw[path]; dup {
		panic("nmos/http: duplicate raw route " + path)
	}
	s.raw[path] = h
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

	// CORS preflight: any OPTIONS request gets a 200 with the CORS
	// allow-headers set. The AMWA NMOS Testing tool's auto_node_10
	// expects 200 (not 204) on OPTIONS — we return an empty JSON body
	// to keep the Content-Type consistent with every other response.
	if r.Method == stdhttp.MethodOptions {
		allowed := s.methodsForPath(r.URL.Path)
		setCORSHeaders(w, allowed)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte("{}"))
		return
	}

	// Raw handlers first, and before the CORS/JSON machinery has
	// touched the ResponseWriter -- a hijack must happen on a
	// connection nothing has written to.
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
		// A trailing slash is not a different resource.
		//
		// The NMOS specs write collection paths with a trailing slash
		// and clients send both forms freely — the AMWA testing tool
		// asks for `single/senders/{id}/staged` while the spec text
		// writes `.../staged/`. Answering 404 for the other form is
		// technically defensible and practically useless: it failed 20
		// IS-05 tests that were exercising handlers which existed and
		// worked.
		//
		// Tried only after the exact match, so a route registered for
		// one specific form still wins.
		fn, ok = s.routes[routeKey{method: r.Method, path: altSlashForm(r.URL.Path)}]
	}
	if !ok {
		// Try a prefix route — longest match first.
		for _, pr := range s.prefixes {
			if pr.method == r.Method && strings.HasPrefix(r.URL.Path, pr.prefix) {
				fn = pr.fn
				ok = true
				break
			}
		}
	}
	if !ok {
		// Choose 404 vs 405 based on whether ANY route matches the path.
		methodNotAllowed := false
		// Both spellings of the path, for the same reason the lookup
		// above tries both: a trailing slash is not a different
		// resource. `GET /bulk/senders` against a POST-only
		// `/bulk/senders/` is a wrong METHOD, not a missing route, and
		// answering 404 tells a controller the endpoint does not exist
		// (IS-05-01 test_34/test_35).
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

// RawBody lets a handler return a non-JSON response (e.g. SDP text on
// IS-04 senders/{id}/transportfile). Set Body + ContentType; the
// server emits them verbatim with CORS headers attached.
type RawBody struct {
	ContentType string
	Body        []byte
}

// WithHeaders lets a handler attach extra response headers (e.g. the
// `Location` header IS-04 §6.1.1 mandates on Registration POST/PUT,
// or `X-Paging-*` on Query API list responses) without changing the
// HandlerFunc signature. Body is JSON-encoded as usual; Headers are
// applied verbatim before WriteHeader. Body may itself be a *RawBody
// to combine non-JSON content with custom headers.
type WithHeaders struct {
	Body    any
	Headers map[string]string
}

// writeJSON serialises body as JSON with the spec-mandated header set.
// As a special case, *RawBody emits a non-JSON response — used for
// IS-04 transportfile (SDP) routes that the spec requires to be
// served as text/plain or application/sdp, NOT JSON. *WithHeaders
// applies extra headers before delegating to the inner body.
func writeJSON(w stdhttp.ResponseWriter, status int, body any) {
	if wh, ok := body.(*WithHeaders); ok {
		for k, v := range wh.Headers {
			w.Header().Set(k, v)
		}
		writeJSON(w, status, wh.Body)
		return
	}
	if rb, ok := body.(*RawBody); ok {
		ct := rb.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ct)
		setCORSHeaders(w, "")
		w.WriteHeader(status)
		_, _ = w.Write(rb.Body)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	setCORSHeaders(w, "")
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

// setCORSHeaders adds the CORS header set every NMOS API response
// emits per IS-04 §4.5 (and the AMWA NMOS Testing tool requires).
// allowMethods, when non-empty, also sets Access-Control-Allow-Methods
// + Allow — used on OPTIONS preflight responses.
func setCORSHeaders(w stdhttp.ResponseWriter, allowMethods string) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	h.Set("Access-Control-Max-Age", "3600")
	if allowMethods != "" {
		h.Set("Access-Control-Allow-Methods", allowMethods)
		h.Set("Allow", allowMethods)
	}
}

// methodsForPath returns the comma-separated set of HTTP methods the
// route table accepts at path. Always includes OPTIONS. Used for the
// CORS preflight response so the peer learns which verbs are real.
func (s *Server) methodsForPath(path string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[string]struct{}{stdhttp.MethodOptions: {}}
	// Both slash forms, for the same reason dispatch accepts both: a
	// preflight for `.../bulk/senders` must advertise the POST that is
	// registered at `.../bulk/senders/`, or the browser refuses the
	// request that would have worked.
	alt := path
	if strings.HasSuffix(alt, "/") {
		alt = strings.TrimSuffix(alt, "/")
	} else {
		alt += "/"
	}
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
	// Stable order: OPTIONS, GET, HEAD, POST, PUT, PATCH, DELETE.
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
