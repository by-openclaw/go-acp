package acp2

import (
	"encoding/binary"
	"testing"
)

func TestEncodeDecodeACP2Message_GetVersion(t *testing.T) {
	msg := &ACP2Message{
		Type: ACP2TypeRequest,
		MTID: 1,
		Func: ACP2FuncGetVersion,
		PID:  0,
	}

	data, err := EncodeACP2Message(msg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if len(data) != ACP2HeaderSize {
		t.Fatalf("expected %d bytes, got %d", ACP2HeaderSize, len(data))
	}
	if data[0] != byte(ACP2TypeRequest) {
		t.Errorf("type: got %d, want %d", data[0], ACP2TypeRequest)
	}
	if data[1] != 1 {
		t.Errorf("mtid: got %d, want 1", data[1])
	}
	if data[2] != byte(ACP2FuncGetVersion) {
		t.Errorf("func: got %d, want %d", data[2], ACP2FuncGetVersion)
	}

	decoded, err := DecodeACP2Message(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Type != msg.Type || decoded.MTID != msg.MTID || decoded.Func != msg.Func {
		t.Errorf("round-trip mismatch: type=%d/%d mtid=%d/%d func=%d/%d",
			decoded.Type, msg.Type, decoded.MTID, msg.MTID, decoded.Func, msg.Func)
	}
}

func TestEncodeDecodeACP2Message_GetObject(t *testing.T) {
	msg := &ACP2Message{
		Type:  ACP2TypeRequest,
		MTID:  5,
		Func:  ACP2FuncGetObject,
		ObjID: 42,
		Idx:   0,
	}

	data, err := EncodeACP2Message(msg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// get_object: header(4) + obj-id(4) + idx(4) = 12 bytes
	if len(data) != ACP2HeaderSize+8 {
		t.Fatalf("expected %d bytes, got %d", ACP2HeaderSize+8, len(data))
	}

	objID := binary.BigEndian.Uint32(data[4:8])
	if objID != 42 {
		t.Errorf("obj-id: got %d, want 42", objID)
	}

	idx := binary.BigEndian.Uint32(data[8:12])
	if idx != 0 {
		t.Errorf("idx: got %d, want 0", idx)
	}
}

func TestDecodeACP2Message_Reply(t *testing.T) {
	// Simulate a get_version reply: type=1, mtid=1, func=0, pid=3 (version=3)
	data := []byte{
		byte(ACP2TypeReply), // type
		1,                    // mtid
		byte(ACP2FuncGetVersion), // func
		3,                    // pid (version number)
	}

	msg, err := DecodeACP2Message(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if msg.Type != ACP2TypeReply {
		t.Errorf("type: got %d, want reply", msg.Type)
	}
	if msg.PID != 3 {
		t.Errorf("version (pid): got %d, want 3", msg.PID)
	}
}

func TestDecodeACP2Message_Error(t *testing.T) {
	// Simulate an error reply: type=3, mtid=2, stat=1 (invalid obj-id), pid=0
	// Body: obj-id = 99
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, 99)
	data := append([]byte{
		byte(ACP2TypeError), // type
		2,                    // mtid
		byte(ErrInvalidObjID), // stat
		0,                    // pid
	}, body...)

	msg, err := DecodeACP2Message(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if msg.Type != ACP2TypeError {
		t.Errorf("type: got %d, want error", msg.Type)
	}
	if ACP2ErrStatus(msg.Func) != ErrInvalidObjID {
		t.Errorf("stat: got %d, want %d", msg.Func, ErrInvalidObjID)
	}
	if msg.ObjID != 99 {
		t.Errorf("obj-id: got %d, want 99", msg.ObjID)
	}

	acp2Err := msg.ToACP2Error()
	if acp2Err == nil {
		t.Fatal("expected non-nil error")
	}
}

func TestDecodeACP2Message_TooShort(t *testing.T) {
	data := []byte{0x00, 0x01}
	_, err := DecodeACP2Message(data)
	if err == nil {
		t.Fatal("expected error for short message")
	}
}

func TestEncodeDecodeACP2Message_GetProperty(t *testing.T) {
	msg := &ACP2Message{
		Type:  ACP2TypeRequest,
		MTID:  10,
		Func:  ACP2FuncGetProperty,
		PID:   PIDValue,
		ObjID: 100,
		Idx:   0,
	}

	data, err := EncodeACP2Message(msg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Should be header(4) + obj-id(4) + idx(4) + prop-header(4) = 16
	if len(data) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(data))
	}

	// Verify the property header in the request.
	if data[12] != PIDValue {
		t.Errorf("prop pid: got %d, want %d", data[12], PIDValue)
	}
}
