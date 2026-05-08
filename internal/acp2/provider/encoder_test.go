package acp2

import (
	"encoding/binary"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/acp2/codec"
)

// helper — round-trips a tree through buildProperties + EncodeProperties
// + DecodeProperties so we assert wire-level correctness rather than
// just struct equality.
func buildAndDecode(t *testing.T, e *entry) []codec.Property {
	t.Helper()
	props, err := buildProperties(e)
	if err != nil {
		t.Fatalf("buildProperties: %v", err)
	}
	raw, err := codec.EncodeProperties(props)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := codec.DecodeProperties(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return decoded
}

func TestBuildProperties_Node(t *testing.T) {
	n := &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "ROOT_NODE_V2", Access: canonical.AccessRead,
		},
	}
	e := &entry{
		objID: 1, label: n.Identifier,
		access:   0x01,
		objType:  codec.ObjTypeNode,
		children: []uint32{2, 3, 4},
		node:     n,
	}
	got := buildAndDecode(t, e)
	want := map[uint8]bool{codec.PIDObjectType: true, codec.PIDLabel: true,
		codec.PIDAccess: true, codec.PIDChildren: true}
	for _, p := range got {
		delete(want, p.PID)
	}
	if len(want) > 0 {
		t.Errorf("missing pids: %v", want)
	}
	for _, p := range got {
		if p.PID == codec.PIDChildren {
			kids, err := codec.PropertyChildren(&p)
			if err != nil {
				t.Fatalf("children decode: %v", err)
			}
			if len(kids) != 3 || kids[0] != 2 || kids[2] != 4 {
				t.Errorf("children=%v want [2,3,4]", kids)
			}
		}
	}
}

func TestBuildProperties_NumberS32(t *testing.T) {
	unit := "dB"
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 5, Identifier: "Level", Access: canonical.AccessReadWrite,
		},
		Type: canonical.ParamInteger,
		Value: int64(-6), Minimum: int64(-60), Maximum: int64(12),
		Step: int64(1), Default: int64(0),
		Unit: &unit,
	}
	e := &entry{
		objID: 5, label: p.Identifier, access: 0x03,
		objType: codec.ObjTypeNumber, numType: codec.NumTypeS32,
		param: p,
	}
	got := buildAndDecode(t, e)
	seen := map[uint8]codec.Property{}
	for i := range got {
		seen[got[i].PID] = got[i]
	}
	for _, pid := range []uint8{codec.PIDObjectType, codec.PIDLabel, codec.PIDAccess,
		codec.PIDNumberType, codec.PIDValue, codec.PIDDefaultValue,
		codec.PIDMinValue, codec.PIDMaxValue, codec.PIDStepSize, codec.PIDUnit} {
		if _, ok := seen[pid]; !ok {
			t.Errorf("missing pid=%d", pid)
		}
	}
	// Verify value round-trip through DecodeNumericValue.
	vp := seen[codec.PIDValue]
	iv, _, _, err := codec.DecodeNumericValue(codec.NumTypeS32, vp.Data)
	if err != nil || iv != -6 {
		t.Errorf("value decode: got %d err=%v want -6", iv, err)
	}
	// Unit string.
	if s := codec.PropertyString(&[]codec.Property{seen[codec.PIDUnit]}[0]); s != "dB" {
		t.Errorf("unit=%q want dB", s)
	}
}

func TestBuildProperties_Enum(t *testing.T) {
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 6, Identifier: "Mute", Access: canonical.AccessReadWrite,
		},
		Type:  canonical.ParamEnum,
		Value: int64(1), Default: int64(0),
		EnumMap: []canonical.EnumEntry{{Key: "Off", Value: 0}, {Key: "On", Value: 1}},
	}
	e := &entry{
		objID: 6, label: p.Identifier, access: 0x03,
		objType: codec.ObjTypeEnum, numType: codec.NumTypeU32,
		param: p,
	}
	got := buildAndDecode(t, e)
	var optsProp, valProp codec.Property
	for _, pr := range got {
		switch pr.PID {
		case codec.PIDOptions:
			optsProp = pr
		case codec.PIDValue:
			valProp = pr
		}
	}
	opts := codec.PropertyOptions(&optsProp)
	if len(opts) != 2 || opts[0] != "Off" || opts[1] != "On" {
		t.Errorf("options=%v want [Off On]", opts)
	}
	_, uv, _, err := codec.DecodeNumericValue(codec.NumTypeU32, valProp.Data)
	if err != nil || uv != 1 {
		t.Errorf("enum value=%d want 1 err=%v", uv, err)
	}
}

func TestBuildProperties_String_WithMaxLen(t *testing.T) {
	mf := "maxLen=16"
	p := &canonical.Parameter{
		Header: canonical.Header{Number: 7, Identifier: "UserLabel",
			Access: canonical.AccessReadWrite},
		Type: canonical.ParamString, Value: "Input-A", Format: &mf,
	}
	e := &entry{
		objID: 7, label: p.Identifier, access: 0x03,
		objType: codec.ObjTypeString, numType: codec.NumTypeString,
		param: p,
	}
	got := buildAndDecode(t, e)
	var maxLen, val codec.Property
	for _, pr := range got {
		switch pr.PID {
		case codec.PIDStringMaxLength:
			maxLen = pr
		case codec.PIDValue:
			val = pr
		}
	}
	if maxLen.PID != codec.PIDStringMaxLength {
		t.Fatal("missing pid=6 string_max_length")
	}
	// pid 6 per spec §5.4: plen=6, body = u16 len + u16 pad.
	// After DecodeProperties, body is the 2-byte u16; pad is stripped.
	if len(maxLen.Data) < 2 || binary.BigEndian.Uint16(maxLen.Data[0:2]) != 16 {
		t.Errorf("maxLen data=%x want u16=16", maxLen.Data)
	}
	if s := codec.PropertyString(&val); s != "Input-A" {
		t.Errorf("string value=%q want Input-A", s)
	}
}

// TestBuildProperties_RequiredConstraints verifies pids 9/10/11
// (default_value, min_value, max_value) are always present on Number,
// Enum, and Preset replies per spec §"Property fields" matrix
// (Y on those rows). Encoder must fall back to type-derived defaults
// when canonical lacks Default/Min/Max.
func TestBuildProperties_RequiredConstraints(t *testing.T) {
	t.Run("Number_no_canonical_constraints_falls_back_to_type_defaults", func(t *testing.T) {
		// u8 number with no Default/Min/Max set on canonical.
		p := &canonical.Parameter{
			Header: canonical.Header{Number: 5, Identifier: "Foo",
				Access: canonical.AccessReadWrite},
			Type: canonical.ParamInteger, Value: int64(0),
		}
		e := &entry{
			objID: 5, label: p.Identifier, access: 0x03,
			objType: codec.ObjTypeNumber, numType: codec.NumTypeU8, param: p,
		}
		got := buildAndDecode(t, e)
		bm := pidMap(got)
		// pid 9 default = 0
		if _, _, _, err := codec.DecodeNumericValue(codec.NumTypeU8, bm[codec.PIDDefaultValue].Data); err != nil {
			t.Errorf("pid 9 default decode: %v", err)
		}
		// pid 10 min = u8 type-min = 0
		_, mn, _, err := codec.DecodeNumericValue(codec.NumTypeU8, bm[codec.PIDMinValue].Data)
		if err != nil || mn != 0 {
			t.Errorf("pid 10 min=%d want 0 (u8 type-min), err=%v", mn, err)
		}
		// pid 11 max = u8 type-max = 255
		_, mx, _, err := codec.DecodeNumericValue(codec.NumTypeU8, bm[codec.PIDMaxValue].Data)
		if err != nil || mx != 255 {
			t.Errorf("pid 11 max=%d want 255 (u8 type-max), err=%v", mx, err)
		}
	})

	t.Run("Number_s16_type_min_max", func(t *testing.T) {
		p := &canonical.Parameter{
			Header: canonical.Header{Number: 5, Identifier: "Bar",
				Access: canonical.AccessReadWrite},
			Type: canonical.ParamInteger, Value: int64(0),
		}
		e := &entry{
			objID: 5, label: p.Identifier, access: 0x03,
			objType: codec.ObjTypeNumber, numType: codec.NumTypeS16, param: p,
		}
		got := buildAndDecode(t, e)
		bm := pidMap(got)
		mn, _, _, _ := codec.DecodeNumericValue(codec.NumTypeS16, bm[codec.PIDMinValue].Data)
		if mn != -32768 {
			t.Errorf("s16 type-min=%d want -32768", mn)
		}
		mx, _, _, _ := codec.DecodeNumericValue(codec.NumTypeS16, bm[codec.PIDMaxValue].Data)
		if mx != 32767 {
			t.Errorf("s16 type-max=%d want 32767", mx)
		}
	})

	t.Run("Enum_uses_EnumMap_bounds", func(t *testing.T) {
		// EnumMap with non-contiguous indices; pid 10 = 5, pid 11 = 99.
		p := &canonical.Parameter{
			Header: canonical.Header{Number: 6, Identifier: "Mode",
				Access: canonical.AccessReadWrite},
			Type:    canonical.ParamEnum, Value: int64(5),
			EnumMap: []canonical.EnumEntry{{Key: "A", Value: 5}, {Key: "B", Value: 42}, {Key: "C", Value: 99}},
		}
		e := &entry{
			objID: 6, label: p.Identifier, access: 0x03,
			objType: codec.ObjTypeEnum, numType: codec.NumTypeU32, param: p,
		}
		got := buildAndDecode(t, e)
		bm := pidMap(got)
		// pid 9 default (no canonical Default → first EnumMap.Value = 5)
		_, def, _, _ := codec.DecodeNumericValue(codec.NumTypePreset, bm[codec.PIDDefaultValue].Data)
		if def != 5 {
			t.Errorf("enum default=%d want 5 (first EnumMap.Value)", def)
		}
		_, mn, _, _ := codec.DecodeNumericValue(codec.NumTypePreset, bm[codec.PIDMinValue].Data)
		if mn != 5 {
			t.Errorf("enum min=%d want 5", mn)
		}
		_, mx, _, _ := codec.DecodeNumericValue(codec.NumTypePreset, bm[codec.PIDMaxValue].Data)
		if mx != 99 {
			t.Errorf("enum max=%d want 99", mx)
		}
	})

	t.Run("Preset_emits_pid_9_10_11_per_depth", func(t *testing.T) {
		p := &canonical.Parameter{
			Header: canonical.Header{Number: 9, Identifier: "Slot",
				Access: canonical.AccessReadWrite},
			Type: canonical.ParamInteger, Value: int64(0),
		}
		e := &entry{
			objID: 9, label: p.Identifier, access: 0x03,
			objType: codec.ObjTypePreset, numType: codec.NumTypeU8,
			presetDepth: 3, param: p,
		}
		got := buildAndDecode(t, e)
		var n9, n10, n11 int
		for _, pr := range got {
			switch pr.PID {
			case codec.PIDDefaultValue:
				n9++
			case codec.PIDMinValue:
				n10++
			case codec.PIDMaxValue:
				n11++
			}
		}
		if n9 != 3 || n10 != 3 || n11 != 3 {
			t.Errorf("preset depth=3 expected pid 9/10/11 each 3 times; got 9=%d 10=%d 11=%d", n9, n10, n11)
		}
	})

	t.Run("Number_with_canonical_constraints_keeps_canonical_values", func(t *testing.T) {
		p := &canonical.Parameter{
			Header: canonical.Header{Number: 5, Identifier: "Vol",
				Access: canonical.AccessReadWrite},
			Type:    canonical.ParamInteger, Value: int64(-3),
			Default: int64(-5), Minimum: int64(-60), Maximum: int64(12),
		}
		e := &entry{
			objID: 5, label: p.Identifier, access: 0x03,
			objType: codec.ObjTypeNumber, numType: codec.NumTypeS32, param: p,
		}
		got := buildAndDecode(t, e)
		bm := pidMap(got)
		def, _, _, _ := codec.DecodeNumericValue(codec.NumTypeS32, bm[codec.PIDDefaultValue].Data)
		if def != -5 {
			t.Errorf("default=%d want -5 (canonical)", def)
		}
		mn, _, _, _ := codec.DecodeNumericValue(codec.NumTypeS32, bm[codec.PIDMinValue].Data)
		if mn != -60 {
			t.Errorf("min=%d want -60 (canonical)", mn)
		}
		mx, _, _, _ := codec.DecodeNumericValue(codec.NumTypeS32, bm[codec.PIDMaxValue].Data)
		if mx != 12 {
			t.Errorf("max=%d want 12 (canonical)", mx)
		}
	})
}

// pidMap collects properties keyed by pid for assertion ergonomics.
// Last write wins for repeated pids (callers needing repetition counts
// iterate the props slice directly).
func pidMap(props []codec.Property) map[uint8]codec.Property {
	out := make(map[uint8]codec.Property, len(props))
	for _, p := range props {
		out[p.PID] = p
	}
	return out
}

func TestBuildProperties_IPv4(t *testing.T) {
	ipf := "ipv4"
	p := &canonical.Parameter{
		Header: canonical.Header{Number: 8, Identifier: "Gateway",
			Access: canonical.AccessReadWrite},
		Type: canonical.ParamString, Value: "192.168.1.1", Format: &ipf,
	}
	e := &entry{
		objID: 8, label: p.Identifier, access: 0x03,
		objType: codec.ObjTypeIPv4, numType: codec.NumTypeIPv4,
		param: p,
	}
	got := buildAndDecode(t, e)
	var val codec.Property
	for _, pr := range got {
		if pr.PID == codec.PIDValue {
			val = pr
		}
	}
	if len(val.Data) != 4 {
		t.Fatalf("ipv4 value len=%d want 4", len(val.Data))
	}
	if val.Data[0] != 192 || val.Data[1] != 168 || val.Data[2] != 1 || val.Data[3] != 1 {
		t.Errorf("ipv4 bytes=%v want 192.168.1.1", val.Data)
	}
}
