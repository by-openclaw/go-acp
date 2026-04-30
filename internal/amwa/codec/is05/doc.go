// Package is05 implements the AMWA NMOS IS-05 Connection Management
// codec. Stdlib-only, spec-strict per
// https://specs.amwa.tv/is-05/.
//
// Required versions per `internal/amwa/CLAUDE.md` "Versioning":
//   - v1.1 (latest patch v1.1.2) — adds bulk endpoints + extended
//     transport_params for ST 2022-7 / mux / WebSocket / MQTT /
//     SMPTE 2022-1.
//   - v1.0 (latest patch v1.0.2) — RTP-only single endpoints, no
//     bulk operations.
//
// Resources covered:
//
//   StagedSender   — target state being staged, then activated
//   StagedReceiver — same on Receiver side
//   ActiveSender   — read-only mirror of currently-running Sender state
//   ActiveReceiver — same on Receiver side
//   Activation     — sub-object on staged that triggers the
//                    activate_immediate / activate_scheduled_relative
//                    / activate_scheduled_absolute mode
//   TransportParams — polymorphic per transport URN (rtp / rtp.mcast /
//                    rtp.ucast / dash / websocket / mqtt). v1.0 ships
//                    rtp + rtp.mcast + rtp.ucast only; v1.1 adds the
//                    rest.
//
// This package follows the locked NMOS-wide codec pattern from
// `internal/amwa/codec/spec/`: canonical Go structs in this package,
// per-minor Strategy impls in vXX/. cmd/dhs/main.go blank-imports
// each minor to wire init()-time registration.
package is05
