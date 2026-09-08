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
// the device's own OpenAPI document declares it, served 100% to that
// document: the self-describing tree, the document at its well-known path,
// the §12 error envelope, and writes only where and how the document says.
// Nothing dhs invents is ever mounted here. Our own additions — the Swagger landing, the rendered README, a
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
// read + describe + write surface: the self-describing DM tree, the OpenAPI
// document, and the writes that document declares (resources and matrix
// levels alike — the matrix is part of the document). The change-stream
// WebSocket is a separate unit.
type Server struct {
	tree *Tree
	spec []byte // OAS 3.1 api.yml served at {prefix}/docs/api.yml; nil = 404
	// contract is the write contract read from spec: which paths accept
	// which operations and with what status. Nil spec = nothing declared.
	contract *contract
	prefix   string

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
		tree:     tree,
		spec:     spec,
		contract: newContract(spec),
		prefix:   DefaultPrefix,
		logger:   deps.Logger.With(slog.String("plugin", "ccm-provider")),
		met:      deps.Metrics,
		http:     thttp.NewServer(deps.Logger),
	}
	// One prefix route per method covers the whole tree plus the spec; the
	// handlers separate them so route precedence never has to be reasoned
	// about. Both write verbs route to one handler that consults the
	// document: the shipped api.yml declares PUT only, so PATCH is 405
	// everywhere until a device document declares it.
	s.http.HandlePrefix(s.prefix, http.MethodGet, s.handleGet)
	s.http.HandlePrefix(s.prefix, http.MethodPut, s.handleWrite)
	s.http.HandlePrefix(s.prefix, http.MethodPatch, s.handleWrite)
	return s
}

// maxWriteBody bounds a write body. CCM resources are small objects; a
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

// handleWrite answers a write under the prefix exactly as the device's own
// OpenAPI document declares it. The document is the contract: a write is
// accepted only on a path where it declares that operation (the real
// api.yml declares PUT on 35 resources and on the matrix main/backup
// levels, and no PATCH), the body must be a JSON object (every declared
// request schema is one; a MatrixState is an object of strings), and the
// response is the status the document promises with the resource as it now
// reads — 200 + body on this firmware. Anything the document does not
// declare is 405, an unknown path is 404, a malformed body is 400, all with
// the §12 {code,message} envelope. Nothing here is inferred beyond the
// document; without a document the replay is GET-only.
func (s *Server) handleWrite(_ context.Context, r *http.Request) (int, any, error) {
	start := time.Now()
	sub := strings.Trim(strings.TrimPrefix(r.URL.Path, s.prefix), "/")

	op, declared := s.contract.lookup(r.Method, specRoot+"/"+sub)
	if !declared {
		// The document itself is served, not stored in the tree; a write to
		// it is undeclared like any other, not an unknown path.
		if _, kind := s.tree.Get(sub); kind == KindAbsent && sub != specPath {
			return http.StatusNotFound, apiError(http.StatusNotFound, fmt.Sprintf("no resource at %q", sub)), nil
		}
		return http.StatusMethodNotAllowed, apiError(http.StatusMethodNotAllowed,
			fmt.Sprintf("%s is not declared for this path by the API document", r.Method)), nil
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxWriteBody))
	if err != nil {
		return http.StatusBadRequest, apiError(http.StatusBadRequest, "read body: "+err.Error()), nil
	}
	s.met.ObserveRx(len(raw))

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return http.StatusBadRequest, apiError(http.StatusBadRequest,
			fmt.Sprintf("body must be a JSON object (%s)", op.Body)), nil
	}
	if op.Body == matrixStateSchema {
		if err := checkMatrixState(fields); err != nil {
			return http.StatusBadRequest, apiError(http.StatusBadRequest, err.Error()), nil
		}
	}

	switch err := s.tree.Update(sub, fields, r.Method == http.MethodPut); {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, apiError(http.StatusNotFound, fmt.Sprintf("no resource at %q", sub)), nil
	case errors.Is(err, ErrNotMutable):
		return http.StatusMethodNotAllowed, apiError(http.StatusMethodNotAllowed, "not a mutable resource"), nil
	}

	status := op.Success
	if status == 0 {
		status = http.StatusOK
	}
	body, _ := s.tree.Get(sub)
	s.met.ObserveTx(len(body), time.Since(start))
	return status, &thttp.RawBody{ContentType: "application/json", Body: body}, nil
}

// specRoot is the path prefix the OpenAPI document uses for every operation
// ("/v1/self"); the served prefix is DefaultPrefix ("/api/v1/self").
const specRoot = "/v1"

// matrixStateSchema is the request/response schema of a matrix level in the
// device's api.yml: `type: object, additionalProperties: {type: string}` —
// a flat destination -> source map of string ids.
const matrixStateSchema = "MatrixState"

// checkMatrixState enforces the MatrixState schema on a level write: every
// value is a string. The keys are unconstrained by the document
// (additionalProperties), so they are not checked here.
func checkMatrixState(fields map[string]json.RawMessage) error {
	for dst, raw := range fields {
		var src string
		if err := json.Unmarshal(raw, &src); err != nil {
			return fmt.Errorf("MatrixState: value of %q must be a string, got %s", dst, strings.TrimSpace(string(raw)))
		}
	}
	return nil
}

// apiError builds the §12 GenericApiMessage envelope for a refused request.
func apiError(code int, msg string) GenericApiMessage {
	return GenericApiMessage{Code: code, Message: msg}
}
