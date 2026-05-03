package is04

import (
	"dhs/internal/amwa/codec/spec"
)

// SpecID is the AMWA NMOS catalogue slug for IS-04. Every codec
// registered under this package returns this value from SpecID().
const SpecID = "is-04"

// Codec is the IS-04 wire codec contract — one implementation per
// supported wire minor (v1.1 / v1.2 / v1.3 today; v1.4 / v2.0 in the
// future). Every implementation satisfies [spec.Versioned] so the
// shared infrastructure (DNS-SD api_ver advertisement, peer-version
// selection, compliance event reporting) works uniformly across every
// NMOS spec.
//
// Per-resource methods come in three groups:
//
//   - EncodeXxx — serialise the canonical struct into the JSON shape
//     for the codec's wire minor. Fields that don't exist in this
//     minor are omitted (omitempty + per-codec field gating).
//
//   - DecodeXxx — parse JSON returned by a peer of this wire minor
//     into the canonical struct. Unknown fields are rejected
//     (DisallowUnknownFields) — peers sending fields that don't
//     belong on this minor's wire fire a compliance event.
//
//   - ValidateXxx — apply the minor's required-field set, regex
//     patterns, and URN enums. Returns nil on conformance, a typed
//     error otherwise. Validate runs on both encode and decode paths
//     so we never emit a non-compliant payload to a peer.
//
// Implementations are stateless and safe for concurrent use.
type Codec interface {
	spec.Versioned

	EncodeNode(Node) ([]byte, error)
	DecodeNode([]byte) (Node, error)
	ValidateNode(Node) error

	EncodeDevice(Device) ([]byte, error)
	DecodeDevice([]byte) (Device, error)
	ValidateDevice(Device) error

	EncodeSource(Source) ([]byte, error)
	DecodeSource([]byte) (Source, error)
	ValidateSource(Source) error

	EncodeFlow(Flow) ([]byte, error)
	DecodeFlow([]byte) (Flow, error)
	ValidateFlow(Flow) error

	EncodeSender(Sender) ([]byte, error)
	DecodeSender([]byte) (Sender, error)
	ValidateSender(Sender) error

	EncodeReceiver(Receiver) ([]byte, error)
	DecodeReceiver([]byte) (Receiver, error)
	ValidateReceiver(Receiver) error
}

// versions is the per-process Registry of IS-04 codec implementations.
// Each minor's vXX/ subpackage calls Register exactly once from init();
// cmd/dhs/main.go blank-imports those packages to wire registration.
var versions = spec.NewRegistry[Codec]()

// Register installs a codec for one IS-04 wire minor. Called from
// vXX.init(). Idempotent — safe to call repeatedly with the same
// instance for the same APIVer; panics on duplicate-different-instance
// or any empty field. Same semantics as [spec.Registry.Register].
//
// Registering a codec whose SpecID is not [SpecID] panics.
func Register(c Codec) {
	if c.SpecID() != SpecID {
		panic("is04.Register: SpecID must be " + SpecID + ", got " + c.SpecID())
	}
	versions.Register(c)
}

// Get returns the codec registered for the given APIVer (e.g. "v1.3"),
// or false when no such version is wired in. Plugin code uses this to
// dispatch per-URL-tree requests to the right encoder.
func Get(apiVer string) (Codec, bool) {
	return versions.Get(apiVer)
}

// AllCodecs returns every registered IS-04 codec sorted by APIVer
// ascending (e.g. v1.1, v1.2, v1.3). Plugin code uses this to install
// per-minor URL routes on the same Registry / Node HTTP face.
func AllCodecs() []Codec {
	return versions.AllCodecs()
}

// SupportedVersions returns every registered APIVer string in
// ascending order — fed verbatim into the DNS-SD `api_ver` TXT
// comma-list. Returns an empty slice when no codecs are wired (test
// environments without blank-imports).
func SupportedVersions() []string {
	return versions.SupportedVersions()
}

// SelectHighest picks the highest version mutually supported between
// us and a peer's advertised list. Returns [spec.ErrNoCommonVersion]
// when the intersection is empty — caller fires a compliance event
// and refuses the peer (never silently downgrade).
func SelectHighest(peerVersions []string) (Codec, error) {
	return spec.SelectHighestMutual(versions, peerVersions)
}

// Default returns the highest registered IS-04 codec — used by
// in-process call sites that don't yet route per-version (e.g.
// internal helper packages, fixture builders). Plugin layer code
// MUST use [Get] / [AllCodecs] / [SelectHighest] instead and route
// per peer-advertised version.
//
// Panics if no codec is registered. Intended to make missing
// blank-imports loudly visible at process start.
func Default() Codec {
	all := versions.AllCodecs()
	if len(all) == 0 {
		panic("is04.Default: no codec registered (forgot to blank-import internal/amwa/codec/is04/vXX?)")
	}
	return all[len(all)-1]
}
