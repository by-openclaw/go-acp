// Black-box spec-compliance tests for ACP1 property decoders.
// Byte sequences from spec v1.4 pages 21-27.
package acp1_test

import (
	"testing"

	"acp/internal/protocol/acp1"
)

func TestSpec_DecodeRoot(t *testing.T) {
	in := []byte{0x00, 0x09, 0x01, 0x00, 0x08, 0x10, 0x04, 0x02, 0x01}
	o, err := acp1.DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.Type != acp1.TypeRoot || o.NumProps != 9 {
		t.Errorf("type/numprops: %d/%d", o.Type, o.NumProps)
	}
	if o.NumIdentity != 8 || o.NumControl != 16 {
		t.Errorf("counts: id=%d ctrl=%d", o.NumIdentity, o.NumControl)
	}
}

func TestSpec_DecodeFloat(t *testing.T) {
	in := []byte{
		0x03, 0x0A, 0x03,
		0xC0, 0xC0, 0x00, 0x00, // -6.0
		0x00, 0x00, 0x00, 0x00,
		0x3F, 0x80, 0x00, 0x00, // 1.0
		0xC2, 0xA0, 0x00, 0x00, // -80.0
		0x41, 0xA0, 0x00, 0x00, // 20.0
		'G', 'a', 'i', 'n', 0x00,
		'd', 'B', 0x00,
	}
	o, err := acp1.DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.FloatVal != -6.0 || o.Label != "Gain" || o.Unit != "dB" {
		t.Errorf("float: val=%v label=%q unit=%q", o.FloatVal, o.Label, o.Unit)
	}
}

func TestSpec_DecodeEnum(t *testing.T) {
	in := []byte{
		0x04, 0x08, 0x03, 0x01, 0x03, 0x00,
		'M', 'o', 'd', 'e', 0x00,
		'O', 'f', 'f', ',', 'O', 'n', ',', 'A', 'u', 't', 'o', 0x00,
	}
	o, err := acp1.DecodeObject(in)
	if err != nil {
		t.Fatalf("DecodeObject: %v", err)
	}
	if o.ByteVal != 1 || o.NumItems != 3 || o.Label != "Mode" {
		t.Errorf("enum: val=%d n=%d label=%q", o.ByteVal, o.NumItems, o.Label)
	}
	want := []string{"Off", "On", "Auto"}
	for i, w := range want {
		if i >= len(o.EnumItems) || o.EnumItems[i] != w {
			t.Errorf("items[%d]: got %q want %q", i, o.EnumItems[i], w)
		}
	}
}
