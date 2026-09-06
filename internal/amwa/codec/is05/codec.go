package is05

import (
	"dhs/internal/amwa/codec/spec"
)

// SpecID is the AMWA NMOS catalogue slug for IS-05 (Connection
// Management).
const SpecID = "is-05"

// Codec is the IS-05 wire codec contract — one implementation per
// supported wire minor (v1.0 + v1.1 today). Each minor's vXX/
// subpackage embeds is05/ canonical types and gates fields per its
// spec text. Plugin code uses this interface; concrete impls are
// wired via init() at process start.
type Codec interface {
	spec.Versioned

	EncodeStagedSender(StagedSender) ([]byte, error)
	DecodeStagedSender([]byte) (StagedSender, error)
	ValidateStagedSender(StagedSender) error

	EncodeStagedReceiver(StagedReceiver) ([]byte, error)
	DecodeStagedReceiver([]byte) (StagedReceiver, error)
	ValidateStagedReceiver(StagedReceiver) error
}

// versions is the per-process Registry of IS-05 codec implementations.
var versions = spec.NewRegistry[Codec]()

// Register installs a codec for one IS-05 wire minor — same semantics
// as is04.Register / is09.Register. Idempotent for same instance;
// panics on conflict.
func Register(c Codec) {
	if c.SpecID() != SpecID {
		panic("is05.Register: SpecID must be " + SpecID + ", got " + c.SpecID())
	}
	versions.Register(c)
}

// Get / AllCodecs / SupportedVersions / SelectHighest / Default —
// same shape as is04 / is09.
func Get(apiVer string) (Codec, bool)            { return versions.Get(apiVer) }
func AllCodecs() []Codec                         { return versions.AllCodecs() }
func SupportedVersions() []string                { return versions.SupportedVersions() }
func SelectHighest(peer []string) (Codec, error) { return spec.SelectHighestMutual(versions, peer) }
func Default() Codec {
	all := versions.AllCodecs()
	if len(all) == 0 {
		panic("is05.Default: no codec registered (forgot to blank-import internal/amwa/codec/is05/vXX?)")
	}
	return all[len(all)-1]
}
