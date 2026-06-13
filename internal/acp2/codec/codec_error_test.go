package codec

import (
	"encoding/binary"
	"testing"
)

// EncodeACP2Message: nil message -> error.
func TestEncodeACP2Message_Nil(t *testing.T) {
	if _, err := EncodeACP2Message(nil); err == nil {
		t.Fatal("expected error encoding nil message")
	}
}

// EncodeACP2Message: set_property with no properties -> error.
func TestEncodeACP2Message_SetPropertyNoProps(t *testing.T) {
	m := &ACP2Message{Type: ACP2TypeRequest, MTID: 1, Func: ACP2FuncSetProperty}
	if _, err := EncodeACP2Message(m); err == nil {
		t.Fatal("expected error for set_property with no properties")
	}
}

// EncodeACP2Message: set_property success path emits header + obj-id + idx +
// encoded property.
func TestEncodeACP2Message_SetPropertySuccess(t *testing.T) {
	prop := MakeValueProperty(PIDValue, NumTypeU32, []byte{0x00, 0x00, 0x00, 0x2A})
	m := &ACP2Message{
		Type:       ACP2TypeRequest,
		MTID:       7,
		Func:       ACP2FuncSetProperty,
		PID:        PIDValue,
		ObjID:      0x11223344,
		Idx:        0,
		Properties: []Property{prop},
	}
	out, err := EncodeACP2Message(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// 4 header + 8 (obj-id+idx) + property (4 hdr + 4 data = 8) = 20.
	if len(out) != ACP2HeaderSize+8+8 {
		t.Fatalf("len: got %d, want %d", len(out), ACP2HeaderSize+8+8)
	}
	if out[0] != byte(ACP2TypeRequest) || out[1] != 7 || out[2] != byte(ACP2FuncSetProperty) || out[3] != PIDValue {
		t.Fatalf("header bytes wrong: % X", out[0:4])
	}
	if got := binary.BigEndian.Uint32(out[4:8]); got != 0x11223344 {
		t.Fatalf("obj-id: got %08X", got)
	}
	if got := binary.BigEndian.Uint32(out[8:12]); got != 0 {
		t.Fatalf("idx: got %d", got)
	}
	// Property header at offset 12: pid, vtype, plen=8.
	if out[12] != PIDValue || out[13] != byte(NumTypeU32) {
		t.Fatalf("property header wrong: % X", out[12:14])
	}
	if got := binary.BigEndian.Uint16(out[14:16]); got != 8 {
		t.Fatalf("property plen: got %d, want 8", got)
	}
}

// EncodeACP2Message: get_property request body = obj-id + idx (no trailing
// property header).
func TestEncodeACP2Message_GetPropertyBody(t *testing.T) {
	m := &ACP2Message{
		Type:  ACP2TypeRequest,
		MTID:  3,
		Func:  ACP2FuncGetProperty,
		PID:   PIDLabel,
		ObjID: 0xAABBCCDD,
		Idx:   1,
	}
	out, err := EncodeACP2Message(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(out) != ACP2HeaderSize+8 {
		t.Fatalf("len: got %d, want %d", len(out), ACP2HeaderSize+8)
	}
	if out[3] != PIDLabel {
		t.Fatalf("pid byte: got %d, want %d", out[3], PIDLabel)
	}
	if got := binary.BigEndian.Uint32(out[4:8]); got != 0xAABBCCDD {
		t.Fatalf("obj-id: got %08X", got)
	}
	if got := binary.BigEndian.Uint32(out[8:12]); got != 1 {
		t.Fatalf("idx: got %d, want 1", got)
	}
}

// EncodeACP2Message: default branch emits header + raw Body for an
// unrecognised func.
func TestEncodeACP2Message_DefaultGeneric(t *testing.T) {
	m := &ACP2Message{
		Type: ACP2TypeReply,
		MTID: 9,
		Func: ACP2Func(200), // not 0-3
		PID:  5,
		Body: []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}
	out, err := EncodeACP2Message(m)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if len(out) != ACP2HeaderSize+4 {
		t.Fatalf("len: got %d, want %d", len(out), ACP2HeaderSize+4)
	}
	if out[2] != 200 {
		t.Fatalf("func byte: got %d, want 200", out[2])
	}
	for i, b := range []byte{0xDE, 0xAD, 0xBE, 0xEF} {
		if out[ACP2HeaderSize+i] != b {
			t.Fatalf("body byte %d: got %02X, want %02X", i, out[ACP2HeaderSize+i], b)
		}
	}
}

// EncodeACP2Message: set_property where the property cannot be encoded.
// EncodeProperty only fails on a nil pointer, which is unreachable from the
// slice; this asserts the success surrounding encode works for a string prop.
func TestEncodeACP2Message_SetPropertyString(t *testing.T) {
	m := &ACP2Message{
		Type:       ACP2TypeRequest,
		MTID:       2,
		Func:       ACP2FuncSetProperty,
		PID:        PIDLabel,
		Properties: []Property{MakeStringProperty(PIDLabel, "hi")},
	}
	if _, err := EncodeACP2Message(m); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

// DecodeACP2Message: get_version reply carries the version in PID, no body.
func TestDecodeACP2Message_GetVersionReply(t *testing.T) {
	data := []byte{byte(ACP2TypeReply), 1, byte(ACP2FuncGetVersion), 42}
	m, err := DecodeACP2Message(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.PID != 42 {
		t.Fatalf("version (PID): got %d, want 42", m.PID)
	}
	if len(m.Properties) != 0 {
		t.Fatalf("expected no properties, got %d", len(m.Properties))
	}
}

// DecodeACP2Message: announce with obj-id + idx + properties decodes the
// embedded property headers.
func TestDecodeACP2Message_AnnounceWithProperties(t *testing.T) {
	// header
	data := []byte{byte(ACP2TypeAnnounce), 0, byte(PIDValue), PIDValue}
	// obj-id + idx
	objIdx := make([]byte, 8)
	binary.BigEndian.PutUint32(objIdx[0:4], 0x01020304)
	binary.BigEndian.PutUint32(objIdx[4:8], 0)
	data = append(data, objIdx...)
	// one property: pid=8, vtype=u32, plen=8, value
	prop, _ := EncodeProperty(&Property{PID: PIDValue, VType: byte(NumTypeU32), Data: []byte{0, 0, 0, 9}})
	data = append(data, prop...)

	m, err := DecodeACP2Message(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.ObjID != 0x01020304 {
		t.Fatalf("obj-id: got %08X", m.ObjID)
	}
	if len(m.Properties) != 1 {
		t.Fatalf("properties: got %d, want 1", len(m.Properties))
	}
	if m.Properties[0].PID != PIDValue {
		t.Fatalf("property pid: got %d", m.Properties[0].PID)
	}
}

// DecodeACP2Message: a reply body that contains a malformed property header
// (plen < 4) propagates the decode error.
func TestDecodeACP2Message_BadPropertyError(t *testing.T) {
	data := []byte{byte(ACP2TypeReply), 1, byte(ACP2FuncGetObject), 0}
	objIdx := make([]byte, 8) // obj-id + idx both zero
	data = append(data, objIdx...)
	// malformed property: plen = 2 (< 4)
	data = append(data, 0x08, 0x06, 0x00, 0x02)
	if _, err := DecodeACP2Message(data); err == nil {
		t.Fatal("expected property decode error")
	}
}

// DecodeACP2Message: reply with fewer than 8 body bytes leaves obj-id and
// properties untouched (the len(body) >= 8 guard is false).
func TestDecodeACP2Message_ReplyShortBody(t *testing.T) {
	data := []byte{byte(ACP2TypeReply), 1, byte(ACP2FuncGetObject), 0, 0x01, 0x02}
	m, err := DecodeACP2Message(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.ObjID != 0 || len(m.Properties) != 0 {
		t.Fatalf("short body should not populate obj-id/properties: obj=%d props=%d", m.ObjID, len(m.Properties))
	}
}

// ToACP2Error: non-error message returns nil.
func TestToACP2Error_NonError(t *testing.T) {
	m := &ACP2Message{Type: ACP2TypeReply, Func: ACP2FuncGetVersion}
	if err := m.ToACP2Error(); err != nil {
		t.Fatalf("expected nil for non-error message, got %v", err)
	}
}

// ToACP2Error: error message maps func->status and yields an *ACP2Error.
func TestToACP2Error_Error(t *testing.T) {
	m := &ACP2Message{Type: ACP2TypeError, Func: ACP2Func(ErrInvalidValue)}
	err := m.ToACP2Error()
	if err == nil {
		t.Fatal("expected an error")
	}
	ae, ok := err.(*ACP2Error)
	if !ok {
		t.Fatalf("got %T, want *ACP2Error", err)
	}
	if ae.Status != ErrInvalidValue {
		t.Fatalf("status: got %d, want %d", ae.Status, ErrInvalidValue)
	}
}
