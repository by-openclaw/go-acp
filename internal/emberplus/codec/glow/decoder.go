// Glow decoder — turns BER TLVs into Go structs defined in types.go.
// Every function below cites the Glow DTD page (Ember+ Documentation.pdf
// v2.50 pp. 83–93) that defines the type it handles. The decoder is
// tolerant: unknown CTX tags are skipped silently; optional fields left
// absent keep their Go zero value.
package glow

import (
	"fmt"

	"acp/internal/emberplus/codec/ber"
)

// DecodeRoot decodes a top-level Glow payload.
//
// Per spec p.93, the outermost CHOICE is Root APPLICATION[0] wrapping
// one of: RootElementCollection APPLICATION[11], StreamCollection
// APPLICATION[6], or InvocationResult APPLICATION[23]. Real-world
// providers also emit bare RootElementCollection without the [APP 0]
// wrapper, so we flatten both shapes here.
func DecodeRoot(data []byte) ([]Element, error) {
	tlvs, err := ber.DecodeAll(data)
	if err != nil {
		return nil, fmt.Errorf("glow decode: %w", err)
	}
	var out []Element
	for _, tlv := range tlvs {
		els, err := decodeElements(tlv)
		if err != nil {
			return nil, err
		}
		out = append(out, els...)
	}
	return out, nil
}

// decodeElements recursively unwraps collection envelopes. Handles:
//   - CONTEXT wrappers (e.g. [0] inside ElementCollection items)
//   - APPLICATION[0] Root CHOICE envelope (spec p.93)
//   - APPLICATION[11] RootElementCollection (spec p.93)
//   - APPLICATION[4]  ElementCollection     (spec p.92)
//   - APPLICATION[6]  StreamCollection      (spec p.93)
//
// and delegates to decodeElement for each leaf.
func decodeElements(tlv ber.TLV) ([]Element, error) {
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
		case TagRoot, TagRootElementCollection, TagElementCollection:
			var out []Element
			for _, child := range tlv.Children {
				els, err := decodeElements(child)
				if err != nil {
					return nil, err
				}
				out = append(out, els...)
			}
			return out, nil
		case TagStreamCollection:
			entries := decodeStreamCollection(tlv)
			return []Element{{Streams: entries}}, nil
		}
	}

	el, err := decodeElement(tlv)
	if err != nil {
		return nil, err
	}
	if el != nil {
		return []Element{*el}, nil
	}
	return nil, nil
}

// decodeElement dispatches a single leaf TLV to the matching type decoder.
// Returns nil (no error) for tags we do not recognise — a tolerant decoder
// keeps working when providers emit vendor-private extensions.
func decodeElement(tlv ber.TLV) (*Element, error) {
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

	if tlv.Tag.Class != ber.ClassApplication {
		return nil, nil
	}
	switch tlv.Tag.Number {
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
	case TagTemplate:
		return decodeTemplate(tlv, false)
	case TagQualifiedTemplate:
		return decodeTemplate(tlv, true)
	case TagInvocationResult:
		return decodeInvocationResult(tlv)
	}
	return nil, nil
}

// --- Node / QualifiedNode (spec p.87) ---

func decodeNode(tlv ber.TLV) (*Element, error) {
	n := &Node{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case NodeNumber:
			n.Number = int32(decodeIntValue(child))
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

// decodeNodeContents fills Node contents fields per spec p.87.
// Expects the CONTEXT[1] wrapper; unwraps the inner SET envelope.
func decodeNodeContents(n *Node, tlv ber.TLV) {
	for _, child := range unwrapSet(tlv) {
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
		case NodeContentSchemaIdentifiers:
			n.SchemaIdentifiers = decodeStringValue(child)
		case NodeContentTemplateReference:
			n.TemplateReference = decodeRelativeOID(child)
		}
	}
}

// --- Parameter / QualifiedParameter (spec p.85) ---

func decodeParameter(tlv ber.TLV) (*Element, error) {
	p := &Parameter{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case ParamNumber:
			p.Number = int32(decodeIntValue(child))
		case ParamContents:
			decodeParamContents(p, child)
		case ParamChildren:
			p.Children = decodeElementCollection(child)
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
		case QParamChildren:
			p.Children = decodeElementCollection(child)
		}
	}
	if len(p.Path) > 0 {
		p.Number = p.Path[len(p.Path)-1]
	}
	return &Element{Parameter: p}, nil
}

// decodeParamContents covers all 18 optional ParameterContents fields.
func decodeParamContents(p *Parameter, tlv ber.TLV) {
	for _, child := range unwrapSet(tlv) {
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
		case ParamContentStreamDescriptor:
			p.StreamDescriptor = decodeStreamDescription(child)
		case ParamContentSchemaIdentifiers:
			p.SchemaIdentifiers = decodeStringValue(child)
		case ParamContentTemplateReference:
			p.TemplateReference = decodeRelativeOID(child)
		}
	}
}

// --- Matrix / QualifiedMatrix (spec p.88) ---

func decodeMatrix(tlv ber.TLV) (*Element, error) {
	m := &Matrix{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case MatrixNumber:
			m.Number = int32(decodeIntValue(child))
		case MatrixContents:
			decodeMatrixContents(m, child)
		case MatrixChildren:
			m.Children = decodeElementCollection(child)
		case MatrixTargets:
			m.Targets = decodeSignalCollection(child)
		case MatrixSources:
			m.Sources = decodeSignalCollection(child)
		case MatrixConnections:
			m.Connections = decodeConnectionCollection(child)
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
		case 1:
			decodeMatrixContents(m, child)
		case 2:
			m.Children = decodeElementCollection(child)
		case 3:
			m.Targets = decodeSignalCollection(child)
		case 4:
			m.Sources = decodeSignalCollection(child)
		case 5:
			m.Connections = decodeConnectionCollection(child)
		}
	}
	if len(m.Path) > 0 {
		m.Number = m.Path[len(m.Path)-1]
	}
	return &Element{Matrix: m}, nil
}

// decodeMatrixContents covers all 12 MatrixContents CTX fields.
func decodeMatrixContents(m *Matrix, tlv ber.TLV) {
	for _, child := range unwrapSet(tlv) {
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
		case MatContentParametersLocation:
			m.ParametersLocation = decodeParametersLocation(child)
		case MatContentGainParameterNumber:
			m.GainParameterNumber = int32(decodeIntValue(child))
		case MatContentLabels:
			m.Labels = decodeLabelCollection(child)
		case MatContentSchemaIdentifiers:
			m.SchemaIdentifiers = decodeStringValue(child)
		case MatContentTemplateReference:
			m.TemplateReference = decodeRelativeOID(child)
		}
	}
}

// decodeParametersLocation handles the CHOICE on spec p.88:
//
//	ParametersLocation ::= CHOICE { basePath RELATIVE-OID, inline Integer32 }
//
// Returns either []int32 (basePath) or int32 (inline). Callers should
// type-assert the result; the plugin resolves both forms to a single path.
func decodeParametersLocation(tlv ber.TLV) any {
	// Look at the inner universal tag to tell the CHOICE alternatives apart.
	data := unwrapPrimitive(tlv)
	if tlv.Tag.Constructed && len(tlv.Children) > 0 {
		inner := tlv.Children[0]
		if inner.Tag.Class == ber.ClassUniversal {
			switch inner.Tag.Number {
			case ber.TagRelativeOID:
				return decodeRelativeOIDBytes(inner.Value)
			case ber.TagInteger:
				v, _ := ber.DecodeInteger(inner.Value)
				return int32(v)
			}
		}
	}
	// Fallback: try integer first (inline is by far the more common form).
	if len(data) <= 4 {
		if v, err := ber.DecodeInteger(data); err == nil {
			return int32(v)
		}
	}
	return decodeRelativeOIDBytes(data)
}

// --- Label (spec p.89) ---

func decodeLabelCollection(tlv ber.TLV) []Label {
	var out []Label
	// LabelCollection is SEQUENCE OF CTX[0] Label. Either a wrapping SEQUENCE
	// or inline children are acceptable; walk every APP[18] we find.
	for _, container := range flattenForApp(tlv, TagLabel) {
		out = append(out, decodeLabel(container))
	}
	return out
}

func decodeLabel(tlv ber.TLV) Label {
	l := Label{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case LabelBasePath:
			l.BasePath = decodeRelativeOID(child)
		case LabelDescription:
			l.Description = decodeStringValue(child)
		}
	}
	return l
}

// --- Signals (Target/Source, spec p.89) ---

// decodeSignalCollection walks both Target (APP[14]) and Source (APP[15])
// collections. Each element's [0] CTX carries the signal number.
func decodeSignalCollection(tlv ber.TLV) []int32 {
	var out []int32
	for _, child := range tlv.Children {
		if child.Tag.Class == ber.ClassApplication &&
			(child.Tag.Number == TagTarget || child.Tag.Number == TagSource) {
			for _, field := range child.Children {
				if field.Tag.Class == ber.ClassContext && field.Tag.Number == SignalNumber {
					out = append(out, int32(decodeIntValue(field)))
				}
			}
			continue
		}
		// Some providers skip the APP wrapper and emit CTX[0] signal numbers directly.
		if child.Tag.Class == ber.ClassContext && child.Tag.Number == 0 {
			for _, inner := range child.Children {
				if inner.Tag.Class == ber.ClassApplication {
					for _, field := range inner.Children {
						if field.Tag.Class == ber.ClassContext && field.Tag.Number == SignalNumber {
							out = append(out, int32(decodeIntValue(field)))
						}
					}
				}
			}
		}
	}
	return out
}

// --- Connections (spec p.89) ---

func decodeConnectionCollection(tlv ber.TLV) []Connection {
	var out []Connection
	for _, container := range flattenForApp(tlv, TagConnection) {
		c := Connection{
			Disposition: ConnDispTally,  // spec: default tally
			Operation:   ConnOpAbsolute, // spec: default absolute
		}
		for _, field := range container.Children {
			if field.Tag.Class != ber.ClassContext {
				continue
			}
			switch field.Tag.Number {
			case ConnTarget:
				c.Target = int32(decodeIntValue(field))
			case ConnSources:
				c.Sources = decodeRelativeOID(field)
			case ConnOperation:
				c.Operation = decodeIntValue(field)
			case ConnDisposition:
				c.Disposition = decodeIntValue(field)
			}
		}
		out = append(out, c)
	}
	return out
}

// --- Function / QualifiedFunction (spec p.91) ---

func decodeFunction(tlv ber.TLV) (*Element, error) {
	f := &Function{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case FuncNumber:
			f.Number = int32(decodeIntValue(child))
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
		case QFuncPath:
			f.Path = decodeRelativeOID(child)
		case QFuncContents:
			decodeFuncContents(f, child)
		case QFuncChildren:
			f.Children = decodeElementCollection(child)
		}
	}
	if len(f.Path) > 0 {
		f.Number = f.Path[len(f.Path)-1]
	}
	return &Element{Function: f}, nil
}

func decodeFuncContents(f *Function, tlv ber.TLV) {
	for _, child := range unwrapSet(tlv) {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case FuncContentIdentifier:
			f.Identifier = decodeStringValue(child)
		case FuncContentDescription:
			f.Description = decodeStringValue(child)
		case FuncContentArguments:
			f.Arguments = decodeTupleDescription(child)
		case FuncContentResult:
			f.Result = decodeTupleDescription(child)
		case FuncContentTemplateReference:
			f.TemplateReference = decodeRelativeOID(child)
		}
	}
}

// --- Command (spec p.86) ---

func decodeCommand(tlv ber.TLV) (*Element, error) {
	c := &Command{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case CmdCtxNumber:
			c.Number = decodeIntValue(child)
		case CmdCtxDirMask:
			c.DirMask = decodeIntValue(child)
		case CmdCtxInvocation:
			c.Invocation = decodeInvocation(child)
		}
	}
	return &Element{Command: c}, nil
}

// decodeInvocation handles Invocation APPLICATION[22] (spec p.91). The CTX[2]
// option slot may wrap the APP tag, or emit fields directly — both accepted.
func decodeInvocation(tlv ber.TLV) *Invocation {
	inv := &Invocation{}
	children := tlv.Children
	for _, c := range tlv.Children {
		if c.Tag.Class == ber.ClassApplication && c.Tag.Number == TagInvocation {
			children = c.Children
			break
		}
	}
	for _, child := range children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case InvInvocationID:
			inv.InvocationID = int32(decodeIntValue(child))
		case InvArguments:
			inv.Arguments = decodeTuple(child)
		}
	}
	return inv
}

// decodeInvocationResult handles APPLICATION[23] (spec p.92).
// success defaults to true when the field is omitted.
func decodeInvocationResult(tlv ber.TLV) (*Element, error) {
	r := &InvocationResult{Success: true}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case InvResInvocationID:
			r.InvocationID = int32(decodeIntValue(child))
		case InvResSuccess:
			r.Success = decodeBoolValue(child)
		case InvResResult:
			r.Result = decodeTuple(child)
		}
	}
	return &Element{InvocationResult: r}, nil
}

// decodeTuple handles Tuple ::= SEQUENCE OF [0] Value (spec p.92).
func decodeTuple(tlv ber.TLV) []any {
	var out []any
	for _, seq := range tlv.Children {
		if seq.Tag.Class == ber.ClassUniversal && seq.Tag.Number == ber.TagSequence {
			for _, item := range seq.Children {
				out = append(out, decodeAnyValue(item))
			}
			return out
		}
	}
	// Some providers skip the SEQUENCE wrapper and emit CTX[0] values directly.
	for _, item := range tlv.Children {
		if item.Tag.Class == ber.ClassContext {
			out = append(out, decodeAnyValue(item))
		}
	}
	return out
}

// decodeTupleDescription handles TupleDescription ::= SEQUENCE OF [0] TupleItemDescription.
func decodeTupleDescription(tlv ber.TLV) []TupleItem {
	var out []TupleItem
	for _, container := range flattenForApp(tlv, TagTupleItemDescription) {
		ti := TupleItem{}
		for _, field := range container.Children {
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
	return out
}

// --- StreamEntry / StreamCollection (spec p.93) ---

func decodeStreamCollection(tlv ber.TLV) []StreamEntry {
	var out []StreamEntry
	for _, container := range flattenForApp(tlv, TagStreamEntry) {
		e := StreamEntry{}
		for _, field := range container.Children {
			if field.Tag.Class != ber.ClassContext {
				continue
			}
			switch field.Tag.Number {
			case StreamEntryIdentifier:
				e.StreamIdentifier = decodeIntValue(field)
			case StreamEntryValue:
				e.Value = decodeAnyValue(field)
			}
		}
		out = append(out, e)
	}
	return out
}

// decodeStreamDescription returns nil when the CTX[16] field is absent or
// malformed. See spec p.86.
func decodeStreamDescription(tlv ber.TLV) *StreamDescription {
	sd := &StreamDescription{}
	// Unwrap the APP[12] envelope if present; accept inline contents too.
	fields := tlv.Children
	for _, c := range tlv.Children {
		if c.Tag.Class == ber.ClassApplication && c.Tag.Number == TagStreamDescription {
			fields = c.Children
			break
		}
	}
	seen := false
	for _, child := range fields {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case StreamDescFormat:
			sd.Format = decodeIntValue(child)
			seen = true
		case StreamDescOffset:
			sd.Offset = decodeIntValue(child)
			seen = true
		}
	}
	if !seen {
		return nil
	}
	return sd
}

// --- Template / QualifiedTemplate (spec p.84) ---

func decodeTemplate(tlv ber.TLV, qualified bool) (*Element, error) {
	t := &Template{Qualified: qualified}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassContext {
			continue
		}
		switch child.Tag.Number {
		case TemplateNumber: // also TemplatePath — same tag number
			if qualified {
				t.Path = decodeRelativeOID(child)
				if len(t.Path) > 0 {
					t.Number = t.Path[len(t.Path)-1]
				}
			} else {
				t.Number = int32(decodeIntValue(child))
			}
		case TemplateElementCtx:
			t.Element = decodeTemplateElement(child)
		case TemplateDescription:
			t.Description = decodeStringValue(child)
		}
	}
	return &Element{Template: t}, nil
}

// decodeTemplateElement realises the CHOICE over Parameter/Node/Matrix/Function
// (spec p.84). The CTX[1] wrapper contains exactly one of the four APP tags.
func decodeTemplateElement(tlv ber.TLV) *TemplateElement {
	te := &TemplateElement{}
	for _, child := range tlv.Children {
		if child.Tag.Class != ber.ClassApplication {
			continue
		}
		switch child.Tag.Number {
		case TagParameter:
			if el, err := decodeParameter(child); err == nil && el != nil {
				te.Parameter = el.Parameter
			}
		case TagNode:
			if el, err := decodeNode(child); err == nil && el != nil {
				te.Node = el.Node
			}
		case TagMatrix:
			if el, err := decodeMatrix(child); err == nil && el != nil {
				te.Matrix = el.Matrix
			}
		case TagFunction:
			if el, err := decodeFunction(child); err == nil && el != nil {
				te.Function = el.Function
			}
		}
	}
	return te
}

// --- generic helpers ---

func decodeElementCollection(tlv ber.TLV) []Element {
	var out []Element
	// ElementCollection may appear directly or wrapped in APP[4]. In both
	// cases each item sits inside CTX[0]. Walk them all.
	containers := []ber.TLV{tlv}
	for _, c := range tlv.Children {
		if c.Tag.Class == ber.ClassApplication && c.Tag.Number == TagElementCollection {
			containers = []ber.TLV{c}
			break
		}
	}
	for _, container := range containers {
		for _, child := range container.Children {
			el, err := decodeElement(child)
			if err == nil && el != nil {
				out = append(out, *el)
			}
		}
	}
	return out
}

// unwrapSet looks for a UNIVERSAL SET inside a CONTEXT wrapper (the standard
// contents layout: [CTX N] → [UNIVERSAL SET] → fields). Returns the SET
// children if present, otherwise the CONTEXT children directly.
func unwrapSet(tlv ber.TLV) []ber.TLV {
	for _, c := range tlv.Children {
		if c.Tag.Class == ber.ClassUniversal && c.Tag.Number == ber.TagSet {
			return c.Children
		}
	}
	return tlv.Children
}

// flattenForApp returns every descendant TLV that carries the given APP tag.
// Used for collections where provider styles vary: some nest CTX[0] above
// the APP wrapper, some skip the CTX; we accept both.
func flattenForApp(tlv ber.TLV, appTag uint32) []ber.TLV {
	var out []ber.TLV
	var walk func(ber.TLV)
	walk = func(t ber.TLV) {
		if t.Tag.Class == ber.ClassApplication && t.Tag.Number == appTag {
			out = append(out, t)
			return
		}
		for _, c := range t.Children {
			walk(c)
		}
	}
	walk(tlv)
	return out
}

// decodeRelativeOID accepts a CTX-wrapped field. Handles primitive and
// constructed forms; unwraps an inner UNIVERSAL RELATIVE-OID when present.
func decodeRelativeOID(tlv ber.TLV) []int32 {
	if tlv.Tag.Constructed && len(tlv.Children) > 0 {
		inner := tlv.Children[0]
		if inner.Tag.Class == ber.ClassUniversal && inner.Tag.Number == ber.TagRelativeOID {
			return decodeRelativeOIDBytes(inner.Value)
		}
	}
	return decodeRelativeOIDBytes(unwrapPrimitive(tlv))
}

// decodeRelativeOIDBytes walks a RelOID body (concatenated base-128 ints).
func decodeRelativeOIDBytes(data []byte) []int32 {
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

// decodeEnumMap handles StringIntegerCollection APPLICATION[8] (spec p.86).
func decodeEnumMap(tlv ber.TLV) map[int64]string {
	m := make(map[int64]string)
	for _, pair := range flattenForApp(tlv, TagStringIntegerPair) {
		var key int64
		var val string
		for _, field := range pair.Children {
			if field.Tag.Class != ber.ClassContext {
				continue
			}
			switch field.Tag.Number {
			case 0:
				val = decodeStringValue(field)
			case 1:
				key = decodeIntValue(field)
			}
		}
		m[key] = val
	}
	return m
}

// unwrapPrimitive returns the value bytes of a TLV, handling both primitive
// and constructed (single child) forms used by providers interchangeably.
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

// decodeAnyValue implements the Value CHOICE (spec p.86): integer, real,
// string, boolean, octets, or null. Returns nil for null/empty/unknown.
func decodeAnyValue(tlv ber.TLV) any {
	if tlv.Tag.Constructed && len(tlv.Children) > 0 {
		inner := tlv.Children[0]
		if inner.Tag.Class == ber.ClassUniversal {
			switch inner.Tag.Number {
			case ber.TagInteger:
				v, _ := ber.DecodeInteger(inner.Value)
				return v
			case ber.TagReal:
				v, _ := ber.DecodeReal(inner.Value)
				return v
			case ber.TagBoolean:
				v, _ := ber.DecodeBoolean(inner.Value)
				return v
			case ber.TagUTF8String:
				return ber.DecodeUTF8String(inner.Value)
			case ber.TagOctetString:
				out := make([]byte, len(inner.Value))
				copy(out, inner.Value)
				return out
			case ber.TagNull:
				return nil
			}
		}
	}
	data := unwrapPrimitive(tlv)
	if len(data) == 0 {
		return nil
	}
	if v, err := ber.DecodeInteger(data); err == nil {
		return v
	}
	return data
}
