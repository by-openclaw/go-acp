package probelsw02p

// Compliance event labels — per-spec named deviations the SW-P-02
// provider observes on inbound traffic. Same philosophy as the
// consumer side: absorb + fire event; never silently work around. See
// memory/feedback_no_workaround.md §7-9.
//
// Authoritative spec:
//
//	internal/probel-sw02p/assets/probel-sw02/SW-P-02_issue_26.txt
//
// The generic Profile counter lives in internal/protocol/compliance/.
// Aggregated across every accepted session since Serve started.
const (
	// Spec §3.1 (Framing): inbound frame with bad checksum. The session
	// drops the bytes and waits for the next SOM. Informational.
	InboundFrameDecodeFailed = "probel_sw02p_inbound_frame_decode_failed"

	// An inbound frame decoded cleanly but carried a CMD id we have
	// no handler for. Per spec this is legal — controllers may send
	// commands the matrix chose not to support. Informational —
	// frequent occurrences suggest a new command worth implementing.
	UnsupportedCommand = "probel_sw02p_unsupported_command"

	// A handler existed for the inbound cmd id but the per-command
	// Decode* helper rejected the MESSAGE (short payload, wrong
	// command id, etc.). Informational — a well-behaved controller
	// never emits these; repeated occurrences indicate wire corruption
	// or a peer with a divergent spec interpretation.
	HandlerDecodeFailed = "probel_sw02p_handler_decode_failed"

	// A handler ran successfully but emitting the reply failed because
	// the session's write returned an error. Non-fatal: the session
	// logs + drops; the server keeps accepting new connections.
	OutboundWriteFailed = "probel_sw02p_outbound_write_failed"

	// Fired once per slot when handleGo / handleGroupGo emits tx 04
	// CROSSPOINT CONNECTED on salvo commit. §3.2.8 / §3.2.37 both
	// specify "No individual CONNECTED messages are issued" and ask
	// controllers to re-INTERROGATE, but no real controller (Lawo VSM,
	// Commie) implements that listener path — both update their
	// tally UI exclusively from tx 04 broadcasts per §3.2.6. The
	// deviation fires once per emitted slot so every occurrence is
	// auditable in metrics and logs. See SW-P-08 issue #92 for the
	// precedent on the sibling protocol.
	SalvoEmittedConnected = "probel_sw02p_salvo_emitted_connected"
)
