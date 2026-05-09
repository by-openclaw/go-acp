package codec

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
	// Per spec §"Error" (line 1207-1250): error message is exactly the
	// 4-byte ACP2 header. No body. Decoder must NOT synthesise ObjID
	// from any trailing bytes.
	data := []byte{
		byte(ACP2TypeError),    // type
		2,                       // mtid
		byte(ErrInvalidObjID),   // stat (in func slot)
		0,                       // pid
	}

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
	if msg.ObjID != 0 {
		t.Errorf("obj-id=%d want 0 (spec: error has no body)", msg.ObjID)
	}

	acp2Err := msg.ToACP2Error()
	if acp2Err == nil {
		t.Fatal("expected non-nil error")
	}
}

// TestDecodeACP2Message_Error_TolerantOfTrailingBytes verifies that a
// non-compliant peer that emits an error with a body (e.g. trailing
// obj-id) does not crash the decoder, and that ObjID remains 0
// (never synthesised from those bytes — they may be garbage).
func TestDecodeACP2Message_Error_TolerantOfTrailingBytes(t *testing.T) {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, 99) // bogus trailing obj-id
	data := append([]byte{
		byte(ACP2TypeError),
		3,
		byte(ErrInvalidObjID),
		0,
	}, body...)

	msg, err := DecodeACP2Message(data)
	if err != nil {
		t.Fatalf("Decode: %v (decoder must tolerate trailing bytes)", err)
	}
	if msg.ObjID != 0 {
		t.Errorf("obj-id=%d want 0 (decoder MUST NOT synthesise obj-id "+
			"from trailing bytes per spec §Error)", msg.ObjID)
	}
	if len(msg.Body) != 4 {
		t.Errorf("Body len=%d want 4 (trailing bytes preserved for "+
			"caller-side compliance inspection)", len(msg.Body))
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

	// Per spec §"Get property" Request (acp2_protocol.docx line 990-1020):
	// body = obj-id (u32 BE) + idx (u32 BE). The pid is carried in ACP2
	// header byte 3; there is NO trailing property header. Total = 12 bytes.
	if len(data) != 12 {
		t.Fatalf("expected 12 bytes (4 hdr + 4 obj-id + 4 idx) per spec; got %d", len(data))
	}

	// Verify pid in header byte 3.
	if data[3] != PIDValue {
		t.Errorf("ACP2 header byte 3 (pid): got %d, want %d", data[3], PIDValue)
	}
	// Verify obj-id at bytes 4-7.
	if got := binary.BigEndian.Uint32(data[4:8]); got != 100 {
		t.Errorf("obj-id: got %d, want 100", got)
	}
	// Verify idx at bytes 8-11.
	if got := binary.BigEndian.Uint32(data[8:12]); got != 0 {
		t.Errorf("idx: got %d, want 0", got)
	}
}
