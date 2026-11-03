package consumer

import (
	"sort"
	"strings"
)

// SpecFor finds what the specification prints about a property
// (generated table specMeta). Names are matched without regard to
// case: RRCS answers LatchingTimeOut where the table has LatchingTimeout.
//
// Two corrections to the printed tables:
//   - the Media blocks of a client card are left to the hand-kept table:
//     their printed columns do not line up;
//   - KeyMode as RRCS reports it is one more than the value an edit
//     takes ("the integer values for Auto, Momentary and Latching are
//     increased by 1 on the GetKeyConfiguration API", specification
//     history 8.x).
func SpecFor(object, block, field string) SpecProp {
	if block == "Media_1" || block == "Media_2" {
		return SpecProp{}
	}
	props := specMeta[object][block]
	p, ok := props[field]
	if !ok {
		for name, candidate := range props {
			if strings.EqualFold(name, field) {
				p, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		return SpecProp{}
	}
	if p.Type != "int" {
		p.Min, p.Max = "", ""
	}
	if object == "panel-key" && field == "KeyMode" {
		p.Enum, p.Default = "1=Auto|2=Momentary(PTT)|3=Latching", "2"
	}
	return p
}

// SpecObjectTypes lists the object types the specification prints
// property tables for, sorted.
func SpecObjectTypes() []string {
	out := make([]string, 0, len(specMeta))
	for t := range specMeta {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// SpecBlocks lists the blocks the specification prints for an object
// type, sorted; "" is the object itself.
func SpecBlocks(object string) []string {
	out := make([]string, 0, len(specMeta[object]))
	for b := range specMeta[object] {
		out = append(out, b)
	}
	sort.Strings(out)
	return out
}

// SpecFields lists the properties the specification prints in one block
// of an object type, sorted.
func SpecFields(object, block string) []string {
	out := make([]string, 0, len(specMeta[object][block]))
	for f := range specMeta[object][block] {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// CommandTypes lists the object types of the specification that are
// commands of a key or of a virtual function (§8.10.1), sorted.
func CommandTypes() []string {
	var out []string
	for _, t := range SpecObjectTypes() {
		if strings.HasSuffix(t, "-cmd") || strings.HasSuffix(t, "-command") || strings.HasPrefix(t, "call-to-") {
			out = append(out, t)
		}
	}
	return out
}
