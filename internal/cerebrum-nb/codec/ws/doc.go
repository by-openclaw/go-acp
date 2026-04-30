// Package ws is a stdlib-only RFC 6455 WebSocket client.
//
// Scope is intentionally narrow: just enough to drive the EVS Cerebrum
// Northbound API (XML over WebSocket text frames). Consequences:
//
//   - Client only — no server / Upgrader.
//   - Text + Close + Ping + Pong opcodes only — Binary accepted but
//     never produced.
//   - Single frame on TX; fragmentation accepted on RX.
//   - No permessage-deflate; spec doesn't use it.
//   - No sub-protocol negotiation.
//
// Lift-ready per the project's codec-isolation rule: imports stdlib
// only and never touches `acp/*` symbols.
//
// Spec: RFC 6455 (https://www.rfc-editor.org/rfc/rfc6455).
package ws
