package glow

import (
	"fmt"

	"acp/internal/protocol/emberplus/ber"
)

// DecodeRoot decodes a GlowRootElementCollection from BER bytes.
// This is the top-level entry point for all Glow messages.
func DecodeRoot(data []byte) ([]Element, error) {
	tlvs, err := ber.DecodeAll(data)
	if err != nil {
		return nil, fmt.Errorf("glow decode: %w", err)
	}
	var elements []Element
	for _, tlv := range tlvs {
		els, err := decodeElements(tlv)
		if err != nil {
			return nil, err
		}
		elements = append(elements, els...)
	}
	return elements, nil
}

// decodeElements returns ALL elements from a TLV (not just the first).
func decodeElements(tlv ber.TLV) ([]Element, error) {
	// Context-wrapped elements.
	if tlv.Tag.Class == ber.ClassContext {
		var out []Element
		for _, child := range tlv.Children {
			els, err := decodeElements(child)
			if err != nil {
				return nil, err
			}
			out = append(out, els...)
		}
		return out, nil
	}

	if tlv.Tag.Class == ber.ClassApplication {
		switch tlv.Tag.Number {
		case TagRootApp, TagRootElementCollection, TagElementCollection:
			// Collections — recurse into children.
			var out []Element
			for _, child := range tlv.Children {
				els, err := decodeElements(child)
				if err != nil {
					return nil, err
				}
				out = append(out, els...)
			}
			return out, nil
		}
	}

	// Single element.
	el, err := decodeElement(tlv)
	if err != nil {
		return nil, err
	}
	if el != nil {
		return []Element{*el}, nil
	}
	return nil, nil
}

// TagRootApp is the APPLICATION 0 Root wrapper.
const TagRootApp uint32 = 0

// decodeElement dispatches on the APPLICATION tag to decode the right type.
func decodeElement(tlv ber.TLV) (*Element, error) {
	// Context-wrapped elements (inside ElementCollection).
	if tlv.Tag.Class == ber.ClassContext {
		for _, child := range tlv.Children {
			el, err := decodeElement(child)
			if err != nil {
				return nil, err
			}
			if el != nil {
				return el, nil
			}
		}
		return nil, nil
	}

	if tlv.Tag.Class == ber.ClassApplication {
		switch tlv.Tag.Number {
		case TagRootApp:
			// Root APPLICATION 0 — unwrap and process children.
			return decodeRootCollection(tlv)
		case TagRootElementCollection:
			return decodeRootCollection(tlv)
		case TagNode:
			return decodeNode(tlv)
		case TagQualifiedNode:
			return decodeQualifiedNode(tlv)
		case TagParameter:
			return decodeParameter(tlv)
		case TagQualifiedParameter:
			return decodeQualifiedParameter(tlv)
		case TagMatrix:
			return decodeMatrix(tlv)
		case TagQualifiedMatrix:
			return decodeQualifiedMatrix(tlv)
		case TagFunction:
			return decodeFunction(tlv)
		case TagQualifiedFunction:
			return decodeQualifiedFunction(tlv)
		case TagCommand:
			return decodeCommand(tlv)
		case TagElementCollection:
			return decodeRootCollection(tlv) // same structure
		}
	}
	return nil, nil // skip unknown elements
}

func decodeRootCollection(tlv ber.TLV) (*Element, error) {
	// A collection contains child elements. Process all of them.
	for _, child := range tlv.Children {
		el, err := decodeElement(child)
		if err != nil {
			return nil, err
		}
		if el != nil {
			return el, nil
		}
	}
	return nil, nil
}

func decodeNode(tlv ber.TLV) (*Element, error) {
	n := &Node{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case NodeNumber:
			if !child.Tag.Constructed && len(child.Value) > 0 {
				v, _ := ber.DecodeInteger(child.Value)
				n.Number = int32(v)
			} else if len(child.Children) > 0 {
				v, _ := ber.DecodeInteger(child.Children[0].Value)
				n.Number = int32(v)
			}
		case NodeContents:
			decodeNodeContents(n, child)
		case NodeChildren:
			n.Children = decodeElementCollection(child)
		}
	}
	return &Element{Node: n}, nil
}

func decodeQualifiedNode(tlv ber.TLV) (*Element, error) {
	n := &Node{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case QNodePath:
			n.Path = decodeRelativeOID(child)
		case QNodeContents:
			decodeNodeContents(n, child)
		case QNodeChildren:
			n.Children = decodeElementCollection(child)
		}
	}
	if len(n.Path) > 0 {
		n.Number = n.Path[len(n.Path)-1]
	}
	return &Element{Node: n}, nil
}

func decodeNodeContents(n *Node, tlv ber.TLV) {
	// Contents is CONTEXT[1] wrapping a SET. Unwrap SET if present.
	children := tlv.Children
	for _, c := range tlv.Children {
		if c.Tag.Class == ber.ClassUniversal && c.Tag.Number == ber.TagSet {
			children = c.Children
			break
		}
	}
	for _, child := range children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case NodeContentIdentifier:
			n.Identifier = decodeStringValue(child)
		case NodeContentDescription:
			n.Description = decodeStringValue(child)
		case NodeContentIsRoot:
			n.IsRoot = decodeBoolValue(child)
		case NodeContentIsOnline:
			n.IsOnline = decodeBoolValue(child)
		}
	}
}

func decodeParameter(tlv ber.TLV) (*Element, error) {
	p := &Parameter{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case ParamNumber:
			if !child.Tag.Constructed && len(child.Value) > 0 {
				v, _ := ber.DecodeInteger(child.Value)
				p.Number = int32(v)
			} else if len(child.Children) > 0 {
				v, _ := ber.DecodeInteger(child.Children[0].Value)
				p.Number = int32(v)
			}
		case ParamContents:
			decodeParamContents(p, child)
		case ParamChildren:
			// Parameters can have children (unusual but spec allows it)
		}
	}
	return &Element{Parameter: p}, nil
}

func decodeQualifiedParameter(tlv ber.TLV) (*Element, error) {
	p := &Parameter{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case QParamPath:
			p.Path = decodeRelativeOID(child)
		case QParamContents:
			decodeParamContents(p, child)
		}
	}
	if len(p.Path) > 0 {
		p.Number = p.Path[len(p.Path)-1]
	}
	return &Element{Parameter: p}, nil
}

func decodeParamContents(p *Parameter, tlv ber.TLV) {
	// Unwrap SET if present inside CONTEXT wrapper.
	children := tlv.Children
	for _, c := range tlv.Children {
		if c.Tag.Class == ber.ClassUniversal && c.Tag.Number == ber.TagSet {
			children = c.Children
			break
		}
	}
	for _, child := range children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case ParamContentIdentifier:
			p.Identifier = decodeStringValue(child)
		case ParamContentDescription:
			p.Description = decodeStringValue(child)
		case ParamContentValue:
			p.Value = decodeAnyValue(child)
		case ParamContentMinimum:
			p.Minimum = decodeAnyValue(child)
		case ParamContentMaximum:
			p.Maximum = decodeAnyValue(child)
		case ParamContentAccess:
			p.Access = decodeIntValue(child)
		case ParamContentFormat:
			p.Format = decodeStringValue(child)
		case ParamContentEnumeration:
			p.Enumeration = decodeStringValue(child)
		case ParamContentFactor:
			p.Factor = decodeIntValue(child)
		case ParamContentIsOnline:
			p.IsOnline = decodeBoolValue(child)
		case ParamContentFormula:
			p.Formula = decodeStringValue(child)
		case ParamContentStep:
			p.Step = decodeAnyValue(child)
		case ParamContentDefault:
			p.Default = decodeAnyValue(child)
		case ParamContentType:
			p.Type = decodeIntValue(child)
		case ParamContentStreamIdentifier:
			p.StreamIdentifier = decodeIntValue(child)
		case ParamContentEnumMap:
			p.EnumMap = decodeEnumMap(child)
		case ParamContentSchemaIdentifiers:
			p.SchemaID = decodeStringValue(child)
		}
	}
}

func decodeMatrix(tlv ber.TLV) (*Element, error) {
	m := &Matrix{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case MatrixNumber:
			v, _ := ber.DecodeInteger(unwrapPrimitive(child))
			m.Number = int32(v)
		case MatrixContents:
			decodeMatrixContents(m, child)
		case MatrixChildren:
			m.Children = decodeElementCollection(child)
		case MatrixTargets:
			m.Targets = decodeInt32List(child)
		case MatrixSources:
			m.Sources = decodeInt32List(child)
		case MatrixConnections:
			m.Connections = decodeConnections(child)
		}
	}
	return &Element{Matrix: m}, nil
}

func decodeQualifiedMatrix(tlv ber.TLV) (*Element, error) {
	m := &Matrix{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case 0: // path
			m.Path = decodeRelativeOID(child)
		case 1: // contents
			decodeMatrixContents(m, child)
		case 2: // children
			m.Children = decodeElementCollection(child)
		case 3: // targets
			m.Targets = decodeInt32List(child)
		case 4: // sources
			m.Sources = decodeInt32List(child)
		case 5: // connections
			m.Connections = decodeConnections(child)
		}
	}
	if len(m.Path) > 0 {
		m.Number = m.Path[len(m.Path)-1]
	}
	return &Element{Matrix: m}, nil
}

func decodeMatrixContents(m *Matrix, tlv ber.TLV) {
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case MatContentIdentifier:
			m.Identifier = decodeStringValue(child)
		case MatContentDescription:
			m.Description = decodeStringValue(child)
		case MatContentType:
			m.MatrixType = decodeIntValue(child)
		case MatContentAddressingMode:
			m.AddressingMode = decodeIntValue(child)
		case MatContentTargetCount:
			m.TargetCount = int32(decodeIntValue(child))
		case MatContentSourceCount:
			m.SourceCount = int32(decodeIntValue(child))
		case MatContentMaxTotalConnects:
			m.MaxTotalConnects = int32(decodeIntValue(child))
		case MatContentMaxConnectsPerTgt:
			m.MaxConnectsPerTarget = int32(decodeIntValue(child))
		case MatContentGainParameterNumber:
			m.GainParameterNumber = int32(decodeIntValue(child))
		case MatContentSchemaIdentifiers:
			m.SchemaID = decodeStringValue(child)
		}
	}
}

func decodeFunction(tlv ber.TLV) (*Element, error) {
	f := &Function{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case FuncNumber:
			v, _ := ber.DecodeInteger(unwrapPrimitive(child))
			f.Number = int32(v)
		case FuncContents:
			decodeFuncContents(f, child)
		case FuncChildren:
			f.Children = decodeElementCollection(child)
		}
	}
	return &Element{Function: f}, nil
}

func decodeQualifiedFunction(tlv ber.TLV) (*Element, error) {
	f := &Function{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case 0: // path
			f.Path = decodeRelativeOID(child)
		case 1: // contents
			decodeFuncContents(f, child)
		case 2: // children
			f.Children = decodeElementCollection(child)
		}
	}
	if len(f.Path) > 0 {
		f.Number = f.Path[len(f.Path)-1]
	}
	return &Element{Function: f}, nil
}

func decodeFuncContents(f *Function, tlv ber.TLV) {
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case FuncContentIdentifier:
			f.Identifier = decodeStringValue(child)
		case FuncContentDescription:
			f.Description = decodeStringValue(child)
		case FuncContentArguments:
			f.Arguments = decodeTupleItems(child)
		case FuncContentResult:
			f.Result = decodeTupleItems(child)
		}
	}
}

func decodeCommand(tlv ber.TLV) (*Element, error) {
	c := &Command{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case CmdNumber:
			c.Number = decodeIntValue(child)
		case CmdDirMask:
			c.DirMask = decodeIntValue(child)
		case CmdInvID:
			v, _ := ber.DecodeInteger(unwrapPrimitive(child))
			c.InvID = int32(v)
		}
	}
	return &Element{Command: c}, nil
}

// --- helpers ---

func decodeElementCollection(tlv ber.TLV) []Element {
	var out []Element
	for _, child := range tlv.Children {
		el, err := decodeElement(child)
		if err == nil && el != nil {
			out = append(out, *el)
		}
	}
	return out
}

func decodeRelativeOID(tlv ber.TLV) []int32 {
	// RELATIVE-OID is encoded as a sequence of integers or as
	// context-wrapped OCTET STRING with BER-encoded path.
	data := unwrapPrimitive(tlv)
	if len(data) == 0 {
		return nil
	}
	// Path is encoded as a sequence of base-128 values.
	var path []int32
	pos := 0
	for pos < len(data) {
		var val int32
		for pos < len(data) {
			b := data[pos]
			pos++
			val = (val << 7) | int32(b&0x7F)
			if b&0x80 == 0 {
				break
			}
		}
		path = append(path, val)
	}
	return path
}

func decodeEnumMap(tlv ber.TLV) map[int64]string {
	m := make(map[int64]string)
	for _, child := range tlv.Children {
		if child.Tag.Class == ber.ClassApplication && child.Tag.Number == TagStringIntegerPair {
			var key int64
			var val string
			for _, pair := range child.Children {
				if pair.Tag.Class != ber.ClassContext {
					continue
				}
				switch pair.Tag.Number {
				case 0: // entryString
					val = decodeStringValue(pair)
				case 1: // entryInteger
					key = decodeIntValue(pair)
				}
			}
			m[key] = val
		}
	}
	return m
}

func decodeConnections(tlv ber.TLV) []Connection {
	var out []Connection
	for _, child := range tlv.Children {
		if child.Tag.Class == ber.ClassApplication && child.Tag.Number == TagConnection {
			c := Connection{}
			for _, field := range child.Children {
				if field.Tag.Class != ber.ClassContext {
					continue
				}
				switch field.Tag.Number {
				case ConnTarget:
					c.Target = int32(decodeIntValue(field))
				case ConnSources:
					c.Sources = decodeRelativeOIDAsInt32(field)
				case ConnOperation:
					c.Operation = decodeIntValue(field)
				case ConnDisposition:
					c.Disposition = decodeIntValue(field)
				}
			}
			out = append(out, c)
		}
	}
	return out
}

func decodeInt32List(tlv ber.TLV) []int32 {
	var out []int32
	for _, child := range tlv.Children {
		if child.Tag.Class == ber.ClassApplication {
			// Target or Source element.
			for _, field := range child.Children {
				if field.Tag.Class == ber.ClassContext && field.Tag.Number == 0 {
					v := int32(decodeIntValue(field))
					out = append(out, v)
				}
			}
		}
	}
	return out
}

func decodeTupleItems(tlv ber.TLV) []TupleItem {
	var out []TupleItem
	for _, child := range tlv.Children {
		if child.Tag.Class == ber.ClassApplication && child.Tag.Number == TagTupleItemDescription {
			ti := TupleItem{}
			for _, field := range child.Children {
				if field.Tag.Class != ber.ClassContext {
					continue
				}
				switch field.Tag.Number {
				case TupleType:
					ti.Type = decodeIntValue(field)
				case TupleName:
					ti.Name = decodeStringValue(field)
				}
			}
			out = append(out, ti)
		}
	}
	return out
}

func decodeRelativeOIDAsInt32(tlv ber.TLV) []int32 {
	data := unwrapPrimitive(tlv)
	if len(data) == 0 {
		return nil
	}
	var path []int32
	pos := 0
	for pos < len(data) {
		var val int32
		for pos < len(data) {
			b := data[pos]
			pos++
			val = (val << 7) | int32(b&0x7F)
			if b&0x80 == 0 {
				break
			}
		}
		path = append(path, val)
	}
	return path
}

// unwrapPrimitive returns the value bytes of a TLV, handling both
// primitive (direct value) and constructed (single child) forms.
func unwrapPrimitive(tlv ber.TLV) []byte {
	if !tlv.Tag.Constructed {
		return tlv.Value
	}
	if len(tlv.Children) > 0 {
		return tlv.Children[0].Value
	}
	return nil
}

func decodeStringValue(tlv ber.TLV) string {
	return ber.DecodeUTF8String(unwrapPrimitive(tlv))
}

func decodeBoolValue(tlv ber.TLV) bool {
	v, _ := ber.DecodeBoolean(unwrapPrimitive(tlv))
	return v
}

func decodeIntValue(tlv ber.TLV) int64 {
	v, _ := ber.DecodeInteger(unwrapPrimitive(tlv))
	return v
}

// decodeAnyValue decodes a BER value that could be any type.
// Used for Parameter value/min/max/step/default which are polymorphic.
func decodeAnyValue(tlv ber.TLV) interface{} {
	data := unwrapPrimitive(tlv)
	if len(data) == 0 {
		return nil
	}
	// If the context tag wraps a typed universal element, use that.
	if tlv.Tag.Constructed && len(tlv.Children) > 0 {
		child := tlv.Children[0]
		if child.Tag.Class == ber.ClassUniversal {
			switch child.Tag.Number {
			case ber.TagInteger:
				v, _ := ber.DecodeInteger(child.Value)
				return v
			case ber.TagReal:
				v, _ := ber.DecodeReal(child.Value)
				return v
			case ber.TagBoolean:
				v, _ := ber.DecodeBoolean(child.Value)
				return v
			case ber.TagUTF8String:
				return ber.DecodeUTF8String(child.Value)
			case ber.TagOctetString:
				out := make([]byte, len(child.Value))
				copy(out, child.Value)
				return out
			}
		}
	}
	// Fallback: try integer.
	v, err := ber.DecodeInteger(data)
	if err == nil {
		return v
	}
	return data
}
