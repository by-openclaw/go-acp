package auth

// The NMOS half of the token concern.
//
// internal/auth answers "is this a valid JWT, signed by a key we trust, and
// is the clock right?" — questions with the same answer for every protocol.
// This file answers what those claims MEAN to NMOS: which audience names this
// server, which API a scope grants, and whether an x-nmos-* permission covers
// one request path.
//
// It lives here rather than in codec/is10 for a reason that is not tidiness.
// codec/ is stdlib-only and never imports dhs/* (ADR-0006), so a codec cannot
// reach the shared JWT package; keeping the NMOS rules there would have meant
// keeping a second copy of RFC 7519 there too. Session layer can import both,
// so this is where the two halves meet.

import (
	"encoding/json"
	"fmt"
	stdpath "path"
	"strings"
	"time"

	jwt "dhs/internal/auth"
)

// AlgRS512 is the only JWS algorithm IS-10 permits. Every verification in
// this package passes it explicitly — the shared parser refuses to run
// without a permitted list, which is what makes the pin unskippable.
const AlgRS512 = jwt.AlgRS512

// xNmosPrefix marks the private-claim namespace.
const xNmosPrefix = "x-nmos-"

// Permissions is the value of one x-nmos-* claim: wildcarded URL path
// specifiers per verb class. An omitted key means the permission was not
// granted — the spec minimises token size that way.
type Permissions struct {
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

// Token is a verified IS-10 access token: the registered JWT claims, plus the
// x-nmos-* private claims swept out of the generic remainder.
type Token struct {
	jwt.Claims

	// APIs holds the x-nmos-* claims keyed by API name with the prefix
	// stripped ("registration", "query", …).
	APIs map[string]Permissions
}

// Verify checks a token's signature against the key set and extracts the NMOS
// private claims. Claims are NOT time- or audience-validated here; call
// ValidateClaims (or ValidateClaimsExceptAud) on the result.
func Verify(raw string, keys []jwt.JWK) (Token, error) {
	c, err := jwt.VerifyWithKeys(raw, keys, AlgRS512)
	if err != nil {
		return Token{}, err
	}
	return newToken(c)
}

// Parse decodes without verifying — used only where the caller needs the
// issuer of an unverified token in order to go and fetch its keys.
func Parse(raw string) (Token, error) {
	_, c, _, _, err := jwt.ParseToken(raw, AlgRS512)
	if err != nil {
		return Token{}, err
	}
	return newToken(c)
}

// newToken sweeps the x-nmos-* claims out of the generic remainder.
//
// A malformed permissions object is an ERROR, not a silently-skipped claim: a
// token whose x-nmos-registration claim does not parse would otherwise be
// treated as a token that simply has no registration grant, turning a
// malformed token into a quiet 403 the operator cannot diagnose.
func newToken(c jwt.Claims) (Token, error) {
	t := Token{Claims: c}
	for key, raw := range c.Private {
		if !strings.HasPrefix(key, xNmosPrefix) {
			continue
		}
		var p Permissions
		if err := json.Unmarshal(raw, &p); err != nil {
			return Token{}, fmt.Errorf("is10: claim %s: %w", key, err)
		}
		if t.APIs == nil {
			t.APIs = map[string]Permissions{}
		}
		t.APIs[strings.TrimPrefix(key, xNmosPrefix)] = p
	}
	return t, nil
}

// HasScope reports whether the space-separated scope claim names api.
func (t Token) HasScope(api string) bool {
	for _, s := range strings.Fields(t.Scope) {
		if s == api {
			return true
		}
	}
	return false
}

// ValidateClaims enforces the rules of Behaviour - Access Tokens.md +
// Resource Servers.md against this resource server:
//
//   - iss, sub, aud, exp REQUIRED;
//   - exp in the past / iat or nbf in the future reject (leeway absorbs
//     clock skew both ways);
//   - client_id required unless azp present;
//   - aud must match ONE of this server's identities.
//
// hosts carries every name this server answers to — advertise host, OS
// hostname, hostname.local — because issuers commonly scope tokens by DNS
// wildcards ("https://*.local") that an IP literal can never match.
func ValidateClaims(t Token, hosts []string, now time.Time, leeway time.Duration) error {
	if err := ValidateClaimsExceptAud(t, now, leeway); err != nil {
		return err
	}
	if !AudienceMatchesAny(t.Aud, hosts) {
		return fmt.Errorf("is10: token aud %v does not name this server (%v)", []string(t.Aud), hosts)
	}
	return nil
}

// ValidateClaimsExceptAud runs every claim rule EXCEPT audience matching.
// Resource servers separate the two because their HTTP verdicts differ: a
// malformed or expired token is 401 invalid_token, while a valid token
// addressed to someone else is 403 — the AMWA suite pins exactly that split.
func ValidateClaimsExceptAud(t Token, now time.Time, leeway time.Duration) error {
	if t.Iss == "" {
		return fmt.Errorf("is10: token: iss claim is required")
	}
	if t.Sub == "" {
		return fmt.Errorf("is10: token: sub claim is required")
	}
	if len(t.Aud) == 0 {
		return fmt.Errorf("is10: token: aud claim is required")
	}
	// exp/iat/nbf and the clock are the shared rules.
	if err := jwt.ValidateTime(t.Claims, now, leeway); err != nil {
		return err
	}
	if t.ClientID == "" && t.Azp == "" {
		return fmt.Errorf("is10: token: client_id claim is required when azp is absent")
	}
	return nil
}

// AudienceMatchesAny reports whether any aud entry names any of this server's
// identities.
func AudienceMatchesAny(aud jwt.Audience, hosts []string) bool {
	for _, h := range hosts {
		if AudienceMatches(aud, h) {
			return true
		}
	}
	return false
}

// AudienceMatches reports whether any aud entry names host. Entries may be
// bare domain names or scheme-prefixed URIs (never carrying ports, paths or
// queries per the spec — but a sloppy issuer's port is stripped rather than
// silently failing every request), and may carry '*' wildcards.
func AudienceMatches(aud jwt.Audience, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, ok := jwt.SplitHostPort(host); ok {
		host = h
	}
	for _, a := range aud {
		a = strings.ToLower(strings.TrimSpace(a))
		if i := strings.Index(a, "://"); i >= 0 {
			a = a[i+3:]
		}
		if i := strings.IndexByte(a, '/'); i >= 0 {
			a = a[:i]
		}
		if h, _, ok := jwt.SplitHostPort(a); ok {
			a = h
		}
		if jwt.WildcardMatch(a, host) {
			return true
		}
	}
	return false
}

// readVerbs / writeVerbs classify HTTP methods per the access permissions
// object: read = GET/OPTIONS/HEAD, write = POST/PUT/PATCH/DELETE.
func isReadVerb(method string) bool {
	switch method {
	case "GET", "HEAD", "OPTIONS":
		return true
	}
	return false
}

func isWriteVerb(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	}
	return false
}

// Authorize applies the Behaviour - Resource Servers.md path table to one
// request. It assumes the signature and claims are already validated. A nil
// error means permitted; a non-nil error is a 403.
//
// Path rules (query parameters ignored by contract — pass the path only):
//
//	/                       always read
//	/x-nmos                 always read
//	/x-nmos/<api>[/<ver>]   read with a matching scope OR x-nmos claim
//	/x-nmos/<api>/<ver>/…   x-nmos-<api> claim path specifiers decide
//
// URL normalization is applied first so `..` segments cannot smuggle a path
// past a narrower specifier.
func Authorize(t Token, method, rawPath string) error {
	if !isReadVerb(method) && !isWriteVerb(method) {
		return fmt.Errorf("is10: method %s is not an NMOS API verb", method)
	}
	p := stdpath.Clean("/" + rawPath)

	// Root + /x-nmos: implicit read, no claim checks.
	if p == "/" || p == "/x-nmos" {
		if isReadVerb(method) {
			return nil
		}
		return fmt.Errorf("is10: %s is read-only at %s", method, p)
	}
	if !strings.HasPrefix(p, "/x-nmos/") {
		// Not an NMOS tree. IS-10 defines no grant that could cover it, so a
		// validated token still gets a 403 here.
		return fmt.Errorf("is10: no authorization rule covers %s", p)
	}
	segs := strings.Split(strings.TrimPrefix(p, "/x-nmos/"), "/")
	api := segs[0]
	perms, hasClaim := t.APIs[api]

	// /x-nmos/<api> and /x-nmos/<api>/<ver>: implicit read for a matching
	// scope OR claim.
	if len(segs) <= 2 {
		if isReadVerb(method) {
			if hasClaim || t.HasScope(api) {
				return nil
			}
			return fmt.Errorf("is10: token grants no access to the %s API", api)
		}
		// A write at the version root has no specifier to match — only an
		// explicit "*"-style write grant covers it.
		if hasClaim && matchAny(perms.Write, "") {
			return nil
		}
		return fmt.Errorf("is10: token grants no write access to the %s API root", api)
	}

	// Deep path: the x-nmos-<api> claim decides; a scope alone is not enough
	// (path table row 5).
	if !hasClaim {
		return fmt.Errorf("is10: token carries no x-nmos-%s claim for %s", api, p)
	}
	remainder := strings.Join(segs[2:], "/")
	if isReadVerb(method) {
		if matchAny(perms.Read, remainder) {
			return nil
		}
		return fmt.Errorf("is10: x-nmos-%s read specifiers do not permit %s", api, p)
	}
	if matchAny(perms.Write, remainder) {
		return nil
	}
	return fmt.Errorf("is10: x-nmos-%s write specifiers do not permit %s %s", api, method, p)
}

// matchAny reports whether any wildcarded specifier matches the
// version-relative path remainder. The spec's trailing-slash equivalence
// applies here too: the canonical example grants write ["subscriptions/*"]
// precisely so a client can POST .../subscriptions — so "<seg>" and "<seg>/"
// are the same resource.
func matchAny(specs []string, remainder string) bool {
	alt := strings.TrimSuffix(remainder, "/")
	if alt == remainder {
		alt = remainder + "/"
	}
	for _, s := range specs {
		if jwt.WildcardMatch(s, remainder) || jwt.WildcardMatch(s, alt) {
			return true
		}
	}
	return false
}
