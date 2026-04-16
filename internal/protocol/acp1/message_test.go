package acp1

import (
	"bytes"
	"errors"
	"testing"
)

// All byte expectations in this file are derived from spec v1.4 directly.
// Any deviation from these bytes is a wire-format bug.

func TestEncode_GetFrameStatus(t *testing.T) {
	// Spec p. 8 broadcast: getValue(FrameStatus) from a client.
	// MTID=0 (broadcast), MTYPE=1 (request), MADDR=0 (rack controller),
	// MCODE=0 (getValue), ObjGroup=6 (frame), ObjID=0.
	m := &Message{
		MTID:     0,
		PVER:     PVER,
		MType:    MTypeRequest,
		MAddr:    0,
		MCode:    byte(MethodGetValue),
		ObjGroup: GroupFrame,
		ObjID:    0,
	}
	want := []byte{
		0x00, 0x00, 0x00, 0x00, // MTID = 0 (broadcast)
		0x01,                   // PVER = 1
		0x01,                   // MTYPE = 1 (request)
		0x00,                   // MADDR = 0
		0x00,                   // MCODE = 0 (getValue)
		0x06,                   // ObjGroup = 6 (frame)
		0x00,                   // ObjID = 0
	}
	got, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("wire mismatch:\n got=%x\nwant=%x", got, want)
	}
}

func TestEncode_GetRootObject(t *testing.T) {
	// getObject on the root object of slot 3.
	// MTID=0xCAFEBABE, MTYPE=1, MADDR=3, MCODE=5 (getObject), group=0, id=0.
	m := &Message{
		MTID:     0xCAFEBABE,
		MType:    MTypeRequest,
		MAddr:    3,
		MCode:    byte(MethodGetObject),
		ObjGroup: GroupRoot,
		ObjID:    0,
	}
	want := []byte{
		0xCA, 0xFE, 0xBA, 0xBE, // MTID big-endian
		0x01,                   // PVER default
		0x01,                   // MTYPE = request
		0x03,                   // MADDR = 3
		0x05,                   // MCODE = getObject
		0x00,                   // group = root
		0x00,                   // id = 0
	}
	got, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("wire mismatch:\n got=%x\nwant=%x", got, want)
	}
}

func TestEncode_SetValueWithPayload(t *testing.T) {
	// setValue on control[7], slot 2, value = int16(-123) (0xFF85 BE).
	m := &Message{
		MTID:     1,
		MType:    MTypeRequest,
		MAddr:    2,
		MCode:    byte(MethodSetValue),
		ObjGroup: GroupControl,
		ObjID:    7,
		Value:    []byte{0xFF, 0x85},
	}
	want := []byte{
		0x00, 0x00, 0x00, 0x01,
		0x01,
		0x01,
		0x02,
		0x01,       // setValue
		0x02,       // group control
		0x07,       // object id
		0xFF, 0x85, // value = int16(-123) big-endian
	}
	got, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("wire mismatch:\n got=%x\nwant=%x", got, want)
	}
}

func TestEncode_MADDRValidation(t *testing.T) {
	m := &Message{MAddr: 32, MType: MTypeRequest}
	if _, err := m.Encode(); err == nil {
		t.Fatal("expected error for MADDR=32, got nil")
	}
}

func TestEncode_ValueTooLarge(t *testing.T) {
	m := &Message{
		MType: MTypeRequest,
		Value: make([]byte, MaxValueData+1),
	}
	if _, err := m.Encode(); err == nil {
		t.Fatal("expected error for oversized value, got nil")
	}
}

func TestDecode_RequestRoundTrip(t *testing.T) {
	in := []byte{
		0xDE, 0xAD, 0xBE, 0xEF,
		0x01, 0x01, 0x05,
		0x00, 0x02, 0x0A,
		0xAA, 0xBB,
	}
	m, err := Decode(in)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if m.MTID != 0xDEADBEEF {
		t.Errorf("MTID: got %x, want deadbeef", m.MTID)
	}
	if m.PVER != 1 {
		t.Errorf("PVER: got %d, want 1", m.PVER)
	}
	if m.MType != MTypeRequest {
		t.Errorf("MType: got %d, want request", m.MType)
	}
	if m.MAddr != 5 {
		t.Errorf("MAddr: got %d, want 5", m.MAddr)
	}
	if m.MCode != 0 {
		t.Errorf("MCode: got %d, want getValue(0)", m.MCode)
	}
	if m.ObjGroup != GroupControl {
		t.Errorf("ObjGroup: got %d, want control(2)", m.ObjGroup)
	}
	if m.ObjID != 10 {
		t.Errorf("ObjID: got %d, want 10", m.ObjID)
	}
	if !bytes.Equal(m.Value, []byte{0xAA, 0xBB}) {
		t.Errorf("Value: got %x, want aabb", m.Value)
	}

	// Re-encode and check we reproduce the original bytes.
	out, err := m.Encode()
	if err != nil {
		t.Fatalf("re-Encode: %v", err)
	}
	if !bytes.Equal(out, in) {
		t.Errorf("round-trip mismatch:\n got=%x\nwant=%x", out, in)
	}
}

func TestDecode_ErrorReply_TransportError(t *testing.T) {
	// MType=3, MCODE=3 (TErrTransactionTimeout), no ObjGrp/ObjID supplied.
	in := []byte{0x00, 0x00, 0x00, 0x2A, 0x01, 0x03, 0x01, 0x03}
	m, err := Decode(in)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !m.IsError() {
		t.Fatal("expected IsError true")
	}
	terr, ok := m.ErrCode().(TransportErr)
	if !ok {
		t.Fatalf("ErrCode type: got %T, want TransportErr", m.ErrCode())
	}
	if terr.Code != TErrTransactionTimeout {
		t.Errorf("code: got %d, want 3", terr.Code)
	}
}

func TestDecode_ErrorReply_ObjectError(t *testing.T) {
	// MType=3, MCODE=19 (OErrNoWriteAccess), device echoes group=2, id=7.
	in := []byte{0x00, 0x00, 0x00, 0x2B, 0x01, 0x03, 0x01, 0x13, 0x02, 0x07}
	m, err := Decode(in)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !m.IsError() {
		t.Fatal("expected IsError true")
	}
	oerr, ok := m.ErrCode().(ObjectErr)
	if !ok {
		t.Fatalf("ErrCode type: got %T, want ObjectErr", m.ErrCode())
	}
	if oerr.Code != OErrNoWriteAccess {
		t.Errorf("code: got %d, want 19", oerr.Code)
	}
	if oerr.Group != GroupControl || oerr.ID != 7 {
		t.Errorf("group/id: got %d/%d, want 2/7", oerr.Group, oerr.ID)
	}
}

func TestDecode_Truncated(t *testing.T) {
	for _, n := range []int{0, 1, 6, 7} {
		buf := make([]byte, n)
		if _, err := Decode(buf); !errors.Is(err, ErrTruncated) {
			t.Errorf("len=%d: got %v, want ErrTruncated", n, err)
		}
	}
}

func TestDecode_Oversized(t *testing.T) {
	buf := make([]byte, MaxPacket+1)
	buf[4] = 1 // valid PVER
	if _, err := Decode(buf); !errors.Is(err, ErrOversized) {
		t.Errorf("got %v, want ErrOversized", err)
	}
}

func TestDecode_BadPVer(t *testing.T) {
	in := []byte{0, 0, 0, 1, 0x09, 0x01, 0x00, 0x00, 0x00, 0x00}
	if _, err := Decode(in); !errors.Is(err, ErrBadPVer) {
		t.Errorf("got %v, want ErrBadPVer", err)
	}
}

func TestIsAnnouncement(t *testing.T) {
	// Spec p. 14 Reply Message Matrix:
	//   MTID=0 MTYPE=0 → status/event/frame-status announce
	//   MTID=0 MTYPE=2 → transaction-value-change announce
	cases := []struct {
		mtid  uint32
		mtype MType
		want  bool
	}{
		{0, MTypeAnnounce, true},
		{0, MTypeReply, true},
		{0, MTypeRequest, false},
		{0, MTypeError, false},
		{1, MTypeAnnounce, false},
		{42, MTypeReply, false},
	}
	for _, c := range cases {
		m := &Message{MTID: c.mtid, MType: c.mtype}
		if got := m.IsAnnouncement(); got != c.want {
			t.Errorf("MTID=%d MType=%d: got %v, want %v", c.mtid, c.mtype, got, c.want)
		}
	}
}
