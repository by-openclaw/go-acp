package probelsw02p

// Compliance event labels — per-spec named deviations the SW-P-02
// consumer absorbs on the wire. Every constant here maps to one code
// path that tolerated a spec deviation without failing the operation
// (per memory/feedback_no_workaround.md §7-9: absorb silently = never;
// absorb + fire event = always; spec-section cited in the comment).
//
// Authoritative spec:
//
//	internal/probel-sw08p/assets/probel-sw02/SW-P-02_issue_26.txt
//
// The generic Profile counter lives in internal/protocol/compliance/.
// Classification:
//   - strict  : zero events fired this session
//   - partial : one or more events fired, all within tolerance
//
// Adding a new label is an API change — downstream tooling may
// aggregate by key. Per-command files in follow-up commits extend this
// list with command-specific deviations.
const (
	// Spec §3.1 (Framing): inbound frame with bad checksum. The reader
	// drops the byte and resynchronises on the next SOM (0xFF).
	// Informational — frequent desync suggests a bad physical link.
	InboundFrameDecodeFailed = "probel_sw02p_inbound_frame_decode_failed"
)
