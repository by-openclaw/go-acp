// Package registers is a typed, introspectable model of the AMWA NMOS
// Parameter Registers (#851) — stdlib-only, lift-ready (ADR-0006).
//
// The registers are standardised (https://specs.amwa.tv/nmos-parameter-
// registers/), so this implements a register, not a dhs-specific
// schema: our "valid values" must agree with every other
// implementation's, which is the whole point of a conformance tool.
// What we borrow from the ACP2 / Ember+ models is not their content but
// that they are typed and walkable — you can ask a parameter what it
// accepts and get a machine answer, not a doc link.
//
// Adding a parameter or a whole register is +1 entry in a data file
// (capabilities.go, formats.go, …); nothing else changes (open/closed).
package registers

import "sort"

// Kind is the typed constraint a parameter carries — the machine
// answer to "what does this accept".
type Kind string

const (
	// KindEnum: Values lists the legal strings.
	KindEnum Kind = "enum"
	// KindInteger / KindNumber: Min/Max bound a numeric (Max nil = open).
	KindInteger Kind = "integer"
	KindNumber  Kind = "number"
	// KindRational: a value is {numerator, denominator} (e.g. grain_rate).
	KindRational Kind = "rational"
	// KindBoolean: true/false.
	KindBoolean Kind = "boolean"
	// KindString: a free string (a label); no value constraint.
	KindString Kind = "string"
)

// Param is one register entry — a parameter URN and what it accepts.
type Param struct {
	URN         string   `json:"urn"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Kind        Kind     `json:"kind"`
	Values      []string `json:"values,omitempty"` // KindEnum
	Min         *float64 `json:"minimum,omitempty"`
	Max         *float64 `json:"maximum,omitempty"`
	// Register names the AMWA register this came from (e.g.
	// "capabilities"); RegisterVersion the register version, so every
	// URN carries its provenance (DoD).
	Register        string `json:"register"`
	RegisterVersion string `json:"register_version"`
}

// Register is a named, versioned set of parameters.
type Register struct {
	Name    string
	Version string
	Params  []Param
}

// all is the compiled-in register set, filled by each data file's
// init(). A map keyed by URN for O(1) Lookup plus insertion-stable
// listing via the ordered urns slice.
var (
	byURN = map[string]Param{}
	urns  []string
)

// register adds one register's params to the global catalogue. Called
// from data-file init()s. A duplicate URN panics — that is a
// data-authoring bug, caught at process start like the codec
// registries.
func register(r Register) {
	for _, p := range r.Params {
		p.Register = r.Name
		p.RegisterVersion = r.Version
		if _, dup := byURN[p.URN]; dup {
			panic("registers: duplicate URN " + p.URN)
		}
		byURN[p.URN] = p
		urns = append(urns, p.URN)
	}
}

// Lookup returns the parameter for a URN and whether it is known.
func Lookup(urn string) (Param, bool) {
	p, ok := byURN[urn]
	return p, ok
}

// Known reports whether a URN exists in any register.
func Known(urn string) bool {
	_, ok := byURN[urn]
	return ok
}

// All returns every parameter, URN-sorted (stable across runs so two
// `registers list` outputs diff cleanly).
func All() []Param {
	out := make([]Param, 0, len(urns))
	for _, u := range urns {
		out = append(out, byURN[u])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URN < out[j].URN })
	return out
}

// Accepts reports whether value is legal for an enum/boolean param.
// Numeric and rational kinds return true (range/shape checks live with
// the caller that has the parsed value); an unknown URN returns false.
func (p Param) Accepts(value string) bool {
	switch p.Kind {
	case KindEnum:
		for _, v := range p.Values {
			if v == value {
				return true
			}
		}
		return false
	case KindBoolean:
		return value == "true" || value == "false"
	default:
		return true
	}
}
