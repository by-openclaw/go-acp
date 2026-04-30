package is04

import "regexp"

// UUIDPattern is RFC 4122 v1-v5, per resource_core.json. Used as the
// id pattern across every IS-04 resource type.
var UUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// VersionPattern is the IS-04 TAI-timestamp form `<sec>:<nsec>`.
var VersionPattern = regexp.MustCompile(`^[0-9]+:[0-9]+$`)

// APIVersionPattern is `vMAJOR.MINOR` e.g. `v1.3`.
var APIVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+$`)

// MACPattern is the lowercase-hex `xx-xx-xx-xx-xx-xx` form used by
// interfaces.{chassis_id, port_id, attached_network_device}.
var MACPattern = regexp.MustCompile(`^([0-9a-f]{2}-){5}([0-9a-f]{2})$`)

// IsValidUUID reports whether s matches the v1-5 UUID pattern.
func IsValidUUID(s string) bool { return UUIDPattern.MatchString(s) }

// IsValidVersion reports whether s matches the TAI-timestamp pattern.
func IsValidVersion(s string) bool { return VersionPattern.MatchString(s) }

// IsValidAPIVersion reports whether s matches `v<major>.<minor>`.
func IsValidAPIVersion(s string) bool { return APIVersionPattern.MatchString(s) }

// IsValidMAC reports whether s matches the lowercase-hex MAC pattern.
func IsValidMAC(s string) bool { return MACPattern.MatchString(s) }
