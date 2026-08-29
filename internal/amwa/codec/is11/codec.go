package is11

import (
	"dhs/internal/amwa/codec/spec"
)

// SpecID is the AMWA NMOS catalogue slug for IS-11 (Stream
// Compatibility Management).
const SpecID = "is-11"

// Codec is the IS-11 wire codec contract — one implementation per
// supported wire minor (v1.0, the only published one). Same shape and
// registry semantics as is04 / is05 / is09.
type Codec interface {
	spec.Versioned

	EncodeInput(Input) ([]byte, error)
	DecodeInput([]byte) (Input, error)

	EncodeOutput(Output) ([]byte, error)
	DecodeOutput([]byte) (Output, error)

	EncodeActiveConstraints(ActiveConstraints) ([]byte, error)
	DecodeActiveConstraints([]byte) (ActiveConstraints, error)
	ValidateActiveConstraints(ActiveConstraints) error
}

// versions is the per-process Registry of IS-11 codec implementations.
var versions = spec.NewRegistry[Codec]()

// Register installs a codec for one IS-11 wire minor. Idempotent for
// the same instance; panics on conflict — a duplicate-init bug.
func Register(c Codec) {
	if c.SpecID() != SpecID {
		panic("is11.Register: SpecID must be " + SpecID + ", got " + c.SpecID())
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
		panic("is11.Default: no codec registered (forgot to blank-import internal/amwa/codec/is11/v10?)")
	}
	return all[len(all)-1]
}
