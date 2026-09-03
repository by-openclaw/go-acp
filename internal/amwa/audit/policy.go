package audit

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
)

// Policy is the site-specific input the network-plane checks need but
// no spec can supply (#852): the multicast bandwidth-class map, the
// expected PTP grandmaster/domain, and whether the media plane is
// meant to be private. Absent, every policy-gated check reports SKIP —
// never PASS, so "no policy" can't read as "conformant".
//
// The file is JSON, not YAML: the repo is stdlib-only and does not
// parse YAML (internal/export/yaml.go — "import is JSON-only"). The
// issue's YAML sketch maps field-for-field.
type Policy struct {
	// MulticastClasses maps address ranges to a bandwidth class and a
	// ceiling the fabric controller enforces. A sender in the wrong
	// range for its Flow's essence is admitted nowhere.
	MulticastClasses []MulticastClass `json:"multicast_classes,omitempty"`

	// ExpectedGrandmaster, when set, is the EUI-64 gmid every sender's
	// SDP must reference. ExpectedDomain likewise (nil = unspecified).
	ExpectedGrandmaster string `json:"expected_grandmaster,omitempty"`
	ExpectedDomain      *int   `json:"expected_domain,omitempty"`

	// PrivateMediaPlane, when set true, asserts every source_ip is
	// RFC 1918; false asserts none is. nil = unspecified (SKIP).
	PrivateMediaPlane *bool `json:"private_media_plane,omitempty"`
}

// MulticastClass is one row of the bandwidth-class map.
type MulticastClass struct {
	Range          string  `json:"range"`
	Class          string  `json:"class"`
	MaxBitrateGbps float64 `json:"max_bitrate_gbps,omitempty"`

	ipnet *net.IPNet // parsed once at load
}

// LoadPolicy reads and validates a policy file. A malformed range or
// unparseable JSON is an error — a policy nobody can trust is worse
// than none.
func LoadPolicy(path string) (*Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("audit policy: %w", err)
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("audit policy %s: %w", path, err)
	}
	for i := range p.MulticastClasses {
		mc := &p.MulticastClasses[i]
		_, ipnet, cerr := net.ParseCIDR(mc.Range)
		if cerr != nil {
			return nil, fmt.Errorf("audit policy %s: multicast_classes[%d].range %q: %w", path, i, mc.Range, cerr)
		}
		mc.ipnet = ipnet
	}
	if p.ExpectedDomain != nil && (*p.ExpectedDomain < 0 || *p.ExpectedDomain > 127) {
		return nil, fmt.Errorf("audit policy %s: expected_domain %d out of range 0-127", path, *p.ExpectedDomain)
	}
	if p.ExpectedGrandmaster != "" && !isEUI64(p.ExpectedGrandmaster) {
		return nil, fmt.Errorf("audit policy %s: expected_grandmaster %q is not EUI-64 form", path, p.ExpectedGrandmaster)
	}
	return &p, nil
}

// classify returns the policy class row an address falls in, or nil.
func (p *Policy) classify(ip net.IP) *MulticastClass {
	if p == nil {
		return nil
	}
	for i := range p.MulticastClasses {
		if p.MulticastClasses[i].ipnet != nil && p.MulticastClasses[i].ipnet.Contains(ip) {
			return &p.MulticastClasses[i]
		}
	}
	return nil
}
