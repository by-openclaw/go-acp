package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
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

func TestV50_SControl_RoundTrip(t *testing.T) {
	// SControl body is opaque: Encode appends SControlRaw verbatim, Decode
	// returns it in SControlRaw and fires the undefined-control note.
	raw := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	in := V50Packet{SControl: true, Screen: 9, SControlRaw: raw}
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if wire[V50FLAGSIdx]&(1<<1) == 0 {
		t.Errorf("SCONTROL flag bit not set: FLAGS=0x%02x", wire[V50FLAGSIdx])
	}
	got, err := DecodeV50(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.SControl {
		t.Errorf("SControl flag lost on decode")
	}
	if !bytes.Equal(got.SControlRaw, raw) {
		t.Errorf("SControlRaw=% x, want % x", got.SControlRaw, raw)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_control_data_undefined" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_control_data_undefined note, got %+v", got.Notes)
	}
}

func TestV50Encode_TextTooLong(t *testing.T) {
	// A single DMSG TEXT longer than 0xFFFF bytes hits the per-DMSG length
	// guard before the packet-size guard. 0x10000 printable ASCII bytes.
	long := strings.Repeat("A", 0x10000)
	p := V50Packet{DMSGs: []DMSG{{Index: 0, Text: long}}}
	_, err := p.Encode()
	if err == nil || !strings.Contains(err.Error(), "TEXT too long") {
		t.Errorf("want TEXT-too-long error, got %v", err)
	}
}

func TestV50Encode_PacketTooLarge(t *testing.T) {
	// Body just over the 2048-byte cap (after the 2-byte PBC) must reject.
	// A ~2050-byte ASCII label keeps each DMSG length under 0xFFFF so the
	// packet-size guard (not the TEXT guard) fires.
	p := V50Packet{DMSGs: []DMSG{{Index: 0, Text: strings.Repeat("A", 2050)}}}
	_, err := p.Encode()
	if !errors.Is(err, ErrV50PacketTooLarge) {
		t.Errorf("want ErrV50PacketTooLarge, got %v", err)
	}
}

func TestV50Decode_PacketTooLarge(t *testing.T) {
	// A buffer larger than the 2048 maximum is rejected before parsing.
	big := make([]byte, V50MaxPacketSize+1)
	if _, err := DecodeV50(big); !errors.Is(err, ErrV50PacketTooLarge) {
		t.Errorf("want ErrV50PacketTooLarge, got %v", err)
	}
}

func TestV50Decode_UnknownVersion_FiresNote(t *testing.T) {
	in := V50Packet{DMSGs: []DMSG{{Index: 1, Text: "X"}}}
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	wire[V50VERIdx] = 1 // non-zero minor version
	got, err := DecodeV50(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_version_mismatch" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_version_mismatch note, got %+v", got.Notes)
	}
}

func TestV50Decode_BroadcastIndex_FiresNote(t *testing.T) {
	in := V50Packet{Screen: 0, DMSGs: []DMSG{{Index: V50BroadcastIdx, Text: "X"}}}
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeV50(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_broadcast_received" && strings.Contains(n.Detail, "INDEX") {
			found = true
		}
	}
	if !found {
		t.Errorf("want broadcast INDEX note, got %+v", got.Notes)
	}
}

func TestV50Decode_ReservedControlBits_FiresNote(t *testing.T) {
	in := V50Packet{DMSGs: []DMSG{{Index: 1, ReservedBits: 0x05, Text: "X"}}}
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeV50(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range got.Notes {
		if n.Kind == "tsl_reserved_bit_set" && strings.Contains(n.Detail, "CONTROL") {
			found = true
		}
	}
	if !found {
		t.Errorf("want CONTROL reserved-bit note, got %+v", got.Notes)
	}
	if got.DMSGs[0].ReservedBits != 0x05 {
		t.Errorf("ReservedBits round-trip: got 0x%02x, want 0x05", got.DMSGs[0].ReservedBits)
	}
}

func TestV50Decode_DMSGHeaderTruncated(t *testing.T) {
	// Body has the 6-byte envelope plus 2 stray bytes — not enough for a
	// 4-byte DMSG header (INDEX+CONTROL needs 4).
	body := []byte{0x00 /*VER*/, 0x00 /*FLAGS*/, 0x00, 0x00 /*SCREEN*/, 0xAA, 0xBB}
	pkt := make([]byte, 2+len(body))
	binary.LittleEndian.PutUint16(pkt[V50PBCIdx:], uint16(len(body)))
	copy(pkt[2:], body)
	_, err := DecodeV50(pkt)
	if !errors.Is(err, ErrV50DMSGTruncated) {
		t.Errorf("want ErrV50DMSGTruncated (header), got %v", err)
	}
}

func TestV50Decode_DMSGLengthTruncated(t *testing.T) {
	// DMSG header present (INDEX+CONTROL, bit15=0) but no room for the
	// 2-byte LENGTH field.
	body := []byte{
		0x00, 0x00, 0x00, 0x00, // VER, FLAGS, SCREEN
		0x01, 0x00, // INDEX=1
		0x00, 0x00, // CONTROL=0 (bit15 clear → LENGTH expected)
	}
	pkt := make([]byte, 2+len(body))
	binary.LittleEndian.PutUint16(pkt[V50PBCIdx:], uint16(len(body)))
	copy(pkt[2:], body)
	_, err := DecodeV50(pkt)
	if !errors.Is(err, ErrV50DMSGTruncated) {
		t.Errorf("want ErrV50DMSGTruncated (LENGTH), got %v", err)
	}
}

func TestV50Decode_DMSGTextExceedsBody(t *testing.T) {
	// LENGTH claims more TEXT bytes than the body actually holds.
	body := []byte{
		0x00, 0x00, 0x00, 0x00, // VER, FLAGS, SCREEN
		0x01, 0x00, // INDEX=1
		0x00, 0x00, // CONTROL=0
		0x10, 0x00, // LENGTH=16
		'A', 'B', // only 2 TEXT bytes present
	}
	pkt := make([]byte, 2+len(body))
	binary.LittleEndian.PutUint16(pkt[V50PBCIdx:], uint16(len(body)))
	copy(pkt[2:], body)
	_, err := DecodeV50(pkt)
	if !errors.Is(err, ErrV50DMSGTruncated) {
		t.Errorf("want ErrV50DMSGTruncated (TEXT), got %v", err)
	}
}

func TestV50Decode_OddUTF16Length(t *testing.T) {
	// UTF-16LE flag set but TEXT length is odd → decodeV50Text error,
	// surfaced by DecodeV50.
	body := []byte{
		0x00,       // VER
		0x01,       // FLAGS: UTF16LE
		0x00, 0x00, // SCREEN
		0x01, 0x00, // INDEX=1
		0x00, 0x00, // CONTROL=0
		0x03, 0x00, // LENGTH=3 (odd for UTF-16LE)
		0x41, 0x00, 0x42, // 3 bytes
	}
	pkt := make([]byte, 2+len(body))
	binary.LittleEndian.PutUint16(pkt[V50PBCIdx:], uint16(len(body)))
	copy(pkt[2:], body)
	_, err := DecodeV50(pkt)
	if !errors.Is(err, ErrV50TextDecode) {
		t.Errorf("want ErrV50TextDecode, got %v", err)
	}
}

func TestDecodeV50Text_OddLengthDirect(t *testing.T) {
	// Direct unit on the helper's odd-length guard.
	if _, err := decodeV50Text([]byte{0x41}, true); !errors.Is(err, ErrV50TextDecode) {
		t.Errorf("want ErrV50TextDecode for odd UTF-16LE input, got %v", err)
	}
}

func TestV50Encode_NonASCIIRejected(t *testing.T) {
	p := V50Packet{DMSGs: []DMSG{{Index: 0, Text: "héllo"}}}
	_, err := p.Encode()
	if err == nil {
		t.Errorf("expected error on non-ASCII when UTF16LE flag not set")
	}
}
