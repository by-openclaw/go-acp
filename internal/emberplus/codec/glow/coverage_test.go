package glow

import (
	"bytes"
	"errors"
	"testing"

	"dhs/internal/emberplus/codec/ber"
)

// decodeOne BER-encodes then Glow-decodes a single APPLICATION TLV,
// returning the resulting Element. Mirrors the helper pattern in
// glow_test.go for direct decoder coverage.
func decodeOne(t *testing.T, tlv ber.TLV) *Element {
	t.Helper()
	dec, _, err := ber.DecodeTLV(ber.EncodeTLV(tlv))
	if err != nil {
		t.Fatalf("BER decode: %v", err)
	}
	return decodeElement(dec)
}

// TestDecodeRoot_Error covers the ErrDecodeFailed wrap when the BER layer
// rejects the bytes.
func TestDecodeRoot_Error(t *testing.T) {
	// 0x02 0x05 0x01 — INTEGER claiming 5 value bytes but only 1 present.
	_, err := DecodeRoot([]byte{0x02, 0x05, 0x01})
	if !errors.Is(err, ErrDecodeFailed) {
		t.Errorf("got %v, want ErrDecodeFailed", err)
	}
	if !errors.Is(err, ber.ErrTruncated) {
		t.Errorf("ber cause not preserved in chain: %v", err)
	}
}

// TestDecodeElement_Unrecognised covers the tolerant nil return for an
// unknown APPLICATION tag and a non-application/non-context leaf.
func TestDecodeElement_Unrecognised(t *testing.T) {
	// APPLICATION tag 30 is not a Glow element kind.
	el := decodeOne(t, ber.AppConstructed(30, ber.ContextConstructed(0, ber.Integer(1))))
	if el != nil {
		t.Errorf("unknown APP tag should decode to nil element, got %+v", el)
	}
	// UNIVERSAL primitive leaf → nil.
	dec, _, _ := ber.DecodeTLV(ber.EncodeTLV(ber.Integer(5)))
	if got := decodeElement(dec); got != nil {
		t.Errorf("universal leaf: got %v, want nil", got)
	}

	// Route an unknown APP tag through DecodeRoot → decodeElements, whose
	// leaf branch returns nil (no element appended).
	els, err := DecodeRoot(ber.EncodeTLV(
		ber.AppConstructed(30, ber.ContextConstructed(0, ber.Integer(1))),
	))
	if err != nil {
		t.Fatalf("DecodeRoot unknown app: %v", err)
	}
	if len(els) != 0 {
		t.Errorf("unknown app tag should yield no elements, got %d", len(els))
	}
}

// TestDecodeElement_ContextWrapper covers the CONTEXT-class recursion in
// decodeElement (CTX wrapping an APP element).
func TestDecodeElement_ContextWrapper(t *testing.T) {
	wrapped := ber.ContextConstructed(0,
		ber.AppConstructed(TagNode, ber.ContextConstructed(NodeNumber, ber.Integer(3))),
	)
	dec, _, _ := ber.DecodeTLV(ber.EncodeTLV(wrapped))
	el := decodeElement(dec)
	if el == nil || el.Node == nil || el.Node.Number != 3 {
		t.Fatalf("context-wrapped node: el=%+v", el)
	}
	// Empty CONTEXT wrapper → nil.
	emptyCtx := ber.ContextConstructed(0)
	dec2, _, _ := ber.DecodeTLV(ber.EncodeTLV(emptyCtx))
	if got := decodeElement(dec2); got != nil {
		t.Errorf("empty ctx: got %v, want nil", got)
	}
}

// TestDecodeNode_AllContents covers the Node content fields and children
// not already exercised: isRoot, schemaIdentifiers, templateReference,
// the unknown-CTX recorder, and the children collection.
func TestDecodeNode_AllContents(t *testing.T) {
	node := ber.AppConstructed(TagNode,
		ber.ContextConstructed(NodeNumber, ber.Integer(1)),
		ber.ContextConstructed(NodeContents,
			ber.Set(
				ber.ContextConstructed(NodeContentIsRoot, ber.Boolean(true)),
				ber.ContextConstructed(NodeContentSchemaIdentifiers, ber.UTF8("de.lawo/schema")),
				ber.ContextConstructed(NodeContentTemplateReference,
					ber.RelOID(encodeRelativeOID([]int32{1, 2}))),
				// Unknown CTX (99) recorded, repeated to hit appendUniqueInt32 dedup.
				ber.ContextConstructed(99, ber.Integer(0)),
				ber.ContextConstructed(99, ber.Integer(0)),
			),
		),
		ber.ContextConstructed(NodeChildren,
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0,
					ber.AppConstructed(TagNode, ber.ContextConstructed(NodeNumber, ber.Integer(7))),
				),
			),
		),
	)
	n := decodeOne(t, node).Node
	if !n.IsRoot {
		t.Error("isRoot not decoded")
	}
	if n.SchemaIdentifiers != "de.lawo/schema" {
		t.Errorf("schemaIdentifiers: %q", n.SchemaIdentifiers)
	}
	if len(n.TemplateReference) != 2 {
		t.Errorf("templateReference: %v", n.TemplateReference)
	}
	if len(n.UnknownContents) != 1 || n.UnknownContents[0] != 99 {
		t.Errorf("UnknownContents: %v, want [99] (deduped)", n.UnknownContents)
	}
	if len(n.Children) != 1 || n.Children[0].Node.Number != 7 {
		t.Errorf("children not decoded: %+v", n.Children)
	}
}

// TestDecodeParameter_AllContents covers the parameter content fields and
// children branches not in glow_test.go.
func TestDecodeParameter_AllContents(t *testing.T) {
	param := ber.AppConstructed(TagParameter,
		ber.ContextConstructed(ParamNumber, ber.Integer(1)),
		ber.ContextConstructed(ParamContents,
			ber.Set(
				ber.ContextConstructed(ParamContentEnumeration, ber.UTF8("a\nb\nc")),
				ber.ContextConstructed(ParamContentFactor, ber.Integer(10)),
				ber.ContextConstructed(ParamContentIsOnline, ber.Boolean(true)),
				ber.ContextConstructed(ParamContentFormula, ber.UTF8("x*2")),
				ber.ContextConstructed(ParamContentDefault, ber.Integer(0)),
				ber.ContextConstructed(ParamContentEnumMap,
					ber.AppConstructed(TagStringIntegerCollection,
						ber.ContextConstructed(0,
							ber.AppConstructed(TagStringIntegerPair,
								ber.ContextConstructed(0, ber.UTF8("off")),
								ber.ContextConstructed(1, ber.Integer(0)),
							),
						),
					),
				),
				ber.ContextConstructed(ParamContentSchemaIdentifiers, ber.UTF8("sch")),
				ber.ContextConstructed(123, ber.Integer(0)), // unknown
			),
		),
		ber.ContextConstructed(ParamChildren,
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0,
					ber.AppConstructed(TagParameter, ber.ContextConstructed(ParamNumber, ber.Integer(9))),
				),
			),
		),
	)
	p := decodeOne(t, param).Parameter
	if p.Enumeration != "a\nb\nc" || p.Factor != 10 || !p.IsOnline || p.Formula != "x*2" {
		t.Errorf("scalar contents wrong: %+v", p)
	}
	if p.SchemaIdentifiers != "sch" {
		t.Errorf("schemaIdentifiers: %q", p.SchemaIdentifiers)
	}
	if p.EnumMap[0] != "off" {
		t.Errorf("enumMap: %v", p.EnumMap)
	}
	if len(p.UnknownContents) != 1 || p.UnknownContents[0] != 123 {
		t.Errorf("UnknownContents: %v", p.UnknownContents)
	}
	if len(p.Children) != 1 || p.Children[0].Parameter.Number != 9 {
		t.Errorf("children: %+v", p.Children)
	}
}

// TestDecodeMatrix_Full covers decodeMatrix, decodeMatrixContents (every
// field + unknown), targets/sources signal collections, and connections.
func TestDecodeMatrix_Full(t *testing.T) {
	matrix := ber.AppConstructed(TagMatrix,
		ber.ContextConstructed(MatrixNumber, ber.Integer(1)),
		ber.ContextConstructed(MatrixContents,
			ber.Set(
				ber.ContextConstructed(MatContentIdentifier, ber.UTF8("MV")),
				ber.ContextConstructed(MatContentDescription, ber.UTF8("Main video")),
				ber.ContextConstructed(MatContentType, ber.Integer(MatrixTypeNToN)),
				ber.ContextConstructed(MatContentAddressingMode, ber.Integer(MatrixAddrNonLinear)),
				ber.ContextConstructed(MatContentTargetCount, ber.Integer(4)),
				ber.ContextConstructed(MatContentSourceCount, ber.Integer(8)),
				ber.ContextConstructed(MatContentMaxTotalConnects, ber.Integer(16)),
				ber.ContextConstructed(MatContentMaxConnectsPerTgt, ber.Integer(2)),
				ber.ContextConstructed(MatContentParametersLocation, ber.Integer(99)),
				ber.ContextConstructed(MatContentGainParameterNumber, ber.Integer(5)),
				ber.ContextConstructed(MatContentLabels,
					ber.AppConstructed(TagLabel,
						ber.ContextConstructed(LabelBasePath, ber.RelOID(encodeRelativeOID([]int32{1, 1000}))),
						ber.ContextConstructed(LabelDescription, ber.UTF8("primary")),
					),
				),
				ber.ContextConstructed(MatContentSchemaIdentifiers, ber.UTF8("sch")),
				ber.ContextConstructed(MatContentTemplateReference, ber.RelOID(encodeRelativeOID([]int32{2}))),
				ber.ContextConstructed(77, ber.Integer(0)), // unknown
			),
		),
		ber.ContextConstructed(MatrixTargets,
			ber.AppConstructed(TagTarget, ber.ContextConstructed(SignalNumber, ber.Integer(0))),
			ber.AppConstructed(TagTarget, ber.ContextConstructed(SignalNumber, ber.Integer(1))),
		),
		ber.ContextConstructed(MatrixSources,
			ber.AppConstructed(TagSource, ber.ContextConstructed(SignalNumber, ber.Integer(5))),
		),
		ber.ContextConstructed(MatrixConnections,
			ber.AppConstructed(TagConnection,
				ber.ContextConstructed(ConnTarget, ber.Integer(0)),
				ber.ContextConstructed(ConnSources, ber.RelOID(encodeRelativeOID([]int32{5}))),
				ber.ContextConstructed(ConnOperation, ber.Integer(ConnOpConnect)),
				ber.ContextConstructed(ConnDisposition, ber.Integer(ConnDispModified)),
			),
		),
		ber.ContextConstructed(MatrixChildren,
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0,
					ber.AppConstructed(TagNode, ber.ContextConstructed(NodeNumber, ber.Integer(2))),
				),
			),
		),
	)
	m := decodeOne(t, matrix).Matrix
	if m.Identifier != "MV" || m.Description != "Main video" {
		t.Errorf("identity wrong: %+v", m)
	}
	if m.MatrixType != MatrixTypeNToN || m.AddressingMode != MatrixAddrNonLinear {
		t.Errorf("type/mode wrong: %d/%d", m.MatrixType, m.AddressingMode)
	}
	if m.TargetCount != 4 || m.SourceCount != 8 || m.MaxTotalConnects != 16 || m.MaxConnectsPerTarget != 2 {
		t.Errorf("counts wrong: %+v", m)
	}
	if loc, ok := m.ParametersLocation.(int32); !ok || loc != 99 {
		t.Errorf("parametersLocation (inline int): %v", m.ParametersLocation)
	}
	if m.GainParameterNumber != 5 {
		t.Errorf("gainParameterNumber: %d", m.GainParameterNumber)
	}
	if len(m.Labels) != 1 || m.Labels[0].Description != "primary" || len(m.Labels[0].BasePath) != 2 {
		t.Errorf("labels: %+v", m.Labels)
	}
	if m.SchemaIdentifiers != "sch" || len(m.TemplateReference) != 1 {
		t.Errorf("schema/template wrong: %+v", m)
	}
	if len(m.UnknownContents) != 1 || m.UnknownContents[0] != 77 {
		t.Errorf("UnknownContents: %v", m.UnknownContents)
	}
	if len(m.Targets) != 2 || m.Targets[0] != 0 || m.Targets[1] != 1 {
		t.Errorf("targets: %v", m.Targets)
	}
	if len(m.Sources) != 1 || m.Sources[0] != 5 {
		t.Errorf("sources: %v", m.Sources)
	}
	if len(m.Connections) != 1 || m.Connections[0].Disposition != ConnDispModified {
		t.Errorf("connections: %+v", m.Connections)
	}
	if len(m.Children) != 1 {
		t.Errorf("children: %+v", m.Children)
	}
}

// TestDecodeQualifiedMatrix covers decodeQualifiedMatrix path + every CTX
// field branch (children/targets/sources).
func TestDecodeQualifiedMatrix(t *testing.T) {
	qm := ber.AppConstructed(TagQualifiedMatrix,
		ber.ContextConstructed(0, ber.RelOID(encodeRelativeOID([]int32{1, 3}))),
		ber.ContextConstructed(1, ber.Set(
			ber.ContextConstructed(MatContentTargetCount, ber.Integer(2)),
		)),
		ber.ContextConstructed(2,
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0,
					ber.AppConstructed(TagNode, ber.ContextConstructed(NodeNumber, ber.Integer(9))),
				),
			),
		),
		ber.ContextConstructed(3,
			ber.AppConstructed(TagTarget, ber.ContextConstructed(SignalNumber, ber.Integer(0))),
		),
		ber.ContextConstructed(4,
			ber.AppConstructed(TagSource, ber.ContextConstructed(SignalNumber, ber.Integer(1))),
		),
		ber.ContextConstructed(5,
			ber.AppConstructed(TagConnection, ber.ContextConstructed(ConnTarget, ber.Integer(0))),
		),
	)
	m := decodeOne(t, qm).Matrix
	if len(m.Path) != 2 || m.Number != 3 {
		t.Errorf("path/number: %v / %d", m.Path, m.Number)
	}
	if m.TargetCount != 2 {
		t.Errorf("targetCount: %d", m.TargetCount)
	}
	if len(m.Children) != 1 || len(m.Targets) != 1 || len(m.Sources) != 1 || len(m.Connections) != 1 {
		t.Errorf("collections: %+v", m)
	}
}

// TestDecodeParametersLocation covers the CHOICE: RelOID basePath, inline
// integer, and the fallback paths.
func TestDecodeParametersLocation(t *testing.T) {
	// basePath (RELATIVE-OID inner).
	relTLV := ber.ContextConstructed(MatContentParametersLocation,
		ber.RelOID(encodeRelativeOID([]int32{1, 2, 3})))
	got := decodeParametersLocation(relTLV)
	if path, ok := got.([]int32); !ok || len(path) != 3 {
		t.Errorf("basePath: got %v", got)
	}
	// inline integer inner.
	intTLV := ber.ContextConstructed(MatContentParametersLocation, ber.Integer(42))
	if v, ok := decodeParametersLocation(intTLV).(int32); !ok || v != 42 {
		t.Errorf("inline int: got %v", decodeParametersLocation(intTLV))
	}
	// Fallback: primitive CTX carrying a small integer body (<=4 bytes).
	prim := ber.ContextPrimitive(MatContentParametersLocation, ber.EncodeInteger(7))
	if v, ok := decodeParametersLocation(prim).(int32); !ok || v != 7 {
		t.Errorf("fallback int: got %v", decodeParametersLocation(prim))
	}
	// Fallback: primitive CTX with a >4-byte body → RelOID path.
	bigBody := ber.ContextPrimitive(MatContentParametersLocation, []byte{1, 2, 3, 4, 5})
	if _, ok := decodeParametersLocation(bigBody).([]int32); !ok {
		t.Errorf("fallback relOID: got %v", decodeParametersLocation(bigBody))
	}
}

// TestDecodeSignalCollection_CTXForm covers the wrapper-free CTX[0] form
// where signal numbers are nested under CTX[0] rather than the APP wrapper.
func TestDecodeSignalCollection_CTXForm(t *testing.T) {
	coll := ber.ContextConstructed(MatrixTargets,
		ber.ContextConstructed(0,
			ber.AppConstructed(TagTarget, ber.ContextConstructed(SignalNumber, ber.Integer(3))),
		),
	)
	got := decodeSignalCollection(coll)
	if len(got) != 1 || got[0] != 3 {
		t.Errorf("CTX-form signal collection: got %v, want [3]", got)
	}
}

// TestDecodeFunction covers decodeFunction, decodeFuncContents (every
// field + unknown), arguments/result tuple descriptions, and children.
func TestDecodeFunction(t *testing.T) {
	fn := ber.AppConstructed(TagFunction,
		ber.ContextConstructed(FuncNumber, ber.Integer(5)),
		ber.ContextConstructed(FuncContents,
			ber.Set(
				ber.ContextConstructed(FuncContentIdentifier, ber.UTF8("reboot")),
				ber.ContextConstructed(FuncContentDescription, ber.UTF8("Reboot device")),
				ber.ContextConstructed(FuncContentArguments,
					ber.AppConstructed(TagTupleItemDescription,
						ber.ContextConstructed(TupleType, ber.Integer(ParamTypeInteger)),
						ber.ContextConstructed(TupleName, ber.UTF8("delay")),
					),
				),
				ber.ContextConstructed(FuncContentResult,
					ber.AppConstructed(TagTupleItemDescription,
						ber.ContextConstructed(TupleType, ber.Integer(ParamTypeBoolean)),
						ber.ContextConstructed(TupleName, ber.UTF8("ok")),
					),
				),
				ber.ContextConstructed(FuncContentTemplateReference, ber.RelOID(encodeRelativeOID([]int32{3}))),
				ber.ContextConstructed(88, ber.Integer(0)), // unknown
			),
		),
		ber.ContextConstructed(FuncChildren,
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0,
					ber.AppConstructed(TagFunction, ber.ContextConstructed(FuncNumber, ber.Integer(6))),
				),
			),
		),
	)
	f := decodeOne(t, fn).Function
	if f.Number != 5 || f.Identifier != "reboot" || f.Description != "Reboot device" {
		t.Errorf("function header: %+v", f)
	}
	if len(f.Arguments) != 1 || f.Arguments[0].Name != "delay" || f.Arguments[0].Type != ParamTypeInteger {
		t.Errorf("arguments: %+v", f.Arguments)
	}
	if len(f.Result) != 1 || f.Result[0].Name != "ok" {
		t.Errorf("result: %+v", f.Result)
	}
	if len(f.TemplateReference) != 1 || len(f.UnknownContents) != 1 || f.UnknownContents[0] != 88 {
		t.Errorf("templateRef/unknown: %+v / %v", f.TemplateReference, f.UnknownContents)
	}
	if len(f.Children) != 1 || f.Children[0].Function.Number != 6 {
		t.Errorf("children: %+v", f.Children)
	}
}

// TestDecodeQualifiedFunction covers decodeQualifiedFunction including
// path→number derivation, contents, and children.
func TestDecodeQualifiedFunction(t *testing.T) {
	qf := ber.AppConstructed(TagQualifiedFunction,
		ber.ContextConstructed(QFuncPath, ber.RelOID(encodeRelativeOID([]int32{1, 4}))),
		ber.ContextConstructed(QFuncContents, ber.Set(
			ber.ContextConstructed(FuncContentIdentifier, ber.UTF8("f")),
		)),
		ber.ContextConstructed(QFuncChildren,
			ber.AppConstructed(TagElementCollection,
				ber.ContextConstructed(0,
					ber.AppConstructed(TagFunction, ber.ContextConstructed(FuncNumber, ber.Integer(2))),
				),
			),
		),
	)
	f := decodeOne(t, qf).Function
	if len(f.Path) != 2 || f.Number != 4 || f.Identifier != "f" {
		t.Errorf("qualified function: %+v", f)
	}
	if len(f.Children) != 1 {
		t.Errorf("children: %+v", f.Children)
	}
}

// TestDecodeTuple_WrapperFree covers the CTX[0]-direct tuple form (no
// SEQUENCE wrapper) in decodeTuple, via an InvocationResult.
func TestDecodeTuple_WrapperFree(t *testing.T) {
	res := ber.AppConstructed(TagInvocationResult,
		ber.ContextConstructed(InvResInvocationID, ber.Integer(1)),
		ber.ContextConstructed(InvResResult,
			// No inner SEQUENCE — values sit directly as CTX[0].
			ber.ContextConstructed(0, ber.Integer(11)),
			ber.ContextConstructed(0, ber.UTF8("z")),
		),
	)
	dec, _, _ := ber.DecodeTLV(ber.EncodeTLV(res))
	el := decodeInvocationResult(dec)
	got := el.InvocationResult.Result
	if len(got) != 2 || got[0].(int64) != 11 || got[1].(string) != "z" {
		t.Errorf("wrapper-free tuple: %v", got)
	}
}

// TestDecodeTemplateElement_MatrixFunction covers the Matrix and Function
// CHOICE arms of decodeTemplateElement.
func TestDecodeTemplateElement_MatrixFunction(t *testing.T) {
	tmplMatrix := ber.AppConstructed(TagTemplate,
		ber.ContextConstructed(TemplateNumber, ber.Integer(1)),
		ber.ContextConstructed(TemplateElementCtx,
			ber.AppConstructed(TagMatrix,
				ber.ContextConstructed(MatrixNumber, ber.Integer(0)),
				ber.ContextConstructed(MatrixContents, ber.Set(
					ber.ContextConstructed(MatContentIdentifier, ber.UTF8("mtx_proto")),
				)),
			),
		),
	)
	els, err := DecodeRoot(ber.EncodeTLV(tmplMatrix))
	if err != nil {
		t.Fatalf("decode matrix template: %v", err)
	}
	if els[0].Template.Element == nil || els[0].Template.Element.Matrix == nil ||
		els[0].Template.Element.Matrix.Identifier != "mtx_proto" {
		t.Fatalf("matrix template element: %+v", els[0].Template.Element)
	}

	tmplFunc := ber.AppConstructed(TagTemplate,
		ber.ContextConstructed(TemplateNumber, ber.Integer(2)),
		ber.ContextConstructed(TemplateElementCtx,
			ber.AppConstructed(TagFunction,
				ber.ContextConstructed(FuncNumber, ber.Integer(0)),
				ber.ContextConstructed(FuncContents, ber.Set(
					ber.ContextConstructed(FuncContentIdentifier, ber.UTF8("fn_proto")),
				)),
			),
		),
	)
	els2, err := DecodeRoot(ber.EncodeTLV(tmplFunc))
	if err != nil {
		t.Fatalf("decode func template: %v", err)
	}
	if els2[0].Template.Element == nil || els2[0].Template.Element.Function == nil ||
		els2[0].Template.Element.Function.Identifier != "fn_proto" {
		t.Fatalf("function template element: %+v", els2[0].Template.Element)
	}
}

// TestDecodeAnyValue_OctetAndNull covers the octet-string copy branch, the
// null branch, and the primitive integer fallback / raw-bytes fallback.
func TestDecodeAnyValue_OctetAndNull(t *testing.T) {
	// Octet string inner.
	octParam := ber.AppConstructed(TagParameter,
		ber.ContextConstructed(ParamNumber, ber.Integer(1)),
		ber.ContextConstructed(ParamContents, ber.Set(
			ber.ContextConstructed(ParamContentValue, ber.OctetStr([]byte{0xDE, 0xAD})),
		)),
	)
	p := decodeOne(t, octParam).Parameter
	if b, ok := p.Value.([]byte); !ok || !bytes.Equal(b, []byte{0xDE, 0xAD}) {
		t.Errorf("octet value: got %v", p.Value)
	}

	// Null inner → nil value.
	nullParam := ber.AppConstructed(TagParameter,
		ber.ContextConstructed(ParamNumber, ber.Integer(1)),
		ber.ContextConstructed(ParamContents, ber.Set(
			ber.ContextConstructed(ParamContentValue,
				ber.Primitive(ber.ClassUniversal, ber.TagNull, nil)),
		)),
	)
	if v := decodeOne(t, nullParam).Parameter.Value; v != nil {
		t.Errorf("null value: got %v, want nil", v)
	}

	// Primitive CTX (no constructed inner) carrying an integer body → int64.
	primInt := ber.ContextPrimitive(ParamContentValue, ber.EncodeInteger(123))
	if v, ok := decodeAnyValue(primInt).(int64); !ok || v != 123 {
		t.Errorf("primitive int fallback: got %v", decodeAnyValue(primInt))
	}

	// Empty primitive → nil.
	if v := decodeAnyValue(ber.ContextPrimitive(ParamContentValue, nil)); v != nil {
		t.Errorf("empty value: got %v, want nil", v)
	}
}

// TestEncode_LegacyAndExtras covers the encoders not yet exercised:
// EncodeSubscribeLegacy / EncodeUnsubscribeLegacy (incl. empty-path guard),
// EncodeUnsubscribe, and EncodeMatrixGetDirectory.
func TestEncode_LegacyAndExtras(t *testing.T) {
	// Legacy subscribe over a multi-segment path → decodes to nested Nodes.
	data := EncodeSubscribeLegacy([]int32{1, 2, 3})
	els, err := DecodeRoot(data)
	if err != nil {
		t.Fatalf("legacy subscribe decode: %v", err)
	}
	if len(els) == 0 || els[0].Node == nil {
		t.Fatalf("legacy subscribe shape: %+v", els)
	}
	// Walk to the leaf Parameter carrying the Command.
	var foundCmd bool
	var walk func([]Element)
	walk = func(es []Element) {
		for _, e := range es {
			if e.Command != nil && e.Command.Number == CmdSubscribe {
				foundCmd = true
			}
			if e.Node != nil {
				walk(e.Node.Children)
			}
			if e.Parameter != nil {
				walk(e.Parameter.Children)
			}
		}
	}
	walk(els)
	if !foundCmd {
		t.Error("legacy subscribe: Command(30) not found in tree")
	}

	// Empty path guard returns nil.
	if EncodeSubscribeLegacy(nil) != nil {
		t.Error("legacy subscribe with empty path should return nil")
	}

	// Legacy unsubscribe just needs to produce decodable bytes.
	if _, err := DecodeRoot(EncodeUnsubscribeLegacy([]int32{4, 5})); err != nil {
		t.Fatalf("legacy unsubscribe decode: %v", err)
	}

	// EncodeUnsubscribe (qualified form).
	if _, err := DecodeRoot(EncodeUnsubscribe([]int32{1, 2})); err != nil {
		t.Fatalf("unsubscribe decode: %v", err)
	}

	// EncodeMatrixGetDirectory.
	if _, err := DecodeRoot(EncodeMatrixGetDirectory([]int32{1})); err != nil {
		t.Fatalf("matrix getdir decode: %v", err)
	}
}

// TestEncodeAnyValue_AllTypes covers every arm of encodeAnyValue including
// the default and the negative-relOID clamp in encodeBase128Int32.
func TestEncodeAnyValue_AllTypes(t *testing.T) {
	cases := []struct {
		in   any
		kind string
	}{
		{int(1), "int"},
		{int32(2), "int32"},
		{int64(3), "int64"},
		{float64(1.5), "float64"},
		{float32(2.5), "float32"},
		{"s", "string"},
		{true, "bool"},
		{[]byte{0x01}, "bytes"},
		{struct{}{}, "default"},
	}
	for _, c := range cases {
		tlv := encodeAnyValue(c.in)
		if len(ber.EncodeTLV(tlv)) == 0 {
			t.Errorf("%s: empty TLV", c.kind)
		}
	}

	// Negative path element clamps to 0 (encodeBase128Int32 guard).
	out := encodeRelativeOID([]int32{-5, 200})
	if len(out) == 0 || out[0] != 0x00 {
		t.Errorf("negative relOID clamp: got % x", out)
	}
}

// nonCtx is a UNIVERSAL INTEGER child — a non-CONTEXT sibling used to
// exercise the "skip non-context child" continue branch present in every
// element decoder. Providers legally interleave non-context items.
func nonCtx() ber.TLV { return ber.Integer(0) }

// TestDecoders_SkipNonContextChild drives the `continue` skip-branch in
// every decoder by feeding each one a non-CONTEXT child alongside its
// real fields.
func TestDecoders_SkipNonContextChild(t *testing.T) {
	// Node: non-context child at the wrapper level.
	if decodeOne(t, ber.AppConstructed(TagNode,
		nonCtx(),
		ber.ContextConstructed(NodeNumber, ber.Integer(1)),
	)).Node.Number != 1 {
		t.Error("node skip-branch")
	}
	// QualifiedNode: non-context at wrapper + contents via CTX[1].
	qn := decodeOne(t, ber.AppConstructed(TagQualifiedNode,
		nonCtx(),
		ber.ContextConstructed(QNodePath, ber.RelOID(encodeRelativeOID([]int32{1, 5}))),
		ber.ContextConstructed(QNodeContents, ber.Set(
			ber.ContextConstructed(NodeContentIdentifier, ber.UTF8("qn")),
		)),
	)).Node
	if qn.Number != 5 || qn.Identifier != "qn" {
		t.Errorf("qualified node skip/contents: %+v", qn)
	}
	// NodeContents: non-context child inside the SET.
	if decodeOne(t, ber.AppConstructed(TagNode,
		ber.ContextConstructed(NodeContents, ber.Set(
			nonCtx(),
			ber.ContextConstructed(NodeContentIdentifier, ber.UTF8("x")),
		)),
	)).Node.Identifier != "x" {
		t.Error("node contents skip-branch")
	}
	// Parameter wrapper + ParamContents SET.
	if decodeOne(t, ber.AppConstructed(TagParameter,
		nonCtx(),
		ber.ContextConstructed(ParamContents, ber.Set(
			nonCtx(),
			ber.ContextConstructed(ParamContentIdentifier, ber.UTF8("p")),
		)),
	)).Parameter.Identifier != "p" {
		t.Error("parameter skip-branch")
	}
	// QualifiedParameter wrapper.
	if decodeOne(t, ber.AppConstructed(TagQualifiedParameter,
		nonCtx(),
		ber.ContextConstructed(QParamPath, ber.RelOID(encodeRelativeOID([]int32{2}))),
	)).Parameter.Number != 2 {
		t.Error("qualified parameter skip-branch")
	}
	// Matrix wrapper + MatrixContents SET.
	if decodeOne(t, ber.AppConstructed(TagMatrix,
		nonCtx(),
		ber.ContextConstructed(MatrixContents, ber.Set(
			nonCtx(),
			ber.ContextConstructed(MatContentIdentifier, ber.UTF8("m")),
		)),
	)).Matrix.Identifier != "m" {
		t.Error("matrix skip-branch")
	}
	// QualifiedMatrix wrapper.
	if decodeOne(t, ber.AppConstructed(TagQualifiedMatrix,
		nonCtx(),
		ber.ContextConstructed(0, ber.RelOID(encodeRelativeOID([]int32{3}))),
	)).Matrix.Number != 3 {
		t.Error("qualified matrix skip-branch")
	}
	// Function wrapper + FuncContents SET.
	if decodeOne(t, ber.AppConstructed(TagFunction,
		nonCtx(),
		ber.ContextConstructed(FuncContents, ber.Set(
			nonCtx(),
			ber.ContextConstructed(FuncContentIdentifier, ber.UTF8("fn")),
		)),
	)).Function.Identifier != "fn" {
		t.Error("function skip-branch")
	}
	// QualifiedFunction wrapper.
	if decodeOne(t, ber.AppConstructed(TagQualifiedFunction,
		nonCtx(),
		ber.ContextConstructed(QFuncPath, ber.RelOID(encodeRelativeOID([]int32{4}))),
	)).Function.Number != 4 {
		t.Error("qualified function skip-branch")
	}
	// Command wrapper.
	if decodeOne(t, ber.AppConstructed(TagCommand,
		nonCtx(),
		ber.ContextConstructed(CmdCtxNumber, ber.Integer(CmdGetDirectory)),
	)).Command.Number != CmdGetDirectory {
		t.Error("command skip-branch")
	}
	// InvocationResult wrapper.
	ir := decodeOne(t, ber.AppConstructed(TagInvocationResult,
		nonCtx(),
		ber.ContextConstructed(InvResInvocationID, ber.Integer(9)),
	)).InvocationResult
	if ir.InvocationID != 9 {
		t.Error("invocation result skip-branch")
	}
	// Label: non-context child.
	lbl := decodeLabel(ber.TLV{Children: []ber.TLV{
		nonCtx(),
		ber.ContextConstructed(LabelDescription, ber.UTF8("L")),
	}})
	if lbl.Description != "L" {
		t.Error("label skip-branch")
	}
	// TupleItemDescription: non-context field inside.
	td := decodeTupleDescription(ber.AppConstructed(TagTupleItemDescription,
		nonCtx(),
		ber.ContextConstructed(TupleName, ber.UTF8("n")),
	))
	if len(td) != 1 || td[0].Name != "n" {
		t.Errorf("tuple desc skip-branch: %+v", td)
	}
	// EnumMap pair: non-context field inside the pair.
	em := decodeEnumMap(ber.AppConstructed(TagStringIntegerPair,
		nonCtx(),
		ber.ContextConstructed(0, ber.UTF8("v")),
		ber.ContextConstructed(1, ber.Integer(2)),
	))
	if em[2] != "v" {
		t.Errorf("enum map skip-branch: %+v", em)
	}
	// Invocation: non-context inside.
	inv := decodeInvocation(ber.ContextConstructed(CmdCtxInvocation,
		ber.AppConstructed(TagInvocation,
			nonCtx(),
			ber.ContextConstructed(InvInvocationID, ber.Integer(4)),
		),
	))
	if inv.InvocationID != 4 {
		t.Error("invocation skip-branch")
	}
}

// TestDecodeInvocationResult_TopLevel routes an InvocationResult through
// decodeElement dispatch (rather than calling decodeInvocationResult
// directly), covering the TagInvocationResult arm of decodeElement.
func TestDecodeInvocationResult_TopLevel(t *testing.T) {
	el := decodeOne(t, ber.AppConstructed(TagInvocationResult,
		ber.ContextConstructed(InvResInvocationID, ber.Integer(2)),
	))
	if el == nil || el.InvocationResult == nil || el.InvocationResult.InvocationID != 2 {
		t.Fatalf("top-level invocation result: %+v", el)
	}
}

// TestUnwrapPrimitive_EmptyConstructed covers the nil return when a
// constructed TLV has no children, and the raw-bytes fallback in
// decodeAnyValue (a primitive body that is not a valid INTEGER).
func TestUnwrapPrimitive_EmptyConstructed(t *testing.T) {
	// Constructed, zero children → decodeStringValue("") via unwrapPrimitive nil.
	empty := ber.TLV{Tag: ber.Tag{Class: ber.ClassContext, Constructed: true, Number: 0}}
	if s := decodeStringValue(empty); s != "" {
		t.Errorf("unwrapPrimitive empty constructed: got %q, want empty", s)
	}
	// decodeAnyValue raw-bytes fallback: primitive CTX whose >8-byte body
	// is not a decodable INTEGER, so it returns the raw []byte.
	raw := ber.ContextPrimitive(ParamContentValue, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9})
	if b, ok := decodeAnyValue(raw).([]byte); !ok || len(b) != 9 {
		t.Errorf("decodeAnyValue raw fallback: got %v", decodeAnyValue(raw))
	}
}

// TestMoreSkipBranches covers the remaining `continue` skip-branches in
// connection / stream / template / template-element decoders plus the
// empty RELATIVE-OID guard.
func TestMoreSkipBranches(t *testing.T) {
	// Connection with a non-context field.
	conns := decodeConnectionCollection(ber.ContextConstructed(MatrixConnections,
		ber.AppConstructed(TagConnection,
			nonCtx(),
			ber.ContextConstructed(ConnTarget, ber.Integer(1)),
		),
	))
	if len(conns) != 1 || conns[0].Target != 1 {
		t.Errorf("connection skip-branch: %+v", conns)
	}
	// StreamEntry with a non-context field.
	se := decodeStreamCollection(ber.AppConstructed(TagStreamCollection,
		ber.ContextConstructed(0,
			ber.AppConstructed(TagStreamEntry,
				nonCtx(),
				ber.ContextConstructed(StreamEntryIdentifier, ber.Integer(3)),
			),
		),
	))
	if len(se) != 1 || se[0].StreamIdentifier != 3 {
		t.Errorf("stream entry skip-branch: %+v", se)
	}
	// StreamDescription with a non-context field + a real one.
	sd := decodeStreamDescription(ber.ContextConstructed(ParamContentStreamDescriptor,
		ber.AppConstructed(TagStreamDescription,
			nonCtx(),
			ber.ContextConstructed(StreamDescFormat, ber.Integer(StreamFmtSignedInt8)),
		),
	))
	if sd == nil || sd.Format != StreamFmtSignedInt8 {
		t.Errorf("stream desc skip-branch: %+v", sd)
	}
	// Template with a non-context child.
	tm := decodeOne(t, ber.AppConstructed(TagTemplate,
		nonCtx(),
		ber.ContextConstructed(TemplateNumber, ber.Integer(7)),
	)).Template
	if tm.Number != 7 {
		t.Errorf("template skip-branch: %+v", tm)
	}
	// TemplateElement with a non-application child (CTX) — skipped.
	te := decodeTemplateElement(ber.ContextConstructed(TemplateElementCtx,
		ber.ContextConstructed(0, ber.Integer(0)), // non-application, skipped
		ber.AppConstructed(TagNode, ber.ContextConstructed(NodeNumber, ber.Integer(1))),
	))
	if te.Node == nil || te.Node.Number != 1 {
		t.Errorf("template element non-app skip: %+v", te)
	}
	// Empty RELATIVE-OID returns nil.
	if got := decodeRelativeOID(ber.ContextPrimitive(0, nil)); got != nil {
		t.Errorf("empty relOID: got %v, want nil", got)
	}
}

// TestDecodeStreamDescription_Absent covers the nil return when no
// recognised StreamDescription fields are present.
func TestDecodeStreamDescription_Absent(t *testing.T) {
	// CTX[16] carrying an APP[12] envelope with no format/offset → nil.
	empty := ber.ContextConstructed(ParamContentStreamDescriptor,
		ber.AppConstructed(TagStreamDescription,
			ber.ContextConstructed(9, ber.Integer(0)), // unrecognised field
		),
	)
	if sd := decodeStreamDescription(empty); sd != nil {
		t.Errorf("absent stream desc: got %+v, want nil", sd)
	}
}
