package probelsw02p

// Compliance event labels — per-spec named deviations the SW-P-02
// provider observes on inbound traffic. Same philosophy as the
// consumer side: absorb + fire event; never silently work around. See
// memory/feedback_no_workaround.md §7-9.
//
// Authoritative spec:
//
//	internal/probel-sw08p/assets/probel-sw02/SW-P-02_issue_26.txt
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
)
