package emberplus

import (
	"fmt"

	"acp/internal/export/canonical"
	"acp/internal/emberplus/codec/ber"
	"acp/internal/emberplus/codec/glow"
)

// encodeGetDirReply walks the tree entry e and produces the BER payload
// for a GetDirectory reply.
//
// Spec pattern (observed from TinyEmber+ frame 6 + 10):
//
//  1. GetDirectory at root level (bare Command, empty path)
//     → return the tree's top-level element alone (identifier only)
//
//  2. GetDirectory on a specific path P (Command nested in QualifiedNode(P))
//     → return each direct child of P as a FLAT QualifiedElement with
//       absolute path at the RootElementCollection level (NOT nested
//       inside a wrapper). Minimal contents — strict viewers reject
//       anything beyond identifier (+description where set).
//
// isRoot and isOnline are intentionally omitted: TinyEmber+ does not
// emit them, and EmberViewer treats extra content as schema deviation.
func (s *server) encodeGetDirReply(e *entry, bareRoot bool) ([]byte, error) {
	hdr := e.el.Common()

	var items []ber.TLV
	if bareRoot {
		// Just the queried element itself, identifier only.
		items = append(items, ber.ContextConstructed(0, s.encodeElementMinimal(e)))
	} else {
		// Flat list of direct children, each as a qualified element.
		for _, child := range hdr.Children {
			centry := s.tree.byOID[child.Common().OID]
			if centry == nil {
				return nil, fmt.Errorf("encoder: missing index for %q", child.Common().OID)
			}
			tlv, err := s.encodeQualifiedElement(centry)
			if err != nil {
				return nil, err
			}
			items = append(items, ber.ContextConstructed(0, tlv))
		}
	}

	root := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection, items...),
	)
	return ber.EncodeTLV(root), nil
}

// encodeElementMinimal emits a qualified element with just the identifier
// (and description if present) — the shape TinyEmber+ uses for the
// initial root reply.
func (s *server) encodeElementMinimal(e *entry) ber.TLV {
	hdr := e.el.Common()
	var setChildren []ber.TLV
	setChildren = append(setChildren,
		ber.ContextConstructed(glow.NodeContentIdentifier, ber.UTF8(hdr.Identifier)))
	if hdr.Description != nil && *hdr.Description != "" {
		setChildren = append(setChildren,
			ber.ContextConstructed(glow.NodeContentDescription, ber.UTF8(*hdr.Description)))
	}
	contents := ber.Set(setChildren...)

	// App tag depends on concrete element kind. For the canonical root we
	// always emit QualifiedNode; Parameter / Matrix / Function variants
	// mirror the per-kind encoder below.
	switch e.el.(type) {
	case *canonical.Parameter:
		return ber.AppConstructed(glow.TagQualifiedParameter,
			ber.ContextConstructed(glow.QParamPath, ber.RelOID(encodeRelativeOID(e.oidParts))),
			ber.ContextConstructed(glow.QParamContents, contents),
		)
	case *canonical.Matrix:
		return ber.AppConstructed(glow.TagQualifiedMatrix,
			ber.ContextConstructed(0, ber.RelOID(encodeRelativeOID(e.oidParts))),
			ber.ContextConstructed(1, contents),
		)
	case *canonical.Function:
		return ber.AppConstructed(glow.TagQualifiedFunction,
			ber.ContextConstructed(glow.QFuncPath, ber.RelOID(encodeRelativeOID(e.oidParts))),
			ber.ContextConstructed(glow.QFuncContents, contents),
		)
	default:
		return ber.AppConstructed(glow.TagQualifiedNode,
			ber.ContextConstructed(glow.QNodePath, ber.RelOID(encodeRelativeOID(e.oidParts))),
			ber.ContextConstructed(glow.QNodeContents, contents),
		)
	}
}

// encodeQualifiedElement dispatches by element kind and emits the
// appropriate qualified form WITH contents so a walking consumer learns
// everything it needs about the element in one frame.
func (s *server) encodeQualifiedElement(e *entry) (ber.TLV, error) {
	switch el := e.el.(type) {
	case *canonical.Node:
		return s.encodeQualifiedNode(e, el), nil
	case *canonical.Parameter:
		return s.encodeQualifiedParameter(e, el), nil
	case *canonical.Matrix:
		return s.encodeQualifiedMatrix(e, el)
	case *canonical.Function:
		return s.encodeQualifiedFunction(e, el), nil
	default:
		return ber.TLV{}, fmt.Errorf("encoder: element kind %q not yet implemented", e.el.Kind())
	}
}

func (s *server) encodeQualifiedNode(e *entry, n *canonical.Node) ber.TLV {
	// Minimal NodeContents — identifier + optional description. Matches
	// TinyEmber+'s shape; strict viewers reject isRoot/isOnline padding
	// on per-child replies.
	var kids []ber.TLV
	kids = append(kids,
		ber.ContextConstructed(glow.NodeContentIdentifier, ber.UTF8(n.Identifier)))
	if n.Description != nil && *n.Description != "" {
		kids = append(kids,
			ber.ContextConstructed(glow.NodeContentDescription, ber.UTF8(*n.Description)))
	}
	return ber.AppConstructed(glow.TagQualifiedNode,
		ber.ContextConstructed(glow.QNodePath, ber.RelOID(encodeRelativeOID(e.oidParts))),
		ber.ContextConstructed(glow.QNodeContents, ber.Set(kids...)),
	)
}

func (s *server) encodeQualifiedParameter(e *entry, p *canonical.Parameter) ber.TLV {
	// ParameterContents SET — fields in ASCENDING context tag order
	// (DER requirement + EmberViewer enforces it). Spec p.85:
	//   0 identifier, 1 description, 2 value, 3 minimum, 4 maximum,
	//   5 access, 6 format, 7 enumeration, 8 factor, 11 step,
	//   12 default, 13 type, 15 enumMap.
	var kids []ber.TLV
	kids = append(kids,
		ber.ContextConstructed(glow.ParamContentIdentifier, ber.UTF8(p.Identifier))) // [0]
	if p.Description != nil && *p.Description != "" {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentDescription, ber.UTF8(*p.Description))) // [1]
	}
	if v, ok := encodeValue(p.Type, p.Value); ok {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentValue, v)) // [2]
	}
	if v, ok := encodeValue(p.Type, p.Minimum); ok {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentMinimum, v)) // [3]
	}
	if v, ok := encodeValue(p.Type, p.Maximum); ok {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentMaximum, v)) // [4]
	}
	kids = append(kids,
		ber.ContextConstructed(glow.ParamContentAccess, ber.Integer(accessConst(p.Access)))) // [5]
	if p.Format != nil && *p.Format != "" {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentFormat, ber.UTF8(*p.Format))) // [6]
	}
	if p.Enumeration != nil && *p.Enumeration != "" {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentEnumeration, ber.UTF8(*p.Enumeration))) // [7]
	}
	// Emit factor only when it actually scales something. factor=0 / 1 are
	// no-ops on the spec (display = value * factor or value / factor
	// depending on provider convention). Emitting factor=1 trips the tree
	// cell update path in EmberViewer — skip it.
	if p.Factor != nil && *p.Factor != 0 && *p.Factor != 1 {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentFactor, ber.Integer(*p.Factor))) // [8]
	}
	// Formula [CTX 10]: "provider|consumer" split, newline-separated
	// in spec examples (see Ember+ Formulas.pdf). Full evaluator lands
	// in a follow-up — for now we emit the string so consumers that
	// parse formulas themselves see it.
	if p.Formula != nil && *p.Formula != "" {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentFormula, ber.UTF8(*p.Formula))) // [10]
	}
	if v, ok := encodeValue(p.Type, p.Step); ok {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentStep, v)) // [11]
	}
	if v, ok := encodeValue(p.Type, p.Default); ok {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentDefault, v)) // [12]
	}
	if tc, ok := paramTypeConst(p.Type); ok {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentType, ber.Integer(tc))) // [13]
	}
	if p.StreamIdentifier != nil {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentStreamIdentifier, ber.Integer(*p.StreamIdentifier))) // [14]
	}
	if len(p.EnumMap) > 0 {
		kids = append(kids,
			ber.ContextConstructed(glow.ParamContentEnumMap, encodeEnumMap(p.EnumMap))) // [15]
	}
	return ber.AppConstructed(glow.TagQualifiedParameter,
		ber.ContextConstructed(glow.QParamPath, ber.RelOID(encodeRelativeOID(e.oidParts))),
		ber.ContextConstructed(glow.QParamContents, ber.Set(kids...)),
	)
}

// encodeEnumMap builds a StringIntegerCollection — the canonical form
// of Parameter.enumMap (spec p.86 StringIntegerPair / Collection).
//
//	StringIntegerCollection ::= [APPLICATION 8] SEQUENCE OF [0] StringIntegerPair
//	StringIntegerPair       ::= [APPLICATION 7] SEQUENCE {
//	  entryString  [0] EmberString,
//	  entryInteger [1] Integer64 }
func encodeEnumMap(entries []canonical.EnumEntry) ber.TLV {
	pairs := make([]ber.TLV, 0, len(entries))
	for _, en := range entries {
		pair := ber.AppConstructed(glow.TagStringIntegerPair,
			ber.ContextConstructed(0, ber.UTF8(en.Key)),
			ber.ContextConstructed(1, ber.Integer(en.Value)),
		)
		pairs = append(pairs, ber.ContextConstructed(0, pair))
	}
	return ber.AppConstructed(glow.TagStringIntegerCollection, pairs...)
}

// encodeParamAnnouncement produces a value-change announcement — same
// shape as a walk reply for a single QualifiedParameter, so consumers
// with a subscribe-only path treat it as an update.
func (s *server) encodeParamAnnouncement(e *entry, p *canonical.Parameter) []byte {
	qp := s.encodeQualifiedParameter(e, p)
	root := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection,
			ber.ContextConstructed(0, qp),
		),
	)
	return ber.EncodeTLV(root)
}

// encodeValue dispatches a Parameter's canonical Value to the BER encoding
// for the declared Type. Returns (tlv, true) on success; (_, false) if
// value is nil or type is unsupported — caller skips the field.
func encodeValue(typ string, v any) (ber.TLV, bool) {
	if v == nil {
		return ber.TLV{}, false
	}
	switch typ {
	case canonical.ParamInteger, canonical.ParamEnum:
		i, ok := asInt64(v)
		if !ok {
			return ber.TLV{}, false
		}
		return ber.Integer(i), true
	case canonical.ParamReal:
		f, ok := asFloat64(v)
		if !ok {
			return ber.TLV{}, false
		}
		return ber.Real(f), true
	case canonical.ParamString:
		s, ok := v.(string)
		if !ok {
			return ber.TLV{}, false
		}
		return ber.UTF8(s), true
	case canonical.ParamBoolean:
		b, ok := v.(bool)
		if !ok {
			return ber.TLV{}, false
		}
		return ber.Boolean(b), true
	case canonical.ParamOctets:
		if b, ok := v.([]byte); ok {
			return ber.OctetStr(b), true
		}
		return ber.TLV{}, false
	}
	return ber.TLV{}, false
}

func paramTypeConst(typ string) (int64, bool) {
	switch typ {
	case canonical.ParamInteger:
		return glow.ParamTypeInteger, true
	case canonical.ParamReal:
		return glow.ParamTypeReal, true
	case canonical.ParamString:
		return glow.ParamTypeString, true
	case canonical.ParamBoolean:
		return glow.ParamTypeBoolean, true
	case canonical.ParamTrigger:
		return glow.ParamTypeTrigger, true
	case canonical.ParamEnum:
		return glow.ParamTypeEnum, true
	case canonical.ParamOctets:
		return glow.ParamTypeOctets, true
	}
	return 0, false
}

func accessConst(a string) int64 {
	switch a {
	case canonical.AccessRead:
		return glow.AccessRead
	case canonical.AccessWrite:
		return glow.AccessWrite
	case canonical.AccessReadWrite:
		return glow.AccessReadWrite
	}
	return glow.AccessNone
}

func asInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int:
		return int64(t), true
	case int32:
		return int64(t), true
	case int64:
		return t, true
	case float64:
		return int64(t), true
	}
	return 0, false
}

func asFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	}
	return 0, false
}

// encodeRelativeOID encodes a path as RELATIVE-OID (base-128 per subid).
// Duplicate of glow/encoder.go's private helper — kept local to avoid
// exporting from the consumer-side package.
func encodeRelativeOID(path []uint32) []byte {
	var out []byte
	for _, v := range path {
		out = append(out, encodeBase128(v)...)
	}
	return out
}

func encodeBase128(v uint32) []byte {
	if v < 128 {
		return []byte{byte(v)}
	}
	var parts []byte
	for v > 0 {
		parts = append([]byte{byte(v & 0x7F)}, parts...)
		v >>= 7
	}
	for i := 0; i < len(parts)-1; i++ {
		parts[i] |= 0x80
	}
	return parts
}
