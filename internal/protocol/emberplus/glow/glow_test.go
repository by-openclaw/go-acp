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
		ber.ContextConstructed(CmdNumber, ber.Integer(CmdGetDirectory)),
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
