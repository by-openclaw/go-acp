package http

// AuthGate — the BCP-003-02 / IS-10 resource-server face of every
// NMOS API this process serves.
//
// The gate sits in dispatch() ahead of both the route table and the
// raw (WebSocket) handlers, so one policy covers HTTP and WS alike.
// Verdicts, as the spec and the AMWA suite pin them:
//
//   - `/` and `/x-nmos` stay readable with NO credentials;
//   - missing token           → 401, WWW-Authenticate: Bearer realm=…
//   - malformed/expired token → 401, Bearer error=invalid_token
//   - unknown signing key     → 503 + Retry-After while the keys for
//     the token's iss are fetched (RFC 8414 §3), 401 when no fetcher
//     is wired;
//   - valid token, wrong aud  → 403, Bearer error=invalid_token
//   - valid token, path/verb not granted → 403, error=insufficient_scope
//   - WebSocket handshakes may carry the token as an `access_token`
//     query parameter;
//   - OPTIONS is exempt (CORS preflights are credential-less by
//     browser design).
//
// The error auth-params are deliberately UNQUOTED (RFC 6750 allows
// token form) — the suite tokenises them without unquoting.

import (
	"context"
	"log/slog"
	stdhttp "net/http"
	"strings"
	"sync"
	"time"

	"dhs/internal/amwa/codec/is10"
)

// KeyProvider hands the gate the Authorization Server's current
// public keys. Implementations cache per Resource Server rules
// (hourly refresh, keep stale keys when the server is unreachable).
type KeyProvider interface {
	Keys() []is10.JWK
}

// IssuerFetcher is optionally implemented by a KeyProvider that can
// pull keys for a so-far-unknown issuer (RFC 8414 metadata at the
// token's iss claim). The gate uses it for the 503 retry flow.
type IssuerFetcher interface {
	FetchIssuer(ctx context.Context, issuer string) error
}

// StaticKeys is a KeyProvider for fixed key sets (tests, pinned
// deployments).
type StaticKeys []is10.JWK

// Keys returns the fixed set.
func (s StaticKeys) Keys() []is10.JWK { return s }

// retryAfterSeconds is the Retry-After we advertise while fetching a
// missing public key.
const retryAfterSeconds = "3"

// AuthGate validates Bearer tokens for one server.
type AuthGate struct {
	// Keys supplies the verification keys. Required. When it also
	// implements IssuerFetcher, unknown-key tokens get the 503 +
	// fetch-by-iss flow instead of a flat 401.
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

	// issuer-fetch dedupe: one in-flight fetch per issuer at a time.
	mu       sync.Mutex
	fetching map[string]time.Time
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
// caller writes body with the given status after applying headers.
func (g *AuthGate) Check(r *stdhttp.Request) (status int, headers map[string]string, body ErrorBody, ok bool) {
	log := g.Logger
	if log == nil {
		log = slog.Default()
	}
	leeway := g.Leeway
	if leeway == 0 {
		leeway = 30 * time.Second
	}
	deny := func(st int, authenticate, debug string) (int, map[string]string, ErrorBody, bool) {
		reason := "Unauthorized"
		if st == stdhttp.StatusForbidden {
			reason = "Forbidden"
		}
		if st == stdhttp.StatusServiceUnavailable {
			reason = "Service Unavailable"
		}
		h := map[string]string{"WWW-Authenticate": authenticate}
		if st == stdhttp.StatusServiceUnavailable {
			h["Retry-After"] = retryAfterSeconds
		}
		return st, h, ErrorBody{Code: st, Error: reason, Debug: debug}, false
	}

	// CORS preflight: credential-less by browser design.
	if r.Method == stdhttp.MethodOptions {
		return 0, nil, ErrorBody{}, true
	}

	// The two always-readable base paths need no token at all.
	p := strings.TrimSuffix(r.URL.Path, "/")
	if (p == "" || p == "/x-nmos") && (r.Method == stdhttp.MethodGet || r.Method == stdhttp.MethodHead) {
		return 0, nil, ErrorBody{}, true
	}

	tok := bearerToken(r)
	if tok == "" {
		log.Info("auth: rejected", "reason", "no token", "method", r.Method, "path", r.URL.Path)
		return deny(stdhttp.StatusUnauthorized, `Bearer realm=nmos`, "no access token presented")
	}

	var keys []is10.JWK
	if g.Keys != nil {
		keys = g.Keys.Keys()
	}
	claims, err := is10.VerifyWithKeys(tok, keys)
	if err != nil {
		// A structurally valid RS512 token that no held key verifies
		// may belong to an issuer we have not met — the spec's answer
		// is to fetch that issuer's keys (via its RFC 8414 metadata)
		// and answer 503 + Retry-After meanwhile, not to reject a
		// legitimate token forever.
		if _, c2, _, _, perr := is10.ParseToken(tok); perr == nil && c2.Iss != "" {
			if fetcher, okF := g.Keys.(IssuerFetcher); okF && g.startIssuerFetch(c2.Iss) {
				go func(iss string) {
					fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if ferr := fetcher.FetchIssuer(fctx, iss); ferr != nil {
						log.Warn("auth: issuer key fetch failed", "iss", iss, "err", ferr)
					}
				}(c2.Iss)
				log.Info("auth: unknown key, fetching issuer keys", "iss", c2.Iss,
					"method", r.Method, "path", r.URL.Path)
				return deny(stdhttp.StatusServiceUnavailable, `Bearer error=invalid_token`,
					"fetching public keys for token issuer; retry shortly")
			}
		}
		log.Info("auth: rejected", "reason", "signature", "method", r.Method, "path", r.URL.Path, "err", err)
		return deny(stdhttp.StatusUnauthorized, `Bearer error=invalid_token`, err.Error())
	}
	if err := is10.ValidateClaimsExceptAud(claims, time.Now(), leeway); err != nil {
		log.Info("auth: rejected", "reason", "claims", "method", r.Method, "path", r.URL.Path,
			"iss", claims.Iss, "sub", claims.Sub, "client", claims.ClientID, "err", err)
		return deny(stdhttp.StatusUnauthorized, `Bearer error=invalid_token`, err.Error())
	}
	// Wrong audience: the token is genuine but addressed to someone
	// else — a FORBIDDEN use of this server (the suite pins 403 here).
	if !is10.AudienceMatchesAny(claims.Aud, g.Hosts) {
		log.Info("auth: forbidden", "reason", "audience", "method", r.Method, "path", r.URL.Path,
			"iss", claims.Iss, "aud", []string(claims.Aud))
		return deny(stdhttp.StatusForbidden, `Bearer error=insufficient_scope`,
			"token audience does not name this server")
	}
	if err := is10.Authorize(claims, r.Method, r.URL.Path); err != nil {
		log.Info("auth: forbidden", "method", r.Method, "path", r.URL.Path,
			"iss", claims.Iss, "sub", claims.Sub, "client", claims.ClientID, "err", err)
		return deny(stdhttp.StatusForbidden, `Bearer error=insufficient_scope`, err.Error())
	}
	log.Debug("auth: authorized", "method", r.Method, "path", r.URL.Path,
		"iss", claims.Iss, "sub", claims.Sub, "client", claims.ClientID)
	return 0, nil, ErrorBody{}, true
}

// startIssuerFetch dedupes CONCURRENT fetches per issuer only — the
// window is deliberately short. The spec's rule is "do not re-fetch
// unless none of the held keys validate the token", and this path is
// only reached exactly then; an issuer that rotates its keys (the
// AMWA suite restarts its secondary AS with a fresh key per test)
// must be re-fetchable moments later or every later token from it is
// a permanent 401.
func (g *AuthGate) startIssuerFetch(iss string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.fetching == nil {
		g.fetching = map[string]time.Time{}
	}
	if t, ok := g.fetching[iss]; ok && time.Since(t) < 2*time.Second {
		return false
	}
	g.fetching[iss] = time.Now()
	return true
}
