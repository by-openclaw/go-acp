package acp1

import (
	"errors"
	"math"
	"testing"
)

// Property-decoder tests. Every byte sequence is hand-built from the spec
// tables (pages 21–27) and cross-checked against the C# reference parser
// in ObjectProperty.cs. Any change here that weakens a case must be
// justified by a citation to the spec, not by "the test was failing".

func TestDecodeRoot(t *testing.T) {
	// Root: type=0, num_props=9, access, boot_mode, 5 counts.
	// Spec p. 21.
	in := []byte{
		0x00, // object_type = 0 (root)
		0x09, // num_properties = 9
		0x01, // access = read
		0x00, // boot_mode = 0 (regular operation)
		0x08, // num_identity = 8
		0x10, // num_control = 16
		0x04, // num_status = 4
		0x02, // num_alarm = 2
		0x01, // num_file = 1
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.Type != TypeRoot || o.NumProps != 9 {
		t.Errorf("type/numprops: got %d/%d", o.Type, o.NumProps)
	}
	if o.Access != 0x01 || o.BootMode != 0 {
		t.Errorf("access/boot: got %d/%d", o.Access, o.BootMode)
	}
	if o.NumIdentity != 8 || o.NumControl != 16 || o.NumStatus != 4 ||
		o.NumAlarm != 2 || o.NumFile != 1 {
		t.Errorf("counts: got id=%d c=%d s=%d a=%d f=%d",
			o.NumIdentity, o.NumControl, o.NumStatus, o.NumAlarm, o.NumFile)
	}
}

func TestDecodeInteger(t *testing.T) {
	// Integer: type=1, num_props=10, access, value, default, step, min, max,
	// label, unit. Spec p. 22.
	in := []byte{
		0x01,       // type = 1
		0x0A,       // num_props = 10
		0x03,       // access = read+write
		0xFF, 0x85, // value = int16(-123)
		0x00, 0x00, // default = 0
		0x00, 0x01, // step = 1
		0xFF, 0x00, // min = -256
		0x00, 0xFF, // max = 255
		'V', 'G', 'a', 'i', 'n', 0x00,
		'd', 'B', 0x00,
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.Type != TypeInteger {
		t.Errorf("type: got %d", o.Type)
	}
	if o.IntVal != -123 {
		t.Errorf("value: got %d, want -123", o.IntVal)
	}
	if o.DefInt != 0 || o.StepInt != 1 || o.MinInt != -256 || o.MaxInt != 255 {
		t.Errorf("constraints: def=%d step=%d min=%d max=%d",
			o.DefInt, o.StepInt, o.MinInt, o.MaxInt)
	}
	if o.Label != "VGain" || o.Unit != "dB" {
		t.Errorf("label/unit: %q / %q", o.Label, o.Unit)
	}
}

func TestDecodeLong(t *testing.T) {
	// Long: same layout as Integer but int32. Spec p. 26.
	in := []byte{
		0x09,                   // type = 9
		0x0A,                   // num_props = 10
		0x01,                   // access = read
		0x7F, 0xFF, 0xFF, 0xFF, // value = int32 max
		0x00, 0x00, 0x00, 0x00, // default = 0
		0x00, 0x00, 0x00, 0x01, // step
		0x80, 0x00, 0x00, 0x00, // min = int32 min
		0x7F, 0xFF, 0xFF, 0xFF, // max = int32 max
		'C', 'n', 't', 0x00,
		0x00, // empty unit (just NUL)
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.Type != TypeLong {
		t.Errorf("type: got %d", o.Type)
	}
	if o.IntVal != math.MaxInt32 {
		t.Errorf("value: got %d, want %d", o.IntVal, math.MaxInt32)
	}
	if o.MinInt != math.MinInt32 || o.MaxInt != math.MaxInt32 {
		t.Errorf("range: %d..%d", o.MinInt, o.MaxInt)
	}
	if o.Label != "Cnt" {
		t.Errorf("label: %q", o.Label)
	}
}

func TestDecodeByte(t *testing.T) {
	// Byte: u8 value + u8 constraints. Spec p. 27.
	in := []byte{
		0x0A, // type = 10
		0x0A, // num_props = 10
		0x03, // access = rw
		0x2A, // value = 42
		0x00, // default
		0x01, // step
		0x00, // min
		0xFF, // max
		'P', 'c', 't', 0x00,
		'%', 0x00,
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.ByteVal != 42 || o.MaxByte != 255 {
		t.Errorf("value/max: %d/%d", o.ByteVal, o.MaxByte)
	}
	if o.Label != "Pct" || o.Unit != "%" {
		t.Errorf("label/unit: %q/%q", o.Label, o.Unit)
	}
}

func TestDecodeFloat(t *testing.T) {
	// Float: IEEE-754 single, MSB first. Spec p. 23.
	// -6.0 dB in float32 BE: 0xC0 0xC0 0x00 0x00
	in := []byte{
		0x03, // type = 3
		0x0A, // num_props = 10
		0x03, // access
		0xC0, 0xC0, 0x00, 0x00, // value = -6.0
		0x00, 0x00, 0x00, 0x00, // default = 0.0
		0x3F, 0x80, 0x00, 0x00, // step = 1.0
		0xC2, 0xA0, 0x00, 0x00, // min = -80.0
		0x41, 0xA0, 0x00, 0x00, // max = 20.0
		'G', 'a', 'i', 'n', 0x00,
		'd', 'B', 0x00,
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.FloatVal != -6.0 {
		t.Errorf("value: got %v, want -6.0", o.FloatVal)
	}
	if o.MinFloat != -80.0 || o.MaxFloat != 20.0 {
		t.Errorf("range: %v..%v", o.MinFloat, o.MaxFloat)
	}
	if o.StepFloat != 1.0 || o.DefFloat != 0.0 {
		t.Errorf("def/step: %v/%v", o.DefFloat, o.StepFloat)
	}
	if o.Label != "Gain" || o.Unit != "dB" {
		t.Errorf("label/unit: %q/%q", o.Label, o.Unit)
	}
}

func TestDecodeIPAddr(t *testing.T) {
	// IPAddr: uint32 value + uint32 constraints. Spec p. 22.
	// 192.168.1.5 as uint32 big-endian = 0xC0A80105
	in := []byte{
		0x02, // type = 2
		0x0A, // num_props = 10
		0x03, // access
		0xC0, 0xA8, 0x01, 0x05, // value
		0x00, 0x00, 0x00, 0x00, // default
		0x00, 0x00, 0x00, 0x01, // step
		0x00, 0x00, 0x00, 0x00, // min
		0xFF, 0xFF, 0xFF, 0xFF, // max
		'I', 'P', 0x00,
		0x00,
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.UintVal != 0xC0A80105 {
		t.Errorf("value: got %x, want c0a80105", o.UintVal)
	}
	if o.MaxUint != 0xFFFFFFFF {
		t.Errorf("max: %x", o.MaxUint)
	}
	if o.Label != "IP" {
		t.Errorf("label: %q", o.Label)
	}
}

func TestDecodeEnum(t *testing.T) {
	// Enum: access, value(idx), num_items, default, label, items. Spec p. 23.
	in := []byte{
		0x04, // type = 4
		0x08, // num_props = 8
		0x03, // access
		0x01, // value = idx 1 (→ "On")
		0x03, // num_items = 3
		0x00, // default = 0 (→ "Off")
		'M', 'o', 'd', 'e', 0x00,
		'O', 'f', 'f', ',', 'O', 'n', ',', 'A', 'u', 't', 'o', 0x00,
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.ByteVal != 1 || o.NumItems != 3 || o.DefByte != 0 {
		t.Errorf("enum values: val=%d n=%d def=%d", o.ByteVal, o.NumItems, o.DefByte)
	}
	if o.Label != "Mode" {
		t.Errorf("label: %q", o.Label)
	}
	want := []string{"Off", "On", "Auto"}
	if len(o.EnumItems) != 3 {
		t.Fatalf("items len: got %d, want 3: %v", len(o.EnumItems), o.EnumItems)
	}
	for i, w := range want {
		if o.EnumItems[i] != w {
			t.Errorf("items[%d]: got %q, want %q", i, o.EnumItems[i], w)
		}
	}
}

func TestDecodeEnum_SubGroupMarker(t *testing.T) {
	// Device convention (C# reference): enum with a single " " item marks
	// a Control-group section header. Not in the spec — must still decode
	// cleanly.
	in := []byte{
		0x04, // type = 4
		0x08, // num_props = 8
		0x01, // access = read
		0x00, // value
		0x01, // num_items = 1
		0x00, // default
		'A', 'u', 'd', 'i', 'o', 0x00,
		' ', 0x00, // the magic single-space item
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if !o.IsSubGroupMarker() {
		t.Errorf("expected IsSubGroupMarker true, items=%v", o.EnumItems)
	}
}

func TestDecodeString(t *testing.T) {
	// String: access, value(NUL-term), max_len, label. Spec p. 24.
	in := []byte{
		0x05, // type = 5
		0x06, // num_props = 6
		0x03, // access
		'C', 'H', '1', 0x00, // value
		0x10, // max_len = 16
		'N', 'a', 'm', 'e', 0x00, // label
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.StrValue != "CH1" {
		t.Errorf("value: %q", o.StrValue)
	}
	if o.MaxLen != 16 {
		t.Errorf("max_len: %d", o.MaxLen)
	}
	if o.Label != "Name" {
		t.Errorf("label: %q", o.Label)
	}
}

func TestDecodeFrame(t *testing.T) {
	// Frame Status: access + num_slots + slot_status[]. Spec p. 24.
	// 5-slot frame, all present except slot 3 = error.
	in := []byte{
		0x06, // type = 6
		0x04, // num_props = 4
		0x01, // access = read-only
		0x05, // num_slots = 5
		0x02, 0x02, 0x02, 0x03, 0x02, // statuses
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.NumSlots != 5 {
		t.Errorf("num_slots: %d", o.NumSlots)
	}
	want := []uint8{2, 2, 2, 3, 2}
	if len(o.SlotStatus) != 5 {
		t.Fatalf("slots len: %d", len(o.SlotStatus))
	}
	for i, w := range want {
		if o.SlotStatus[i] != w {
			t.Errorf("slots[%d]: got %d, want %d", i, o.SlotStatus[i], w)
		}
	}
}

func TestDecodeAlarm(t *testing.T) {
	// Alarm: access, priority, tag, label, event_on, event_off. Spec p. 25.
	// 8 properties after the rev 1.2 event_off addition.
	in := []byte{
		0x07, // type = 7
		0x08, // num_props = 8
		0x03, // access
		0x05, // priority = 5
		0x42, // tag
		'T', 'e', 'm', 'p', 0x00,
		'O', 'v', 'e', 'r', 0x00,
		'O', 'K', 0x00,
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.Priority != 5 || o.Tag != 0x42 {
		t.Errorf("priority/tag: %d/%x", o.Priority, o.Tag)
	}
	if o.Label != "Temp" || o.EventOnMsg != "Over" || o.EventOffMsg != "OK" {
		t.Errorf("strings: label=%q on=%q off=%q",
			o.Label, o.EventOnMsg, o.EventOffMsg)
	}
}

func TestDecodeFile(t *testing.T) {
	// File: access, num_fragments (int16), file_name. Fragment property
	// NOT returned by getObject. Spec p. 26.
	in := []byte{
		0x08,       // type = 8
		0x05,       // num_props = 5
		0x01,       // access
		0x00, 0x64, // num_fragments = 100
		'f', 'w', '.', 'b', 'i', 'n', 0x00,
	}
	o, err := DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.NumFragments != 100 {
		t.Errorf("fragments: %d", o.NumFragments)
	}
	if o.FileName != "fw.bin" {
		t.Errorf("name: %q", o.FileName)
	}
}

func TestDecodeObject_Truncated(t *testing.T) {
	// Integer header but cut off mid-value.
	in := []byte{0x01, 0x0A, 0x03, 0xFF}
	_, err := DecodeObject(in)
	if err == nil {
		t.Fatal("expected error for truncated integer, got nil")
	}
	if !errors.Is(err, errEOF) && err.Error() == "" {
		t.Errorf("unexpected error type: %v", err)
	}
}

func TestDecodeObject_Reserved(t *testing.T) {
	in := []byte{0x0B, 0x02, 0x01}
	if _, err := DecodeObject(in); err == nil {
		t.Fatal("expected error for reserved type 11")
	}
}

func TestDecodeObject_Unknown(t *testing.T) {
	in := []byte{0xFF, 0x02, 0x01}
	if _, err := DecodeObject(in); err == nil {
		t.Fatal("expected error for unknown type 255")
	}
}

func TestSplitEnumItems_PadsAndTruncates(t *testing.T) {
	// Device sends 2 items but declares num_items=3 → pad with "".
	if got := splitEnumItems("A,B", 3); len(got) != 3 || got[2] != "" {
		t.Errorf("pad: %v", got)
	}
	// Device sends 4 items but declares num_items=2 → truncate.
	if got := splitEnumItems("A,B,C,D", 2); len(got) != 2 || got[1] != "B" {
		t.Errorf("truncate: %v", got)
	}
}
