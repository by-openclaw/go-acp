package acp1

import (
	"strings"
	"testing"

	"acp/internal/export/canonical"
	iacp1 "acp/internal/acp1/consumer"
)

// TestEncodeDecodeRoundTrip asserts that every object type produced by
// encodeObject parses cleanly through the consumer's DecodeObject and
// the fields we set come back unchanged. This is the core correctness
// contract between the provider and any ACP1 client.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		entry *entry
		check func(*testing.T, *iacp1.DecodedObject)
	}{
		{
			name: "integer",
			entry: &entry{
				acpType: iacp1.TypeInteger,
				access:  iacp1.AccessRead | iacp1.AccessWrite,
				param: param("Level", canonical.ParamInteger,
					withValue(int64(-6)),
					withMin(int64(-60)),
					withMax(int64(12)),
					withStep(int64(1)),
					withDefault(int64(0)),
					withUnit("dB"),
				),
			},
			check: func(t *testing.T, o *iacp1.DecodedObject) {
				if o.Type != iacp1.TypeInteger {
					t.Fatalf("type=%d want %d", o.Type, iacp1.TypeInteger)
				}
				if o.IntVal != -6 {
					t.Errorf("value=%d want -6", o.IntVal)
				}
				if o.MinInt != -60 || o.MaxInt != 12 || o.StepInt != 1 || o.DefInt != 0 {
					t.Errorf("range: min=%d max=%d step=%d def=%d",
						o.MinInt, o.MaxInt, o.StepInt, o.DefInt)
				}
				if o.Label != "Level" {
					t.Errorf("label=%q", o.Label)
				}
				if o.Unit != "dB" {
					t.Errorf("unit=%q", o.Unit)
				}
				if o.Access != iacp1.AccessRead|iacp1.AccessWrite {
					t.Errorf("access=%08b", o.Access)
				}
			},
		},
		{
			name: "long",
			entry: &entry{
				acpType: iacp1.TypeLong,
				access:  iacp1.AccessRead,
				param: param("Counter", canonical.ParamInteger,
					withFormat("int32"),
					withValue(int64(1_000_000)),
					withMin(int64(0)),
					withMax(int64(2_000_000)),
				),
			},
			check: func(t *testing.T, o *iacp1.DecodedObject) {
				if o.Type != iacp1.TypeLong {
					t.Fatalf("type=%d want %d", o.Type, iacp1.TypeLong)
				}
				if o.IntVal != 1_000_000 || o.MinInt != 0 || o.MaxInt != 2_000_000 {
					t.Errorf("value=%d min=%d max=%d", o.IntVal, o.MinInt, o.MaxInt)
				}
			},
		},
		{
			name: "byte",
			entry: &entry{
				acpType: iacp1.TypeByte,
				access:  iacp1.AccessRead | iacp1.AccessWrite,
				param: param("Saturation", canonical.ParamInteger,
					withFormat("uint8"),
					withValue(int64(128)),
					withMin(int64(0)),
					withMax(int64(255)),
				),
			},
			check: func(t *testing.T, o *iacp1.DecodedObject) {
				if o.Type != iacp1.TypeByte {
					t.Fatalf("type=%d want %d", o.Type, iacp1.TypeByte)
				}
				if o.ByteVal != 128 || o.MinByte != 0 || o.MaxByte != 255 {
					t.Errorf("value=%d min=%d max=%d", o.ByteVal, o.MinByte, o.MaxByte)
				}
			},
		},
		{
			name: "float",
			entry: &entry{
				acpType: iacp1.TypeFloat,
				access:  iacp1.AccessRead | iacp1.AccessWrite,
				param: param("Frequency", canonical.ParamReal,
					withValue(float64(440.0)),
					withMin(float64(20.0)),
					withMax(float64(20000.0)),
					withStep(float64(0.5)),
					withDefault(float64(1000.0)),
					withUnit("Hz"),
				),
			},
			check: func(t *testing.T, o *iacp1.DecodedObject) {
				if o.Type != iacp1.TypeFloat {
					t.Fatalf("type=%d want float", o.Type)
				}
				if o.FloatVal < 439.99 || o.FloatVal > 440.01 {
					t.Errorf("value=%g want 440", o.FloatVal)
				}
				if o.Unit != "Hz" {
					t.Errorf("unit=%q", o.Unit)
				}
			},
		},
		{
			name: "ipaddr",
			entry: &entry{
				acpType: iacp1.TypeIPAddr,
				access:  iacp1.AccessRead | iacp1.AccessWrite,
				param: param("Gateway", canonical.ParamString,
					withFormat("ipv4"),
					withValue("192.168.1.1"),
				),
			},
			check: func(t *testing.T, o *iacp1.DecodedObject) {
				if o.Type != iacp1.TypeIPAddr {
					t.Fatalf("type=%d want ipaddr", o.Type)
				}
				want := uint32(192)<<24 | uint32(168)<<16 | uint32(1)<<8 | uint32(1)
				if uint32(o.UintVal) != want {
					t.Errorf("value=%x want %x", o.UintVal, want)
				}
			},
		},
		{
			name: "enum",
			entry: &entry{
				acpType: iacp1.TypeEnum,
				access:  iacp1.AccessRead | iacp1.AccessWrite,
				param: param("Mode", canonical.ParamEnum,
					withValue(int64(2)),
					withDefault(int64(0)),
					withEnumMap("Off", "On", "Auto"),
				),
			},
			check: func(t *testing.T, o *iacp1.DecodedObject) {
				if o.Type != iacp1.TypeEnum {
					t.Fatalf("type=%d want enum", o.Type)
				}
				if o.ByteVal != 2 || o.NumItems != 3 {
					t.Errorf("value=%d numItems=%d", o.ByteVal, o.NumItems)
				}
				if strings.Join(o.EnumItems, ",") != "Off,On,Auto" {
					t.Errorf("items=%v", o.EnumItems)
				}
			},
		},
		{
			name: "string",
			entry: &entry{
				acpType: iacp1.TypeString,
				access:  iacp1.AccessRead | iacp1.AccessWrite,
				param: param("Label", canonical.ParamString,
					withValue("hello"),
					withFormat("maxLen=32"),
				),
			},
			check: func(t *testing.T, o *iacp1.DecodedObject) {
				if o.Type != iacp1.TypeString {
					t.Fatalf("type=%d want string", o.Type)
				}
				if o.StrValue != "hello" {
					t.Errorf("value=%q", o.StrValue)
				}
				if o.MaxLen != 32 {
					t.Errorf("maxLen=%d", o.MaxLen)
				}
			},
		},
		{
			name: "alarm",
			entry: &entry{
				acpType: iacp1.TypeAlarm,
				access:  iacp1.AccessRead,
				param: param("OverTemp", canonical.ParamBoolean,
					withValue(false),
					withFormat("alarm,priority=2,tag=17"),
					withDescription("on: Overheat / off: OK"),
				),
			},
			check: func(t *testing.T, o *iacp1.DecodedObject) {
				if o.Type != iacp1.TypeAlarm {
					t.Fatalf("type=%d want alarm", o.Type)
				}
				if o.Priority != 2 || o.Tag != 17 {
					t.Errorf("priority=%d tag=%d", o.Priority, o.Tag)
				}
				if o.Label != "OverTemp" || o.EventOnMsg != "Overheat" || o.EventOffMsg != "OK" {
					t.Errorf("label=%q on=%q off=%q", o.Label, o.EventOnMsg, o.EventOffMsg)
				}
			},
		},
		{
			name: "file",
			entry: &entry{
				acpType: iacp1.TypeFile,
				access:  iacp1.AccessRead | iacp1.AccessWrite,
				param: param("firmware.bin", canonical.ParamString,
					withFormat("file"),
					withValue("firmware.bin"),
					withDefault(int64(42)),
				),
			},
			check: func(t *testing.T, o *iacp1.DecodedObject) {
				if o.Type != iacp1.TypeFile {
					t.Fatalf("type=%d want file", o.Type)
				}
				if o.FileName != "firmware.bin" {
					t.Errorf("name=%q", o.FileName)
				}
				if o.NumFragments != 42 {
					t.Errorf("fragments=%d", o.NumFragments)
				}
			},
		},
		{
			name: "frame",
			entry: &entry{
				acpType: iacp1.TypeFrame,
				access:  iacp1.AccessRead,
				param: param("slotStatus", canonical.ParamOctets,
					withFormat("frame"),
					withValue([]any{int64(2), int64(2), int64(0), int64(3)}),
				),
			},
			check: func(t *testing.T, o *iacp1.DecodedObject) {
				if o.Type != iacp1.TypeFrame {
					t.Fatalf("type=%d want frame", o.Type)
				}
				if o.NumSlots != 4 {
					t.Errorf("numSlots=%d", o.NumSlots)
				}
				if len(o.SlotStatus) != 4 || o.SlotStatus[0] != 2 || o.SlotStatus[3] != 3 {
					t.Errorf("status=%v", o.SlotStatus)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := encodeObject(tc.entry)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			o, err := iacp1.DecodeObject(raw)
			if err != nil {
				t.Fatalf("decode %q: %v", hexString(raw), err)
			}
			tc.check(t, o)
		})
	}
}

// TestEncodeValue exercises the getValue-reply codec for each type.
func TestEncodeValue(t *testing.T) {
	tests := []struct {
		name    string
		entry   *entry
		want    []byte
		wantErr bool
	}{
		{
			name: "int16 positive",
			entry: &entry{acpType: iacp1.TypeInteger, param: param("v", canonical.ParamInteger, withValue(int64(300)))},
			want: []byte{0x01, 0x2C},
		},
		{
			name: "int16 negative",
			entry: &entry{acpType: iacp1.TypeInteger, param: param("v", canonical.ParamInteger, withValue(int64(-1)))},
			want: []byte{0xFF, 0xFF},
		},
		{
			name: "byte",
			entry: &entry{acpType: iacp1.TypeByte, param: param("v", canonical.ParamInteger, withFormat("uint8"), withValue(int64(200)))},
			want: []byte{0xC8},
		},
		{
			name: "long",
			entry: &entry{acpType: iacp1.TypeLong, param: param("v", canonical.ParamInteger, withFormat("int32"), withValue(int64(1_000_000)))},
			want: []byte{0x00, 0x0F, 0x42, 0x40},
		},
		{
			name: "ipaddr",
			entry: &entry{acpType: iacp1.TypeIPAddr, param: param("v", canonical.ParamString, withFormat("ipv4"), withValue("10.0.0.1"))},
			want: []byte{0x0A, 0x00, 0x00, 0x01},
		},
		{
			name: "enum",
			entry: &entry{acpType: iacp1.TypeEnum, param: param("v", canonical.ParamEnum, withValue(int64(1)))},
			want: []byte{0x01},
		},
		{
			name: "string",
			entry: &entry{acpType: iacp1.TypeString, param: param("v", canonical.ParamString, withValue("hi"))},
			want: []byte{'h', 'i', 0},
		},
		{
			name: "alarm active",
			entry: &entry{acpType: iacp1.TypeAlarm, param: param("v", canonical.ParamBoolean, withFormat("alarm"), withValue(true))},
			want: []byte{0x01},
		},
		{
			name: "frame",
			entry: &entry{acpType: iacp1.TypeFrame, param: param("v", canonical.ParamOctets, withFormat("frame"), withValue([]any{int64(2), int64(0), int64(2)}))},
			want: []byte{0x03, 0x02, 0x00, 0x02},
		},
		{
			name:    "overflow int16",
			entry:   &entry{acpType: iacp1.TypeInteger, param: param("v", canonical.ParamInteger, withValue(int64(40000)))},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodeValue(tc.entry)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got bytes %x", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("encodeValue: %v", err)
			}
			if !bytesEqual(got, tc.want) {
				t.Fatalf("bytes=%x want %x", got, tc.want)
			}
		})
	}
}

// TestDeriveACPType covers the strict canonical -> ACP1 type mapping.
func TestDeriveACPType(t *testing.T) {
	cases := []struct {
		name   string
		p      *canonical.Parameter
		want   iacp1.ObjectType
		wantOK bool
	}{
		{"integer default", param("x", canonical.ParamInteger), iacp1.TypeInteger, true},
		{"integer int32", param("x", canonical.ParamInteger, withFormat("int32")), iacp1.TypeLong, true},
		{"integer uint8", param("x", canonical.ParamInteger, withFormat("uint8")), iacp1.TypeByte, true},
		{"integer bad", param("x", canonical.ParamInteger, withFormat("uint16")), 0, false},
		{"real", param("x", canonical.ParamReal), iacp1.TypeFloat, true},
		{"enum", param("x", canonical.ParamEnum), iacp1.TypeEnum, true},
		{"string default", param("x", canonical.ParamString), iacp1.TypeString, true},
		{"ipv4", param("x", canonical.ParamString, withFormat("ipv4")), iacp1.TypeIPAddr, true},
		{"file", param("x", canonical.ParamString, withFormat("file")), iacp1.TypeFile, true},
		{"alarm", param("x", canonical.ParamBoolean, withFormat("alarm")), iacp1.TypeAlarm, true},
		{"boolean no hint REJECTED", param("x", canonical.ParamBoolean), 0, false},
		{"frame", param("x", canonical.ParamOctets, withFormat("frame")), iacp1.TypeFrame, true},
		{"octets no hint REJECTED", param("x", canonical.ParamOctets), 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deriveACPType(tc.p)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("err=%v", err)
				}
				if got != tc.want {
					t.Fatalf("got %d want %d", got, tc.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error, got type %d", got)
			}
		})
	}
}

// ------------------------------------------------------- test helpers

type paramOpt func(p *canonical.Parameter)

func param(ident, typ string, opts ...paramOpt) *canonical.Parameter {
	p := &canonical.Parameter{
		Header: canonical.Header{
			Identifier: ident,
			IsOnline:   true,
			Access:     canonical.AccessReadWrite,
			Children:   canonical.EmptyChildren(),
		},
		Type: typ,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func withValue(v any) paramOpt       { return func(p *canonical.Parameter) { p.Value = v } }
func withDefault(v any) paramOpt     { return func(p *canonical.Parameter) { p.Default = v } }
func withMin(v any) paramOpt         { return func(p *canonical.Parameter) { p.Minimum = v } }
func withMax(v any) paramOpt         { return func(p *canonical.Parameter) { p.Maximum = v } }
func withStep(v any) paramOpt        { return func(p *canonical.Parameter) { p.Step = v } }
func withUnit(s string) paramOpt     { return func(p *canonical.Parameter) { p.Unit = &s } }
func withFormat(s string) paramOpt   { return func(p *canonical.Parameter) { p.Format = &s } }
func withDescription(s string) paramOpt {
	return func(p *canonical.Parameter) { p.Description = &s }
}
func withEnumMap(items ...string) paramOpt {
	return func(p *canonical.Parameter) {
		p.EnumMap = make([]canonical.EnumEntry, len(items))
		for i, item := range items {
			p.EnumMap[i] = canonical.EnumEntry{Key: item, Value: int64(i)}
		}
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hexString(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, x := range b {
		out = append(out, hex[x>>4], hex[x&0x0f])
	}
	return string(out)
}
