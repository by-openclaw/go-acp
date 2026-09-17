package http

// The NMOS face of an HTTP server.
//
// The machinery — route table, TLS listener, panic barrier, CORS, the
// trailing-slash and 404-vs-405 rules — moved to internal/transport/http,
// where it is generic and where the package now has a server to match its
// client. What stayed here is what is actually NMOS:
//
//   - the IS-04 §4.4 error envelope every API answers with;
//   - the BCP-003-02 token gate, wired in as a transport Gate;
//   - the CORS header set IS-04 §4.5 requires.
//
// Server embeds the transport one, so every existing call site keeps working:
// Handle, HandlePrefix, HandleRaw, MuxHandler and Serve are promoted, and so
// are the Logger and TLS fields.

import (
	"log/slog"
	stdhttp "net/http"

	thttp "dhs/internal/transport/http"
)

// Re-exported so callers keep one import. These are the transport types;
// aliasing rather than wrapping means a *RawBody built here is the same type
// the server checks for.
type (
	// HandlerFunc is the per-route signature.
	HandlerFunc = thttp.HandlerFunc
	// RawBody returns a non-JSON response — IS-04 transportfile (SDP) is
	// served as text/plain, not JSON.
	RawBody = thttp.RawBody
	// WithHeaders attaches extra response headers, e.g. the Location header
	// IS-04 §6.1.1 mandates on Registration POST/PUT, or X-Paging-* on Query
	// API list responses.
	WithHeaders = thttp.WithHeaders
)

// ErrorBody mirrors the IS-04 §4.4 / IS-09 RAML error envelope so peers see a
// uniform shape on 4xx / 5xx.
type ErrorBody struct {
	Code  int    `json:"code"`
	Error string `json:"error"`
	Debug string `json:"debug,omitempty"`
}

// nmosCORS is the header set every NMOS API response carries per IS-04 §4.5
// (and which the AMWA NMOS Testing tool requires).
var nmosCORS = thttp.CORS{
	AllowOrigin:  "*",
	AllowHeaders: "Content-Type, Authorization",
	MaxAge:       "3600",
}

// Server is a transport HTTP server pre-wired with the NMOS error shape, the
// NMOS CORS policy, and the BCP-003-02 gate.
type Server struct {
	*thttp.Server

	// Auth, when non-nil, gates every request (routes AND raw WebSocket
	// handlers) through BCP-003-02 token validation. It may be set at any
	// point before Serve — the gate reads it per request, so wiring order
	// does not matter.
	Auth *AuthGate
}

// NewServer constructs an empty Server.
func NewServer(logger *slog.Logger) *Server {
	s := &Server{Server: thttp.NewServer(logger)}
	s.CORS = nmosCORS
	s.Errors = func(status int, errStr, debug string) any {
		return ErrorBody{Code: status, Error: errStr, Debug: debug}
	}
	// Read s.Auth per request rather than capturing it: callers set the gate
	// after NewServer, and a snapshot taken here would be nil forever.
	s.Gate = thttp.GateFunc(func(r *stdhttp.Request) (int, map[string]string, any, *stdhttp.Request, bool) {
		if s.Auth == nil {
			return 0, nil, nil, nil, true
		}
		status, hdrs, body, clientID, ok := s.Auth.Check(r)
		if !ok {
			return status, hdrs, body, nil, false
		}
		if clientID == "" {
			return 0, nil, nil, nil, true
		}
		// BCP-003-02: hand the authenticated client identity to the handlers
		// so write paths can enforce per-client resource ownership
		// (IS-04-02 test_33 / test_33_1).
		return 0, nil, nil, r.WithContext(WithClientID(r.Context(), clientID)), true
	})
	return s
}
