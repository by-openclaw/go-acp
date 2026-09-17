package is08

import (
	"dhs/internal/amwa/codec/spec"
)

// SpecID is the AMWA NMOS catalogue slug for IS-08 (Audio Channel
// Mapping).
const SpecID = "is-08"

// Codec is the IS-08 wire codec contract — one implementation per
// supported wire minor (only v1.0 today). Each minor's vXX/
// subpackage embeds is08/ canonical types and gates fields per its
// spec text. Plugin code uses this interface; concrete impls are
// wired via init() at process start.
type Codec interface {
	spec.Versioned

	EncodeMapActive(MapActive) ([]byte, error)
	DecodeMapActive([]byte) (MapActive, error)
	ValidateMapActive(MapActive) error

	EncodeMapActivationRequest(MapActivationRequest) ([]byte, error)
	DecodeMapActivationRequest([]byte) (MapActivationRequest, error)
	ValidateMapActivationRequest(MapActivationRequest) error

	EncodeIO(IO) ([]byte, error)
	DecodeIO([]byte) (IO, error)
	ValidateIO(IO) error
}

// versions is the per-process Registry of IS-08 codec implementations.
var versions = spec.NewRegistry[Codec]()

// Register installs a codec for one IS-08 wire minor — same
// semantics as is04.Register / is05.Register / is07.Register.
// Idempotent for same instance; panics on conflict.
func Register(c Codec) {
	if c.SpecID() != SpecID {
		panic("is08.Register: SpecID must be " + SpecID + ", got " + c.SpecID())
	}
	versions.Register(c)
}

// Get / AllCodecs / SupportedVersions / SelectHighest / Default —
// same shape as the other NMOS specs.
func Get(apiVer string) (Codec, bool)            { return versions.Get(apiVer) }
func AllCodecs() []Codec                         { return versions.AllCodecs() }
func SupportedVersions() []string                { return versions.SupportedVersions() }
func SelectHighest(peer []string) (Codec, error) { return spec.SelectHighestMutual(versions, peer) }
func Default() Codec {
	all := versions.AllCodecs()
	if len(all) == 0 {
		panic("is08.Default: no codec registered (forgot to blank-import internal/amwa/codec/is08/vXX?)")
	}
	return all[len(all)-1]
}
