// Package is12 implements the AMWA NMOS IS-12 Control Protocol
// codec. Stdlib-only, spec-strict per https://specs.amwa.tv/is-12/.
//
// Required versions per `internal/amwa/CLAUDE.md` "Versioning":
//
//   - v1.0 (latest patch v1.0.1) — single track. Wire layer is JSON
//     over WebSocket. The Node serves the IS-12 endpoint at the path
//     advertised in the IS-04 Device controls array
//     (`urn:x-nmos:control:ncp/v1.0`); the Controller dials it and
//     marshals MS-05-02 commands / notifications.
//
// Wire envelopes (six messageType discriminators):
//
//	0  Command               — controller -> node (method invocation)
//	1  CommandResponse       — node -> controller (paired by handle)
//	2  Notification          — node -> controller (subscribed events)
//	3  Subscription          — controller -> node (oid list)
//	4  SubscriptionResponse  — node -> controller (accepted oids)
//	5  Error                 — node -> controller (envelope-level)
//
// MS-05-02 datatype + class marshalling lives in the
// `internal/amwa/codec/ms05/` package (Step 9). is12 is the
// transport envelope only.
//
// This package follows the locked NMOS-wide codec pattern from
// `internal/amwa/codec/spec/`: canonical Go structs in this package,
// per-minor Strategy impls in vXX/. cmd/dhs/main.go blank-imports
// each minor to wire init()-time registration.
package is12
