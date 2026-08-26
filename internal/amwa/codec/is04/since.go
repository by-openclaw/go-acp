package is04

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"dhs/internal/amwa/codec/spec"
)

// LaterMinorFieldCode is fired when a peer sends us a property that
// IS-04 did not define until a minor later than the tree it served it
// on. We absorb the property and keep the resource; the event is the
// audit trail.
const LaterMinorFieldCode = "nmos_is04_later_minor_field"

// FieldSince names one IS-04 property and the wire minor that
// introduced it.
//
// Path is a dot-separated JSON path. A segment ending in "[]" descends
// into every element of that array, so one row covers a property
// wherever it repeats:
//
//	"controls"                             top-level key
//	"caps.constraint_sets"                 nested key
//	"interfaces[].attached_network_device" key on every array element
type FieldSince struct {
	Path  string
	Since string // "v1.1" · "v1.2" · "v1.3"
}

// Since is the single source of truth for when each IS-04 property
// arrived. Both directions read it and nothing else:
//
//   - [StripLaterThan] on ENCODE — a tree we serve at v1.x MUST NOT
//     carry a property introduced after v1.x.
//   - [AbsorbLaterThan] on DECODE — a peer that sends one anyway is
//     still readable; the deviation is reported, not fatal.
//
// Before this table the same deltas lived four times over, written
// three different ways (flat key lists in v10/v11, bespoke nested
// walkers in v12, struct-copy field nilling in v12's encoders), and
// each delta was spelled out twice — once to strip, once to reject.
// Adding a minor meant editing every package. Now it means adding
// rows here plus a vXX package holding only that minor's validators.
//
// Keys are the resource kind as it appears in compliance events:
// node · device · source · flow · sender · receiver.
//
// Derived from the AMWA schema sets under testdata/schemas/ at
// v1.0.3 / v1.1.3 / v1.2.2 / v1.3.3.
var Since = map[string][]FieldSince{
	"node": {
		{"api", "v1.1"},
		{"clocks", "v1.1"},
		{"interfaces", "v1.2"},
		{"interfaces[].attached_network_device", "v1.3"},
		{"services[].authorization", "v1.3"},
		{"api.endpoints[].authorization", "v1.3"},
	},
	"device": {
		{"controls", "v1.1"},
		{"controls[].authorization", "v1.3"},
	},
	"source": {
		{"clock_name", "v1.1"},
		{"grain_rate", "v1.1"},
		{"channels", "v1.2"},
	},
	"flow": {
		{"device_id", "v1.1"},
		{"grain_rate", "v1.1"},
		{"media_type", "v1.1"},
		{"components", "v1.1"},
		{"frame_width", "v1.1"},
		{"frame_height", "v1.1"},
		{"interlace_mode", "v1.1"},
		{"colorspace", "v1.1"},
		{"transfer_characteristic", "v1.1"},
		{"sample_rate", "v1.1"},
		{"bit_depth", "v1.1"},
		{"DID_SDID", "v1.1"},
		{"event_type", "v1.1"},
	},
	"sender": {
		{"caps", "v1.2"},
		{"interface_bindings", "v1.2"},
		{"subscription", "v1.2"},
	},
	"receiver": {
		{"interface_bindings", "v1.2"},
		{"subscription.active", "v1.2"},
		{"caps.constraint_sets", "v1.3"},
		{"caps.version", "v1.3"},
	},
}

// StripLaterThan removes every property [Since] says arrived after
// apiVer, returning the payload a v{apiVer} tree may legally serve.
//
// Strict by design: emitting a later minor's field is our bug, and
// AMWA IS-04-01 fails the Node for it.
func StripLaterThan(raw []byte, kind, apiVer string) ([]byte, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("is04 %s: strip %s: %w", apiVer, kind, err)
	}
	for _, f := range Since[kind] {
		if atLeast(apiVer, f.Since) {
			continue
		}
		walk(doc, strings.Split(f.Path, "."), func(obj map[string]any, key string) {
			delete(obj, key)
		})
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("is04 %s: strip %s: %w", apiVer, kind, err)
	}
	return out, nil
}

// AbsorbLaterThan reports — and does NOT reject — every property
// [Since] says arrived after apiVer.
//
// Tolerant by design, and the asymmetry with [StripLaterThan] is the
// point: strict on what we emit, tolerant of what we read. Refusing a
// payload teaches the operator nothing and costs them the whole
// resource. A real EVS Neuron serves `controls` on its v1.0 Device
// tree; rejecting that lost the Device outright.
//
// A nil Reporter is a no-op — the deviation still does not stop the
// decode, it simply goes unrecorded.
func AbsorbLaterThan(raw []byte, kind, apiVer string, r spec.Reporter) {
	if r == nil {
		return
	}
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return // the canonical decoder reports the parse failure
	}
	for _, f := range Since[kind] {
		if atLeast(apiVer, f.Since) {
			continue
		}
		walk(doc, strings.Split(f.Path, "."), func(obj map[string]any, key string) {
			if _, present := obj[key]; !present {
				return
			}
			r.Report(spec.ComplianceEvent{
				SpecID:   SpecID,
				APIVer:   apiVer,
				Code:     LaterMinorFieldCode,
				Severity: spec.SeverityWarn,
				Detail: fmt.Sprintf(
					"%s.%s is not defined before IS-04 %s, but the peer sent it on its %s tree; absorbed and kept",
					kind, f.Path, f.Since, apiVer),
				Resource: kind,
				At:       time.Now(),
			})
		})
	}
}

// walk resolves segs against node and calls fn(parent, finalKey) at
// every place the property could sit. A segment ending in "[]"
// fans out across that array, so fn may fire more than once.
func walk(node any, segs []string, fn func(obj map[string]any, key string)) {
	if len(segs) == 0 {
		return
	}
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	if len(segs) == 1 {
		fn(obj, segs[0])
		return
	}
	key, fanOut := strings.CutSuffix(segs[0], "[]")
	child, present := obj[key]
	if !present {
		return
	}
	if !fanOut {
		walk(child, segs[1:], fn)
		return
	}
	arr, ok := child.([]any)
	if !ok {
		return
	}
	for _, el := range arr {
		walk(el, segs[1:], fn)
	}
}

// atLeast reports whether wire minor have is >= want. Both are
// "vMAJOR.MINOR". An unparseable version sorts as older, so a typo in
// [Since] strips rather than leaks.
func atLeast(have, want string) bool {
	hMaj, hMin := parseMinor(have)
	wMaj, wMin := parseMinor(want)
	if hMaj != wMaj {
		return hMaj > wMaj
	}
	return hMin >= wMin
}

func parseMinor(v string) (int, int) {
	maj, min, ok := strings.Cut(strings.TrimPrefix(v, "v"), ".")
	if !ok {
		return -1, -1
	}
	m, err1 := strconv.Atoi(maj)
	n, err2 := strconv.Atoi(min)
	if err1 != nil || err2 != nil {
		return -1, -1
	}
	return m, n
}

// LaterThan returns the [Since] rows for kind that arrived after
// apiVer — exactly the set [StripLaterThan] removes and
// [AbsorbLaterThan] reports. Exposed so a caller (or a test) can ask
// what the delta between two minors actually is, rather than
// restating it.
func LaterThan(kind, apiVer string) []FieldSince {
	var out []FieldSince
	for _, f := range Since[kind] {
		if !atLeast(apiVer, f.Since) {
			out = append(out, f)
		}
	}
	return out
}
