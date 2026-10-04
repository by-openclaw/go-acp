package registry

// A resource as an earlier minor shows it.
//
// IS-04 v1.3.3, docs/Upgrade Path, "Requirements for Registries": a
// Query API that serves several minors MUST show a resource registered
// at a later minor on its earlier endpoints too, "by removing keys …
// which are not present" there. "Version Translations" lists the keys,
// minor by minor, and adds that the result is not to be validated: a
// later minor's enum value stays, and may not fit the earlier schema.
//
// So this is a removal of the listed keys from the document that was
// registered — nothing is decoded into a type, nothing is checked, and
// a key the specification does not know (a vendor's) is left alone.

import (
	"bytes"
	"encoding/json"
	"strings"

	"dhs/internal/amwa/codec/is04"
)

// introducedAt lists, newest minor first, the keys each minor added to
// each resource. "a.b" is key b of object a; "a[].b" is key b of every
// element of array a.
var introducedAt = []struct {
	minor string
	keys  map[is04.ResourceType][]string
}{
	{"v1.3", map[is04.ResourceType][]string{
		is04.ResourceNode:   {"interfaces[].attached_network_device", "api.endpoints[].authorization", "services[].authorization"},
		is04.ResourceDevice: {"controls[].authorization"},
		is04.ResourceSource: {"event_type"},
		is04.ResourceFlow:   {"event_type"},
	}},
	{"v1.2", map[is04.ResourceType][]string{
		is04.ResourceNode:     {"interfaces"},
		is04.ResourceSender:   {"caps", "interface_bindings", "subscription"},
		is04.ResourceReceiver: {"interface_bindings", "subscription.active"},
	}},
	{"v1.1", map[is04.ResourceType][]string{
		is04.ResourceNode:   {"api", "clocks", "description", "tags"},
		is04.ResourceDevice: {"controls", "description", "tags"},
		is04.ResourceSource: {"channels", "clock_name", "grain_rate"},
		is04.ResourceFlow: {"bit_depth", "colorspace", "components", "device_id", "DID_SDID", "frame_height",
			"frame_width", "grain_rate", "interlace_mode", "media_type", "sample_rate", "transfer_characteristic"},
	}},
}

// translateDown conforms doc, registered at minor from, to the earlier
// minor to: every key a minor in (to, from] introduced is removed. A
// document that is not a JSON object is returned as it is.
func translateDown(t is04.ResourceType, doc json.RawMessage, from, to string) json.RawMessage {
	d := json.NewDecoder(bytes.NewReader(doc))
	d.UseNumber() // a number goes back out as it came in
	var obj map[string]any
	if err := d.Decode(&obj); err != nil {
		return doc
	}
	for _, step := range introducedAt {
		if !apiVerLE(step.minor, from) || apiVerLE(step.minor, to) {
			continue // not between the two minors
		}
		for _, path := range step.keys[t] {
			removeKey(obj, strings.Split(path, "."))
		}
	}
	out, _ := json.Marshal(obj) // what was decoded from JSON encodes
	return out
}

// removeKey removes the key a path names. A path that the document does
// not have — a missing object, a value of another shape — removes
// nothing.
func removeKey(obj map[string]any, path []string) {
	name, each := strings.CutSuffix(path[0], "[]")
	if len(path) == 1 {
		delete(obj, name)
		return
	}
	if !each {
		if child, ok := obj[name].(map[string]any); ok {
			removeKey(child, path[1:])
		}
		return
	}
	list, _ := obj[name].([]any)
	for _, item := range list {
		if child, ok := item.(map[string]any); ok {
			removeKey(child, path[1:])
		}
	}
}
