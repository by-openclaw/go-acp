// Package events implements the AMWA NMOS IS-07 transport layer:
//
//   - WebSocket Publisher (Node side) — accepts upgrade requests at
//     a configurable path, manages per-client subscription sets,
//     fans out state events to subscribed sources, and replies to
//     command_health probes with health envelopes.
//   - WebSocket Subscriber (Controller side) — dials a Node, sends
//     command_subscription, decodes incoming Message frames, and
//     periodically emits command_health probes.
//
// Layer assignment per `internal/amwa/docs/dependencies.md`:
//
//	LAYER 2 — SESSION
//	  internal/amwa/session/events/   (this package)
//
// Allowed imports: stdlib, internal/amwa/codec/*, internal/amwa/session/http
// (for WebSocket primitives), internal/consumer/compliance,
// internal/metrics. Never imports any plugin layer.
//
// MQTT bridging is intentionally out of scope here — IS-07 v1.0.1
// permits selecting WS or MQTT per Sender; an MQTT bridge can layer
// onto Publisher.Publish() in a follow-up package
// (`internal/amwa/session/events/mqtt`) without changing this API.
package events
