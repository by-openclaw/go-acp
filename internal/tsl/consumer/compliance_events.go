package tsl

import "dhs/internal/tsl/codec"

// Compliance event labels — per-spec named deviations the TSL consumer
// surfaces from the codec's ComplianceNote stream into the
// compliance.Profile event log. Every label here mirrors a documented
// row in internal/tsl/CLAUDE.md "Compliance events" and a Note kind
// the codec already emits during Decode.
//
// Authoritative spec: internal/tsl/assets/tsl-umd-consumer.txt.
//
// The generic Profile counter lives in internal/consumer/compliance/.
// Classification:
//   - strict  : zero events fired this session
//   - partial : one or more events fired, all within tolerance
//
// Adding a new label is an API change — downstream tooling may
// aggregate by key. Keep this list stable + documented in
// internal/tsl/CLAUDE.md "Compliance events".
const (
	// v3.1 CTRL bit 6 is "reserved (clear on tx)"; on rx, a frame with
	// the bit set is decoded normally and this event is fired so
	// aggregate counts can flag flaky producers. CLAUDE.md row 1.
	ReservedBitSet = "tsl_reserved_bit_set"

	// v4.0 VBC encodes a minor version (bits 6-4). v4.0 producers MUST
	// emit minor=0; any non-zero value indicates a future micro-revision
	// the consumer does not understand. Frame still decodes (XDATA is
	// length-prefixed) and this event fires. CLAUDE.md row 2.
	VersionMismatch = "tsl_version_mismatch"

	// v4.0 CHKSUM is a 7-bit two's-complement of the v3.1 prefix. A
	// mismatch is surfaced (frame body still parses; consumer caller
	// chooses whether to drop). CLAUDE.md row 3.
	ChecksumFail = "tsl_checksum_fail"

	// v4.0 CTRL.6 = 1 ("control data") and v5.0 CONTROL.15 = 1
	// ("control data flag") are documented but spec-undefined in the
	// current revision. Consumer treats the payload as opaque + fires.
	// CLAUDE.md row 4.
	ControlDataUndefined = "tsl_control_data_undefined"

	// A v5.0 DMSG arrives for an INDEX the consumer has not modelled
	// (no display config row, no prior packet). Decode succeeds but the
	// caller cannot map the frame to a display. CLAUDE.md row 5.
	UnknownDisplayIndex = "tsl_unknown_display_index"

	// v5.0 SCREEN=0xFFFF or DMSG INDEX=0xFFFF is a broadcast value per
	// spec §5.0. Decode succeeds; this event fires so the operator can
	// observe broadcast cadence. CLAUDE.md row 6.
	BroadcastReceived = "tsl_broadcast_received"

	// v5.0 frames marked UTF-16LE in FLAGS bit 0 are decoded then
	// transcoded to UTF-8 for the consumer-side string surface. Fired
	// once per transcode so callers can audit charset traffic.
	// CLAUDE.md row 7.
	CharsetTranscode = "tsl_charset_transcode"

	// v3.1 / v4.0 frame received with a DATA segment whose length is
	// not the spec-mandated 16 bytes. Decoder accepts what it has and
	// fires. CLAUDE.md row 8.
	LabelLengthMismatch = "tsl_label_length_mismatch"

	// v3.1 producers MUST space-pad (0x20) DATA to 16 bytes. Some
	// producers null-pad (0x00) — frame still decodes; this event
	// fires. CLAUDE.md row 9.
	V31NullPad = "tsl_v31_null_pad"

	// v5.0 over TCP is spec-required to use the DLE/STX wrapper
	// (§Phy). A packet arriving on a TCP listener WITHOUT the wrapper
	// is decoded as a best effort + fired. CLAUDE.md row 10.
	V5TcpUnwrapped = "tsl_v5_tcp_unwrapped"
)

// EventForNote maps a codec.ComplianceNote.Kind back to the consumer-
// layer compliance event constant declared above. Returns "" when the
// note kind is unknown (caller should not fire — a no-op event slot
// here means the codec exposed a deviation the consumer hasn't lifted
// yet, which is itself worth noticing). Exposed so unit tests can
// assert mapping correctness without standing up a session.
func EventForNote(n codec.ComplianceNote) string {
	switch n.Kind {
	case ReservedBitSet,
		VersionMismatch,
		ChecksumFail,
		ControlDataUndefined,
		UnknownDisplayIndex,
		BroadcastReceived,
		CharsetTranscode,
		LabelLengthMismatch,
		V31NullPad,
		V5TcpUnwrapped:
		return n.Kind
	}
	return ""
}
