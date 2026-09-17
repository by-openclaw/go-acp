package codec

import "testing"

// Covers the enum String() stringers, the error-type Error() methods, and the
// PropertyU16/U32 + MakeValueProperty value helpers. These are pure functions
// (no transport) so they're table-driven against the spec-literal names in
// ../CLAUDE.md. Each stringer table includes an out-of-range value to exercise
// the fmt.Sprintf default branch.

func TestAN2Proto_String(t *testing.T) {
	cases := map[AN2Proto]string{
		AN2ProtoInternal: "AN2",
		AN2ProtoACP1:     "ACP1",
		AN2ProtoACP2:     "ACP2",
		AN2ProtoACMP:     "ACMP",
		AN2Proto(9):      "proto(9)",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("AN2Proto(%d).String() = %q, want %q", in, got, want)
		}
	}
}

func TestAN2Type_String(t *testing.T) {
	cases := map[AN2Type]string{
		AN2TypeRequest: "request",
		AN2TypeReply:   "reply",
		AN2TypeEvent:   "event",
		AN2TypeError:   "error",
		AN2TypeData:    "data",
		AN2Type(9):     "type(9)",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("AN2Type(%d).String() = %q, want %q", in, got, want)
		}
	}
}

func TestACP2MsgType_String(t *testing.T) {
	cases := map[ACP2MsgType]string{
		ACP2TypeRequest:  "request",
		ACP2TypeReply:    "reply",
		ACP2TypeAnnounce: "announce",
		ACP2TypeError:    "error",
		ACP2MsgType(9):   "acp2type(9)",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("ACP2MsgType(%d).String() = %q, want %q", in, got, want)
		}
	}
}

func TestACP2Func_String(t *testing.T) {
	cases := map[ACP2Func]string{
		ACP2FuncGetVersion:  "get_version",
		ACP2FuncGetObject:   "get_object",
		ACP2FuncGetProperty: "get_property",
		ACP2FuncSetProperty: "set_property",
		ACP2Func(9):         "func(9)",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("ACP2Func(%d).String() = %q, want %q", in, got, want)
		}
	}
}

func TestACP2ObjType_String(t *testing.T) {
	cases := map[ACP2ObjType]string{
		ObjTypeNode:    "node",
		ObjTypePreset:  "preset",
		ObjTypeEnum:    "enum",
		ObjTypeNumber:  "number",
		ObjTypeIPv4:    "ipv4",
		ObjTypeString:  "string",
		ACP2ObjType(9): "objtype(9)",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("ACP2ObjType(%d).String() = %q, want %q", in, got, want)
		}
	}
}

func TestNumberType_String(t *testing.T) {
	cases := map[NumberType]string{
		NumTypeS8:      "s8",
		NumTypeS16:     "s16",
		NumTypeS32:     "s32",
		NumTypeS64:     "s64",
		NumTypeU8:      "u8",
		NumTypeU16:     "u16",
		NumTypeU32:     "u32",
		NumTypeU64:     "u64",
		NumTypeFloat:   "float",
		NumTypePreset:  "preset",
		NumTypeIPv4:    "ipv4",
		NumTypeString:  "string",
		NumberType(99): "numtype(99)",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("NumberType(%d).String() = %q, want %q", in, got, want)
		}
	}
}

func TestACP2Error_Error(t *testing.T) {
	cases := []struct {
		status ACP2ErrStatus
		want   string
	}{
		{ErrProtocol, "acp2 error (obj-id 7): protocol error"},
		{ErrInvalidObjID, "acp2 error (obj-id 7): invalid obj-id"},
		{ErrInvalidIdx, "acp2 error (obj-id 7): invalid idx"},
		{ErrInvalidPID, "acp2 error (obj-id 7): invalid pid"},
		{ErrNoAccess, "acp2 error (obj-id 7): no access"},
		{ErrInvalidValue, "acp2 error (obj-id 7): invalid value"},
		{ACP2ErrStatus(42), "acp2 error (obj-id 7): unknown status 42"},
	}
	for _, c := range cases {
		e := &ACP2Error{Status: c.status, ObjID: 7}
		if got := e.Error(); got != c.want {
			t.Errorf("ACP2Error{status:%d}.Error() = %q, want %q", c.status, got, c.want)
		}
		e.dhsError() // marker method — invoke for coverage
	}
}

func TestAN2Error_Error(t *testing.T) {
	e := &AN2Error{Slot: 3, Func: 2, Msg: "boom"}
	const want = "an2 error (slot 3, func 2): boom"
	if got := e.Error(); got != want {
		t.Errorf("AN2Error.Error() = %q, want %q", got, want)
	}
	e.dhsError() // marker method — invoke for coverage
}

func TestPropertyU32(t *testing.T) {
	p := &Property{PID: PIDValue, Data: []byte{0x01, 0x02, 0x03, 0x04}}
	got, err := PropertyU32(p)
	if err != nil {
		t.Fatalf("PropertyU32: %v", err)
	}
	if got != 0x01020304 {
		t.Errorf("PropertyU32 = %#x, want 0x01020304", got)
	}
	// too short -> error
	if _, err := PropertyU32(&Property{PID: PIDValue, Data: []byte{0x01, 0x02}}); err == nil {
		t.Error("PropertyU32: want error for short data, got nil")
	}
}

func TestPropertyU16(t *testing.T) {
	p := &Property{PID: PIDStringMaxLength, Data: []byte{0xAB, 0xCD}}
	got, err := PropertyU16(p)
	if err != nil {
		t.Fatalf("PropertyU16: %v", err)
	}
	if got != 0xABCD {
		t.Errorf("PropertyU16 = %#x, want 0xABCD", got)
	}
	// too short -> error
	if _, err := PropertyU16(&Property{PID: PIDStringMaxLength, Data: []byte{0xAB}}); err == nil {
		t.Error("PropertyU16: want error for short data, got nil")
	}
}

func TestMakeValueProperty(t *testing.T) {
	data := []byte{0x00, 0x05}
	p := MakeValueProperty(PIDValue, NumTypeS16, data)
	if p.PID != PIDValue {
		t.Errorf("PID = %d, want %d", p.PID, PIDValue)
	}
	if p.VType != uint8(NumTypeS16) {
		t.Errorf("VType = %d, want %d", p.VType, uint8(NumTypeS16))
	}
	if p.PLen != uint16(4+len(data)) {
		t.Errorf("PLen = %d, want %d", p.PLen, 4+len(data))
	}
	// MakeValueProperty must round-trip through EncodeProperty/DecodeProperties.
	enc, err := EncodeProperty(&p)
	if err != nil {
		t.Fatalf("EncodeProperty: %v", err)
	}
	props, err := DecodeProperties(enc)
	if err != nil {
		t.Fatalf("DecodeProperties: %v", err)
	}
	if len(props) != 1 || props[0].PID != PIDValue || string(props[0].Data) != string(data) {
		t.Errorf("round-trip = %+v, want pid=%d data=%v", props, PIDValue, data)
	}
}
