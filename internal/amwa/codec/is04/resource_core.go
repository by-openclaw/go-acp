package is04

import (
	"fmt"
	"strings"
)

// ResourceCore is the shared base every IS-04 resource extends, per
// `resource_core.json` — id, version, label, description, tags. Reused
// across Node / Device / Source / Flow / Sender / Receiver.
type ResourceCore struct {
	ID          string              `json:"id"`
	Version     string              `json:"version"`
	Label       string              `json:"label"`
	Description string              `json:"description"`
	Tags        map[string][]string `json:"tags"`
}

// validateCore returns the slice of human-readable validation errors for
// the embedded ResourceCore. Caller appends additional resource-type
// rules.
//
// IS-04 v1.3.3 resource_core.json marks `label` and `description` as
// required fields of type string, and `tags` as required of type
// object — but empty values are valid in each case. Real-world peers
// (and the AMWA Testing tool's per-version fixtures) ship `"label": ""`
// regularly, and the v1.0 schemas don't require `tags`/`description`
// at all. We enforce only id/version *type and pattern* here; per-
// version key-presence is enforced upstream at the registry POST
// handler, which knows the URL's api_ver.
func validateCore(c *ResourceCore, where string) []string {
	var errs []string
	if c.ID == "" || !IsValidUUID(c.ID) {
		errs = append(errs, fmt.Sprintf("%s.id %q: must match RFC 4122 v1-v5 UUID pattern", where, c.ID))
	}
	if c.Version == "" || !IsValidVersion(c.Version) {
		errs = append(errs, fmt.Sprintf("%s.version %q: must match `<sec>:<nsec>` TAI form", where, c.Version))
	}
	return errs
}

// joinErrs renders a non-empty validation slice into a single error.
// Returns nil when errs is empty.
func joinErrs(prefix string, errs []string) error {
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %s", prefix, strings.Join(errs, "; "))
}
