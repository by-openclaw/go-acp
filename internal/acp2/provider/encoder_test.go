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

// TestBuildProperties_Enum_NonPositionalIdx verifies pid=15 (options)
// uses the EnumMap.Value as the wire idx, not positional 0..N-1.
// Real Axon firmware assigns sparse idx (e.g. 7="Off", 8="On"); pid=8
// (value) and pid=9 (default) reference those wire idx values, so the
// option records MUST carry them or Cerebrum cannot resolve the active
// label. Spec §5.4 row 15 + observed real-Neuron wire trace
// (raw.an2.jsonl 2026-05-06): `00000007 "Off"... 00000008 "On"...`.
func TestBuildProperties_Enum_NonPositionalIdx(t *testing.T) {
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 6, Identifier: "Mute", Access: canonical.AccessReadWrite,
		},
		Type:  canonical.ParamEnum,
		Value: int64(8), Default: int64(7),
		EnumMap: []canonical.EnumEntry{
			{Key: "Off", Value: 7},
			{Key: "On", Value: 8},
		},
	}
	e := &entry{
		objID: 6, label: p.Identifier, access: 0x03,
		objType: codec.ObjTypeEnum, numType: codec.NumTypeU32,
		param: p,
	}
	got := buildAndDecode(t, e)
	var optsProp codec.Property
	for _, pr := range got {
		if pr.PID == codec.PIDOptions {
			optsProp = pr
		}
	}
	if optsProp.PID != codec.PIDOptions {
		t.Fatal("missing pid=15 options")
	}
	// Strict spec §5.4: plen = 4 + 72*N. After DecodeProperties strips
	// the 4-byte header, body is 72*N. For N=2 expect 144.
	if got, want := len(optsProp.Data), 2*codec.ACP2OptionSize; got != want {
		t.Errorf("options body len=%d want %d", got, want)
	}
	m := codec.PropertyOptionsMap(&optsProp)
	if m[7] != "Off" {
		t.Errorf("idx=7 -> %q want Off", m[7])
	}
	if m[8] != "On" {
		t.Errorf("idx=8 -> %q want On", m[8])
	}
	// Sanity: positional encoding would have produced idx=0,1.
	if _, ok := m[0]; ok {
		t.Errorf("positional idx=0 leaked into wire — encoder regressed to positional")
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
