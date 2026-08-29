package http

// AuthGate — the BCP-003-02 / IS-10 resource-server face of every
// NMOS API this process serves.
//
// The gate sits in dispatch() ahead of both the route table and the
// raw (WebSocket) handlers, so one policy covers HTTP and WS alike.
// Per Behaviour - Resource Servers.md:
//
//   - `/` and `/x-nmos` stay readable with NO credentials;
//   - a missing/expired/invalid token is 401 + WWW-Authenticate:
//     Bearer (RFC 6750 §3);
//   - a valid token whose x-nmos-* claims do not permit the path/verb
//     is 403;
//   - WebSocket handshakes may carry the token as an `access_token`
//     query parameter instead of the Authorization header;
//   - every decision is auditable (slog, token details minus
//     anything secret — the claims are not secret, the signature is
//     not logged).
//
// OPTIONS is exempt: CORS preflights are sent by browsers WITHOUT
// credentials by design, and a 401 there blocks the very request that
// would have carried the token.

import (
	"log/slog"
	stdhttp "net/http"
	"strings"
	"time"

	"dhs/internal/amwa/codec/is10"
)

// KeyProvider hands the gate the Authorization Server's current
// public keys. Implementations cache per Resource Server rules
// (hourly refresh, keep stale keys when the server is unreachable).
type KeyProvider interface {
	Keys() []is10.JWK
}

// StaticKeys is a KeyProvider for fixed key sets (tests, pinned
// deployments).
type StaticKeys []is10.JWK

// Keys returns the fixed set.
func (s StaticKeys) Keys() []is10.JWK { return s }

// AuthGate validates Bearer tokens for one server.
type AuthGate struct {
	// Keys supplies the verification keys. Required.
	Keys KeyProvider
	// Hosts are this server's identities for aud matching — advertise
	// host plus OS hostname (+ .local), because issuers scope tokens
	// with DNS wildcards an IP literal can never satisfy. At least one
	// entry required.
	Hosts []string
	// Leeway absorbs clock skew on exp/iat/nbf. Zero means 30s.
	Leeway time.Duration
	// Logger receives the audit trail. Nil uses slog.Default().
	Logger *slog.Logger
}

// bearerToken extracts the access token from the Authorization
// header, falling back to the RFC 6750 access_token query parameter
// (the WebSocket-handshake affordance).
func bearerToken(r *stdhttp.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) >= 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return r.URL.Query().Get("access_token")
}

// Check evaluates one request. ok=true means proceed; otherwise the
// caller writes body with the given status and WWW-Authenticate
// header value.
func (g *AuthGate) Check(r *stdhttp.Request) (status int, authenticate string, body ErrorBody, ok bool) {
	log := g.Logger
	if log == nil {
		log = slog.Default()
	}
	leeway := g.Leeway
	if leeway == 0 {
		leeway = 30 * time.Second
	}

	// CORS preflight: credential-less by browser design.
	if r.Method == stdhttp.MethodOptions {
		return 0, "", ErrorBody{}, true
	}

	// The two always-readable base paths need no token at all.
	p := strings.TrimSuffix(r.URL.Path, "/")
	if (p == "" || p == "/x-nmos") && (r.Method == stdhttp.MethodGet || r.Method == stdhttp.MethodHead) {
		return 0, "", ErrorBody{}, true
	}

	tok := bearerToken(r)
	if tok == "" {
		log.Info("auth: rejected", "reason", "no token", "method", r.Method, "path", r.URL.Path)
		return stdhttp.StatusUnauthorized, `Bearer`,
			ErrorBody{Code: 401, Error: "Unauthorized", Debug: "no access token presented"}, false
	}

	var keys []is10.JWK
	if g.Keys != nil {
		keys = g.Keys.Keys()
	}
	claims, err := is10.VerifyWithKeys(tok, keys)
	if err != nil {
		log.Info("auth: rejected", "reason", "signature", "method", r.Method, "path", r.URL.Path, "err", err)
		return stdhttp.StatusUnauthorized, `Bearer error="invalid_token"`,
			ErrorBody{Code: 401, Error: "Unauthorized", Debug: err.Error()}, false
	}
	if err := is10.ValidateClaims(claims, g.Hosts, time.Now(), leeway); err != nil {
		log.Info("auth: rejected", "reason", "claims", "method", r.Method, "path", r.URL.Path,
			"iss", claims.Iss, "sub", claims.Sub, "client", claims.ClientID, "err", err)
		return stdhttp.StatusUnauthorized, `Bearer error="invalid_token"`,
			ErrorBody{Code: 401, Error: "Unauthorized", Debug: err.Error()}, false
	}
	if err := is10.Authorize(claims, r.Method, r.URL.Path); err != nil {
		log.Info("auth: forbidden", "method", r.Method, "path", r.URL.Path,
			"iss", claims.Iss, "sub", claims.Sub, "client", claims.ClientID, "err", err)
		return stdhttp.StatusForbidden, `Bearer error="insufficient_scope"`,
			ErrorBody{Code: 403, Error: "Forbidden", Debug: err.Error()}, false
	}
	log.Debug("auth: authorized", "method", r.Method, "path", r.URL.Path,
		"iss", claims.Iss, "sub", claims.Sub, "client", claims.ClientID)
	return 0, "", ErrorBody{}, true
}
