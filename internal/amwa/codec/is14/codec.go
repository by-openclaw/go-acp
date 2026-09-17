package is14

import (
	"dhs/internal/amwa/codec/spec"
)

// SpecID is the AMWA NMOS catalogue slug for IS-14 (Device
// Configuration).
const SpecID = "is-14"

// ControlType is the device-control URN IS-14's "IS-04 interactions"
// doc fixes for advertising the Configuration API (version suffix is
// appended per served minor).
const ControlType = "urn:x-nmos:control:configuration/"

// Codec is the IS-14 wire codec contract — one implementation per
// supported wire minor (v1.0, the only published one). Same shape and
// registry semantics as is04 / is05 / is11.
type Codec interface {
	spec.Versioned

	EncodeBulkPropertiesHolder(BulkPropertiesHolder) ([]byte, error)
	DecodeBulkPropertiesHolder([]byte) (BulkPropertiesHolder, error)

	DecodeBulkPropertiesSetRequest([]byte) (BulkPropertiesSetRequest, error)
	DecodePropertyValuePutRequest([]byte) (PropertyValuePutRequest, error)
	DecodeMethodPatchRequest([]byte) (MethodPatchRequest, error)
}

// versions is the per-process Registry of IS-14 codec implementations.
var versions = spec.NewRegistry[Codec]()

// Register installs a codec for one IS-14 wire minor. Idempotent for
// the same instance; panics on conflict — a duplicate-init bug.
func Register(c Codec) {
	if c.SpecID() != SpecID {
		panic("is14.Register: SpecID must be " + SpecID + ", got " + c.SpecID())
	}
	versions.Register(c)
}

// Get / AllCodecs / SupportedVersions / SelectHighest / Default —
// same shape as the sibling spec packages.
func Get(apiVer string) (Codec, bool)            { return versions.Get(apiVer) }
func AllCodecs() []Codec                         { return versions.AllCodecs() }
func SupportedVersions() []string                { return versions.SupportedVersions() }
func SelectHighest(peer []string) (Codec, error) { return spec.SelectHighestMutual(versions, peer) }
func Default() Codec {
	all := versions.AllCodecs()
	if len(all) == 0 {
		panic("is14.Default: no codec registered (forgot to blank-import internal/amwa/codec/is14/v10?)")
	}
	return all[len(all)-1]
}
