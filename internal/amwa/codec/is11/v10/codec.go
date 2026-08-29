// Package v10 is the AMWA NMOS IS-11 v1.0.0 wire codec — the only
// published minor. The canonical shapes in is11/ carry v1.0's whole
// surface, so this package is identity plus the registry entry; a
// future v1.1 gets its own drop table here, per the locked pattern.
package v10

import (
	"dhs/internal/amwa/codec/is11"
)

// SpecPatch — the patch release the codec is audited against.
const SpecPatch = "v1.0.0"

// Codec implements [is11.Codec] for IS-11 wire minor v1.0.
type Codec struct{}

// New returns a Codec.
func New() Codec { return Codec{} }

func (Codec) SpecID() string    { return is11.SpecID }
func (Codec) APIVer() string    { return "v1.0" }
func (Codec) SpecPatch() string { return SpecPatch }

func (Codec) EncodeInput(in is11.Input) ([]byte, error)  { return is11.EncodeInput(in) }
func (Codec) DecodeInput(raw []byte) (is11.Input, error) { return is11.DecodeInput(raw) }

func (Codec) EncodeOutput(o is11.Output) ([]byte, error)   { return is11.EncodeOutput(o) }
func (Codec) DecodeOutput(raw []byte) (is11.Output, error) { return is11.DecodeOutput(raw) }

func (Codec) EncodeActiveConstraints(a is11.ActiveConstraints) ([]byte, error) {
	return is11.EncodeActiveConstraints(a)
}
func (Codec) DecodeActiveConstraints(raw []byte) (is11.ActiveConstraints, error) {
	return is11.DecodeActiveConstraints(raw)
}
func (Codec) ValidateActiveConstraints(a is11.ActiveConstraints) error {
	return is11.ValidateActiveConstraints(a)
}

func init() {
	is11.Register(Codec{})
}
