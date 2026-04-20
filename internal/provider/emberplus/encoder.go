package emberplus

import (
	"fmt"

	"acp/internal/export/canonical"
	"acp/internal/protocol/emberplus/ber"
	"acp/internal/protocol/emberplus/glow"
)

// encodeGetDirReply walks the tree entry e and produces the BER payload
// for a GetDirectory reply. The shape is:
//
//	Root → RootElementCollection → Context[0] → QualifiedNode(path) →
//	  children → ElementCollection →
//	    Context[0] → QualifiedNode | QualifiedParameter | QualifiedMatrix | QualifiedFunction
//	    Context[0] → (next sibling)
//	    ...
//
// Always emits qualified forms so consumers walking by path resolve each
// child independently — the lax "bare Node with number only" form is
// valid wire but upsets strict providers' walkers; provider should
// always emit the strict-safe shape.
func (s *server) encodeGetDirReply(e *entry) ([]byte, error) {
	hdr := e.el.Common()

	// Build the children collection as [0]-wrapped qualified elements.
	kids := make([]ber.TLV, 0, len(hdr.Children))
	for _, child := range hdr.Children {
		centry := s.tree.byOID[child.Common().OID]
		if centry == nil {
			return nil, fmt.Errorf("encoder: missing index for %q", child.Common().OID)
		}
		tlv, err := s.encodeQualifiedElement(centry)
		if err != nil {
			return nil, err
		}
		kids = append(kids, ber.ContextConstructed(0, tlv))
	}
	childCollection := ber.AppConstructed(glow.TagElementCollection, kids...)

	// Wrap the queried element itself as QualifiedNode with children[].
	node := ber.AppConstructed(glow.TagQualifiedNode,
		ber.ContextConstructed(glow.QNodePath, ber.RelOID(encodeRelativeOID(e.oidParts))),
		ber.ContextConstructed(glow.QNodeChildren, childCollection),
	)
	root := ber.AppConstructed(glow.TagRoot,
		ber.AppConstructed(glow.TagRootElementCollection,
			ber.ContextConstructed(0, node),
		),
	)
	return ber.EncodeTLV(root), nil
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
	default:
		return ber.TLV{}, fmt.Errorf("encoder: element kind %q not yet implemented", e.el.Kind())
	}
}

func (s *server) encodeQualifiedNode(e *entry, n *canonical.Node) ber.TLV {
	contents := ber.Set(
		ber.ContextConstructed(glow.NodeContentIdentifier, ber.UTF8(n.Identifier)),
	)
	if n.Description != nil && *n.Description != "" {
		contents.Children = append(contents.Children,
			ber.ContextConstructed(glow.NodeContentDescription, ber.UTF8(*n.Description)))
	}
	contents.Children = append(contents.Children,
		ber.ContextConstructed(glow.NodeContentIsOnline, ber.Boolean(n.IsOnline)))

	return ber.AppConstructed(glow.TagQualifiedNode,
		ber.ContextConstructed(glow.QNodePath, ber.RelOID(encodeRelativeOID(e.oidParts))),
		ber.ContextConstructed(glow.QNodeContents, contents),
	)
}

func (s *server) encodeQualifiedParameter(e *entry, p *canonical.Parameter) ber.TLV {
	contents := ber.Set(
		ber.ContextConstructed(glow.ParamContentIdentifier, ber.UTF8(p.Identifier)),
	)
	if p.Description != nil && *p.Description != "" {
		contents.Children = append(contents.Children,
			ber.ContextConstructed(glow.ParamContentDescription, ber.UTF8(*p.Description)))
	}
	// value
	if v, ok := encodeValue(p.Type, p.Value); ok {
		contents.Children = append(contents.Children,
			ber.ContextConstructed(glow.ParamContentValue, v))
	}
	// type
	if tc, ok := paramTypeConst(p.Type); ok {
		contents.Children = append(contents.Children,
			ber.ContextConstructed(glow.ParamContentType, ber.Integer(tc)))
	}
	// access
	contents.Children = append(contents.Children,
		ber.ContextConstructed(glow.ParamContentAccess, ber.Integer(accessConst(p.Access))))

	return ber.AppConstructed(glow.TagQualifiedParameter,
		ber.ContextConstructed(glow.QParamPath, ber.RelOID(encodeRelativeOID(e.oidParts))),
		ber.ContextConstructed(glow.QParamContents, contents),
	)
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
