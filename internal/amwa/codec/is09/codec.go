package is09

import (
	"acp/internal/amwa/codec/spec"
)

// SpecID is the AMWA NMOS catalogue slug for IS-09 (System API).
const SpecID = "is-09"

// Codec is the IS-09 wire codec contract — one implementation per
// supported wire minor. IS-09 currently has a single stable minor
// (v1.0), so the registry holds one entry. Adding a future v1.1 = +1
// file in vXX/ + 1 init Register call; zero edits to this file.
//
// Per-resource methods cover the only resource IS-09 publishes — the
// Global config object served at `GET /global`.
//
// Implementations are stateless and safe for concurrent use.
type Codec interface {
	spec.Versioned

	EncodeGlobal(Global) ([]byte, error)
	DecodeGlobal([]byte) (Global, error)
	ValidateGlobal(Global) error
}

// versions is the per-process Registry of IS-09 codec implementations.
var versions = spec.NewRegistry[Codec]()

// Register installs a codec for one IS-09 wire minor. Called from each
// minor's init(). Idempotent for the same instance; panics on
// duplicate-different-instance or any empty SpecID/APIVer/SpecPatch
// — same semantics as is04.Register.
func Register(c Codec) {
	if c.SpecID() != SpecID {
		panic("is09.Register: SpecID must be " + SpecID + ", got " + c.SpecID())
	}
	versions.Register(c)
}

// Get returns the codec registered for the given APIVer (e.g. "v1.0"),
// or false when no such version is wired in.
func Get(apiVer string) (Codec, bool) {
	return versions.Get(apiVer)
}

// AllCodecs returns every registered IS-09 codec sorted by APIVer
// ascending.
func AllCodecs() []Codec {
	return versions.AllCodecs()
}

// SupportedVersions returns every registered APIVer string in ascending
// order — fed verbatim into the DNS-SD `api_ver` TXT comma-list for
// `_nmos-system._tcp` records.
func SupportedVersions() []string {
	return versions.SupportedVersions()
}

// SelectHighest picks the highest version mutually supported between
// us and a peer's advertised list.
func SelectHighest(peerVersions []string) (Codec, error) {
	return spec.SelectHighestMutual(versions, peerVersions)
}

// Default returns the highest registered IS-09 codec — used by call
// sites that don't yet route per-version. Panics if no codec is
// registered.
func Default() Codec {
	all := versions.AllCodecs()
	if len(all) == 0 {
		panic("is09.Default: no codec registered (forgot to blank-import internal/amwa/codec/is09/v10?)")
	}
	return all[len(all)-1]
}
