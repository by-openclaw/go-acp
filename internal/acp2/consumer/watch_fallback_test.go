package acp2

import (
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
)

// TestObjTypeFromVType pins #351: the announce fallback maps the
// property header's vtype byte (spec §5.2.2) to the right ObjType so
// Enum / IPv4 / String announces decode typed instead of falling
// through to KindRaw or being misclassified as KindUint via the
// "always Number" old fallback path.
func TestObjTypeFromVType(t *testing.T) {
	cases := []struct {
		nt   codec.NumberType
		want codec.ACP2ObjType
	}{
		{codec.NumTypeS8, codec.ObjTypeNumber},
		{codec.NumTypeS32, codec.ObjTypeNumber},
		{codec.NumTypeU32, codec.ObjTypeNumber},
		{codec.NumTypeFloat, codec.ObjTypeNumber},
		{codec.NumTypePreset, codec.ObjTypeEnum},   // 9 → Enum
		{codec.NumTypeIPv4, codec.ObjTypeIPv4},     // 10 → IPv4
		{codec.NumTypeString, codec.ObjTypeString}, // 11 → String
	}
	for _, c := range cases {
		got := objTypeFromVType(c.nt)
		if got != c.want {
			t.Errorf("objTypeFromVType(%v) = %v, want %v", c.nt, got, c.want)
		}
	}
}

// TestDecodePropertyValue_FallbackTypes verifies decodePropertyValue
// handles each object type when called via the announce fallback
// (tree=nil) using the vtype-derived ObjType from objTypeFromVType.
// Pre-fix: only Number returned a typed Value; Enum/IPv4/String fell
// through to KindRaw.
func TestDecodePropertyValue_FallbackTypes(t *testing.T) {
	t.Run("enum_vtype9", func(t *testing.T) {
		// pid 8 enum value: vtype=9 (preset/enum), body = u32 BE wire idx.
		// Real Neuron obj 163613 Phase: idx 674 = "180 degrees".
		body := []byte{0x00, 0x00, 0x02, 0xa2}
		p := &codec.Property{
			PID:   codec.PIDValue,
			VType: uint8(codec.NumTypePreset),
			PLen:  8,
			Data:  body,
		}
		v, err := decodePropertyValue(p, objTypeFromVType(codec.NumberType(p.VType)),
			codec.NumberType(p.VType), nil, 163613)
		if err != nil {
			t.Fatalf("decodePropertyValue: %v", err)
		}
		if v.Kind != consumer.KindEnum {
			t.Errorf("Kind=%v want %v", v.Kind, consumer.KindEnum)
		}
		if v.Uint != 674 {
			t.Errorf("Uint=%d want 674", v.Uint)
		}
	})

	t.Run("ipv4_vtype10", func(t *testing.T) {
		// pid 8 ipv4 value: vtype=10, body = 4 octets.
		// Real Neuron obj 15828 Neighbor IP: 10.41.40.5.
		body := []byte{10, 41, 40, 5}
		p := &codec.Property{
			PID:   codec.PIDValue,
			VType: uint8(codec.NumTypeIPv4),
			PLen:  8,
			Data:  body,
		}
		v, err := decodePropertyValue(p, objTypeFromVType(codec.NumberType(p.VType)),
			codec.NumberType(p.VType), nil, 15828)
		if err != nil {
			t.Fatalf("decodePropertyValue: %v", err)
		}
		if v.Kind != consumer.KindIPAddr {
			t.Errorf("Kind=%v want %v", v.Kind, consumer.KindIPAddr)
		}
		if v.IPAddr != [4]byte{10, 41, 40, 5} {
			t.Errorf("IPAddr=%v want [10 41 40 5]", v.IPAddr)
		}
	})

	t.Run("string_vtype11", func(t *testing.T) {
		// pid 8 string value: vtype=11, body = NUL-terminated UTF-8.
		body := []byte{'O', 'S', 'D', '-', '1', 0x00}
		p := &codec.Property{
			PID:   codec.PIDValue,
			VType: uint8(codec.NumTypeString),
			PLen:  uint16(4 + len(body)),
			Data:  body,
		}
		v, err := decodePropertyValue(p, objTypeFromVType(codec.NumberType(p.VType)),
			codec.NumberType(p.VType), nil, 15555)
		if err != nil {
			t.Fatalf("decodePropertyValue: %v", err)
		}
		if v.Kind != consumer.KindString {
			t.Errorf("Kind=%v want %v", v.Kind, consumer.KindString)
		}
		if v.Str != "OSD-1" {
			t.Errorf("Str=%q want %q", v.Str, "OSD-1")
		}
	})

	t.Run("number_vtype4_u8", func(t *testing.T) {
		// pid 8 u8 value: vtype=4, body = 4-byte u32 (sign-extended u8).
		body := []byte{0x00, 0x00, 0x00, 0x0a}
		p := &codec.Property{
			PID:   codec.PIDValue,
			VType: uint8(codec.NumTypeU8),
			PLen:  8,
			Data:  body,
		}
		v, err := decodePropertyValue(p, objTypeFromVType(codec.NumberType(p.VType)),
			codec.NumberType(p.VType), nil, 15211)
		if err != nil {
			t.Fatalf("decodePropertyValue: %v", err)
		}
		if v.Kind != consumer.KindUint {
			t.Errorf("Kind=%v want %v", v.Kind, consumer.KindUint)
		}
		if v.Uint != 10 {
			t.Errorf("Uint=%d want 10", v.Uint)
		}
	})
}
