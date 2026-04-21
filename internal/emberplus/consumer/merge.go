package emberplus

import "acp/internal/emberplus/codec/glow"

// mergeAnnouncedParameter overlays the fields an announce actually
// carried onto the previously-walked Parameter, preserving metadata
// the announce did not include.
//
// Ember+ announces (spec p.85 ParameterContents) typically send ONLY
// the changed field — usually Value, sometimes Access, rarely more.
// Every other field arrives as its Go zero value. A naïve overwrite
// would strip Type / Identifier / ranges / enumMap / streamDescriptor
// from the tree on every announce, which is exactly the "decoded
// value mismatch" symptom users see during watch.
//
// Merge rules:
//
//   - Announce fields that are non-zero / non-nil win (the announce
//     is expressing the new truth).
//   - Announce fields that are zero / nil fall back to the prior walk
//     value (the announce simply did not send that field).
//   - Value is special: nil on the wire still means "no change" under
//     the zero/nil rule. An explicitly-null value must use its typed
//     nil (e.g. (*int64)(nil)) which the decoder does not currently
//     produce for announces — so this is correct in practice.
//   - Identifier is kept from the walk even if the announce provides
//     one, because identifier changes on a live parameter are not
//     spec-defined and would invalidate our path indexes.
//   - Path and Number come from the announce when present, because
//     they are how the consumer locates the target. The caller has
//     already resolved numPath by the time merge runs.
func mergeAnnouncedParameter(existing, incoming *glow.Parameter) *glow.Parameter {
	if existing == nil {
		return incoming
	}
	if incoming == nil {
		return existing
	}
	merged := *existing

	// Identifier stays from the walk — see doc comment.
	// merged.Identifier = existing.Identifier (already copied)

	// Path/Number: prefer incoming if provided, else keep walked.
	if len(incoming.Path) > 0 {
		merged.Path = incoming.Path
	}
	if incoming.Number != 0 {
		merged.Number = incoming.Number
	}

	if incoming.Description != "" {
		merged.Description = incoming.Description
	}
	if incoming.Value != nil {
		merged.Value = incoming.Value
	}
	if incoming.Minimum != nil {
		merged.Minimum = incoming.Minimum
	}
	if incoming.Maximum != nil {
		merged.Maximum = incoming.Maximum
	}
	if incoming.Access != 0 {
		merged.Access = incoming.Access
	}
	if incoming.Format != "" {
		merged.Format = incoming.Format
	}
	if incoming.Enumeration != "" {
		merged.Enumeration = incoming.Enumeration
	}
	if incoming.Factor != 0 {
		merged.Factor = incoming.Factor
	}
	// IsOnline is a bool — we cannot distinguish "absent" from
	// "explicitly false". Apply the announce when true (flip
	// online); leave the walked value when the announce is false,
	// because most announces omit the field entirely.
	if incoming.IsOnline {
		merged.IsOnline = true
	}
	if incoming.Formula != "" {
		merged.Formula = incoming.Formula
	}
	if incoming.Step != nil {
		merged.Step = incoming.Step
	}
	if incoming.Default != nil {
		merged.Default = incoming.Default
	}
	if incoming.Type != 0 {
		merged.Type = incoming.Type
	}
	if incoming.StreamIdentifier != 0 {
		merged.StreamIdentifier = incoming.StreamIdentifier
	}
	if len(incoming.EnumMap) > 0 {
		merged.EnumMap = incoming.EnumMap
	}
	if incoming.StreamDescriptor != nil {
		merged.StreamDescriptor = incoming.StreamDescriptor
	}
	if incoming.SchemaIdentifiers != "" {
		merged.SchemaIdentifiers = incoming.SchemaIdentifiers
	}
	if len(incoming.TemplateReference) > 0 {
		merged.TemplateReference = incoming.TemplateReference
	}
	if len(incoming.Children) > 0 {
		merged.Children = incoming.Children
	}

	return &merged
}
