package emberplus

import (
	"fmt"

	"acp/internal/export/canonical"
	"acp/internal/protocol/emberplus/ber"
	"acp/internal/protocol/emberplus/glow"
)

// encodeQualifiedMatrix emits a [APPLICATION 17] QualifiedMatrix. Contents
// fields are emitted in ASCENDING CTX-tag order (DER requirement the
// consumer decoder does not enforce but EmberViewer's strict path does).
//
// Spec p.88:
//
//	QualifiedMatrix ::= [APPLICATION 17] SEQUENCE {
//	  path         [0] RELATIVE-OID,
//	  contents     [1] MatrixContents         OPTIONAL,
//	  children     [2] ElementCollection      OPTIONAL,
//	  targets      [3] TargetCollection       OPTIONAL,
//	  sources      [4] SourceCollection       OPTIONAL,
//	  connections  [5] ConnectionCollection   OPTIONAL
//	}
func (s *server) encodeQualifiedMatrix(e *entry, m *canonical.Matrix) (ber.TLV, error) {
	contents, err := encodeMatrixContents(m)
	if err != nil {
		return ber.TLV{}, err
	}
	fields := []ber.TLV{
		ber.ContextConstructed(0, ber.RelOID(encodeRelativeOID(e.oidParts))),
		ber.ContextConstructed(1, contents),
	}
	// Targets / sources are only emitted when the provider has explicit
	// (non-linear) signal lists. For linear matrices we omit them and the
	// consumer infers implicit [0..targetCount-1] / [0..sourceCount-1].
	if len(m.Targets) > 0 {
		fields = append(fields, ber.ContextConstructed(3, encodeTargets(m.Targets)))
	}
	if len(m.Sources) > 0 {
		fields = append(fields, ber.ContextConstructed(4, encodeSources(m.Sources)))
	}
	if len(m.Connections) > 0 {
		fields = append(fields, ber.ContextConstructed(5, encodeConnections(m.Connections)))
	}
	return ber.AppConstructed(glow.TagQualifiedMatrix, fields...), nil
}

// encodeMatrixContents builds the [UNIVERSAL SET] inside [CTX 1] contents.
// Field order is ascending CTX tag; optional fields absent when the
// canonical value is zero / nil / default (type=oneToN, mode=linear).
func encodeMatrixContents(m *canonical.Matrix) (ber.TLV, error) {
	var kids []ber.TLV
	kids = append(kids,
		ber.ContextConstructed(glow.MatContentIdentifier, ber.UTF8(m.Identifier))) // [0]
	if m.Description != nil && *m.Description != "" {
		kids = append(kids,
			ber.ContextConstructed(glow.MatContentDescription, ber.UTF8(*m.Description))) // [1]
	}
	if tc, ok := matrixTypeConst(m.Type); ok && tc != glow.MatrixTypeOneToN {
		kids = append(kids,
			ber.ContextConstructed(glow.MatContentType, ber.Integer(tc))) // [2]
	}
	if addressingModeConst(m.Mode) == glow.MatrixAddrNonLinear {
		kids = append(kids,
			ber.ContextConstructed(glow.MatContentAddressingMode, ber.Integer(glow.MatrixAddrNonLinear))) // [3]
	}
	kids = append(kids,
		ber.ContextConstructed(glow.MatContentTargetCount, ber.Integer(m.TargetCount))) // [4]
	kids = append(kids,
		ber.ContextConstructed(glow.MatContentSourceCount, ber.Integer(m.SourceCount))) // [5]
	if m.MaximumTotalConnects != nil {
		kids = append(kids,
			ber.ContextConstructed(glow.MatContentMaxTotalConnects, ber.Integer(*m.MaximumTotalConnects))) // [6]
	}
	if m.MaximumConnectsPerTarget != nil {
		kids = append(kids,
			ber.ContextConstructed(glow.MatContentMaxConnectsPerTgt, ber.Integer(*m.MaximumConnectsPerTarget))) // [7]
	}
	if m.ParametersLocation != nil && *m.ParametersLocation != "" {
		parts, err := parseOID(*m.ParametersLocation)
		if err != nil {
			return ber.TLV{}, fmt.Errorf("parametersLocation %q: %w", *m.ParametersLocation, err)
		}
		kids = append(kids,
			ber.ContextConstructed(glow.MatContentParametersLocation, ber.RelOID(encodeRelativeOID(parts)))) // [8]
	}
	if m.GainParameterNumber != nil {
		kids = append(kids,
			ber.ContextConstructed(glow.MatContentGainParameterNumber, ber.Integer(*m.GainParameterNumber))) // [9]
	}
	if len(m.Labels) > 0 {
		items := make([]ber.TLV, 0, len(m.Labels))
		for _, l := range m.Labels {
			lb, err := encodeLabel(l)
			if err != nil {
				return ber.TLV{}, err
			}
			items = append(items, ber.ContextConstructed(0, lb))
		}
		kids = append(kids,
			ber.ContextConstructed(glow.MatContentLabels, ber.Sequence(items...))) // [10]
	}
	return ber.Set(kids...), nil
}

// encodeLabel emits one [APPLICATION 18] Label. Both fields are CTX-wrapped.
func encodeLabel(l canonical.MatrixLabel) (ber.TLV, error) {
	parts, err := parseOID(l.BasePath)
	if err != nil {
		return ber.TLV{}, fmt.Errorf("label basePath %q: %w", l.BasePath, err)
	}
	fields := []ber.TLV{
		ber.ContextConstructed(glow.LabelBasePath, ber.RelOID(encodeRelativeOID(parts))),
	}
	if l.Description != nil && *l.Description != "" {
		fields = append(fields,
			ber.ContextConstructed(glow.LabelDescription, ber.UTF8(*l.Description)))
	}
	return ber.AppConstructed(glow.TagLabel, fields...), nil
}

// encodeTargets / encodeSources emit a TargetCollection / SourceCollection
// ([UNIVERSAL SEQUENCE] holding [CTX 0] { [APP 14|15] Signal }). Each
// signal carries just its number.
func encodeTargets(targets []canonical.MatrixTarget) ber.TLV {
	items := make([]ber.TLV, 0, len(targets))
	for _, t := range targets {
		sig := ber.AppConstructed(glow.TagTarget,
			ber.ContextConstructed(glow.SignalNumber, ber.Integer(t.Number)))
		items = append(items, ber.ContextConstructed(0, sig))
	}
	return ber.Sequence(items...)
}

func encodeSources(sources []canonical.MatrixSource) ber.TLV {
	items := make([]ber.TLV, 0, len(sources))
	for _, s := range sources {
		sig := ber.AppConstructed(glow.TagSource,
			ber.ContextConstructed(glow.SignalNumber, ber.Integer(s.Number)))
		items = append(items, ber.ContextConstructed(0, sig))
	}
	return ber.Sequence(items...)
}

// encodeConnections emits a ConnectionCollection inside [CTX 5] of the
// QualifiedMatrix wrapper. Each Connection is CTX[0]-wrapped per the spec
// grammar `SEQUENCE OF [0] Connection`.
func encodeConnections(conns []canonical.MatrixConnection) ber.TLV {
	items := make([]ber.TLV, 0, len(conns))
	for _, c := range conns {
		items = append(items, ber.ContextConstructed(0, encodeConnection(c)))
	}
	return ber.Sequence(items...)
}

// encodeConnection emits one [APPLICATION 16] Connection. operation and
// disposition are omitted when they match spec defaults (absolute/tally)
// to match TinyEmber+ wire economy.
//
// sources is ALWAYS emitted — even when empty. EmberViewer (and the
// spec's CHOICE semantics) treat an absent sources field as "unchanged"
// rather than "disconnected"; the last-crosspoint-disconnect click
// leaves the cell lit unless the provider sends an explicit empty
// RelOID tally. See router oneToOne disconnect regression on #69.
func encodeConnection(c canonical.MatrixConnection) ber.TLV {
	parts := make([]uint32, len(c.Sources))
	for i, v := range c.Sources {
		parts[i] = uint32(v)
	}
	fields := []ber.TLV{
		ber.ContextConstructed(glow.ConnTarget, ber.Integer(c.Target)),
		ber.ContextConstructed(glow.ConnSources, ber.RelOID(encodeRelativeOID(parts))),
	}
	if op := connOperationConst(c.Operation); op != glow.ConnOpAbsolute {
		fields = append(fields,
			ber.ContextConstructed(glow.ConnOperation, ber.Integer(op)))
	}
	if disp := connDispositionConst(c.Disposition); disp != glow.ConnDispTally {
		fields = append(fields,
			ber.ContextConstructed(glow.ConnDisposition, ber.Integer(disp)))
	}
	return ber.AppConstructed(glow.TagConnection, fields...)
}

// matrixTypeConst maps canonical type strings to the MatrixType enum.
// Returns (_, false) for an empty / unknown string so callers can skip
// emitting the field (default oneToN applies).
func matrixTypeConst(t string) (int64, bool) {
	switch t {
	case canonical.MatrixOneToN, "":
		return glow.MatrixTypeOneToN, true
	case canonical.MatrixOneToOne:
		return glow.MatrixTypeOneToOne, true
	case canonical.MatrixNToN:
		return glow.MatrixTypeNToN, true
	}
	return 0, false
}

func addressingModeConst(m string) int64 {
	if m == canonical.ModeNonLinear {
		return glow.MatrixAddrNonLinear
	}
	return glow.MatrixAddrLinear
}

func connOperationConst(op string) int64 {
	switch op {
	case canonical.ConnOpConnect:
		return glow.ConnOpConnect
	case canonical.ConnOpDisconnect:
		return glow.ConnOpDisconnect
	}
	return glow.ConnOpAbsolute
}

func connDispositionConst(d string) int64 {
	switch d {
	case canonical.ConnDispModified:
		return glow.ConnDispModified
	case canonical.ConnDispPending:
		return glow.ConnDispPending
	case canonical.ConnDispLocked:
		return glow.ConnDispLocked
	}
	return glow.ConnDispTally
}
