package probel

// Compliance event labels — per-spec named deviations the Probel
// consumer absorbs on the wire. Every constant here maps to one code
// path that tolerated a spec deviation without failing the operation
// (per memory/feedback_no_workaround.md §7–9: absorb silently = never;
// absorb + fire event = always; spec-section cited in the comment).
//
// Authoritative spec: assets/probel/probel-sw08p/SW-P-08 Issue 30.doc.
//
// The generic Profile counter lives in internal/protocol/compliance/.
// Classification:
//   - strict  : zero events fired this session
//   - partial : one or more events fired, all within tolerance
//
// Adding a new label is an API change — downstream tooling may
// aggregate by key.
const (
	// Spec §2 (Transmission Protocol): DATA field guaranteed portable
	// up to 128 bytes; 129-255 is "custom applications". Frames in
	// that window are sent but fire this event so operators see that
	// a peer may encounter the frame's size as non-portable.
	DataFieldOversize = "probel_data_field_oversize"

	// Spec §2: peer NAKed a frame we sent. The Send retries internally
	// up to the configured attempt ceiling; each NAK observed along
	// the way increments this counter. Informational.
	NAKReceived = "probel_nak_received"

	// Spec §2: peer did not emit DLE ACK within ACKTimeout. The Send
	// retries internally up to the configured attempt ceiling; each
	// ack-timeout along the way increments this counter. Informational.
	ACKTimeoutElapsed = "probel_ack_timeout_elapsed"

	// Spec §2: per-attempt retry fired. Fires alongside the NAKReceived
	// or ACKTimeoutElapsed that triggered it, so this counter matches
	// the sum of those minus any exhausted-then-failed send. Informational.
	RetryAttempted = "probel_retry_attempted"

	// Spec §2: reply frame arrived before peer's DLE ACK — the spec
	// mandates ACK first, but some matrices fold the two into one
	// reply-as-ack. Frame accepted; this counter lets operators
	// identify lax peers. Informational.
	ReplyWithoutACK = "probel_reply_without_ack"

	// Spec §3.4 (framing): inbound frame with bad checksum, bad byte
	// count, or malformed DLE stuffing. The reader emits DLE NAK to
	// the peer and drops the bytes. Informational — frequent desync
	// suggests a serial-line / MTU issue, not a protocol error.
	InboundFrameDecodeFailed = "probel_inbound_frame_decode_failed"
)
