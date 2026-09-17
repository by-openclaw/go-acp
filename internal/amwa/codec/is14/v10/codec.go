// Package v10 is the AMWA NMOS IS-14 v1.0.0 wire codec — the only
// published minor. The canonical shapes in is14/ carry v1.0's whole
// surface, so this package is identity plus the registry entry; a
// future v1.1 gets its own drop table here, per the locked pattern.
package v10

import (
	"dhs/internal/amwa/codec/is14"
)

// SpecPatch — the patch release the codec is audited against.
const SpecPatch = "v1.0.0"

// Codec implements [is14.Codec] for IS-14 wire minor v1.0.
type Codec struct{}

// New returns a Codec.
func New() Codec { return Codec{} }

func (Codec) SpecID() string    { return is14.SpecID }
func (Codec) APIVer() string    { return "v1.0" }
func (Codec) SpecPatch() string { return SpecPatch }

func (Codec) EncodeBulkPropertiesHolder(h is14.BulkPropertiesHolder) ([]byte, error) {
	return is14.EncodeBulkPropertiesHolder(h)
}
func (Codec) DecodeBulkPropertiesHolder(raw []byte) (is14.BulkPropertiesHolder, error) {
	return is14.DecodeBulkPropertiesHolder(raw)
}
func (Codec) DecodeBulkPropertiesSetRequest(raw []byte) (is14.BulkPropertiesSetRequest, error) {
	return is14.DecodeBulkPropertiesSetRequest(raw)
}
func (Codec) DecodePropertyValuePutRequest(raw []byte) (is14.PropertyValuePutRequest, error) {
	return is14.DecodePropertyValuePutRequest(raw)
}
func (Codec) DecodeMethodPatchRequest(raw []byte) (is14.MethodPatchRequest, error) {
	return is14.DecodeMethodPatchRequest(raw)
}

func init() {
	is14.Register(Codec{})
}
