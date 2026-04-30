// Package is07 implements the AMWA NMOS IS-07 Event & Tally codec.
// Stdlib-only, spec-strict per https://specs.amwa.tv/is-07/.
//
// Required versions per `internal/amwa/CLAUDE.md` "Versioning":
//
//   - v1.0 (latest patch v1.0.1) — single track. Wire layer is JSON
//     over WebSocket OR MQTT. The Node serves the Events API
//     (`/x-nmos/events/v1.0/...`) over HTTP and the WebSocket / MQTT
//     transport in parallel; transport selection is signalled in the
//     IS-04 Sender's `transport` URN and IS-05
//     `connection_uri` / `broker_*` parameters.
//
// Wire envelopes (sender → receiver):
//
//	state              — EventBoolean / EventNumber / EventString / EventObject
//	health             — heartbeat response
//	reboot / shutdown  — controlled shutdown notifications
//	connection_status  — MQTT-only Will/announce message
//
// Wire envelopes (receiver → sender):
//
//	health        — heartbeat probe
//	subscription  — subscribe to source_id list
//
// Type descriptors (REST `/sources/{id}/type`):
//
//	type_boolean / type_number / type_string  — base types
//	type_boolean_enum / type_number_enum
//	  / type_string_enum                       — enumerated variants
//
// This package follows the locked NMOS-wide codec pattern from
// `internal/amwa/codec/spec/`: canonical Go structs in this package,
// per-minor Strategy impls in vXX/. cmd/dhs/main.go blank-imports
// each minor to wire init()-time registration.
package is07
