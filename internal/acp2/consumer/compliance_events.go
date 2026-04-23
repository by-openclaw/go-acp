package acp2

// Compliance event labels — per-spec named deviations the ACP2
// consumer absorbs on the wire. Every constant here maps to one code
// path that tolerated a device defect without failing the operation
// (per memory/feedback_no_workaround.md §7–9: absorb silently = never;
// absorb + fire event = always; spec-page cited in the comment).
//
// Authoritative specs:
//   - internal/acp2/assets/acp2_protocol.pdf  (ACP2 framing, property headers,
//     error codes, object types)
//   - internal/acp2/assets/an2_protocol.pdf   (AN2 transport, magic, frame
//     layout, ProtocolEvents handshake)
//
// The generic Profile counter lives in internal/protocol/compliance/.
// Classification:
//   - strict  : zero events fired this session
//   - partial : one or more events fired, all within tolerance
//
// Adding a new label is an API change — downstream tooling may
// aggregate by key. Keep this list stable + documented in
// internal/acp2/docs/consumer.md §"Compliance events".
const (
	// AN2 spec §"Frame Header" (an2_protocol.pdf p.3): magic is
	// 0xC635 on every frame. When the first two bytes received do
	// not match, the framer drops the frame, resyncs, and fires this
	// event. Recoverable — the TCP byte stream realigns on the next
	// valid magic. Informational.
	AN2MagicMismatch = "acp2_an2_magic_mismatch"

	// AN2 spec §"Frame Header": dlen is a u16 payload length
	// (excluding the 8-byte header). If a frame is received with
	// fewer payload bytes than dlen claims (short-read before socket
	// closed, or truncated transmission), we fire this and drop the
	// partial payload. Informational.
	AN2ShortPayload = "acp2_an2_short_payload"

	// AN2 spec §"Protocol Events": before any ACP2 announces can
	// flow, the consumer MUST call AN2 EnableProtocolEvents([2]) per
	// the spec §"Connection Setup" handshake. Firing this event means
	// the consumer received an ACP2 type=2 (announce) WITHOUT having
	// sent EnableProtocolEvents in the session — a provider bug.
	// Consumer still decodes the announce. Informational.
	AnnounceBeforeEnableEvents = "acp2_announce_before_enable_events"

	// ACP2 spec §"Message Header" (acp2_protocol.pdf p.4): mtid=0 is
	// reserved for announces + events, mtid 1..255 for request/reply
	// correlation. If a reply arrives with mtid=0, we drop it (no
	// waiter possible) and fire. Provider bug. Informational.
	ReplyZeroMtid = "acp2_reply_zero_mtid"

	// Spec p.5 §"Error Status Codes": errors use type=3 with stat
	// 0..5. Receiving a stat=0 (protocol error) on an otherwise
	// well-formed request indicates a provider-side parsing defect.
	// The consumer surfaces the error to the caller and fires this
	// so aggregate counts can flag flaky firmware.
	ProtocolErrorReceived = "acp2_protocol_error_received"

	// Spec p.5: stat=1 invalid obj-id. Fired when a GetObject /
	// GetProperty / SetProperty targets an obj-id the device does
	// not host. Usually means a stale walked tree (firmware reboot
	// renumbered objects) — caller retries after a fresh Walk.
	InvalidObjectReceived = "acp2_invalid_object_received"

	// Spec p.5: stat=2 invalid idx. Fired when a preset idx is out
	// of the range declared by pid=7 (preset_depth). Consumer
	// surfaces the error; no retry. Informational.
	InvalidIndexReceived = "acp2_invalid_index_received"

	// Spec p.5: stat=3 invalid pid. The device claims the requested
	// property does not exist on the target object. In practice this
	// fires when a freshly-walked tree advertises a pid that the
	// firmware has since removed / renamed. Informational.
	InvalidPidReceived = "acp2_invalid_pid_received"

	// Spec p.5: stat=4 no access. SetProperty rejected because the
	// access mask (pid=3) does not permit writes. Fired at the event
	// level so policy / UI layers can report gracefully without each
	// caller string-matching the error.
	AccessDeniedReceived = "acp2_access_denied_received"

	// Spec p.5: stat=5 invalid value. Fired when a SetProperty value
	// is out of the object's min/max range, violates the step, or is
	// not in an enum's options set. Informational.
	InvalidValueReceived = "acp2_invalid_value_received"

	// Spec p.6 §"Property Header": plen is a u16 byte count including
	// the 4-byte header but EXCLUDING alignment padding. After each
	// property, skip (4 - (plen % 4)) % 4 bytes. Fired when a
	// property header's plen would read past the enclosing message
	// payload — framing inconsistency. Consumer stops parsing the
	// message + fires. Informational.
	PropertyLengthOverrun = "acp2_property_length_overrun"

	// Spec p.6: every property has a PID in range [1,20]. Fired when
	// a get_object reply carries a PID outside this set (vendor-
	// private extension). Consumer skips the property + fires.
	// Informational — does not break the walk.
	UnknownPropertyID = "acp2_unknown_property_id"

	// Spec p.7 §"Object Types": 0..5 defined (node, preset, enum,
	// number, ipv4, string). Receiving pid=1 with a value outside
	// this range means a newer firmware added a type — consumer
	// falls back on ValueKind=KindRaw and fires. Informational.
	UnknownObjectType = "acp2_unknown_object_type"

	// Spec p.7 §"Number Types": 0..11 defined. An unknown number
	// type on pid=5 / pid=8 falls back to raw bytes and fires.
	// Informational.
	UnknownNumberType = "acp2_unknown_number_type"

	// Spec p.8 §"Preset Depth": pid=7 lists valid idx values. When
	// a reply's idx is NOT in the declared list, we accept the value
	// for the delivered idx and fire. Informational.
	PresetIndexOutOfRange = "acp2_preset_index_out_of_range"

	// Spec §"Active Index": idx=0 means "substitute active idx" in
	// requests; in replies, idx=0 is returned as-is from the device.
	// Some providers echo idx=0 on the reply even though an explicit
	// idx was requested — informational.
	ActiveIndexEchoed = "acp2_active_index_echoed"

	// Spec p.9 §"Announces": type=2 frames MUST carry mtid=0 (spec
	// invariant — announces are never correlated). When an announce
	// arrives with mtid != 0 we process it normally + fire, since
	// the payload is still decodable. Informational.
	AnnounceNonZeroMtid = "acp2_announce_non_zero_mtid"

	// SetProperty replies echo the confirmed value. When the echo
	// differs from what we sent (value coerced / clamped by the
	// device), we accept it + fire so the operator sees write-path
	// coercion. Informational.
	SetValueCoerced = "acp2_set_value_coerced"
)

// EventForErrStatus maps an ACP2 error status (spec p.5) to the
// compliance event label the session fires when that error is
// received. Returns "" for status codes outside the defined range —
// caller should not fire an event in that case. Exposed so replay
// tests can assert the correct mapping without standing up a session.
func EventForErrStatus(s ACP2ErrStatus) string {
	switch s {
	case ErrProtocol:
		return ProtocolErrorReceived
	case ErrInvalidObjID:
		return InvalidObjectReceived
	case ErrInvalidIdx:
		return InvalidIndexReceived
	case ErrInvalidPID:
		return InvalidPidReceived
	case ErrNoAccess:
		return AccessDeniedReceived
	case ErrInvalidValue:
		return InvalidValueReceived
	}
	return ""
}
