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
func validateCore(c *ResourceCore, where string) []string {
	var errs []string
	if c.ID == "" || !IsValidUUID(c.ID) {
		errs = append(errs, fmt.Sprintf("%s.id %q: must match RFC 4122 v1-v5 UUID pattern", where, c.ID))
	}
	if c.Version == "" || !IsValidVersion(c.Version) {
		errs = append(errs, fmt.Sprintf("%s.version %q: must match `<sec>:<nsec>` TAI form", where, c.Version))
	}
	if c.Label == "" {
		errs = append(errs, where+".label: required (resource_core)")
	}
	if c.Description == "" {
		errs = append(errs, where+".description: required (resource_core)")
	}
	if c.Tags == nil {
		errs = append(errs, where+".tags: required (may be empty object, but key must be present)")
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
