package v13

import (
	"strings"
	"testing"

	"dhs/internal/amwa/codec/is04/schemas"
)

// TestDropTableNeverStripsARequiredProperty is the guard that catches a
// wrong row in `drop` at unit-test speed instead of twenty minutes into
// an AMWA conformance run.
//
// A property this minor's own schemas mark REQUIRED is, by definition,
// not a later-minor field. Stripping one produces a payload AMWA
// rejects, and the rejection surfaces far from the cause: listing
// `channels` here made every audio Source fail to register at v1.1, and
// IS-04-01 reported it as test_09/10/11/26 "not found in the registry"
// — four tests away from anything mentioning a schema.
func TestDropTableNeverStripsARequiredProperty(t *testing.T) {
	for kind, paths := range drop {
		required, err := schemas.RequiredLeaves(APIVer, kind)
		if err != nil {
			t.Fatalf("load %s schema for %s: %v", APIVer, kind, err)
		}
		if len(required) == 0 {
			t.Fatalf("%s.json declares nothing required — the schema walk is "+
				"broken, not the drop table", kind)
		}
		for _, p := range paths {
			if strings.Contains(p, ".") {
				// Nested paths are out of scope: the check matches on
				// name, and a nested name collides with a root one
				// (caps.version vs the resource's own version).
				continue
			}
			if required[p] {
				t.Errorf("%s drop lists %s.%s, but AMWA %s marks it required in "+
					"%s.json or something it $refs — a required property is not "+
					"a later-minor field", APIVer, kind, p, APIVer, kind)
			}
		}
	}
}

// TestDropTablePathsAreWellFormed: a typo in a path silently strips
// nothing, and nothing is exactly what a passing test looks like.
func TestDropTablePathsAreWellFormed(t *testing.T) {
	known := map[string]bool{"node": true, "device": true, "source": true,
		"flow": true, "sender": true, "receiver": true}
	for kind, paths := range drop {
		if !known[kind] {
			t.Errorf("drop has kind %q, which is not an IS-04 resource", kind)
		}
		seen := map[string]bool{}
		for _, p := range paths {
			if p == "" || strings.HasPrefix(p, ".") || strings.HasSuffix(p, ".") {
				t.Errorf("%s: %q is not a usable path", kind, p)
			}
			if seen[p] {
				t.Errorf("%s: duplicate path %q", kind, p)
			}
			seen[p] = true
		}
	}
}
