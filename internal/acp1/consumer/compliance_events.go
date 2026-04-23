package acp1

// Compliance event labels — per-spec named deviations the ACP1
// consumer absorbs on the wire. Every constant here maps to one code
// path that tolerated a device defect without failing the operation
// (per memory/feedback_no_workaround.md §7–9: absorb silently = never;
// absorb + fire event = always; spec-page cited in the comment).
//
// Authoritative spec: internal/acp1/assets/AXON-ACP_v1_4.pdf.
//
// The generic Profile counter lives in internal/protocol/compliance/.
// Classification:
//   - strict  : zero events fired this session
//   - partial : one or more events fired, all within tolerance
//
// Adding a new label is an API change — downstream tooling may
// aggregate by key.
const (
	// Spec p.11 ("ACP Header"): MTYPE=3 errors split at MCODE=16.
	// When a transport-level error is returned (MCODE < 16) on an
	// operation that ought to have succeeded (e.g. request/reply
	// timeout or out-of-resources), the consumer returns the error
	// but fires this event so the aggregate count can flag flaky
	// devices. Informational.
	TransportErrorReceived = "acp1_transport_error_received"

	// Spec p.29 ("AxonNet error codes"): object-layer errors use
	// MCODE >= 16. Receiving one on a known-good object/slot
	// combination suggests the walked tree went stale (firmware
	// reboot changed IDs, per spec p.34 recommendation to use
	// labels not IDs). Informational.
	ObjectErrorReceived = "acp1_object_error_received"

	// Spec p.11: MDATA is at least {MCODE, ObjGroup, ObjId} = 3 bytes
	// for every MTYPE<3 reply. Error replies (MTYPE=3) MAY omit
	// ObjGroup/ObjId per spec §"ACP Header" note. When we receive
	// a reply whose MDATA is shorter than expected for its claimed
	// method, we fire this and fall back on whatever bytes were
	// provided. Informational.
	ShortMDATA = "acp1_short_mdata"

	// Spec p.28 ("Methods"): exactly six method IDs exist (0..5).
	// When an announce or reply carries an MCODE not in this set
	// and MTYPE < 3, we treat it as an unknown method, record the
	// event, and skip dispatch. Informational.
	UnknownMethod = "acp1_unknown_method"

	// Spec p.19 ("Object details"): every object's first byte is
	// its ObjectType (1..10, with 11 reserved). When getObject
	// returns an ObjectType we don't know, the decoder reads as
	// many bytes as it can and fires this. Informational.
	UnknownObjectType = "acp1_unknown_object_type"

	// Spec p.20: strings are length-bounded per field (Label 16,
	// Unit 4, Alarm Msg 32) and NUL-terminated. If a device omits
	// the terminator or sends a longer string, we truncate at the
	// spec limit and fire this event. Informational.
	StringMissingTerminator = "acp1_string_missing_terminator"

	// Spec p.24 ("Enumerated"): the item_list is a comma-delimited
	// NUL-terminated string, num_items is a count, value is an
	// index. When value >= num_items we keep the raw byte but flag
	// the out-of-band state. Informational.
	EnumValueOutOfRange = "acp1_enum_value_out_of_range"

	// Spec p.8 ("ACP Message Types"): MTYPE=0 announcements carry
	// MTID=0. When an announce arrives with MTID != 0 it's a
	// provider bug (or a late reply mis-classified); we still
	// process it but fire this event. Informational.
	AnnounceNonZeroMtid = "acp1_announce_non_zero_mtid"

	// Spec p.12 ("MADDR"): slot addressing — announcements carry
	// the source slot in MADDR. When the MADDR on an announce
	// doesn't match the slot currently walking (e.g. card moved,
	// hot-swap), we route the update to the correct slot if we
	// have it cached, else drop. Informational.
	AnnounceSlotMismatch = "acp1_announce_slot_mismatch"

	// Spec p.21 ("Integer type"): getObject returns all 10
	// properties in order: type, num_properties, access, value,
	// default_value, step_size, min_value, max_value, label, unit.
	// When a device ships num_properties < 10 (truncated shape),
	// we fill missing fields with zero values and fire this.
	// Informational.
	ObjectPropertiesTruncated = "acp1_object_properties_truncated"

	// Spec p.26 ("File Type"): getObject MUST NOT return the
	// Fragment property. If we see more properties than expected
	// on File or any other type, we ignore the extras and fire.
	// Informational.
	ObjectPropertiesExtra = "acp1_object_properties_extra"

	// SetValue replies echo the confirmed value. When the echo
	// differs from what we sent by more than the step size
	// (coerced / clamped), we accept it but fire this so the
	// operator sees write-path coercion. Informational.
	SetValueCoerced = "acp1_set_value_coerced"
)
