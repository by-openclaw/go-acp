package acp2

import (
	"encoding/binary"
	"testing"

	"acp/internal/export/canonical"
	iacp2 "acp/internal/protocol/acp2"
)

// helper — round-trips a tree through buildProperties + EncodeProperties
// + DecodeProperties so we assert wire-level correctness rather than
// just struct equality.
func buildAndDecode(t *testing.T, e *entry) []iacp2.Property {
	t.Helper()
	props, err := buildProperties(e)
	if err != nil {
		t.Fatalf("buildProperties: %v", err)
	}
	raw, err := iacp2.EncodeProperties(props)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := iacp2.DecodeProperties(raw)
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
		objType:  iacp2.ObjTypeNode,
		children: []uint32{2, 3, 4},
		node:     n,
	}
	got := buildAndDecode(t, e)
	want := map[uint8]bool{iacp2.PIDObjectType: true, iacp2.PIDLabel: true,
		iacp2.PIDAccess: true, iacp2.PIDChildren: true}
	for _, p := range got {
		delete(want, p.PID)
	}
	if len(want) > 0 {
		t.Errorf("missing pids: %v", want)
	}
	for _, p := range got {
		if p.PID == iacp2.PIDChildren {
			kids, err := iacp2.PropertyChildren(&p)
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
		objType: iacp2.ObjTypeNumber, numType: iacp2.NumTypeS32,
		param: p,
	}
	got := buildAndDecode(t, e)
	seen := map[uint8]iacp2.Property{}
	for i := range got {
		seen[got[i].PID] = got[i]
	}
	for _, pid := range []uint8{iacp2.PIDObjectType, iacp2.PIDLabel, iacp2.PIDAccess,
		iacp2.PIDNumberType, iacp2.PIDValue, iacp2.PIDDefaultValue,
		iacp2.PIDMinValue, iacp2.PIDMaxValue, iacp2.PIDStepSize, iacp2.PIDUnit} {
		if _, ok := seen[pid]; !ok {
			t.Errorf("missing pid=%d", pid)
		}
	}
	// Verify value round-trip through DecodeNumericValue.
	vp := seen[iacp2.PIDValue]
	iv, _, _, err := iacp2.DecodeNumericValue(iacp2.NumTypeS32, vp.Data)
	if err != nil || iv != -6 {
		t.Errorf("value decode: got %d err=%v want -6", iv, err)
	}
	// Unit string.
	if s := iacp2.PropertyString(&[]iacp2.Property{seen[iacp2.PIDUnit]}[0]); s != "dB" {
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
		objType: iacp2.ObjTypeEnum, numType: iacp2.NumTypeU32,
		param: p,
	}
	got := buildAndDecode(t, e)
	var optsProp, valProp iacp2.Property
	for _, pr := range got {
		switch pr.PID {
		case iacp2.PIDOptions:
			optsProp = pr
		case iacp2.PIDValue:
			valProp = pr
		}
	}
	opts := iacp2.PropertyOptions(&optsProp)
	if len(opts) != 2 || opts[0] != "Off" || opts[1] != "On" {
		t.Errorf("options=%v want [Off On]", opts)
	}
	_, uv, _, err := iacp2.DecodeNumericValue(iacp2.NumTypeU32, valProp.Data)
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
		objType: iacp2.ObjTypeString, numType: iacp2.NumTypeString,
		param: p,
	}
	got := buildAndDecode(t, e)
	var maxLen, val iacp2.Property
	for _, pr := range got {
		switch pr.PID {
		case iacp2.PIDStringMaxLength:
			maxLen = pr
		case iacp2.PIDValue:
			val = pr
		}
	}
	if maxLen.PID != iacp2.PIDStringMaxLength {
		t.Fatal("missing pid=6 string_max_length")
	}
	// pid 6 per spec §5.4: plen=6, body = u16 len + u16 pad.
	// After DecodeProperties, body is the 2-byte u16; pad is stripped.
	if len(maxLen.Data) < 2 || binary.BigEndian.Uint16(maxLen.Data[0:2]) != 16 {
		t.Errorf("maxLen data=%x want u16=16", maxLen.Data)
	}
	if s := iacp2.PropertyString(&val); s != "Input-A" {
		t.Errorf("string value=%q want Input-A", s)
	}
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
		objType: iacp2.ObjTypeIPv4, numType: iacp2.NumTypeIPv4,
		param: p,
	}
	got := buildAndDecode(t, e)
	var val iacp2.Property
	for _, pr := range got {
		if pr.PID == iacp2.PIDValue {
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
