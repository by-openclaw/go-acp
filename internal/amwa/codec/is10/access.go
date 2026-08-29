package is10

import (
	"fmt"
	stdpath "path"
	"strings"
)

// readVerbs / writeVerbs classify HTTP methods per the access
// permissions object: read = GET/OPTIONS/HEAD, write =
// POST/PUT/PATCH/DELETE.
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

// Authorize applies the Behaviour - Resource Servers.md path table to
// one request. It assumes the token's signature and claims have
// already been validated (VerifyWithKeys + ValidateClaims). A nil
// error means the request is permitted; a non-nil error is a 403.
//
// Path rules (query parameters ignored by contract — pass the path
// only):
//
//	/                       always read
//	/x-nmos                 always read
//	/x-nmos/<api>[/<ver>]   read with a matching scope OR x-nmos claim
//	/x-nmos/<api>/<ver>/…   x-nmos-<api> claim path specifiers decide
//
// URL normalization is applied first so `..` segments cannot smuggle
// a path past a narrower specifier.
func Authorize(c Claims, method, rawPath string) error {
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
		// Not an NMOS tree. IS-10 defines no grant that could cover
		// it, so a validated token still gets a 403 here.
		return fmt.Errorf("is10: no authorization rule covers %s", p)
	}
	segs := strings.Split(strings.TrimPrefix(p, "/x-nmos/"), "/")
	api := segs[0]
	perms, hasClaim := c.APIs[api]

	// /x-nmos/<api> and /x-nmos/<api>/<ver>: implicit read for a
	// matching scope OR claim.
	if len(segs) <= 2 {
		if isReadVerb(method) {
			if hasClaim || c.HasScope(api) {
				return nil
			}
			return fmt.Errorf("is10: token grants no access to the %s API", api)
		}
		// A write at the version root has no specifier to match —
		// only an explicit "*"-style write grant covers it.
		if hasClaim && matchAny(perms.Write, "") {
			return nil
		}
		return fmt.Errorf("is10: token grants no write access to the %s API root", api)
	}

	// Deep path: the x-nmos-<api> claim decides; a scope alone is not
	// enough (path table row 5).
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
// version-relative path remainder. The spec's trailing-slash
// equivalence applies here too: the canonical example grants write
// ["subscriptions/*"] precisely so a client can POST
// .../subscriptions — so "<seg>" and "<seg>/" are the same resource.
func matchAny(specs []string, remainder string) bool {
	alt := strings.TrimSuffix(remainder, "/")
	if alt == remainder {
		alt = remainder + "/"
	}
	for _, s := range specs {
		if wildcardMatch(s, remainder) || wildcardMatch(s, alt) {
			return true
		}
	}
	return false
}
