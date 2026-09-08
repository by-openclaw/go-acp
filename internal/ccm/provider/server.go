package ccm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"dhs/internal/metrics"
	"dhs/internal/plugin"
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
	// One prefix route covers the whole tree plus the spec; the handler
	// separates them so route precedence never has to be reasoned about.
	s.http.HandlePrefix(s.prefix, http.MethodGet, s.handleGet)
	return s
}

// Metrics returns the provider's counter set. Always non-nil.
func (s *Server) Metrics() *metrics.Connector { return s.met }

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
			return http.StatusNotFound, s.notFound("no OpenAPI document is served"), nil
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
		return http.StatusNotFound, s.notFound(fmt.Sprintf("no resource at %q", sub)), nil
	}
}

// notFound builds the CCM error envelope for a missing path.
func (s *Server) notFound(msg string) GenericApiMessage {
	return GenericApiMessage{Code: http.StatusNotFound, Message: msg}
}
