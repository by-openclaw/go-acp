package acp2

// Compliance event labels — per-spec named deviations the ACP2
// provider deliberately emits to match the de-facto wire contract
// every shipping controller implements. Same philosophy as the
// consumer side (internal/acp2/consumer/compliance_events.go) and
// the Probel provider (internal/probel-sw08p/provider/compliance_events.go):
// absorb + fire event; never silently work around the spec. See
// memory/feedback_no_workaround.md "Exception — when every shipping
// controller contradicts the spec".
//
// Authoritative spec: internal/acp2/assets/acp2_protocol.docx.
//
// The generic Profile counter lives in internal/protocol/compliance/.
// Aggregated across every accepted session since Serve started.
const (
	// Spec acp2_protocol.docx §5.4 row 15 specifies fixed 72-byte
	// stride per option (`plen = 4 + 72*N`, each slot = u32 idx +
	// 68-byte NUL-padded name). No production controller implements
	// this layout: real EVS Neuron firmware emits variable-length
	// records (u32 idx + NUL-terminated name + 0-3 byte align,
	// verified 2026-05-06 against 9,827 pid=15 frames), and Cerebrum
	// + VSM Studio decode only the variable-length form. Emitting the
	// spec-literal layout isolates the implementation from the entire
	// shipping ecosystem. To match the de-facto contract we emit the
	// variable-length form and fire this event once per Enum object
	// served. Every firing is logged + countable.
	OptionsVariableLengthPerDeviceConvention = "acp2_options_variable_length_per_device_convention"
)
