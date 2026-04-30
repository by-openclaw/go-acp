package codec

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestV50Encode_SingleDMSG_ASCII(t *testing.T) {
	p := V50Packet{
		Screen: 1,
		DMSGs: []DMSG{
			{
				Index:      7,
				RH:         TallyRed,
				TextTally:  TallyGreen,
				LH:         TallyAmber,
				Brightness: BrightnessFull,
				Text:       "CAM 1",
			},
		},
	}

	wire, err := p.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// PBC = len(body)
	pbc := binary.LittleEndian.Uint16(wire[V50PBCIdx:])
	if int(pbc)+2 != len(wire) {
		t.Errorf("PBC=%d, total=%d — mismatch", pbc, len(wire))
	}
	if wire[V50VERIdx] != 0 {
		t.Errorf("VER=%d, want 0", wire[V50VERIdx])
	}
	if wire[V50FLAGSIdx] != 0 {
		t.Errorf("FLAGS=0x%02x, want 0 (ASCII + DMSG)", wire[V50FLAGSIdx])
	}
	if binary.LittleEndian.Uint16(wire[V50SCREENIdx:]) != 1 {
		t.Errorf("SCREEN mismatch")
	}

	// DMSG block starts at offset 6.
	idx := binary.LittleEndian.Uint16(wire[V50HeaderSize+0:])
	if idx != 7 {
		t.Errorf("INDEX=%d, want 7", idx)
	}
	ctrl := binary.LittleEndian.Uint16(wire[V50HeaderSize+2:])
	// CONTROL: RH=1, Text=2, LH=3, Brightness=3
	wantCtrl := uint16(0x1) | (0x2 << 2) | (0x3 << 4) | (0x3 << 6)
	if ctrl != wantCtrl {
		t.Errorf("CONTROL=0x%04x, want 0x%04x", ctrl, wantCtrl)
	}
	length := binary.LittleEndian.Uint16(wire[V50HeaderSize+4:])
	if length != 5 {
		t.Errorf("LENGTH=%d, want 5", length)
	}
	got := string(wire[V50HeaderSize+6 : V50HeaderSize+6+5])
	if got != "CAM 1" {
		t.Errorf("TEXT=%q, want CAM 1", got)
	}
}

func TestV50RoundTrip_ASCII(t *testing.T) {
	in := V50Packet{
		Screen: 42,
		DMSGs: []DMSG{
			{Index: 1, LH: TallyGreen, TextTally: TallyRed, RH: TallyOff, Brightness: BrightnessHalf, Text: "CAM A"},
			{Index: 2, LH: TallyOff, TextTally: TallyAmber, RH: TallyRed, Brightness: BrightnessFull, Text: "CAM B"},
		},
	}
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeV50(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Screen != 42 || got.UTF16LE || got.SControl {
		t.Errorf("envelope: %+v", got)
	}
	if len(got.DMSGs) != 2 {
		t.Fatalf("got %d DMSGs, want 2", len(got.DMSGs))
	}
	for i, d := range got.DMSGs {
		if d.Index != in.DMSGs[i].Index || d.LH != in.DMSGs[i].LH || d.TextTally != in.DMSGs[i].TextTally || d.Text != in.DMSGs[i].Text {
			t.Errorf("DMSG[%d] mismatch: got %+v, want %+v", i, d, in.DMSGs[i])
		}
	}
}

func TestV50RoundTrip_UTF16LE(t *testing.T) {
	in := V50Packet{
		UTF16LE: true,
		Screen:  0,
		DMSGs: []DMSG{
			{Index: 0, LH: TallyRed, TextTally: TallyRed, RH: TallyRed, Brightness: BrightnessFull, Text: "CAMÉRA 1"},
			{Index: 1, LH: TallyOff, TextTally: TallyOff, RH: TallyOff, Brightness: BrightnessOff, Text: "日本語"},
		},
	}
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeV50(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.UTF16LE {
		t.Errorf("UTF16LE flag not set on rx")
	}
	if got.DMSGs[0].Text != "CAMÉRA 1" {
		t.Errorf("Latin-1 char UTF-16 round-trip failed: %q", got.DMSGs[0].Text)
	}
	if got.DMSGs[1].Text != "日本語" {
		t.Errorf("Japanese UTF-16 round-trip failed: %q", got.DMSGs[1].Text)
	}
	// charset_transcode note should fire once per DMSG.
	transcode := 0
	for _, n := range got.Notes {
		if n.Kind == "tsl_charset_transcode" {
			transcode++
		}
	}
	if transcode != 2 {
		t.Errorf("charset_transcode notes = %d, want 2", transcode)
	}
}

func TestV50Decode_BroadcastScreen_FiresNote(t *testing.T) {
	in := V50Packet{
		Screen: V50BroadcastIdx,
		DMSGs:  []DMSG{{Index: 0, Text: "X"}},
	}
	wire, _ := in.Encode()
	got, err := DecodeV50(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_broadcast_received" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_broadcast_received note, got %+v", got.Notes)
	}
}

func TestV50Decode_ReservedFlags_FiresNote(t *testing.T) {
	in := V50Packet{DMSGs: []DMSG{{Index: 1, Text: "X"}}}
	wire, _ := in.Encode()
	// Force FLAGS bit 4 set.
	wire[V50FLAGSIdx] |= 1 << 4
	got, err := DecodeV50(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_reserved_bit_set" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_reserved_bit_set, got %+v", got.Notes)
	}
}

func TestV50Decode_ControlDataFlag_FiresNote(t *testing.T) {
	in := V50Packet{
		DMSGs: []DMSG{{Index: 1, ControlData: true, ControlDataBytes: []byte{0xAA, 0xBB}}},
	}
	wire, _ := in.Encode()
	got, err := DecodeV50(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_control_data_undefined" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_control_data_undefined, got %+v", got.Notes)
	}
}

func TestV50Decode_PBCMismatch(t *testing.T) {
	in := V50Packet{DMSGs: []DMSG{{Index: 1, Text: "Y"}}}
	wire, _ := in.Encode()
	// Corrupt the PBC to a too-small value.
	binary.LittleEndian.PutUint16(wire[V50PBCIdx:], 2)
	_, err := DecodeV50(wire)
	if !errors.Is(err, ErrV50PBCMismatch) {
		t.Errorf("want ErrV50PBCMismatch, got %v", err)
	}
}

func TestV50Decode_PacketTooSmall(t *testing.T) {
	_, err := DecodeV50(make([]byte, 4))
	if !errors.Is(err, ErrV50PacketTooSmall) {
		t.Errorf("want ErrV50PacketTooSmall, got %v", err)
	}
}

func TestV50Encode_NonASCIIRejected(t *testing.T) {
	p := V50Packet{DMSGs: []DMSG{{Index: 0, Text: "héllo"}}}
	_, err := p.Encode()
	if err == nil {
		t.Errorf("expected error on non-ASCII when UTF16LE flag not set")
	}
}
