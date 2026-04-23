package probelsw08p

// Compliance event labels — per-spec named deviations the Probel
// provider observes on inbound traffic. Same philosophy as the
// consumer side (internal/protocol/probel/compliance_events.go):
// absorb + fire event; never silently work around. See
// memory/feedback_no_workaround.md §7–9.
//
// Authoritative spec: internal/probel-sw08p/assets/probel-sw08p/SW-P-08 Issue 30.doc.
//
// The generic Profile counter lives in internal/protocol/compliance/.
// Aggregated across every accepted session since Serve started.
//
// Adding a new label is an API change — downstream tooling may
// aggregate by key.
const (
	// Spec §3.4 (framing): inbound frame with bad checksum, bad byte
	// count, or malformed DLE stuffing. The session emits DLE NAK to
	// the peer and drops the bytes. Informational — frequent desync
	// suggests a bad physical link on the controller side.
	InboundFrameDecodeFailed = "probel_inbound_frame_decode_failed"

	// Handler returned an error — dst/src out of range, unknown
	// matrix/level, or an attempt to override an owned protect. The
	// session does NOT reply (SW-P-08 has no per-command error reply
	// for most commands); the controller learns via timeout. The
	// error is logged and this event fires. Informational.
	HandlerRejected = "probel_handler_rejected"

	// A tally fan-out to a peer session failed on the write side
	// (e.g. peer socket closed mid-broadcast). The server logs and
	// continues broadcasting to remaining peers; fires one event per
	// failure. Informational.
	TallyBroadcastFailed = "probel_tally_broadcast_failed"

	// An inbound frame decoded cleanly but carried a CMD id we have
	// no handler for. Per spec this is legal — controllers may send
	// commands the matrix chose not to support; we ACK at the framing
	// layer and silently ignore the payload. Informational — frequent
	// occurrences suggest a new command worth implementing.
	UnsupportedCommand = "probel_unsupported_command"

	// Deliberate interpretation of §3.2.3 over §3.2.30: on rx 121 Set,
	// the matrix fans out one tx 004 Crosspoint Connected per applied
	// slot to every session. §3.2.3 states cmd 04 is "issued
	// spontaneously by the controller on all ports after confirmation
	// that a route has been made" — which we honour for the salvo path
	// too, matching observed XD/ECLIPSE behaviour and the de-facto
	// contract real SW-P-08 controllers (VSM, Commie) rely on to keep
	// their tally UI in sync. §3.2.30's "No individual CONNECTED
	// messages" advice is unreachable in practice because listeners
	// don't implement the cmd 122/123 tally-tracking path it names.
	// Fires once per applied slot; every firing is logged and countable.
	SalvoEmittedConnected = "probel_salvo_emitted_connected"
)
