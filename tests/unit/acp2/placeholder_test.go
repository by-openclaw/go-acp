// Package acp2_test contains black-box spec compliance tests for the ACP2
// protocol plugin. These tests validate the public behaviour of the codec,
// framer, and property layers against the spec in CLAUDE.md.
package acp2_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"acp/internal/protocol/acp2"
)

// TestAN2FrameRoundTrip verifies that an AN2 frame survives encode → decode.
func TestAN2FrameRoundTrip(t *testing.T) {
	frame := &acp2.AN2Frame{
		Proto:   acp2.AN2ProtoACP2,
		Slot:    3,
		MTID:    0,
		Type:    acp2.AN2TypeData,
		Payload: []byte{0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x2A, 0x00, 0x00, 0x00, 0x00},
	}

	data, err := acp2.EncodeAN2Frame(frame)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Spec: magic must be 0xC635 at offset 0-1.
	if data[0] != 0xC6 || data[1] != 0x35 {
		t.Errorf("magic: got 0x%02X%02X, want 0xC635", data[0], data[1])
	}

	// Spec: proto at offset 2 must be 2 for ACP2.
	if data[2] != 2 {
		t.Errorf("proto: got %d, want 2", data[2])
	}

	// Decode via stream reader (as real TCP would).
	decoded, err := acp2.ReadAN2Frame(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ReadAN2Frame: %v", err)
	}
	if decoded.Proto != acp2.AN2ProtoACP2 {
		t.Errorf("decoded proto: got %d, want 2", decoded.Proto)
	}
	if decoded.Slot != 3 {
		t.Errorf("decoded slot: got %d, want 3", decoded.Slot)
	}
	if !bytes.Equal(decoded.Payload, frame.Payload) {
		t.Errorf("payload mismatch")
	}
}

// TestAN2MagicValidation ensures bad magic is rejected.
func TestAN2MagicValidation(t *testing.T) {
	badData := []byte{0xFF, 0xFF, 0x02, 0x00, 0x00, 0x04, 0x00, 0x00}
	_, _, err := acp2.DecodeAN2Frame(badData)
	if err == nil {
		t.Fatal("expected error for bad magic")
	}
}

// TestACP2MessageHeader verifies the 4-byte ACP2 header layout.
func TestACP2MessageHeader(t *testing.T) {
	msg := &acp2.ACP2Message{
		Type: acp2.ACP2TypeRequest,
		MTID: 7,
		Func: acp2.ACP2FuncGetObject,
		PID:  0,
		ObjID: 1,
		Idx:   0,
	}

	data, err := acp2.EncodeACP2Message(msg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Spec: byte 0 = type, byte 1 = mtid, byte 2 = func, byte 3 = pid.
	if data[0] != 0 { // request
		t.Errorf("type: got %d, want 0", data[0])
	}
	if data[1] != 7 {
		t.Errorf("mtid: got %d, want 7", data[1])
	}
	if data[2] != 1 { // get_object
		t.Errorf("func: got %d, want 1", data[2])
	}
}

// TestACP2ErrorDecode verifies error reply decoding.
func TestACP2ErrorDecode(t *testing.T) {
	// Build: type=3, mtid=2, stat=4 (no access), pid=0, body: obj-id=50
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, 50)
	data := append([]byte{3, 2, 4, 0}, body...)

	msg, err := acp2.DecodeACP2Message(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if msg.Type != acp2.ACP2TypeError {
		t.Errorf("type: got %d, want 3", msg.Type)
	}
	if msg.ObjID != 50 {
		t.Errorf("obj-id: got %d, want 50", msg.ObjID)
	}

	acp2Err := msg.ToACP2Error()
	if acp2Err == nil {
		t.Fatal("expected non-nil error")
	}
}

// TestPropertyAlignment verifies the 4-byte alignment rule.
func TestPropertyAlignment(t *testing.T) {
	// String "AB\0" = 3 bytes, plen = 7, pad = (4-7%4)%4 = 1.
	prop := acp2.MakeStringProperty(acp2.PIDLabel, "AB")
	encoded, err := acp2.EncodeProperty(&prop)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// plen = 4 + 3 = 7, total with pad = 8.
	if len(encoded) != 8 {
		t.Fatalf("expected 8 bytes (7 + 1 pad), got %d", len(encoded))
	}
	// Padding byte must be zero.
	if encoded[7] != 0 {
		t.Errorf("padding byte: got 0x%02X, want 0x00", encoded[7])
	}

	// Decode should still work.
	props, err := acp2.DecodeProperties(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 property, got %d", len(props))
	}
	if acp2.PropertyString(&props[0]) != "AB" {
		t.Errorf("string: got %q, want %q", acp2.PropertyString(&props[0]), "AB")
	}
}

// TestPropertyAlignmentMultiple verifies alignment across multiple properties.
func TestPropertyAlignmentMultiple(t *testing.T) {
	// Two properties: "X\0" (plen=5, pad=3) + u32 (plen=8, pad=0).
	p1 := acp2.MakeStringProperty(acp2.PIDLabel, "X")
	u32data := make([]byte, 4)
	binary.BigEndian.PutUint32(u32data, 999)
	p2 := acp2.Property{PID: acp2.PIDValue, VType: uint8(acp2.NumTypeU32), PLen: 8, Data: u32data}

	encoded, err := acp2.EncodeProperties([]acp2.Property{p1, p2})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// p1: plen=5, pad=3 → 8 bytes; p2: plen=8, pad=0 → 8 bytes. Total 16.
	if len(encoded) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(encoded))
	}

	decoded, err := acp2.DecodeProperties(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 properties, got %d", len(decoded))
	}
	if acp2.PropertyString(&decoded[0]) != "X" {
		t.Errorf("first: got %q, want %q", acp2.PropertyString(&decoded[0]), "X")
	}
	v, err := acp2.PropertyU32(&decoded[1])
	if err != nil {
		t.Fatalf("PropertyU32: %v", err)
	}
	if v != 999 {
		t.Errorf("second: got %d, want 999", v)
	}
}

// TestNumberTypeMapping verifies all number types decode correctly.
func TestNumberTypeMapping(t *testing.T) {
	// S32 = -500
	var s32val int32 = -500
	s32data := make([]byte, 4)
	binary.BigEndian.PutUint32(s32data, uint32(s32val))
	intV, _, _, err := acp2.DecodeNumericValue(acp2.NumTypeS32, s32data)
	if err != nil {
		t.Fatalf("S32: %v", err)
	}
	if intV != -500 {
		t.Errorf("S32: got %d, want -500", intV)
	}

	// Float = 1.5
	fdata := make([]byte, 4)
	binary.BigEndian.PutUint32(fdata, math.Float32bits(1.5))
	_, _, floatV, err := acp2.DecodeNumericValue(acp2.NumTypeFloat, fdata)
	if err != nil {
		t.Fatalf("Float: %v", err)
	}
	if floatV != float64(float32(1.5)) {
		t.Errorf("Float: got %f, want 1.5", floatV)
	}
}

// TestPluginRegistration verifies the ACP2 plugin registered itself.
func TestPluginRegistration(t *testing.T) {
	names := make(map[string]bool)
	// Import the plugin package to trigger init().
	// The import is in the test file's import block.
	// We can't directly test registration without importing the protocol
	// package, but we verify the Factory returns correct metadata.
	f := &acp2.Factory{}
	m := f.Meta()
	if m.Name != "acp2" {
		t.Errorf("name: got %q, want %q", m.Name, "acp2")
	}
	if m.DefaultPort != 2072 {
		t.Errorf("port: got %d, want 2072", m.DefaultPort)
	}
	_ = names
}
