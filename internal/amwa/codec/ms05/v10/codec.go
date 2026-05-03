// Package v10 is the AMWA NMOS MS-05-02 v1.0.0 wire codec — the
// single track defined by the spec today.
package v10

import "dhs/internal/amwa/codec/ms05"

// SpecPatch — the patch release the codec is audited against.
const SpecPatch = "v1.0.0"

// Codec implements [ms05.Codec] for MS-05-02 wire minor v1.0.
type Codec struct{}

// New returns a Codec.
func New() Codec { return Codec{} }

func (Codec) SpecID() string    { return ms05.SpecID }
func (Codec) APIVer() string    { return "v1.0" }
func (Codec) SpecPatch() string { return SpecPatch }

func init() {
	ms05.Register(Codec{})
}
