// Package v10 is the AMWA NMOS IS-12 v1.0.1 wire codec — the single
// track defined by the spec today.
package v10

import (
	"acp/internal/amwa/codec/is12"
)

// SpecPatch — the patch release the codec is audited against.
const SpecPatch = "v1.0.1"

// Codec implements [is12.Codec] for IS-12 wire minor v1.0.
type Codec struct{}

// New returns a Codec.
func New() Codec { return Codec{} }

func (Codec) SpecID() string    { return is12.SpecID }
func (Codec) APIVer() string    { return "v1.0" }
func (Codec) SpecPatch() string { return SpecPatch }

func (Codec) Encode(m is12.Message) ([]byte, error)    { return is12.Encode(m) }
func (Codec) Decode(raw []byte) (is12.Message, error)  { return is12.Decode(raw) }

func init() {
	is12.Register(Codec{})
}
