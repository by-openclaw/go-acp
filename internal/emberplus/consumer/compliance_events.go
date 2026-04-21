package emberplus

// Compliance event labels — per-spec named deviations the Ember+
// consumer absorbs on the wire. Every `*` constant here maps to one
// code path that tolerated a provider defect without failing the
// operation (per memory/feedback_no_workaround.md §7–8: absorb silently
// = never; absorb + fire event = always).
//
// Keep this list short and stable — documented in
// internal/emberplus/docs/consumer.md §"compliance.Profile event labels"
// and in docs/protocols/schema.md §6. Adding a new label is an API
// change: downstream tooling may aggregate by key.
//
// The generic Profile counter lives in internal/protocol/compliance/.
const (
	// NonQualifiedElement fires when a provider delivers a Node /
	// Parameter / Matrix / Function without a RELATIVE-OID path,
	// forcing the consumer to derive it from the parent walk
	// ancestry. Spec p.85/p.87 permit both forms; real-world
	// Qualified* wrappers are the modern default.
	NonQualifiedElement = "non_qualified_element"

	// MultiFrameReassembly fires when S101 FlagFirst/FlagLast is
	// observed and the payload had to be reassembled across frames.
	// Any walk of more than a few hundred objects triggers this.
	MultiFrameReassembly = "multi_frame_reassembly"

	// InvocationSuccessDefault fires when InvocationResult arrives
	// without the success field (spec p.92: "True or omitted if no
	// errors"). Common on Lawo-derived providers.
	InvocationSuccessDefault = "invocation_success_default"

	// ConnectionOperationDefault fires when a Connection omits the
	// operation field and the decoder falls back to absolute
	// (spec p.89 default).
	ConnectionOperationDefault = "connection_operation_default"

	// ConnectionDispositionDefault fires when a Connection omits
	// the disposition field; decoder falls back to tally (p.89).
	ConnectionDispositionDefault = "connection_disposition_default"

	// ContentsSetOmitted fires when contents are delivered as a
	// bare CTX[1] sequence without the UNIVERSAL SET envelope.
	// Spec p.85 mandates the SET; some providers skip it.
	ContentsSetOmitted = "contents_set_omitted"

	// TupleDirectCtx fires when a Tuple arrives as direct CTX[0]
	// elements with no enclosing UNIVERSAL SEQUENCE. Spec p.92
	// defines the SEQUENCE; some providers inline the elements.
	TupleDirectCtx = "tuple_direct_ctx"

	// ElementCollectionBare fires when an ElementCollection is
	// inlined as CTX[0] children without the APP[4] wrapper.
	// Common in request frames from consumers that need it back.
	ElementCollectionBare = "element_collection_bare"

	// UnknownTagSkipped fires when the decoder encounters an
	// APP or CTX tag it does not recognise. Usually a vendor-
	// private extension; harmless.
	UnknownTagSkipped = "unknown_tag_skipped"

	// EnumMaskedItem fires when an enum option carries a masked
	// flag (smh `~Label` convention; stripped and marked
	// non-selectable). Canonical schema §4.5.
	EnumMaskedItem = "enum_masked_item"

	// EnumDoubleSource fires when a Parameter carries both a
	// native EnumMap and the LF-joined Enumeration with differing
	// counts. Canonical schema §6.
	EnumDoubleSource = "enum_double_source"

	// EnumMapDerived fires when the canonical EnumMap was
	// synthesised from the legacy Enumeration string (no native
	// EnumMap on the wire). Informational.
	EnumMapDerived = "enum_map_derived"

	// FieldInferred fires when a canonical field was synthesised
	// from a protocol-specific source (e.g. Parameter.Type
	// inferred from Value CHOICE). Canonical schema §6.
	FieldInferred = "field_inferred"

	// --- Resolver events (spec §5.1.1 / §5.1.2 / §8) ---
	// Matrix-scoped events fire when --labels / --gain / --templates
	// request inline or both and the resolver encounters a malformed
	// or missing reference. They never block the export; the canonical
	// JSON still ships with wire-faithful pointer data even when the
	// referenced subtree is absent.

	// MatrixLabelBasepathUnresolved fires when a Matrix carries
	// labels[i].basePath that does not resolve to a walked Node.
	// Pointer-mode output preserves the dangling ref; inline-mode
	// output skips the level entirely (no empty inner map).
	MatrixLabelBasepathUnresolved = "matrix_label_basepath_unresolved"

	// MatrixLabelNone fires when a Matrix ships no labels[] array
	// or an empty one. Informational — small providers routinely
	// omit labels and rely on target/source identifier as display.
	MatrixLabelNone = "matrix_label_none"

	// MatrixLabelDescriptionEmpty fires when a labels[i] entry has
	// no description. The resolver falls back to the basePath as
	// the outer map key in inline mode.
	MatrixLabelDescriptionEmpty = "matrix_label_description_empty"

	// MatrixLabelLevelMismatch fires when two label levels expose
	// a different number of target/source entries. Informational —
	// may indicate a stale label tree or a partial walk.
	MatrixLabelLevelMismatch = "matrix_label_level_mismatch"

	// MatrixParametersLocationUnresolved fires when a Matrix
	// carries parametersLocation that does not resolve to a walked
	// Node. Mirrors MatrixLabelBasepathUnresolved semantics.
	MatrixParametersLocationUnresolved = "matrix_parameters_location_unresolved"

	// TemplateReferenceUnresolved fires when a Parameter / Node
	// / Matrix carries templateReference pointing to a path with
	// no walked Template. Pointer-mode keeps the ref; inline-mode
	// leaves the element shape as delivered on the wire.
	TemplateReferenceUnresolved = "template_reference_unresolved"

	// LabelsAbsorbed fires once per Matrix when --labels=inline
	// successfully collapses at least one label level into the
	// matrix element and removes the source subtree from the tree.
	LabelsAbsorbed = "labels_absorbed"

	// GainAbsorbed fires once per Matrix when --gain=inline
	// successfully collapses parametersLocation into the matrix
	// element and removes the source subtree from the tree.
	GainAbsorbed = "gain_absorbed"

	// TemplateAbsorbed fires once per resolved templateReference
	// when --templates=inline successfully inflates the referenced
	// Template into the referring element.
	TemplateAbsorbed = "template_absorbed"

	// StreamIDCollisionNoDescriptor fires when the consumer sees
	// two or more Parameters sharing the same streamIdentifier
	// with at least one of them missing a streamDescriptor.
	// Spec §7 (Streams): a shared streamIdentifier is legal only
	// in CollectionAggregate mode, where EVERY participating
	// Parameter MUST carry a streamDescriptor (format + offset)
	// so the consumer can split the aggregated blob. Without a
	// descriptor the streamIdentifier is implicitly exclusive;
	// reusing it across Parameters is a provider-side bug that
	// causes value mis-dispatch.
	StreamIDCollisionNoDescriptor = "stream_id_collision_no_descriptor"
)
