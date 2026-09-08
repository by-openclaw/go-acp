package ccm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"dhs/internal/metrics"
	"dhs/internal/plugin"
	"dhs/internal/transport"
	thttp "dhs/internal/transport/http"
)

// DefaultPrefix is the CCM REST base a controller (Cerebrum) walks. The device
// serves its model under /api, and the OpenAPI documents paths under /v1, so
// the full base is /api/v1 — the same base the CCM consumer dials.
//
// COMPLIANCE BOUNDARY. Everything under DefaultPrefix is the CCM protocol as
// EVS defines it, served 100% to the spec: the self-describing tree, the
// OpenAPI document at its well-known path, the §12 error envelope, and (in
// later slices) §11.2 PATCH semantics. Nothing dhs invents is ever mounted
// here. Our own additions — the Swagger landing, the rendered README, a
// capabilities view — go under ExtensionPrefix, so a controller that speaks
// CCM sees exactly a CCM device and our contract stays separable. This is the
// same discipline EVS applies to itself, keeping /x-evs/ beside /x-nmos/.
const DefaultPrefix = "/api/v1"

// ExtensionPrefix is where dhs-specific endpoints live. It is deliberately
// outside the CCM namespace: adding to it can never change what a CCM
// controller observes under DefaultPrefix. Extensions are registered through
// HandleExtension, which is the only way to mount a route here, so the
// boundary is enforced by construction rather than by review.
const ExtensionPrefix = "/x-dhs"

// specPath is the well-known location of the OpenAPI document, relative to the
// prefix. The consumer's FetchSpec GETs exactly this — verified live against
// a BRIDGE (issue #984), which is why it is /api/v1/docs/api.yml rather than
// the /api/docs/openapi.yml the 0v1 paper proposed.
const specPath = "docs/api.yml"

// GenericApiMessage is the CCM §12 error envelope returned for a bad request —
// a plain {code,message}, not RFC 7807 (that shape is reserved for the auth
// endpoints).
type GenericApiMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server replays a captured CCM device model over HTTP so a controller can
// drive dhs as if it were a real CCM (EVS BRIDGE / Neuron) device. It is the
// read + describe surface: the self-describing DM tree and the OpenAPI
// document. Config writes (PATCH) and the change-stream WebSocket are separate
// units; matrix routing is deliberately out of scope (owner decision,
// docs/spec-review-0v1.md) — routing stays with Cerebrum and the router
// protocols.
type Server struct {
	tree   *Tree
	spec   []byte // OAS 3.1 api.yml served at {prefix}/docs/api.yml; nil = 404
	prefix string

	logger *slog.Logger
	// met counts what this provider serves, exposed via Metrics() so
	// --metrics-addr scrapes it. Aggregate rather than command-keyed: CCM is
	// REST, there is no command byte, only a path. Always non-nil.
	met  *metrics.Connector
	http *thttp.Server
}

// NewServer builds a CCM provider from an already-loaded device model and an
// optional OpenAPI document. deps supplies the logger and the metrics
// connector (DI); tree is the model to replay (see LoadTree).
func NewServer(deps plugin.Deps, tree *Tree, spec []byte) *Server {
	deps = deps.WithDefaults()
	s := &Server{
		tree:   tree,
		spec:   spec,
		prefix: DefaultPrefix,
		logger: deps.Logger.With(slog.String("plugin", "ccm-provider")),
		met:    deps.Metrics,
		http:   thttp.NewServer(deps.Logger),
	}
	// One prefix route per method covers the whole tree plus the spec; the
	// handlers separate them so route precedence never has to be reasoned
	// about. PUT and PATCH are the §11 mutations (the shipped OpenAPI lists
	// PUT; §11.2 makes PATCH mandatory — a compliant device serves both).
	s.http.HandlePrefix(s.prefix, http.MethodGet, s.handleGet)
	s.http.HandlePrefix(s.prefix, http.MethodPut, s.handleWrite)
	s.http.HandlePrefix(s.prefix, http.MethodPatch, s.handleWrite)
	return s
}

// maxWriteBody bounds a PUT/PATCH body. CCM resources are small objects; a
// megabyte is generous and stops a runaway client from exhausting memory.
const maxWriteBody = 1 << 20

// Metrics returns the provider's counter set. Always non-nil.
func (s *Server) Metrics() *metrics.Connector { return s.met }

// WithTLS makes Serve listen with HTTPS from a TLS posture. A real CCM device
// serves HTTPS on 443, so an emulation that must be indistinguishable to a
// controller enables it here. The posture — certificate, the shared TLS 1.2
// floor, optional client CAs — is decided by transport.TLSOptions, never in
// this package: a protocol package holds no crypto/tls code (architecture
// gate), it only hands the built config to its HTTP server. A posture with
// Enable false leaves plain HTTP for lab captures.
func (s *Server) WithTLS(opts transport.TLSOptions) error {
	cfg, err := opts.Server()
	if err != nil {
		return fmt.Errorf("ccm provider: tls: %w", err)
	}
	s.http.TLS = cfg
	return nil
}

// Handler exposes the routed HTTP handler so a test can drive the provider
// through httptest without binding a socket.
func (s *Server) Handler() http.Handler { return s.http.MuxHandler() }

// HandleExtension mounts a dhs-specific route under ExtensionPrefix. path is
// relative ("landing", "readme", "capabilities"); the prefix is prepended
// here, so an extension cannot be registered under the CCM namespace even by
// mistake — the compliance boundary is enforced by this being the only
// extension entry point. Re-registering the same (method, path) replaces the
// previous handler, matching transport/http.
func (s *Server) HandleExtension(method, path string, fn thttp.HandlerFunc) {
	s.http.Handle(method, ExtensionPrefix+"/"+strings.Trim(path, "/"), fn)
}

// Serve binds addr and serves until ctx is cancelled. Pass a *tls.Config on
// the embedded server (via WithTLS) before calling to serve HTTPS.
func (s *Server) Serve(ctx context.Context, addr string) error {
	s.logger.Info("ccm provider listening",
		slog.String("addr", addr),
		slog.String("prefix", s.prefix),
		slog.Int("resources", s.tree.Len()),
		slog.Int("nodes", s.tree.Nodes()),
		slog.Bool("spec", s.spec != nil),
	)
	return s.http.Serve(ctx, addr)
}

// handleGet answers every GET under the prefix: the OpenAPI document at the
// well-known spec path, otherwise the DM tree (a node lists its children, a
// resource returns its body, anything else is a 404). Each response records
// its size and the handler's own elapsed time so the connector reports send
// latency (footprint) like every other connector.
func (s *Server) handleGet(_ context.Context, r *http.Request) (int, any, error) {
	start := time.Now()
	s.met.ObserveRx(0) // a GET carries no body; count the request itself

	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, s.prefix), "/")

	if sub == specPath {
		if s.spec == nil {
			return http.StatusNotFound, apiError(http.StatusNotFound, "no OpenAPI document is served"), nil
		}
		s.met.ObserveTx(len(s.spec), time.Since(start))
		return http.StatusOK, &thttp.RawBody{ContentType: "application/yaml", Body: s.spec}, nil
	}

	body, kind := s.tree.Get(sub)
	switch kind {
	case KindResource, KindNode:
		s.met.ObserveTx(len(body), time.Since(start))
		return http.StatusOK, &thttp.RawBody{ContentType: "application/json", Body: body}, nil
	default:
		return http.StatusNotFound, apiError(http.StatusNotFound, fmt.Sprintf("no resource at %q", sub)), nil
	}
}

// handleWrite answers PUT and PATCH under the prefix per §11: the body is a
// JSON object of one or more mutable fields (maps, not arrays — §11.2), the
// change is applied to the stored resource, and the response is an EMPTY 202
// (§11.1) — accepted as well-formed and applied; the caller confirms by
// reading the resource or its /status back (§11.3 split request/status).
// Read-only endpoints (the /status views and the OpenAPI document) expose
// GET only (§11.1) and refuse writes with 405. Every refusal carries the §12
// {code,message} envelope.
func (s *Server) handleWrite(_ context.Context, r *http.Request) (int, any, error) {
	start := time.Now()
	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, s.prefix), "/")

	if sub == specPath || strings.HasSuffix(sub, "/status") {
		return http.StatusMethodNotAllowed, apiError(http.StatusMethodNotAllowed, "read-only endpoint: GET only"), nil
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxWriteBody))
	if err != nil {
		return http.StatusBadRequest, apiError(http.StatusBadRequest, "read body: "+err.Error()), nil
	}
	s.met.ObserveRx(len(raw))

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return http.StatusBadRequest, apiError(http.StatusBadRequest,
			"body must be a JSON object of mutable fields (maps, not arrays)"), nil
	}
	if len(fields) == 0 {
		return http.StatusBadRequest, apiError(http.StatusBadRequest,
			"body needs one or more mutable fields (minProperties: 1)"), nil
	}

	switch err := s.tree.Update(sub, fields, r.Method == http.MethodPut); {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, apiError(http.StatusNotFound, fmt.Sprintf("no resource at %q", sub)), nil
	case errors.Is(err, ErrNotMutable):
		return http.StatusMethodNotAllowed, apiError(http.StatusMethodNotAllowed, "not a mutable resource"), nil
	}

	s.met.ObserveTx(0, time.Since(start))
	// An explicitly empty RawBody: the default JSON path would write "null",
	// and §11.1 says the response is empty.
	return http.StatusAccepted, &thttp.RawBody{ContentType: "application/json"}, nil
}

// apiError builds the §12 GenericApiMessage envelope for a refused request.
func apiError(code int, msg string) GenericApiMessage {
	return GenericApiMessage{Code: code, Message: msg}
}
