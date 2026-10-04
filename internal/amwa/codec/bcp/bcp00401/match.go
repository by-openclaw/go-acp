package bcp00401

// The evaluation half of BCP-004-01: does a stream satisfy a
// Receiver's declared capabilities?
//
// A Receiver's caps.constraint_sets is an OR of sets, each an AND of
// parameter constraints (enum / minimum / maximum, the parameter named
// by its capability URN). A stream is acceptable when one enabled set
// is satisfied. The stream's parameters come from what IS-04 says
// about it — the Flow, its Source — and a parameter IS-04 does not
// carry is not a violation: it is unknown, and unknown does not refuse
// a route.
//
// One rule for everyone that asks: the controller before it sends a
// connection, and the testing facade when the AMWA controller suites
// ask which Receivers a Sender can feed.

import (
	"fmt"
	"sort"
	"strings"

	"dhs/internal/amwa/codec/is04"
)

// Meta keys of a constraint set (BCP-004-01 §"Constraint Sets").
const (
	metaPrefix  = "urn:x-nmos:cap:meta:"
	MetaLabel   = "urn:x-nmos:cap:meta:label"
	MetaEnabled = "urn:x-nmos:cap:meta:enabled"
)

// Capability parameter URNs evaluated from IS-04 (the capabilities
// register's format:* family that a Flow or its Source states).
const (
	CapMediaType      = "urn:x-nmos:cap:format:media_type"
	CapGrainRate      = "urn:x-nmos:cap:format:grain_rate"
	CapFrameWidth     = "urn:x-nmos:cap:format:frame_width"
	CapFrameHeight    = "urn:x-nmos:cap:format:frame_height"
	CapInterlaceMode  = "urn:x-nmos:cap:format:interlace_mode"
	CapColorspace     = "urn:x-nmos:cap:format:colorspace"
	CapTransferChar   = "urn:x-nmos:cap:format:transfer_characteristic"
	CapComponentDepth = "urn:x-nmos:cap:format:component_depth"
	CapChannelCount   = "urn:x-nmos:cap:format:channel_count"
	CapSampleRate     = "urn:x-nmos:cap:format:sample_rate"
	CapSampleDepth    = "urn:x-nmos:cap:format:sample_depth"
	CapProfile        = "urn:x-nmos:cap:format:profile"
	CapLevel          = "urn:x-nmos:cap:format:level"
	CapSublevel       = "urn:x-nmos:cap:format:sublevel"
	CapFormatBitRate  = "urn:x-nmos:cap:format:bit_rate"
)

// FlowParams is what IS-04 states about a stream, keyed by capability
// URN: strings as strings, numbers as float64 (so they compare with
// JSON-decoded constraint values), rates as is04.GrainRate. A member
// the Flow does not carry is left out — absent, not zero. source may
// be nil; it contributes the audio channel count.
func FlowParams(f *is04.Flow, source *is04.Source) map[string]any {
	out := map[string]any{}
	str := func(urn, v string) {
		if v != "" {
			out[urn] = v
		}
	}
	num := func(urn string, v int) {
		if v != 0 {
			out[urn] = float64(v)
		}
	}
	str(CapMediaType, f.MediaType)
	str(CapInterlaceMode, f.Interlace)
	str(CapColorspace, f.ColorSpace)
	str(CapTransferChar, f.TransferChar)
	str(CapProfile, f.Profile)
	str(CapLevel, f.Level)
	str(CapSublevel, f.Sublevel)
	num(CapFrameWidth, f.FrameWidth)
	num(CapFrameHeight, f.FrameHeight)
	num(CapSampleDepth, f.BitDepth)
	num(CapFormatBitRate, f.FlowBitRate)
	if f.GrainRate != nil {
		out[CapGrainRate] = *f.GrainRate
	}
	if f.SampleRate != nil {
		out[CapSampleRate] = *f.SampleRate
	}
	depth := 0
	for _, c := range f.Components {
		if c.BitDepth > depth {
			depth = c.BitDepth
		}
	}
	num(CapComponentDepth, depth)
	if source != nil {
		num(CapChannelCount, len(source.Channels))
	}
	return out
}

// Satisfied applies one parameter constraint — enum, minimum, maximum,
// any or all of them — to one value. A value of a kind the constraint
// cannot be compared with does not satisfy it.
func Satisfied(v any, constraint map[string]any) bool {
	if enum, ok := constraint["enum"].([]any); ok {
		found := false
		for _, e := range enum {
			if valueEqual(v, e) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if lo, ok := constraint["minimum"]; ok {
		if c, comparable := compare(v, lo); !comparable || c < 0 {
			return false
		}
	}
	if hi, ok := constraint["maximum"]; ok {
		if c, comparable := compare(v, hi); !comparable || c > 0 {
			return false
		}
	}
	return true
}

// valueEqual compares a stream value with one JSON-decoded enum entry.
// Rationals compare as fractions, with the schema's default
// denominator of 1.
func valueEqual(v, entry any) bool {
	switch fv := v.(type) {
	case string:
		s, ok := entry.(string)
		return ok && s == fv
	case bool:
		b, ok := entry.(bool)
		return ok && b == fv
	}
	c, comparable := compare(v, entry)
	return comparable && c == 0
}

// compare orders a stream value against a constraint bound: -1, 0, 1,
// and whether the two are of comparable kinds (number with number,
// rational with rational).
func compare(v, bound any) (int, bool) {
	switch fv := v.(type) {
	case float64:
		n, ok := bound.(float64)
		if !ok {
			return 0, false
		}
		switch {
		case fv < n:
			return -1, true
		case fv > n:
			return 1, true
		}
		return 0, true
	case is04.GrainRate:
		m, ok := bound.(map[string]any)
		if !ok {
			return 0, false
		}
		num, ok := m["numerator"].(float64)
		if !ok {
			return 0, false
		}
		den := 1.0
		if d, ok := m["denominator"].(float64); ok && d != 0 {
			den = d
		}
		fDen := float64(fv.Denominator)
		if fDen == 0 {
			fDen = 1
		}
		// a/b ? c/d  ⇔  a·d ? c·b  (denominators positive per the schema)
		left, right := float64(fv.Numerator)*den, num*fDen
		switch {
		case left < right:
			return -1, true
		case left > right:
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// SetSatisfied applies one constraint set to a stream's parameters.
// Meta keys are not constraints; a parameter the stream does not state
// is skipped. When the set is not satisfied, why names the first
// violated parameter (in URN order, so the answer is stable).
func SetSatisfied(params map[string]any, set map[string]any) (ok bool, why string) {
	urns := make([]string, 0, len(set))
	for urn := range set {
		urns = append(urns, urn)
	}
	sort.Strings(urns)
	for _, urn := range urns {
		if strings.HasPrefix(urn, metaPrefix) {
			continue
		}
		constraint, isConstraint := set[urn].(map[string]any)
		if !isConstraint {
			continue
		}
		v, known := params[urn]
		if !known {
			continue
		}
		if !Satisfied(v, constraint) {
			return false, fmt.Sprintf("%s is %s, outside %s", shortURN(urn), show(v), showConstraint(constraint))
		}
	}
	return true, ""
}

// Enabled reports whether a constraint set is in force: meta:enabled
// defaults to true, and a set switched off constrains nothing and
// admits nothing.
func Enabled(set map[string]any) bool {
	if v, ok := set[MetaEnabled].(bool); ok {
		return v
	}
	return true
}

// AnySetSatisfied is the Receiver-level rule: the stream satisfies at
// least one ENABLED constraint set. With none satisfied, why carries
// one line per enabled set, each naming what that set refused — the
// reason an operator needs to see. A Receiver that declares no
// (enabled) sets has stated no constraint: ok is true and declared is
// false, so a caller that requires a declaration can tell.
func AnySetSatisfied(params map[string]any, sets []map[string]any) (ok, declared bool, why []string) {
	for i, set := range sets {
		if !Enabled(set) {
			continue
		}
		declared = true
		satisfied, reason := SetSatisfied(params, set)
		if satisfied {
			return true, true, nil
		}
		label, _ := set[MetaLabel].(string)
		if label == "" {
			label = fmt.Sprintf("constraint set %d", i)
		}
		why = append(why, label+": "+reason)
	}
	if !declared {
		return true, false, nil
	}
	return false, true, why
}

func shortURN(urn string) string { return strings.TrimPrefix(urn, "urn:x-nmos:cap:") }

func show(v any) string {
	if r, ok := v.(is04.GrainRate); ok {
		if r.Denominator > 1 {
			return fmt.Sprintf("%d/%d", r.Numerator, r.Denominator)
		}
		return fmt.Sprintf("%d", r.Numerator)
	}
	return fmt.Sprintf("%v", v)
}

func showConstraint(c map[string]any) string {
	var parts []string
	if e, ok := c["enum"]; ok {
		parts = append(parts, fmt.Sprintf("enum %v", e))
	}
	if lo, ok := c["minimum"]; ok {
		parts = append(parts, fmt.Sprintf("minimum %v", lo))
	}
	if hi, ok := c["maximum"]; ok {
		parts = append(parts, fmt.Sprintf("maximum %v", hi))
	}
	return strings.Join(parts, ", ")
}
