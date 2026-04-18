package glow

import (
	"testing"

	"acp/internal/protocol/emberplus/ber"
)

func TestEncodeDecodeGetDirectory(t *testing.T) {
	data := EncodeGetDirectory()
	if len(data) == 0 {
		t.Fatal("empty GetDirectory")
	}

	// Decode and verify it's a root collection with a command.
	tlvs, err := ber.DecodeAll(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tlvs) != 1 {
		t.Fatalf("expected 1 TLV, got %d", len(tlvs))
	}
	root := tlvs[0]
	if root.Tag.Class != ber.ClassApplication || root.Tag.Number != TagRootElementCollection {
		t.Errorf("root tag: got %+v", root.Tag)
	}
}

func TestEncodeDecodeGetDirectoryFor(t *testing.T) {
	path := []int32{1, 2, 3}
	data := EncodeGetDirectoryFor(path)
	if len(data) == 0 {
		t.Fatal("empty GetDirectoryFor")
	}

	elements, err := DecodeRoot(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(elements) == 0 {
		t.Fatal("no elements")
	}
}

func TestEncodeDecodeSetValue(t *testing.T) {
	cases := []struct {
		name  string
		path  []int32
		value interface{}
	}{
		{"integer", []int32{1, 2}, int64(42)},
		{"float", []int32{1, 3}, float64(3.14)},
		{"string", []int32{1, 4}, "hello"},
		{"boolean", []int32{1, 5}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := EncodeSetValue(tc.path, tc.value)
			if len(data) == 0 {
				t.Fatal("empty SetValue")
			}
			// Verify it decodes without error.
			_, err := DecodeRoot(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
		})
	}
}

func TestEncodeDecodeSubscribe(t *testing.T) {
	data := EncodeSubscribe([]int32{1, 2, 3})
	if len(data) == 0 {
		t.Fatal("empty Subscribe")
	}
	_, err := DecodeRoot(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestEncodeMatrixConnect(t *testing.T) {
	data := EncodeMatrixConnect([]int32{1}, 0, []int32{2, 3}, ConnOpConnect)
	if len(data) == 0 {
		t.Fatal("empty MatrixConnect")
	}
	_, err := DecodeRoot(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestEncodeInvoke(t *testing.T) {
	data := EncodeInvoke([]int32{1, 5}, 1, []interface{}{int64(42), "arg2"})
	if len(data) == 0 {
		t.Fatal("empty Invoke")
	}
	_, err := DecodeRoot(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestDecodeNode(t *testing.T) {
	// Build a Node manually via BER.
	node := ber.AppConstructed(TagNode,
		ber.ContextConstructed(NodeNumber, ber.Integer(1)),
		ber.ContextConstructed(NodeContents,
			ber.ContextConstructed(NodeContentIdentifier, ber.UTF8("TestNode")),
			ber.ContextConstructed(NodeContentDescription, ber.UTF8("A test node")),
			ber.ContextConstructed(NodeContentIsOnline, ber.Boolean(true)),
		),
	)
	data := ber.EncodeTLV(node)

	tlv, _, err := ber.DecodeTLV(data)
	if err != nil {
		t.Fatalf("BER decode: %v", err)
	}
	el, err := decodeElement(tlv)
	if err != nil {
		t.Fatalf("Glow decode: %v", err)
	}
	if el == nil || el.Node == nil {
		t.Fatal("expected Node")
	}
	n := el.Node
	if n.Number != 1 {
		t.Errorf("number: got %d", n.Number)
	}
	if n.Identifier != "TestNode" {
		t.Errorf("identifier: got %q", n.Identifier)
	}
	if n.Description != "A test node" {
		t.Errorf("description: got %q", n.Description)
	}
	if !n.IsOnline {
		t.Error("expected IsOnline true")
	}
}

func TestDecodeParameter(t *testing.T) {
	param := ber.AppConstructed(TagParameter,
		ber.ContextConstructed(ParamNumber, ber.Integer(7)),
		ber.ContextConstructed(ParamContents,
			ber.ContextConstructed(ParamContentIdentifier, ber.UTF8("Gain")),
			ber.ContextConstructed(ParamContentDescription, ber.UTF8("Audio gain")),
			ber.ContextConstructed(ParamContentValue, ber.Real(-12.5)),
			ber.ContextConstructed(ParamContentMinimum, ber.Real(-60.0)),
			ber.ContextConstructed(ParamContentMaximum, ber.Real(0.0)),
			ber.ContextConstructed(ParamContentAccess, ber.Integer(AccessReadWrite)),
			ber.ContextConstructed(ParamContentFormat, ber.UTF8("%+.1f dB")),
			ber.ContextConstructed(ParamContentStep, ber.Real(0.5)),
			ber.ContextConstructed(ParamContentType, ber.Integer(ParamTypeReal)),
		),
	)
	data := ber.EncodeTLV(param)

	tlv, _, err := ber.DecodeTLV(data)
	if err != nil {
		t.Fatalf("BER decode: %v", err)
	}
	el, err := decodeElement(tlv)
	if err != nil {
		t.Fatalf("Glow decode: %v", err)
	}
	if el == nil || el.Parameter == nil {
		t.Fatal("expected Parameter")
	}
	p := el.Parameter
	if p.Number != 7 {
		t.Errorf("number: got %d", p.Number)
	}
	if p.Identifier != "Gain" {
		t.Errorf("identifier: got %q", p.Identifier)
	}
	if v, ok := p.Value.(float64); !ok || v != -12.5 {
		t.Errorf("value: got %v", p.Value)
	}
	if v, ok := p.Minimum.(float64); !ok || v != -60.0 {
		t.Errorf("minimum: got %v", p.Minimum)
	}
	if p.Access != AccessReadWrite {
		t.Errorf("access: got %d", p.Access)
	}
	if p.Format != "%+.1f dB" {
		t.Errorf("format: got %q", p.Format)
	}
	if p.Type != ParamTypeReal {
		t.Errorf("type: got %d", p.Type)
	}
}

func TestDecodeCommand(t *testing.T) {
	cmd := ber.AppConstructed(TagCommand,
		ber.ContextConstructed(CmdCtxNumber, ber.Integer(CmdGetDirectory)),
	)
	data := ber.EncodeTLV(cmd)

	tlv, _, err := ber.DecodeTLV(data)
	if err != nil {
		t.Fatalf("BER decode: %v", err)
	}
	el, err := decodeElement(tlv)
	if err != nil {
		t.Fatalf("Glow decode: %v", err)
	}
	if el == nil || el.Command == nil {
		t.Fatal("expected Command")
	}
	if el.Command.Number != CmdGetDirectory {
		t.Errorf("command: got %d, want %d", el.Command.Number, CmdGetDirectory)
	}
}

func TestDecodeInvocationResult_SuccessDefaultsTrue(t *testing.T) {
	// Spec p.92: "success [1] BOOLEAN OPTIONAL — True or omitted if no errors."
	// Provider emits only invocationId (0) + result (2); success absent.
	res := ber.AppConstructed(TagInvocationResult,
		ber.ContextConstructed(InvResInvocationID, ber.Integer(7)),
		ber.ContextConstructed(InvResResult,
			ber.Sequence(
				ber.ContextConstructed(0, ber.Integer(42)),
			),
		),
	)
	data := ber.EncodeTLV(res)

	tlv, _, err := ber.DecodeTLV(data)
	if err != nil {
		t.Fatalf("BER decode: %v", err)
	}
	el, err := decodeInvocationResult(tlv)
	if err != nil {
		t.Fatalf("glow decode: %v", err)
	}
	r := el.InvocationResult
	if r.InvocationID != 7 {
		t.Errorf("id: got %d, want 7", r.InvocationID)
	}
	if !r.Success {
		t.Error("success should default to true when field omitted (spec p.92)")
	}
	if len(r.Result) != 1 || r.Result[0] != int64(42) {
		t.Errorf("result tuple: got %v", r.Result)
	}
}

func TestDecodeInvocationResult_SuccessFalseExplicit(t *testing.T) {
	res := ber.AppConstructed(TagInvocationResult,
		ber.ContextConstructed(InvResInvocationID, ber.Integer(3)),
		ber.ContextConstructed(InvResSuccess, ber.Boolean(false)),
	)
	data := ber.EncodeTLV(res)

	tlv, _, err := ber.DecodeTLV(data)
	if err != nil {
		t.Fatalf("BER decode: %v", err)
	}
	el, err := decodeInvocationResult(tlv)
	if err != nil {
		t.Fatalf("glow decode: %v", err)
	}
	if el.InvocationResult.Success {
		t.Error("explicit success=false should decode as false")
	}
	if len(el.InvocationResult.Result) != 0 {
		t.Errorf("result should be empty when omitted, got %v", el.InvocationResult.Result)
	}
}

func TestDecodeInvocationResult_MultiValueTuple(t *testing.T) {
	res := ber.AppConstructed(TagInvocationResult,
		ber.ContextConstructed(InvResInvocationID, ber.Integer(1)),
		ber.ContextConstructed(InvResResult,
			ber.Sequence(
				ber.ContextConstructed(0, ber.Integer(8)),
				ber.ContextConstructed(0, ber.UTF8("ok")),
				ber.ContextConstructed(0, ber.Real(1.5)),
			),
		),
	)
	tlv, _, err := ber.DecodeTLV(ber.EncodeTLV(res))
	if err != nil {
		t.Fatalf("BER decode: %v", err)
	}
	el, err := decodeInvocationResult(tlv)
	if err != nil {
		t.Fatalf("glow decode: %v", err)
	}
	got := el.InvocationResult.Result
	if len(got) != 3 {
		t.Fatalf("expected 3 tuple values, got %d: %v", len(got), got)
	}
	if n, ok := got[0].(int64); !ok || n != 8 {
		t.Errorf("tuple[0]: got %v", got[0])
	}
	if s, ok := got[1].(string); !ok || s != "ok" {
		t.Errorf("tuple[1]: got %v", got[1])
	}
	if f, ok := got[2].(float64); !ok || f != 1.5 {
		t.Errorf("tuple[2]: got %v", got[2])
	}
}

func TestEncodeInvoke_RoundTripArgumentTypes(t *testing.T) {
	data := EncodeInvoke([]int32{1, 5}, 42, []any{int64(3), int64(5), "label", true, 1.25})
	if len(data) == 0 {
		t.Fatal("empty Invoke")
	}
	elements, err := DecodeRoot(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Find the Command inside the nested QualifiedFunction/ElementCollection.
	var cmd *Command
	var walk func([]Element)
	walk = func(els []Element) {
		for _, e := range els {
			if e.Command != nil && cmd == nil {
				cmd = e.Command
				return
			}
			if e.Node != nil {
				walk(e.Node.Children)
			}
			if e.Function != nil {
				walk(e.Function.Children)
			}
		}
	}
	walk(elements)
	if cmd == nil || cmd.Invocation == nil {
		t.Fatalf("expected Invocation in root tree, got %d elements", len(elements))
	}
	if cmd.Invocation.InvocationID != 42 {
		t.Errorf("invocationID: got %d", cmd.Invocation.InvocationID)
	}
	if len(cmd.Invocation.Arguments) != 5 {
		t.Fatalf("expected 5 args, got %d: %v", len(cmd.Invocation.Arguments), cmd.Invocation.Arguments)
	}
}

func TestDecodeStreamCollection(t *testing.T) {
	// Spec p.93 StreamCollection = SEQUENCE OF CTX[0] StreamEntry.
	// Each StreamEntry APP[5] carries streamIdentifier (CTX 0) + value (CTX 1).
	stream := ber.AppConstructed(TagStreamCollection,
		ber.ContextConstructed(0,
			ber.AppConstructed(TagStreamEntry,
				ber.ContextConstructed(StreamEntryIdentifier, ber.Integer(45)),
				ber.ContextConstructed(StreamEntryValue, ber.Integer(-18)),
			),
		),
		ber.ContextConstructed(0,
			ber.AppConstructed(TagStreamEntry,
				ber.ContextConstructed(StreamEntryIdentifier, ber.Integer(46)),
				ber.ContextConstructed(StreamEntryValue, ber.Real(1.25)),
			),
		),
	)
	data := ber.EncodeTLV(stream)
	elements, err := DecodeRoot(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(elements) != 1 {
		t.Fatalf("expected 1 element, got %d", len(elements))
	}
	entries := elements[0].Streams
	if len(entries) != 2 {
		t.Fatalf("expected 2 StreamEntry, got %d", len(entries))
	}
	if entries[0].StreamIdentifier != 45 {
		t.Errorf("entry[0].id: got %d", entries[0].StreamIdentifier)
	}
	if v, ok := entries[0].Value.(int64); !ok || v != -18 {
		t.Errorf("entry[0].value: got %v", entries[0].Value)
	}
	if entries[1].StreamIdentifier != 46 {
		t.Errorf("entry[1].id: got %d", entries[1].StreamIdentifier)
	}
	if v, ok := entries[1].Value.(float64); !ok || v != 1.25 {
		t.Errorf("entry[1].value: got %v", entries[1].Value)
	}
}

func TestDecodeStreamDescriptorOnParameter(t *testing.T) {
	// StreamDescription APP[12] carried as Parameter CTX 16.
	desc := ber.AppConstructed(TagStreamDescription,
		ber.ContextConstructed(StreamDescFormat, ber.Integer(StreamFmtSignedInt16BigEndian)),
		ber.ContextConstructed(StreamDescOffset, ber.Integer(4)),
	)
	param := ber.AppConstructed(TagParameter,
		ber.ContextConstructed(ParamNumber, ber.Integer(1)),
		ber.ContextConstructed(ParamContents,
			ber.ContextConstructed(ParamContentIdentifier, ber.UTF8("level")),
			ber.ContextConstructed(ParamContentStreamIdentifier, ber.Integer(99)),
			ber.ContextConstructed(ParamContentStreamDescriptor, desc),
		),
	)
	tlv, _, err := ber.DecodeTLV(ber.EncodeTLV(param))
	if err != nil {
		t.Fatalf("BER decode: %v", err)
	}
	el, err := decodeElement(tlv)
	if err != nil {
		t.Fatalf("glow decode: %v", err)
	}
	p := el.Parameter
	if p.StreamIdentifier != 99 {
		t.Errorf("streamIdentifier: got %d", p.StreamIdentifier)
	}
	if p.StreamDescriptor == nil {
		t.Fatal("expected StreamDescriptor")
	}
	if p.StreamDescriptor.Format != StreamFmtSignedInt16BigEndian {
		t.Errorf("format: got %d", p.StreamDescriptor.Format)
	}
	if p.StreamDescriptor.Offset != 4 {
		t.Errorf("offset: got %d", p.StreamDescriptor.Offset)
	}
}

func TestRelativeOID_RoundTrip(t *testing.T) {
	cases := [][]int32{
		{1},
		{1, 2, 3},
		{0, 127, 128, 255},
		{1, 2, 3, 4, 5, 6, 7, 8},
	}
	for _, path := range cases {
		data := encodeRelativeOID(path)
		got := decodeRelativeOID(ber.TLV{Value: data})
		if len(got) != len(path) {
			t.Errorf("path %v: got len %d", path, len(got))
			continue
		}
		for i := range path {
			if got[i] != path[i] {
				t.Errorf("path %v[%d]: got %d", path, i, got[i])
			}
		}
	}
}
