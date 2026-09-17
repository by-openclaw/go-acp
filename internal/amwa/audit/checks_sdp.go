package audit

import (
	"fmt"
	"path"
	"sort"

	"dhs/internal/amwa/codec/sdp"
)

// checkSDPConformance parses every captured SDP transport file and
// reports the deviations the sdp codec finds (#850). A sender can
// publish a non-conformant SDP that the controller relays verbatim
// (correctly — a controller must never author stream parameters it
// does not own), the receiver then cannot decode, and IS-04/IS-05 both
// report the route "up". The deviation is the only signal, and until
// now nothing consumed codec/sdp's []Deviation.
//
// Each SDP is keyed by its capture-relative path (e.g.
// "is05/<id>.sdp"); the id is the resource the finding names.
func checkSDPConformance(h *Harvest) []Finding {
	if len(h.SDP) == 0 {
		return nil
	}
	keys := make([]string, 0, len(h.SDP))
	for k := range h.SDP {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []Finding
	for _, key := range keys {
		id := sdpResourceID(key)
		sess, devs, err := sdp.Parse(string(h.SDP[key]))
		if err != nil {
			out = append(out, Finding{
				Code:     "NMOS-SDP-UNPARSEABLE",
				Severity: SevError,
				Target:   h.Target,
				Device:   h.Label,
				Resource: "sender/" + id,
				Detail:   "SDP transport file is structurally invalid: " + err.Error(),
				Spec:     "SMPTE ST 2110 / RFC 4566 SDP; BCP-004-01 transport file",
				Hint:     "the receiver cannot decode this stream even though IS-04/IS-05 report the route up",
			})
			continue
		}
		for _, d := range devs {
			out = append(out, Finding{
				Code:     "NMOS-SDP-DEVIATION",
				Severity: SevWarn,
				Target:   h.Target,
				Device:   h.Label,
				Resource: "sender/" + id,
				Detail:   sdpDeviationDetail(key, d),
				Spec:     "SMPTE ST 2110 / RFC 4566 SDP",
				Hint:     "a non-conformant SDP a controller relays verbatim; the receiver may fail to decode",
			})
		}
		// A well-formed but leg-incoherent SDP is its own failure: a
		// 2022-7 sender advertising a=group:DUP whose tags do not both
		// resolve to media sections joins one path only.
		if f := checkDupLegs(h, id, sess); f != nil {
			out = append(out, *f)
		}
	}
	return out
}

// checkDupLegs flags an a=group:DUP whose tags do not both resolve to
// a media section — a ST 2022-7 sender that will come up single-path.
func checkDupLegs(h *Harvest, id string, sess *sdp.Session) *Finding {
	hasDUP := false
	for _, g := range sess.Groups {
		if g.Semantics == "DUP" {
			hasDUP = true
		}
	}
	if !hasDUP {
		return nil
	}
	legs := sess.Legs()
	if len(legs) >= 2 {
		return nil
	}
	return &Finding{
		Code:     "NMOS-SDP-DUP-INCOMPLETE",
		Severity: SevWarn,
		Target:   h.Target,
		Device:   h.Label,
		Resource: "sender/" + id,
		Detail:   "SDP declares a=group:DUP but only one leg resolves — the sender advertises ST 2022-7 redundancy it does not carry",
		Spec:     "SMPTE ST 2022-7; SDP a=group:DUP",
		Hint:     "check that both a=mid tags name present m= sections with distinct c= addresses",
	}
}

// sdpResourceID recovers the resource id from a capture key like
// "is05/<uuid>.sdp" or "<uuid>.sdp".
func sdpResourceID(key string) string {
	base := path.Base(key)
	return base[:len(base)-len(path.Ext(base))]
}

// sdpDeviationDetail renders one deviation with its capture location
// and line number.
func sdpDeviationDetail(key string, d sdp.Deviation) string {
	return fmt.Sprintf("%s line %d: %s (%s)", key, d.Line, d.Reason, d.Text)
}
