package consumer

import "strings"

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
