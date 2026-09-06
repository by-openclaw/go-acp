package auth

import (
	"net/http"
	"strings"
)

// BearerToken extracts the token from an Authorization header, or "" when
// there is none.
//
// The scheme match is case-insensitive because RFC 7235 says the scheme is
// case-insensitive, and real clients send "bearer". The token itself is
// returned untouched — deciding whether it is well-formed is the parser's
// job, not this function's, and a "helpful" pre-check here would give two
// places an opinion about the same string.
func BearerToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if h == "" {
		return ""
	}
	const scheme = "bearer "
	if len(h) < len(scheme) || !strings.EqualFold(h[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(h[len(scheme):])
}
