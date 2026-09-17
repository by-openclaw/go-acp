// Package ws is a stdlib-only RFC 6455 WebSocket, both ends.
//
// It began as a client for the EVS Cerebrum Northbound API (XML over text
// frames) and grew a server when the AMWA Query API's subscription
// WebSocket needed the same code rather than a second implementation.
// Scope today:
//
//   - Client (Dial) AND server (Accept) — one framing layer, one idle
//     bound, one Ping/Pong path for both.
//
//   - Text + Close + Ping + Pong opcodes — Binary accepted but never
//     produced.
//
//   - Single frame on TX; fragmentation accepted on RX.
//
//   - Sub-protocol echoed on Accept via AcceptOptions.Subprotocol; never
//     offered on Dial.
//
//   - No permessage-deflate (RFC 7692), deliberately. Extensions are
//     negotiated, so declining is spec-correct: Dial never offers it and
//     Accept never echoes it, and a peer that compresses anyway is
//     rejected by the reserved-bits check in readFrame rather than
//     silently mis-decoded.
//
//     Worth building if a capture ever shows a peer OFFERING it — our
//     payloads are XML and JSON, which compress 5-10x, and compress/flate
//     is stdlib so it costs no dependency. It would want
//     no_context_takeover: the default mode holds a ~64 KB sliding window
//     per connection per direction, which buys bandwidth by spending the
//     memory footprint this lib exists to keep small. Parked until a peer
//     asks.
//
// Lift-ready per the project's codec-isolation rule: imports stdlib
// only and never touches `dhs/*` symbols.
//
// Spec: RFC 6455 (https://www.rfc-editor.org/rfc/rfc6455).
package ws
