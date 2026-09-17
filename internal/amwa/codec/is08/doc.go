// Package is08 implements the AMWA NMOS IS-08 Audio Channel Mapping
// codec. Stdlib-only, spec-strict per https://specs.amwa.tv/is-08/.
//
// Required versions per `internal/amwa/CLAUDE.md` "Versioning":
//
//   - v1.0 (latest patch v1.0.1) — single track. Wire layer is HTTP /
//     JSON REST under `/x-nmos/channelmapping/v1.0/...`. No separate
//     transport — the Node serves the Channel Mapping API; the
//     Controller polls it and uses POST /map/activations to apply
//     re-routes either immediately or on a TAI schedule.
//
// Resources covered (every schema bundled at testdata/schemas/v1.0.1):
//
//	IO                       — full /map/io view (inputs + outputs)
//	InputProperties / Caps / Channels / Parent
//	OutputProperties / Caps / Channels / SourceID
//	MapActive                — the active channel-routing map
//	MapActivationRequest     — POST /map/activations body
//	MapActivationResponse    — GET /map/activations[/{id}]
//	Activation / ActivationResponse
//	MapEntries               — { outputID: { channelIdxStr: MapEntry } }
//	MapEntry                 — { input *string, channel_index *int }
//	ErrorBody                — 4xx/5xx response shape
//
// This package follows the locked NMOS-wide codec pattern from
// `internal/amwa/codec/spec/`: canonical Go structs in this package,
// per-minor Strategy impls in vXX/. cmd/dhs/main.go blank-imports
// each minor to wire init()-time registration.
package is08
