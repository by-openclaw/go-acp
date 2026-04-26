// Package codec is the stdlib-only XML wire codec for the EVS Cerebrum
// Northbound API v0.13. It owns:
//
//   - Element-AST parser (case-insensitive — accepts the lowercase form
//     spec §2-§5 use AND the UPPERCASE form spec §4.1 examples + the
//     Skyline driver use).
//   - Typed encoders for every TX command in §2 (login / poll / action /
//     subscribe / obtain / unsubscribe / unsubscribe_all) and every
//     action body in §4 (routing / category / salvo / device).
//   - Typed accessors for every RX response (ack / nack / busy /
//     login_reply / poll_reply) and every event (§5.1 routing_change,
//     §5.2 category_change, §5.3 salvo_change, §5.4 device_change,
//     §5.5 datastore_change).
//   - The §6 NACK error-code table.
//
// The full element / attribute / enum catalogue is at
// `internal/cerebrum-nb/docs/keys.md` — this package's structs and
// constants mirror that catalogue 1:1.
//
// Library independence: stdlib-only, no acp/* imports. Lift-ready per
// `feedback_codec_isolation`.
//
// WebSocket transport lives in the sibling `codec/ws` package; this
// package emits / consumes only the XML payload bytes.
package codec
