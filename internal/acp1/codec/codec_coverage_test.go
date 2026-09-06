package codec

import (
	"errors"
	"strings"
	"testing"
)

// Targeted coverage of codec helper + error paths that the spec/round-trip
// tests don't reach: reader EOF branches, group name mapping, enum splitting,
// sub-group detection, message Encode/Decode validation, and the typed error
// stringers. Pushes acp1/codec toward full statement coverage.

func TestObjGroup_String(t *testing.T) {
	cases := map[ObjGroup]string{
		GroupRoot: "root", GroupIdentity: "identity", GroupControl: "control",
		GroupStatus: "status", GroupAlarm: "alarm", GroupFile: "file",
		GroupFrame: "frame", ObjGroup(99): "unknown",
	}
	for g, want := range cases {
		if got := g.String(); got != want {
			t.Errorf("ObjGroup(%d).String() = %q, want %q", g, got, want)
		}
	}
}

func TestParseGroup(t *testing.T) {
	ok := map[string]ObjGroup{
		"root": GroupRoot, "ROOT": GroupRoot, "Root": GroupRoot,
		"identity": GroupIdentity, "control": GroupControl, "STATUS": GroupStatus,
		"Alarm": GroupAlarm, "file": GroupFile, "frame": GroupFrame,
	}
	for name, want := range ok {
		if g, found := ParseGroup(name); !found || g != want {
			t.Errorf("ParseGroup(%q) = (%d,%v), want (%d,true)", name, g, found, want)
		}
	}
	if _, found := ParseGroup("nope"); found {
		t.Error("ParseGroup(nope) found=true, want false")
	}
}

func TestReader_EOFPaths(t *testing.T) {
	empty := func() *reader { return &reader{buf: nil} }
	if _, err := empty().u8(); !errors.Is(err, errEOF) {
		t.Errorf("u8 empty: %v, want errEOF", err)
	}
	if _, err := empty().i16(); !errors.Is(err, errEOF) {
		t.Errorf("i16 empty: %v, want errEOF", err)
	}
	if _, err := empty().u32(); !errors.Is(err, errEOF) {
		t.Errorf("u32 empty: %v, want errEOF", err)
	}
	if _, err := empty().i32(); !errors.Is(err, errEOF) {
		t.Errorf("i32 empty: %v, want errEOF", err)
	}
	if _, err := empty().f32(); !errors.Is(err, errEOF) {
		t.Errorf("f32 empty: %v, want errEOF", err)
	}
	if _, err := empty().cstr(); !errors.Is(err, errEOF) {
		t.Errorf("cstr empty: %v, want errEOF", err)
	}
	// Unterminated string (no NUL) → errEOF.
	if _, err := (&reader{buf: []byte("abc")}).cstr(); !errors.Is(err, errEOF) {
		t.Errorf("cstr unterminated: %v, want errEOF", err)
	}
	// Happy paths advance the cursor.
	r := &reader{buf: []byte{0x01, 0x00, 0x05, 'h', 'i', 0x00}}
	if v, err := r.u8(); err != nil || v != 1 {
		t.Errorf("u8 = %d,%v", v, err)
	}
	if v, err := r.i16(); err != nil || v != 5 {
		t.Errorf("i16 = %d,%v", v, err)
	}
	if s, err := r.cstr(); err != nil || s != "hi" {
		t.Errorf("cstr = %q,%v", s, err)
	}
	if r.remaining() != 0 {
		t.Errorf("remaining = %d, want 0", r.remaining())
	}
}

func TestWrap(t *testing.T) {
	err := wrap(errEOF, "root access")
	if !errors.Is(err, errEOF) {
		t.Error("wrap lost the wrapped error")
	}
	if !strings.Contains(err.Error(), "root access") {
		t.Errorf("wrap message = %q, want context", err.Error())
	}
}

func TestSplitEnumItems(t *testing.T) {
	cases := []struct {
		raw  string
		n    int
		want []string
	}{
		{"", 0, nil},
		{"Off,On", 2, []string{"Off", "On"}},
		{"Off,On,Auto", 2, []string{"Off", "On"}}, // truncate to n
		{"Off", 3, []string{"Off", "", ""}},       // pad to n
		{" ", 1, []string{" "}},                   // sub-group marker
	}
	for _, c := range cases {
		got := splitEnumItems(c.raw, c.n)
		if len(got) != len(c.want) {
			t.Errorf("splitEnumItems(%q,%d) len=%d, want %d (%v)", c.raw, c.n, len(got), len(c.want), got)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("splitEnumItems(%q,%d)[%d]=%q, want %q", c.raw, c.n, i, got[i], c.want[i])
			}
		}
	}
}

func TestIsSubGroupMarker(t *testing.T) {
	enumMarker := &DecodedObject{Type: TypeEnum, EnumItems: []string{" "}}
	strMarkerSpace := &DecodedObject{Type: TypeString, Label: " section"}
	strMarkerTab := &DecodedObject{Type: TypeString, Label: "\theader"}
	normalEnum := &DecodedObject{Type: TypeEnum, EnumItems: []string{"Off", "On"}}
	normalStr := &DecodedObject{Type: TypeString, Label: "Card name"}
	for _, c := range []struct {
		o    *DecodedObject
		want bool
	}{
		{enumMarker, true}, {strMarkerSpace, true}, {strMarkerTab, true},
		{normalEnum, false}, {normalStr, false},
	} {
		if got := c.o.IsSubGroupMarker(); got != c.want {
			t.Errorf("IsSubGroupMarker(%+v) = %v, want %v", c.o, got, c.want)
		}
	}
}

func TestDecodeObject_ErrorPaths(t *testing.T) {
	if _, err := DecodeObject(nil); !errors.Is(err, ErrBadProperty) {
		t.Errorf("empty: %v, want ErrBadProperty", err)
	}
	if _, err := DecodeObject([]byte{0x01}); !errors.Is(err, ErrBadProperty) {
		t.Errorf("1 byte (no num_props): %v, want ErrBadProperty", err)
	}
	if _, err := DecodeObject([]byte{byte(TypeReserved), 0}); err == nil {
		t.Error("reserved type 11: want error")
	}
	if _, err := DecodeObject([]byte{200, 0}); err == nil {
		t.Error("unknown type: want error")
	}
}

func TestMessage_EncodeErrors(t *testing.T) {
	var nilMsg *Message
	if _, err := nilMsg.Encode(); err == nil {
		t.Error("nil Encode: want error")
	}
	if _, err := (&Message{MType: 99}).Encode(); err == nil {
		t.Error("bad MType: want error")
	}
	if _, err := (&Message{MType: MTypeRequest, MAddr: 32}).Encode(); err == nil {
		t.Error("MADDR>31: want error")
	}
	if _, err := (&Message{MType: MTypeRequest, Value: make([]byte, MaxValueData+1)}).Encode(); err == nil {
		t.Error("oversized value: want error")
	}
}

func TestMessage_EncodeDecode_RoundTrip(t *testing.T) {
	orig := &Message{MTID: 42, MType: MTypeReply, MAddr: 3, MCode: 1,
		ObjGroup: GroupControl, ObjID: 7, Value: []byte{0x00, 0x05}}
	wire, err := orig.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.MTID != 42 || got.MType != MTypeReply || got.MAddr != 3 ||
		got.MCode != 1 || got.ObjGroup != GroupControl || got.ObjID != 7 ||
		len(got.Value) != 2 || got.Value[1] != 5 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestMessage_DecodeErrors(t *testing.T) {
	if _, err := Decode([]byte{1, 2, 3}); !errors.Is(err, ErrTruncated) {
		t.Errorf("short: %v, want ErrTruncated", err)
	}
	if _, err := Decode(make([]byte, MaxPacket+1)); !errors.Is(err, ErrOversized) {
		t.Errorf("oversized: %v, want ErrOversized", err)
	}
	if _, err := Decode([]byte{0, 0, 0, 0, 2, 1, 0, 5}); !errors.Is(err, ErrBadPVer) {
		t.Errorf("bad PVER: %v, want ErrBadPVer", err)
	}
	if _, err := Decode([]byte{0, 0, 0, 0, 1, 9, 0, 5}); err == nil {
		t.Error("invalid MType: want error")
	}
	// MType<3 with MDATA < 3 bytes.
	if _, err := Decode([]byte{0, 0, 0, 0, 1, byte(MTypeRequest), 0, 5}); !errors.Is(err, ErrTruncated) {
		t.Errorf("short MDATA: %v, want ErrTruncated", err)
	}
}

func TestMessage_ErrCodeAndFlags(t *testing.T) {
	// Non-error → nil.
	if e := (&Message{MType: MTypeReply}).ErrCode(); e != nil {
		t.Errorf("ErrCode on reply = %v, want nil", e)
	}
	// Transport error (MCode < 16).
	var te TransportErr
	if e := (&Message{MType: MTypeError, MCode: 3}).ErrCode(); !errors.As(e, &te) {
		t.Errorf("ErrCode MCode=3 = %T, want TransportErr", e)
	}
	// Object error (MCode >= 16).
	var oe ObjectErr
	if e := (&Message{MType: MTypeError, MCode: 17, ObjGroup: GroupControl, ObjID: 7}).ErrCode(); !errors.As(e, &oe) {
		t.Errorf("ErrCode MCode=17 = %T, want ObjectErr", e)
	}
	// IsAnnouncement / IsError.
	if !(&Message{MTID: 0, MType: MTypeAnnounce}).IsAnnouncement() {
		t.Error("MTID=0 announce: want IsAnnouncement")
	}
	if !(&Message{MTID: 0, MType: MTypeReply}).IsAnnouncement() {
		t.Error("MTID=0 reply: want IsAnnouncement")
	}
	if (&Message{MTID: 5, MType: MTypeAnnounce}).IsAnnouncement() {
		t.Error("MTID!=0: want not announcement")
	}
	if (&Message{MTID: 0, MType: MTypeError}).IsAnnouncement() {
		t.Error("MTID=0 error: want not announcement")
	}
	if !(&Message{MType: MTypeError}).IsError() {
		t.Error("want IsError")
	}
}

// TestDecodeObject_TruncationErrors truncates valid per-type buffers within the
// FIXED-WIDTH field region and asserts an error — exercising the per-field EOF
// branches in decodeRoot / decodeFloat / decodeEnum. Trailing NUL-terminated
// string fields are best-effort (EOF -> empty), so the loop stops before them
// (fixedEnd = first byte index of the first string field).
func TestDecodeObject_TruncationErrors(t *testing.T) {
	// mk builds a valid numeric-object buffer: type, num_props, access, then
	// `numeric` zero bytes (value/default/step/min/max), then a terminated
	// label and no unit (the unit read is EOF-tolerant). Fixed region ends
	// where the label starts (3 + numeric).
	mk := func(ty byte, numeric int) []byte {
		b := []byte{ty, 0x0A, 0x03}
		b = append(b, make([]byte, numeric)...)
		return append(b, 'L', 0x00)
	}
	cases := []struct {
		name     string
		full     []byte
		fixedEnd int
	}{
		{"root", []byte{0x00, 0x09, 0x01, 0x00, 0x08, 0x10, 0x04, 0x02, 0x01}, 9},
		{"integer", mk(0x01, 10), 13},
		{"long", mk(0x09, 20), 23},
		{"byte", mk(0x0A, 5), 8},
		{"ipaddr", mk(0x02, 20), 23},
		{"float", []byte{
			0x03, 0x0A, 0x03,
			0xC0, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x3F, 0x80, 0x00, 0x00, 0xC2, 0xA0, 0x00, 0x00, 0x41, 0xA0, 0x00, 0x00,
			'G', 'a', 'i', 'n', 0x00, 'd', 'B', 0x00,
		}, 23},
		{"enum", []byte{
			0x04, 0x08, 0x03, 0x01, 0x03, 0x00,
			'M', 'o', 'd', 'e', 0x00,
			'O', 'f', 'f', ',', 'O', 'n', ',', 'A', 'u', 't', 'o', 0x00,
		}, 6},
	}
	for _, c := range cases {
		if _, err := DecodeObject(c.full); err != nil {
			t.Fatalf("%s full decode: %v", c.name, err)
		}
		for n := 2; n < c.fixedEnd; n++ {
			if _, err := DecodeObject(c.full[:n]); err == nil {
				t.Errorf("%s truncated to %d bytes (fixed region): want error", c.name, n)
			}
		}
	}
}

// TestDecodeObject_PerTypeNoBody hits each remaining decoder's first field-read
// error branch (empty body after the 2-byte type/num-props prefix).
func TestDecodeObject_PerTypeNoBody(t *testing.T) {
	for _, ty := range []ObjectType{
		TypeInteger, TypeIPAddr, TypeString, TypeFrame,
		TypeAlarm, TypeFile, TypeLong, TypeByte,
	} {
		if _, err := DecodeObject([]byte{byte(ty), 0x00}); err == nil {
			t.Errorf("type %d with empty body: want error", ty)
		}
	}
}

func TestTypedErr_Stringers(t *testing.T) {
	for _, c := range []TransportErrCode{TErrUndefined, TErrInternalBusComm,
		TErrInternalBusTimeout, TErrTransactionTimeout, TErrOutOfResources, 99} {
		if s := (TransportErr{Code: c}).Error(); s == "" {
			t.Errorf("TransportErr(%d).Error() empty", c)
		}
	}
	for _, c := range []ObjectErrCode{OErrGroupNoExist, OErrInstanceNoExist,
		OErrNoWriteAccess, OErrIllegalForType, OErrSPFBufferFull, 250} {
		if s := (ObjectErr{Code: c, Group: GroupControl, ID: 1}).Error(); s == "" {
			t.Errorf("ObjectErr(%d).Error() empty", c)
		}
	}
}
