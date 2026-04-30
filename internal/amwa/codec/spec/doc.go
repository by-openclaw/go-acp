// Package spec is the NMOS-wide codec base — the cross-cutting contract
// every AMWA NMOS specification implementation satisfies.
//
// NMOS is a suite of ~14 specifications (IS-04, IS-05, IS-07, IS-08,
// IS-09, IS-12, MS-05-01, MS-05-02, plus the BCP-* JSON-shape
// validators) that collectively define the AMWA Networked Media Open
// Specifications stack. Every spec has multiple stable versions that
// MUST coexist on the wire (DNS-SD `api_ver` TXT advertises every
// supported minor; URL trees serve every minor in parallel). The
// dhs NMOS plugin is required to implement every track listed in
// `internal/amwa/CLAUDE.md` "Versioning" and `internal/amwa/reference.md`.
//
// To keep the cost of supporting every current AND future version
// tractable, every NMOS codec follows the same pattern locked in this
// package:
//
//   - One canonical Go struct per resource type (union of every
//     minor's fields, tagged json:",omitempty"). Stored once in the
//     spec's package (e.g. is04.Node, is05.StagedSender).
//
//   - One Codec interface per spec, extending [Versioned] with the
//     spec's encode / decode / validate methods.
//
//   - One concrete Codec impl per minor, in vXX/ subpackages. Each
//     impl is a thin Strategy: it gates which canonical fields appear
//     on its wire and which validation rules apply. Adding a new
//     minor (e.g. IS-04 v1.4 when AMWA ships it) is +1 file +
//     1 init-time Register call — zero edits to existing code.
//
//   - One per-spec Registry[T] (instantiated from this package's
//     generic helper), populated at process start by each minor's
//     init(). Plugin code looks up codecs through the interface,
//     never through concrete versions.
//
//   - Per-spec [SelectHighestMutual] consults the registry to
//     pick the highest version mutually supported between us and a
//     peer. Peers that advertise a version we don't implement, OR vice
//     versa, fire a [ComplianceEvent].
//
// This package itself is stdlib-only (per the layer-1 codec rule
// from `internal/amwa/CLAUDE.md`). No spec-specific knowledge leaks
// here — only the contract.
//
// # Idempotence
//
// Every public function is referentially transparent given the same
// input. [Registry.Register] is the one stateful operation; calling it
// repeatedly with the same Versioned instance is a no-op. Registering
// a different instance under the same (SpecID, APIVer) key is a
// programming error and panics — same semantics as
// [internal/protocol].Register.
package spec
