package acp2

import (
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// Tests in this file pin the producer's GetObject reply property set
// against bytes captured from real Axon Neuron firmware (10.41.40.4)
// on 2026-05-09. Capture file: bin/neuron-fresh/out-real-walk-slot0.jsonl.
// Each test asserts which pids appear in our wire reply for one
// representative obj per object type, plus the wire shape of pid 4
// event_delay and pid 9 default_value (the spec deltas this PR fixes).
//
// Spec authority: ACP2 §5.1 Property fields per object type
// (acp2_protocol.docx). Reading: cell shading ✓ = required, op¹ =
// optional, blank — = does not apply.

func pidSet(props []codec.Property) map[uint8]bool {
	out := map[uint8]bool{}
	for _, p := range props {
		out[p.PID] = true
	}
	return out
}

func wantPids(t *testing.T, props []codec.Property, required, forbidden []uint8) {
	t.Helper()
	got := pidSet(props)
	for _, pid := range required {
		if !got[pid] {
			t.Errorf("missing required pid %d", pid)
		}
	}
	for _, pid := range forbidden {
		if got[pid] {
			t.Errorf("forbidden pid %d emitted", pid)
		}
	}
}

// TestEncoder_Node_RealNeuronShape — real Neuron obj 15364
// MANAGEMENT PORT: {pid 1 obj_type, pid 2 label, pid 14 children}.
// No pid 3, no pid 4. Verified bytes: out-real-walk-slot0.jsonl.
func TestEncoder_Node_RealNeuronShape(t *testing.T) {
	e := &entry{
		objID: 15364, label: "MANAGEMENT PORT",
		access:   0x01,
		objType:  codec.ObjTypeNode,
		children: []uint32{19378, 15252, 15246},
	}
	got := buildAndDecode(t, e)
	wantPids(t, got,
		[]uint8{codec.PIDObjectType, codec.PIDLabel, codec.PIDChildren},
		[]uint8{codec.PIDAccess, codec.PIDAnnounceDelay,
			codec.PIDNumberType, codec.PIDValue, codec.PIDDefaultValue,
			codec.PIDStringMaxLength, codec.PIDOptions},
	)
}

// TestEncoder_Number_RealNeuronShape — real Neuron obj 15211 Fan Speed
// (u8): {1, 2, 3, 4, 5, 8, 9, 10, 11, 12, 13}. dlen=96.
func TestEncoder_Number_RealNeuronShape(t *testing.T) {
	unit := "%"
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 15211, Identifier: "Fan Speed", Access: canonical.AccessRead,
		},
		Type:  canonical.ParamInteger,
		Value: int64(10), Default: int64(0),
		Minimum: int64(0), Maximum: int64(100),
		Step: int64(1), Unit: &unit,
	}
	e := &entry{
		objID: 15211, label: p.Identifier, access: 0x01,
		objType: codec.ObjTypeNumber, numType: codec.NumTypeU8,
		param: p,
	}
	got := buildAndDecode(t, e)
	wantPids(t, got,
		[]uint8{codec.PIDObjectType, codec.PIDLabel, codec.PIDAccess,
			codec.PIDAnnounceDelay, codec.PIDNumberType, codec.PIDValue,
			codec.PIDDefaultValue, codec.PIDMinValue, codec.PIDMaxValue,
			codec.PIDStepSize, codec.PIDUnit},
		[]uint8{codec.PIDChildren, codec.PIDStringMaxLength, codec.PIDOptions},
	)
}

// TestEncoder_Number_RequiredPidsAlwaysEmittedZero — pid 9/10/11
// must be present even when canonical Default/Min/Max are nil. Real
// Neuron emits all three on every Number reply (§5.1 cells shaded).
func TestEncoder_Number_RequiredPidsAlwaysEmittedZero(t *testing.T) {
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 99, Identifier: "Bare Counter", Access: canonical.AccessRead,
		},
		Type:  canonical.ParamInteger,
		Value: int64(0),
		// Default/Min/Max all nil
	}
	e := &entry{
		objID: 99, label: p.Identifier, access: 0x01,
		objType: codec.ObjTypeNumber, numType: codec.NumTypeU32,
		param: p,
	}
	got := buildAndDecode(t, e)
	wantPids(t, got,
		[]uint8{codec.PIDDefaultValue, codec.PIDMinValue, codec.PIDMaxValue},
		nil,
	)
}

// TestEncoder_Enum_RealNeuronShape — real Neuron obj 21127 Fan Control
// (RW): {1, 2, 3, 4, 8, 9, 15}. pid 9 carries vtype=preset/enum (9)
// and the same u32 idx as pid 8 (or canonical Default if separate).
func TestEncoder_Enum_RealNeuronShape(t *testing.T) {
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 21127, Identifier: "Fan Control", Access: canonical.AccessReadWrite,
		},
		Type:  canonical.ParamEnum,
		Value: int64(786),
		EnumMap: []canonical.EnumEntry{
			{Key: "Automatic", Value: 786},
			{Key: "Full Speed", Value: 791},
		},
	}
	e := &entry{
		objID: 21127, label: p.Identifier, access: 0x03,
		objType: codec.ObjTypeEnum, numType: codec.NumTypeU32,
		param: p,
	}
	got := buildAndDecode(t, e)
	wantPids(t, got,
		[]uint8{codec.PIDObjectType, codec.PIDLabel, codec.PIDAccess,
			codec.PIDAnnounceDelay, codec.PIDValue, codec.PIDDefaultValue,
			codec.PIDOptions},
		[]uint8{codec.PIDNumberType, codec.PIDChildren, codec.PIDStringMaxLength,
			codec.PIDMinValue, codec.PIDMaxValue},
	)
	// pid 9 default value == current value (786) when canonical lacks
	// separate Default — matches real Neuron obj 21127.
	for _, pr := range got {
		if pr.PID == codec.PIDDefaultValue {
			if pr.VType != uint8(codec.NumTypePreset) {
				t.Errorf("pid 9 vtype=%d want %d (preset/enum)", pr.VType, codec.NumTypePreset)
			}
			if len(pr.Data) != 4 {
				t.Errorf("pid 9 body len=%d want 4", len(pr.Data))
			}
			val := uint32(pr.Data[0])<<24 | uint32(pr.Data[1])<<16 |
				uint32(pr.Data[2])<<8 | uint32(pr.Data[3])
			if val != 786 {
				t.Errorf("pid 9 default=%d want 786", val)
			}
		}
	}
}

// TestEncoder_IPv4_RealNeuronShape — real Neuron obj 15828 Neighbor IP
// {1, 2, 3, 4, 8, 9}. pid 9 vtype=ipv4(10), 4-byte body, default 0.0.0.0.
func TestEncoder_IPv4_RealNeuronShape(t *testing.T) {
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 15828, Identifier: "Neighbor IP", Access: canonical.AccessRead,
		},
		Type:   canonical.ParamString, // canonical stores IP as string
		Value:  "10.41.40.5",
		Format: strPtr("ipv4"),
	}
	e := &entry{
		objID: 15828, label: p.Identifier, access: 0x01,
		objType: codec.ObjTypeIPv4, numType: codec.NumTypeIPv4,
		param: p,
	}
	got := buildAndDecode(t, e)
	wantPids(t, got,
		[]uint8{codec.PIDObjectType, codec.PIDLabel, codec.PIDAccess,
			codec.PIDAnnounceDelay, codec.PIDValue, codec.PIDDefaultValue},
		[]uint8{codec.PIDNumberType, codec.PIDChildren, codec.PIDStringMaxLength,
			codec.PIDOptions, codec.PIDMinValue, codec.PIDMaxValue},
	)
}

// TestEncoder_String_RealNeuronShape — real Neuron obj 2 Card Name
// {1, 2, 3, 4, 6, 8, 9}. pid 6 always emitted (max length); pid 9
// vtype=string(11).
func TestEncoder_String_RealNeuronShape(t *testing.T) {
	hint := "maxLen=17"
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 2, Identifier: "Card Name", Access: canonical.AccessRead,
		},
		Type:   canonical.ParamString,
		Value:  "SHPRM1",
		Format: &hint,
	}
	e := &entry{
		objID: 2, label: p.Identifier, access: 0x01,
		objType: codec.ObjTypeString, numType: codec.NumTypeString,
		param: p,
	}
	got := buildAndDecode(t, e)
	wantPids(t, got,
		[]uint8{codec.PIDObjectType, codec.PIDLabel, codec.PIDAccess,
			codec.PIDAnnounceDelay, codec.PIDStringMaxLength,
			codec.PIDValue, codec.PIDDefaultValue},
		[]uint8{codec.PIDNumberType, codec.PIDChildren, codec.PIDOptions,
			codec.PIDMinValue, codec.PIDMaxValue},
	)
}

// TestEncoder_String_DefaultEmptyShape — when canonical Default is
// absent, pid 9 emits a single NUL byte (empty string), plen=5.
// Verified against real Neuron obj 2 Card Name where default is "".
func TestEncoder_String_DefaultEmptyShape(t *testing.T) {
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number: 2, Identifier: "X", Access: canonical.AccessRead,
		},
		Type:  canonical.ParamString,
		Value: "any",
	}
	e := &entry{
		objID: 2, label: p.Identifier, access: 0x01,
		objType: codec.ObjTypeString, numType: codec.NumTypeString,
		param: p,
	}
	props, err := buildProperties(e)
	if err != nil {
		t.Fatalf("buildProperties: %v", err)
	}
	for _, pr := range props {
		if pr.PID == codec.PIDDefaultValue {
			// Spec §5.4: pid=9 with vtype=string(11), empty body = "\0", plen=5.
			if pr.VType != uint8(codec.NumTypeString) {
				t.Errorf("pid 9 vtype=%d want %d (string)", pr.VType, codec.NumTypeString)
			}
			if pr.PLen != 5 {
				t.Errorf("pid 9 plen=%d want 5", pr.PLen)
			}
			if len(pr.Data) != 1 || pr.Data[0] != 0 {
				t.Errorf("pid 9 data=%x want 00 (empty NUL-terminated)", pr.Data)
			}
			return
		}
	}
	t.Error("pid 9 default_value not emitted on String reply")
}

// TestEncoder_EventDelay_WireShape — pid 4 must be `04 00 00 08 +
// u32 BE rate`. Verified against real Neuron obj 17671 (rate=0).
func TestEncoder_EventDelay_WireShape(t *testing.T) {
	p := &canonical.Parameter{
		Header: canonical.Header{Number: 1, Identifier: "x", Access: canonical.AccessRead},
		Type:   canonical.ParamEnum,
		Value:  int64(0),
		EnumMap: []canonical.EnumEntry{
			{Key: "Off", Value: 0},
		},
	}
	e := &entry{
		objID: 1, label: "x", access: 0x01,
		objType: codec.ObjTypeEnum, numType: codec.NumTypeU32,
		param: p,
	}
	props, err := buildProperties(e)
	if err != nil {
		t.Fatalf("buildProperties: %v", err)
	}
	for _, pr := range props {
		if pr.PID == codec.PIDAnnounceDelay {
			if pr.VType != 0 {
				t.Errorf("pid 4 data byte=%d want 0 per spec §5.4", pr.VType)
			}
			if pr.PLen != 8 {
				t.Errorf("pid 4 plen=%d want 8 per spec §5.4", pr.PLen)
			}
			if len(pr.Data) != 4 {
				t.Errorf("pid 4 body len=%d want 4 (u32 rate)", len(pr.Data))
			}
			return
		}
	}
	t.Error("pid 4 event_delay not emitted on Enum reply")
}

func strPtr(s string) *string { return &s }
