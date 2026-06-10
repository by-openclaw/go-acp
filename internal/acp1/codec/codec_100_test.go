package codec

import (
	"testing"
)

// TestDecode_RequiredLabelMissing truncates each numeric object buffer exactly
// at the start of its (required) label field, hitting the label-read error
// return that the fixed-region truncation loop stops short of.
func TestDecode_RequiredLabelMissing(t *testing.T) {
	mk := func(ty byte, numeric int) []byte {
		b := []byte{ty, 0x0A, 0x03}
		return append(b, make([]byte, numeric)...) // access + numerics, NO label
	}
	cases := map[string][]byte{
		"integer": mk(0x01, 10),
		"long":    mk(0x09, 20),
		"byte":    mk(0x0A, 5),
		"ipaddr":  mk(0x02, 20),
		"float":   mk(0x03, 20),
		// enum: type,num_props,access,value,num_items,default — then label.
		"enum": {0x04, 0x08, 0x03, 0x01, 0x03, 0x00},
	}
	for name, buf := range cases {
		if _, err := DecodeObject(buf); err == nil {
			t.Errorf("%s with no label bytes: want label-required error", name)
		}
	}
}

// TestDecode_StringFrameAlarmFile_Truncation walks each non-numeric decoder
// through its required-field error returns.
func TestDecode_StringFrameAlarmFile_Truncation(t *testing.T) {
	cases := []struct {
		name  string
		bufs  [][]byte // each must fail to decode
		valid []byte   // must decode cleanly
	}{
		{
			name: "string",
			bufs: [][]byte{
				{0x05, 0x06, 0x03},                   // access ok, value cstr EOF (required)
				{0x05, 0x06, 0x03, 'h', 'i', 0x00},   // value ok, max_len u8 EOF
			},
			valid: []byte{0x05, 0x06, 0x03, 'h', 'i', 0x00, 0x10, 'L', 0x00},
		},
		{
			name: "frame",
			bufs: [][]byte{
				{0x06, 0x04, 0x03},       // access ok, num_slots EOF
				{0x06, 0x04, 0x03, 0x02}, // num_slots=2, no slot bytes
			},
			valid: []byte{0x06, 0x04, 0x03, 0x02, 0x02, 0x00},
		},
		{
			name: "alarm",
			bufs: [][]byte{
				{0x07, 0x08, 0x03},             // access ok, priority EOF
				{0x07, 0x08, 0x03, 0x01},       // priority ok, tag EOF
				{0x07, 0x08, 0x03, 0x01, 0x00}, // tag ok, label cstr EOF (required)
			},
			valid: []byte{0x07, 0x08, 0x03, 0x01, 0x00, 'A', 0x00},
		},
		{
			name: "file",
			bufs: [][]byte{
				{0x08, 0x05, 0x03},       // access ok, num_fragments i16 EOF
				{0x08, 0x05, 0x03, 0x00}, // only 1 of 2 num_fragments bytes
			},
			valid: []byte{0x08, 0x05, 0x03, 0x00, 0x02, 'f', 0x00},
		},
	}
	for _, c := range cases {
		for i, b := range c.bufs {
			if _, err := DecodeObject(b); err == nil {
				t.Errorf("%s buf #%d (%v): want error", c.name, i, b)
			}
		}
		if _, err := DecodeObject(c.valid); err != nil {
			t.Errorf("%s valid buffer should decode: %v", c.name, err)
		}
	}
}

// TestDecode_OptionalTrailingFieldsTolerated proves the optCstr-backed trailing
// fields decode cleanly when omitted on the wire (the behaviour the removed
// dead EOF-guards used to express, now centralised in optCstr).
func TestDecode_OptionalTrailingFieldsTolerated(t *testing.T) {
	// Integer with label but NO unit (unit is optional).
	noUnit := []byte{0x01, 0x0A, 0x03,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // 10 numeric bytes
		'L', 0x00, // label, no unit
	}
	o, err := DecodeObject(noUnit)
	if err != nil {
		t.Fatalf("integer without unit should decode: %v", err)
	}
	if o.Label != "L" || o.Unit != "" {
		t.Errorf("label=%q unit=%q, want L / empty", o.Label, o.Unit)
	}

	// Alarm with label but no event messages.
	noMsgs := []byte{0x07, 0x08, 0x03, 0x01, 0x00, 'A', 0x00}
	if o, err = DecodeObject(noMsgs); err != nil {
		t.Fatalf("alarm without messages should decode: %v", err)
	}
	if o.EventOnMsg != "" || o.EventOffMsg != "" {
		t.Errorf("event messages should be empty, got on=%q off=%q", o.EventOnMsg, o.EventOffMsg)
	}
}

// TestOptCstr covers the trailing-optional-string reader helper directly.
func TestOptCstr(t *testing.T) {
	if s := (&reader{buf: nil}).optCstr(); s != "" {
		t.Errorf("optCstr on empty = %q, want empty", s)
	}
	if s := (&reader{buf: []byte("no-nul")}).optCstr(); s != "" {
		t.Errorf("optCstr on unterminated = %q, want empty (EOF tolerated)", s)
	}
	r := &reader{buf: []byte{'h', 'i', 0x00, 'x'}}
	if s := r.optCstr(); s != "hi" {
		t.Errorf("optCstr = %q, want hi", s)
	}
	if r.remaining() != 1 {
		t.Errorf("optCstr should advance past the NUL, remaining=%d", r.remaining())
	}
}

// TestObjectErr_AllCodes calls ObjectErr.Error for every named code plus an
// unknown one, covering the full message switch.
func TestObjectErr_AllCodes(t *testing.T) {
	codes := []ObjectErrCode{
		OErrGroupNoExist, OErrInstanceNoExist, OErrPropertyNoExist,
		OErrNoWriteAccess, OErrNoReadAccess, OErrNoSetDefAccess,
		OErrTypeNoExist, OErrIllegalMethod, OErrIllegalForType,
		OErrFile, OErrSPFConstraint, OErrSPFBufferFull,
		ObjectErrCode(200), // unknown → default arm
	}
	for _, c := range codes {
		if s := (ObjectErr{Code: c, Group: GroupControl, ID: 3}).Error(); s == "" {
			t.Errorf("ObjectErr(%d).Error() empty", c)
		}
	}
}

// TestEncode_ValueLengthBranch reaches the len(Value) > MaxValueData guard via
// an Error message (mdataLen stays 1, so the MDATA-size check passes first and
// the value-length check is the one that fires).
func TestEncode_ValueLengthBranch(t *testing.T) {
	m := &Message{MType: MTypeError, MCode: 17, Value: make([]byte, MaxValueData+1)}
	if _, err := m.Encode(); err == nil {
		t.Error("oversized Value on an Error message: want error")
	}
	// And the MDATA-too-large branch via a request whose preamble+value exceeds
	// MaxMDATA.
	big := &Message{MType: MTypeReply, Value: make([]byte, MaxMDATA)}
	if _, err := big.Encode(); err == nil {
		t.Error("MDATA over MaxMDATA: want error")
	}
}

// TestEncode_ErrorMessage covers the MType=Error write path: only the MCode
// byte is emitted as MDATA (no ObjGroup/ObjID/Value).
func TestEncode_ErrorMessage(t *testing.T) {
	m := &Message{MTID: 7, MType: MTypeError, MAddr: 2, MCode: byte(OErrInstanceNoExist)}
	wire, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode error message: %v", err)
	}
	if len(wire) != HeaderSize+1 {
		t.Fatalf("error frame len = %d, want %d", len(wire), HeaderSize+1)
	}
	if wire[HeaderSize] != byte(OErrInstanceNoExist) {
		t.Errorf("MDATA[0] = %d, want %d", wire[HeaderSize], OErrInstanceNoExist)
	}
	// Round-trips back to an error message.
	got, err := Decode(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.IsError() || got.MCode != byte(OErrInstanceNoExist) {
		t.Errorf("decoded = %+v, want error with MCode %d", got, OErrInstanceNoExist)
	}
}
