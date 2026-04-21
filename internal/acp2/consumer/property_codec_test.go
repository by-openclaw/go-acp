package acp2

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func TestPropertyPadding(t *testing.T) {
	tests := []struct {
		plen uint16
		want int
	}{
		{4, 0},  // 4 % 4 = 0, pad = 0
		{5, 3},  // 5 % 4 = 1, pad = 3
		{6, 2},  // 6 % 4 = 2, pad = 2
		{7, 1},  // 7 % 4 = 3, pad = 1
		{8, 0},  // 8 % 4 = 0, pad = 0
		{12, 0}, // aligned
		{13, 3},
		{14, 2},
		{15, 1},
	}
	for _, tt := range tests {
		got := propertyPadding(tt.plen)
		if got != tt.want {
			t.Errorf("propertyPadding(%d) = %d, want %d", tt.plen, got, tt.want)
		}
	}
}

func TestEncodeDecodeProperty_U32(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, 42)
	prop := Property{
		PID:   PIDValue,
		VType: uint8(NumTypeU32),
		PLen:  8,
		Data:  data,
	}

	encoded, err := EncodeProperty(&prop)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// plen=8, pad=(4-8%4)%4=0 → total 8 bytes
	if len(encoded) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(encoded))
	}
	if encoded[0] != PIDValue {
		t.Errorf("pid: got %d, want %d", encoded[0], PIDValue)
	}
	if encoded[1] != uint8(NumTypeU32) {
		t.Errorf("vtype: got %d, want %d", encoded[1], NumTypeU32)
	}

	// Decode round-trip.
	props, err := DecodeProperties(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 property, got %d", len(props))
	}
	if props[0].PID != PIDValue {
		t.Errorf("decoded pid: got %d, want %d", props[0].PID, PIDValue)
	}
	if !bytes.Equal(props[0].Data, data) {
		t.Errorf("decoded data mismatch")
	}
}

func TestEncodeDecodeProperty_String(t *testing.T) {
	prop := MakeStringProperty(PIDLabel, "GainA")

	encoded, err := EncodeProperty(&prop)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// "GainA\0" = 6 bytes, plen = 10, pad = (4-10%4)%4 = 2
	expectedPlen := uint16(4 + 6)
	if prop.PLen != expectedPlen {
		t.Errorf("plen: got %d, want %d", prop.PLen, expectedPlen)
	}
	expectedTotal := 10 + 2 // 12
	if len(encoded) != expectedTotal {
		t.Fatalf("expected %d bytes, got %d", expectedTotal, len(encoded))
	}

	// Verify padding bytes are zero.
	if encoded[10] != 0 || encoded[11] != 0 {
		t.Errorf("padding not zero: %v", encoded[10:12])
	}

	// Decode round-trip.
	props, err := DecodeProperties(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(props) != 1 {
		t.Fatalf("expected 1 property, got %d", len(props))
	}
	got := PropertyString(&props[0])
	if got != "GainA" {
		t.Errorf("decoded string: got %q, want %q", got, "GainA")
	}
}

func TestEncodeDecodeProperties_Multiple(t *testing.T) {
	// Build two properties: a u32 and a string.
	data1 := make([]byte, 4)
	binary.BigEndian.PutUint32(data1, 100)
	p1 := Property{PID: PIDValue, VType: uint8(NumTypeU32), PLen: 8, Data: data1}
	p2 := MakeStringProperty(PIDLabel, "Test")

	encoded, err := EncodeProperties([]Property{p1, p2})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	decoded, err := DecodeProperties(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 properties, got %d", len(decoded))
	}
	if decoded[0].PID != PIDValue {
		t.Errorf("first pid: got %d, want %d", decoded[0].PID, PIDValue)
	}
	if decoded[1].PID != PIDLabel {
		t.Errorf("second pid: got %d, want %d", decoded[1].PID, PIDLabel)
	}
	if PropertyString(&decoded[1]) != "Test" {
		t.Errorf("second string: got %q, want %q", PropertyString(&decoded[1]), "Test")
	}
}

func TestDecodeNumericValue_AllTypes(t *testing.T) {
	tests := []struct {
		name    string
		nt      NumberType
		data    []byte
		wantInt int64
		wantU   uint64
		wantF   float64
	}{
		{
			name:    "S8 positive",
			nt:      NumTypeS8,
			data:    u32Bytes(42),
			wantInt: 42,
		},
		{
			name:    "S8 negative",
			nt:      NumTypeS8,
			data:    s32Bytes(-5),
			wantInt: -5,
		},
		{
			name:    "S16",
			nt:      NumTypeS16,
			data:    s32Bytes(-1000),
			wantInt: -1000,
		},
		{
			name:    "S32",
			nt:      NumTypeS32,
			data:    s32Bytes(-100000),
			wantInt: -100000,
		},
		{
			name:    "U8",
			nt:      NumTypeU8,
			data:    u32Bytes(200),
			wantU:   200,
		},
		{
			name:    "U16",
			nt:      NumTypeU16,
			data:    u32Bytes(60000),
			wantU:   60000,
		},
		{
			name:    "U32",
			nt:      NumTypeU32,
			data:    u32Bytes(0xDEADBEEF),
			wantU:   0xDEADBEEF,
		},
		{
			name:    "Float",
			nt:      NumTypeFloat,
			data:    f32Bytes(3.14),
			wantF:   float64(float32(3.14)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intV, uintV, floatV, err := DecodeNumericValue(tt.nt, tt.data)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			switch {
			case tt.nt <= NumTypeS64:
				if intV != tt.wantInt {
					t.Errorf("int: got %d, want %d", intV, tt.wantInt)
				}
			case tt.nt >= NumTypeU8 && tt.nt <= NumTypeU64:
				if uintV != tt.wantU {
					t.Errorf("uint: got %d, want %d", uintV, tt.wantU)
				}
			case tt.nt == NumTypeFloat:
				if floatV != tt.wantF {
					t.Errorf("float: got %f, want %f", floatV, tt.wantF)
				}
			}
		})
	}
}

func TestDecodeNumericValue_S64(t *testing.T) {
	var v64 int64 = -99999999
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, uint64(v64))
	intV, _, _, err := DecodeNumericValue(NumTypeS64, data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if intV != -99999999 {
		t.Errorf("got %d, want -99999999", intV)
	}
}

func TestDecodeNumericValue_U64(t *testing.T) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, 0xFFFFFFFFFFFFFFFF)
	_, uintV, _, err := DecodeNumericValue(NumTypeU64, data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if uintV != 0xFFFFFFFFFFFFFFFF {
		t.Errorf("got %d, want max uint64", uintV)
	}
}

func TestEncodeDecodeNumericRoundTrip(t *testing.T) {
	tests := []struct {
		nt    NumberType
		intV  int64
		uintV uint64
		fV    float64
	}{
		{NumTypeS32, -42, 0, 0},
		{NumTypeU32, 0, 12345, 0},
		{NumTypeFloat, 0, 0, 2.5},
	}
	for _, tt := range tests {
		data, err := EncodeNumericValue(tt.nt, tt.intV, tt.uintV, tt.fV)
		if err != nil {
			t.Fatalf("Encode %s: %v", tt.nt, err)
		}
		gotI, gotU, gotF, err := DecodeNumericValue(tt.nt, data)
		if err != nil {
			t.Fatalf("Decode %s: %v", tt.nt, err)
		}
		switch {
		case tt.nt <= NumTypeS64:
			if gotI != tt.intV {
				t.Errorf("%s: int got %d, want %d", tt.nt, gotI, tt.intV)
			}
		case tt.nt >= NumTypeU8 && tt.nt <= NumTypeU64:
			if gotU != tt.uintV {
				t.Errorf("%s: uint got %d, want %d", tt.nt, gotU, tt.uintV)
			}
		case tt.nt == NumTypeFloat:
			if gotF != tt.fV {
				t.Errorf("%s: float got %f, want %f", tt.nt, gotF, tt.fV)
			}
		}
	}
}

func TestPropertyChildren(t *testing.T) {
	data := make([]byte, 12)
	binary.BigEndian.PutUint32(data[0:4], 10)
	binary.BigEndian.PutUint32(data[4:8], 20)
	binary.BigEndian.PutUint32(data[8:12], 30)

	p := &Property{PID: PIDChildren, Data: data}
	ids, err := PropertyChildren(p)
	if err != nil {
		t.Fatalf("PropertyChildren: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 children, got %d", len(ids))
	}
	if ids[0] != 10 || ids[1] != 20 || ids[2] != 30 {
		t.Errorf("children: got %v, want [10, 20, 30]", ids)
	}
}

func TestPropertyOptions(t *testing.T) {
	// Spec §5.4 pid 15: fixed 72 bytes per option = u32 BE index + 68-byte
	// NUL-padded UTF-8 name. Build two options: idx=7 "Off", idx=8 "On".
	data := make([]byte, 2*ACP2OptionSize)
	binary.BigEndian.PutUint32(data[0:4], 7)
	copy(data[4:], "Off")
	binary.BigEndian.PutUint32(data[ACP2OptionSize:ACP2OptionSize+4], 8)
	copy(data[ACP2OptionSize+4:], "On")

	p := &Property{PID: PIDOptions, Data: data}
	opts := PropertyOptions(p)
	if len(opts) != 2 {
		t.Fatalf("expected 2 options, got %d", len(opts))
	}
	if opts[0] != "Off" || opts[1] != "On" {
		t.Errorf("options: got %v, want [Off, On]", opts)
	}

	m := PropertyOptionsMap(p)
	if len(m) != 2 {
		t.Fatalf("expected 2 map entries, got %d", len(m))
	}
	if m[7] != "Off" {
		t.Errorf("map[7]: got %q, want %q", m[7], "Off")
	}
	if m[8] != "On" {
		t.Errorf("map[8]: got %q, want %q", m[8], "On")
	}
}

func TestPropertyEventMessages(t *testing.T) {
	data := []byte("alarm on\x00alarm off\x00")
	p := &Property{PID: PIDEventMessages, Data: data}
	on, off := PropertyEventMessages(p)
	if on != "alarm on" {
		t.Errorf("on: got %q, want %q", on, "alarm on")
	}
	if off != "alarm off" {
		t.Errorf("off: got %q, want %q", off, "alarm off")
	}
}

// ---- helpers ----

func u32Bytes(v uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, v)
	return buf
}

func s32Bytes(v int32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(v))
	return buf
}

func f32Bytes(v float32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, math.Float32bits(v))
	return buf
}
