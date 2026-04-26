package codec

import (
	"errors"
	"strings"
	"testing"
)

func TestV31Encode(t *testing.T) {
	cases := []struct {
		name string
		in   V31Frame
		want []byte // 18 bytes
	}{
		{
			name: "all zeros — addr 0, no tallies, brightness off, 16 spaces",
			in:   V31Frame{},
			want: []byte{
				0x80, 0x00,
				0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
				0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			},
		},
		{
			name: "addr 1, all tallies, full brightness, 'CAM 1' padded",
			in: V31Frame{
				Address:    1,
				Tally1:     true, Tally2: true, Tally3: true, Tally4: true,
				Brightness: BrightnessFull,
				Text:       "CAM 1",
			},
			// HEADER = 0x80|0x01 = 0x81; CTRL = 0x0F|0x30 = 0x3F; text + space-pad
			want: []byte{
				0x81, 0x3F,
				'C', 'A', 'M', ' ', '1', 0x20, 0x20, 0x20,
				0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			},
		},
		{
			name: "addr 126 (max), tally1 only, brightness 1/2, text fills 16",
			in: V31Frame{
				Address:    126,
				Tally1:     true,
				Brightness: BrightnessHalf,
				Text:       "0123456789ABCDEF",
			},
			// HEADER = 0x80|0x7E = 0xFE; CTRL = 0x01|0x20 = 0x21
			want: []byte{
				0xFE, 0x21,
				'0', '1', '2', '3', '4', '5', '6', '7',
				'8', '9', 'A', 'B', 'C', 'D', 'E', 'F',
			},
		},
		{
			name: "brightness 1/7 only (CTRL bits 4+5 = 01)",
			in: V31Frame{
				Address:    42,
				Brightness: BrightnessOneSeveth,
			},
			// HEADER 0x80|42 = 0xAA; CTRL = 0x10
			want: append([]byte{0xAA, 0x10},
				[]byte(strings.Repeat(" ", 16))...),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.in.Encode()
			if err != nil {
				t.Fatalf("Encode error: %v", err)
			}
			if len(got) != V31FrameSize {
				t.Fatalf("len=%d, want %d", len(got), V31FrameSize)
			}
			for i, b := range got {
				if b != c.want[i] {
					t.Errorf("byte[%d] = 0x%02x, want 0x%02x (full got=% x)", i, b, c.want[i], got)
				}
			}
		})
	}
}

func TestV31EncodeErrors(t *testing.T) {
	if _, err := (V31Frame{Address: 127}).Encode(); !errors.Is(err, ErrV31AddressRng) {
		t.Errorf("Address=127 should hit ErrV31AddressRng, got %v", err)
	}
	if _, err := (V31Frame{Text: "hello\x00world"}).Encode(); !errors.Is(err, ErrV31NonPrintTx) {
		t.Errorf("non-printable byte should hit ErrV31NonPrintTx, got %v", err)
	}
	if _, err := (V31Frame{Text: string([]byte{0x1F})}).Encode(); !errors.Is(err, ErrV31NonPrintTx) {
		t.Errorf("byte 0x1F should hit ErrV31NonPrintTx, got %v", err)
	}
}

func TestV31Decode_RoundTrip(t *testing.T) {
	in := V31Frame{
		Address:    5,
		Tally1:     true,
		Tally3:     true,
		Brightness: BrightnessFull,
		Text:       "PGM",
	}
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeV31(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Address != 5 || !got.Tally1 || got.Tally2 || !got.Tally3 || got.Tally4 {
		t.Errorf("tally round-trip wrong: %+v", got)
	}
	if got.Brightness != BrightnessFull {
		t.Errorf("brightness=%v, want full", got.Brightness)
	}
	if got.Text != "PGM             " {
		t.Errorf("Text=%q, want space-padded 16 chars", got.Text)
	}
	if len(got.Notes) != 0 {
		t.Errorf("clean frame should have no notes, got %+v", got.Notes)
	}
}

func TestV31Decode_BadSize(t *testing.T) {
	_, err := DecodeV31(make([]byte, 17))
	if !errors.Is(err, ErrV31FrameSize) {
		t.Errorf("size=17 should hit ErrV31FrameSize, got %v", err)
	}
}

func TestV31Decode_HeaderBit7Clear(t *testing.T) {
	bad := make([]byte, V31FrameSize)
	bad[0] = 0x7F // bit 7 clear
	_, err := DecodeV31(bad)
	if !errors.Is(err, ErrV31HeaderMSB) {
		t.Errorf("header MSB clear should hit ErrV31HeaderMSB, got %v", err)
	}
}

func TestV31Decode_ReservedBit6(t *testing.T) {
	frame := make([]byte, V31FrameSize)
	frame[0] = 0x80
	frame[1] = 0x40 // CTRL bit 6 set
	for i := 2; i < V31FrameSize; i++ {
		frame[i] = 0x20
	}
	f, err := DecodeV31(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(f.Notes) == 0 {
		t.Fatalf("want reserved-bit note, got no notes")
	}
	found := false
	for _, n := range f.Notes {
		if n.Kind == "tsl_reserved_bit_set" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing tsl_reserved_bit_set in %+v", f.Notes)
	}
}

func TestV31Decode_NullPadNote(t *testing.T) {
	frame := make([]byte, V31FrameSize)
	frame[0] = 0x80
	frame[1] = 0x00
	copy(frame[2:], []byte("CAM 1"))
	// indices 7..17 remain 0x00 — the Decimator/TallyArbiter deviation
	f, err := DecodeV31(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, n := range f.Notes {
		if n.Kind == "tsl_v31_null_pad" {
			found = true
		}
	}
	if !found {
		t.Errorf("want tsl_v31_null_pad note, got %+v", f.Notes)
	}
}

func TestV31Decode_AllAddressesRoundTrip(t *testing.T) {
	for addr := uint8(0); addr <= V31AddressMax; addr++ {
		frame := V31Frame{Address: addr, Text: "X"}
		wire, err := frame.Encode()
		if err != nil {
			t.Fatalf("addr=%d encode: %v", addr, err)
		}
		if wire[0] != (addr|V31HeaderMSB) {
			t.Fatalf("addr=%d: wire[0]=0x%02x want 0x%02x", addr, wire[0], addr|V31HeaderMSB)
		}
		got, err := DecodeV31(wire)
		if err != nil {
			t.Fatalf("addr=%d decode: %v", addr, err)
		}
		if got.Address != addr {
			t.Errorf("addr round-trip %d → %d", addr, got.Address)
		}
	}
}
