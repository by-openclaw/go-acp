package mnset

import (
	"strings"

	"dhs/internal/consumer"
)

// The module addresses everything by uuid (flows/<uuid>, sdi_output/<uuid>,
// devices[N].receivers[M] …), which is what a PUT needs and therefore
// what Object.Path keeps. A human reads channels: Object.Label carries
// "CH8 · rx flow 5 pri · dst_ip_addr" so an export filters by channel
// and a leaf reads as what it is. The names come from the module itself
// (devices[].label, flows/<uuid>.name, sdi_output/<uuid>.label): nothing
// is invented, a module that hides them keeps the plain leaf label.

// labelObjects rewrites Label on the objects of one walk, in place.
func labelObjects(objs []consumer.Object) {
	// Pass 1: what the module calls things.
	byPath := make(map[string]string, len(objs))
	for _, o := range objs {
		if o.Value.Kind == consumer.KindString {
			byPath[strings.Join(o.Path, ".")] = o.Value.Str
		}
	}
	channel := map[string]string{} // device uuid → "CH8"
	owner := map[string]string{}   // receiver/sender/flow uuid → "CH8"
	for p, val := range byPath {
		seg := strings.Split(p, ".")
		if len(seg) == 3 && seg[0] == "devices" && seg[2] == "label" {
			if id, ok := byPath["devices."+seg[1]+".id"]; ok {
				channel[id] = shortChannel(val)
			}
		}
	}
	for p, val := range byPath {
		seg := strings.Split(p, ".")
		if len(seg) == 4 && seg[0] == "devices" && (seg[2] == "receivers" || seg[2] == "senders") {
			if id, ok := byPath["devices."+seg[1]+".id"]; ok {
				owner[val] = channel[id]
			}
		}
	}
	for p, val := range byPath {
		seg := strings.Split(p, ".")
		if len(seg) == 4 && (seg[0] == "receivers" || seg[0] == "senders") && seg[2] == "flow_id" {
			if id, ok := byPath[seg[0]+"."+seg[1]+".id"]; ok {
				owner[val] = owner[id]
			}
		}
	}

	// Pass 2: name each leaf.
	for i := range objs {
		o := &objs[i]
		seg := o.Path
		if len(seg) < 2 {
			continue
		}
		var ch, name string
		switch seg[0] {
		case "flows":
			ch, name = owner[seg[1]], byPath["flows."+seg[1]+".name"]
		case "receivers", "senders":
			if id, ok := byPath[seg[0]+"."+seg[1]+".id"]; ok {
				ch = owner[id]
			}
			name = strings.TrimSuffix(seg[0], "s") + " " + seg[1] + " " + byPath[seg[0]+"."+seg[1]+".format"]
		case "sdi_output", "sdi_audio", "sdi_input":
			ch = channel[byPath[seg[0]+"."+seg[1]+".device_id"]]
			name = strings.TrimSpace(byPath[seg[0]+"."+seg[1]+".label"])
			if name == "" {
				name = seg[0]
			}
		case "clean_switch":
			ch, name = channel[seg[1]], "clean_switch"
		case "devices":
			ch = channel[byPath["devices."+seg[1]+".id"]]
			name = "device"
		case "sdp", "receivers_sdp", "senders_sdp":
			ch, name = owner[seg[1]], seg[0]
		default:
			continue
		}
		if ch == "" && name == "" {
			continue
		}
		leaf := strings.Join(seg[2:], ".")
		parts := make([]string, 0, 3)
		if ch != "" {
			parts = append(parts, ch)
		}
		if name = strings.TrimSpace(name); name != "" {
			parts = append(parts, name)
		}
		if leaf != "" {
			parts = append(parts, leaf)
		}
		o.Label = strings.Join(parts, " · ")
	}
}

// shortChannel turns the module's "Device CH8" into "CH8"; any other
// label is kept as it is.
func shortChannel(label string) string {
	label = strings.TrimSpace(label)
	if strings.HasPrefix(label, "Device ") {
		return strings.TrimPrefix(label, "Device ")
	}
	return label
}
