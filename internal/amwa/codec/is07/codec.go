package is07

import (
	"dhs/internal/amwa/codec/spec"
)

// SpecID is the AMWA NMOS catalogue slug for IS-07 (Event & Tally).
const SpecID = "is-07"

// Codec is the IS-07 wire codec contract — one implementation per
// supported wire minor (only v1.0 today). Each minor's vXX/
// subpackage embeds is07/ canonical types and gates fields per its
// spec text. Plugin code uses this interface; concrete impls are
// wired via init() at process start.
type Codec interface {
	spec.Versioned

	EncodeMessage(Message) ([]byte, error)
	DecodeMessage([]byte) (Message, error)

	EncodeCommand(Command) ([]byte, error)
	DecodeCommand([]byte) (Command, error)
}

// versions is the per-process Registry of IS-07 codec implementations.
var versions = spec.NewRegistry[Codec]()

// Register installs a codec for one IS-07 wire minor — same
// semantics as is04.Register / is05.Register / is09.Register.
// Idempotent for same instance; panics on conflict.
func Register(c Codec) {
	if c.SpecID() != SpecID {
		panic("is07.Register: SpecID must be " + SpecID + ", got " + c.SpecID())
	}
	versions.Register(c)
}

// Get / AllCodecs / SupportedVersions / SelectHighest / Default —
// same shape as is04 / is05 / is09.
func Get(apiVer string) (Codec, bool)             { return versions.Get(apiVer) }
func AllCodecs() []Codec                          { return versions.AllCodecs() }
func SupportedVersions() []string                 { return versions.SupportedVersions() }
func SelectHighest(peer []string) (Codec, error)  { return spec.SelectHighestMutual(versions, peer) }
func Default() Codec {
	all := versions.AllCodecs()
	if len(all) == 0 {
		panic("is07.Default: no codec registered (forgot to blank-import internal/amwa/codec/is07/vXX?)")
	}
	return all[len(all)-1]
}
