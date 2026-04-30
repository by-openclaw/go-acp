package is12

import (
	"acp/internal/amwa/codec/spec"
)

// SpecID is the AMWA NMOS catalogue slug for IS-12 (Control Protocol).
const SpecID = "is-12"

// Codec is the IS-12 wire codec contract — one implementation per
// supported wire minor (only v1.0 today).
type Codec interface {
	spec.Versioned

	Encode(Message) ([]byte, error)
	Decode([]byte) (Message, error)
}

var versions = spec.NewRegistry[Codec]()

// Register installs a codec for one IS-12 wire minor.
func Register(c Codec) {
	if c.SpecID() != SpecID {
		panic("is12.Register: SpecID must be " + SpecID + ", got " + c.SpecID())
	}
	versions.Register(c)
}

func Get(apiVer string) (Codec, bool)             { return versions.Get(apiVer) }
func AllCodecs() []Codec                          { return versions.AllCodecs() }
func SupportedVersions() []string                 { return versions.SupportedVersions() }
func SelectHighest(peer []string) (Codec, error)  { return spec.SelectHighestMutual(versions, peer) }
func Default() Codec {
	all := versions.AllCodecs()
	if len(all) == 0 {
		panic("is12.Default: no codec registered (forgot to blank-import internal/amwa/codec/is12/vXX?)")
	}
	return all[len(all)-1]
}
