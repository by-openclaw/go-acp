package v12

import "encoding/json"

// marshalIndent is the canonical JSON serialiser used by every Encode
// method in this package — 2-space indent matches the formatting AMWA
// uses in spec annexes and the existing v1.3 codec, so fixture round-
// trips compare byte-exact.
func marshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
