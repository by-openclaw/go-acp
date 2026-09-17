package is10

import (
	"dhs/internal/amwa/codec/spec"
)

// SpecID is the AMWA NMOS catalogue slug for IS-10 (Authorization).
const SpecID = "is-10"

// ServiceAuth is the DNS-SD service type Authorization Servers
// advertise on (Discovery.md).
const ServiceAuth = "_nmos-auth._tcp"

// WellKnownPath is the RFC 8414 metadata suffix (Discovery.md). An
// api_selector TXT value, when present, is appended as a further path
// segment.
const WellKnownPath = "/.well-known/oauth-authorization-server"

// Codec is the IS-10 wire codec contract — one implementation per
// supported wire minor (v1.0, the only published one).
//
// Only the Authorization Server metadata document is versioned wire.
// Key sets and token verification are RFC 7515/7517/7519, which IS-10
// references rather than redefines, so they carry no NMOS minor and
// live in internal/auth; the NMOS claim policy that DOES belong to the
// spec lives in internal/amwa/session/auth, which — unlike a codec —
// is allowed to import it.
type Codec interface {
	spec.Versioned

	DecodeMetadata([]byte) (Metadata, error)
}

// versions is the per-process Registry of IS-10 codec implementations.
var versions = spec.NewRegistry[Codec]()

// Register installs a codec for one IS-10 wire minor.
func Register(c Codec) {
	if c.SpecID() != SpecID {
		panic("is10.Register: SpecID must be " + SpecID + ", got " + c.SpecID())
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
		panic("is10.Default: no codec registered (forgot to blank-import internal/amwa/codec/is10/v10?)")
	}
	return all[len(all)-1]
}
