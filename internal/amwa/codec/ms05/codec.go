package ms05

import (
	"dhs/internal/amwa/codec/spec"
)

// SpecID is the AMWA NMOS catalogue slug for MS-05-02 (Control
// Framework). The MS-05-01 architecture document carries no wire
// format — it informs the framework but doesn't extend the codec
// surface.
const SpecID = "ms-05-02"

// Codec is the MS-05-02 marshalling contract — encode / decode /
// validate every datatype + class. Implementations satisfy the
// spec.Versioned interface so they participate in the cross-spec
// version-selection machinery.
//
// The interface is deliberately minimal: callers reach into the
// concrete types via Go directly. The wire envelope for IS-12
// passes value bytes through json.RawMessage; any caller who needs
// to deserialise into a specific type does so with `json.Unmarshal`
// directly — there's no need for hundreds of typed helper methods.
type Codec interface {
	spec.Versioned
}

var versions = spec.NewRegistry[Codec]()

// Register installs a codec for one MS-05-02 wire minor.
func Register(c Codec) {
	if c.SpecID() != SpecID {
		panic("ms05.Register: SpecID must be " + SpecID + ", got " + c.SpecID())
	}
	versions.Register(c)
}

func Get(apiVer string) (Codec, bool)            { return versions.Get(apiVer) }
func AllCodecs() []Codec                         { return versions.AllCodecs() }
func SupportedVersions() []string                { return versions.SupportedVersions() }
func SelectHighest(peer []string) (Codec, error) { return spec.SelectHighestMutual(versions, peer) }
func Default() Codec {
	all := versions.AllCodecs()
	if len(all) == 0 {
		panic("ms05.Default: no codec registered (forgot to blank-import internal/amwa/codec/ms05/vXX?)")
	}
	return all[len(all)-1]
}
