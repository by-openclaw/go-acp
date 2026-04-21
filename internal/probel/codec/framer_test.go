package codec

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestChecksum8(t *testing.T) {
	// Reference values computed from TS BufferUtility.calculateChecksum8.
	// sum([0x01, 0x02, 0x03]) = 6 → (~6+1) & 0xFF = 0xFA.
	if got, want := Checksum8([]byte{0x01, 0x02, 0x03}), byte(0xFA); got != want {
		t.Errorf("Checksum8 basic: got %#x want %#x", got, want)
	}
	// Empty buffer → (~0+1) & 0xFF = 0.
	if got := Checksum8(nil); got != 0 {
		t.Errorf("Checksum8 empty: got %#x want 0", got)
	}
	// sum = 0x100 → (~0+1)&0xFF = 0.
	if got := Checksum8([]byte{0xFF, 0x01}); got != 0 {
		t.Errorf("Checksum8 wrap: got %#x want 0", got)
	}
}

func TestPackRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		frame   Frame
		wantHex []byte // whole wire output, hand-computed
	}{
		{
			name:  "no payload",
			frame: Frame{ID: RxMaintenance}, // id=0x07, btc=1, chk = ~(7+1)+1 = 0xF8
			wantHex: []byte{
				0x10, 0x02, // SOM
				0x07,       // DATA
				0x01,       // BTC
				0xF8,       // CHK
				0x10, 0x03, // EOM
			},
		},
		{
			name: "3-byte payload, no DLE",
			frame: Frame{
				ID:      RxCrosspointInterrogate, // 0x01
				Payload: []byte{0x10 + 1, 0x00, 0x02},
			},
			// data = 01 11 00 02, btc=4, sum=01+11+00+02+04=0x18, chk=0xE8.
			// DATA has no 0x10 byte → no escaping.
			wantHex: []byte{
				0x10, 0x02,
				0x01, 0x11, 0x00, 0x02,
				0x04,
				0xE8,
				0x10, 0x03,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Pack(tc.frame)
			if !bytes.Equal(got, tc.wantHex) {
				t.Fatalf("wire mismatch\n got %X\nwant %X", got, tc.wantHex)
			}
			f, n, err := Unpack(got)
			if err != nil {
				t.Fatalf("Unpack after Pack: %v", err)
			}
			if n != len(got) {
				t.Errorf("Unpack n = %d want %d", n, len(got))
			}
			if f.ID != tc.frame.ID {
				t.Errorf("ID mismatch: got %#x want %#x", f.ID, tc.frame.ID)
			}
			if !bytes.Equal(f.Payload, tc.frame.Payload) {
				t.Errorf("Payload mismatch: got %X want %X", f.Payload, tc.frame.Payload)
			}
		})
	}
}

func TestPackEscapesDLEInData(t *testing.T) {
	// Payload contains a literal DLE (0x10) — must be doubled in DATA.
	f := Frame{ID: RxCrosspointConnect, Payload: []byte{0x10, 0xAA}}
	out := Pack(f)

	// data = 02 10 AA, btc=3, chkIn = 02 10 AA 03 = 0xBF, chk = ~0xBF+1 = 0x41.
	// DATA on wire: 02 10 10 AA (DLE doubled). BTC=3, CHK=0x41. Neither BTC
	// nor CHK equals DLE so they appear once each.
	want := []byte{
		0x10, 0x02,
		0x02, 0x10, 0x10, 0xAA,
		0x03,
		0x41,
		0x10, 0x03,
	}
	if !bytes.Equal(out, want) {
		t.Fatalf("wire mismatch\n got %X\nwant %X", out, want)
	}

	// Round-trip decodes the single embedded DLE, not two.
	got, _, err := Unpack(out)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if !bytes.Equal(got.Payload, []byte{0x10, 0xAA}) {
		t.Errorf("payload round-trip: got %X want 10 AA", got.Payload)
	}
}

func TestPackEscapesDLEInBTC(t *testing.T) {
	// Craft a payload whose DATA is exactly 0x10 bytes, giving BTC = 0x10
	// (= DLE).  BTC itself must be DLE-doubled on the wire.
	payload := make([]byte, 0x10-1) // + 1-byte ID = 0x10 total DATA
	f := Frame{ID: RxMaintenance, Payload: payload}
	out := Pack(f)

	// Expect ... 10 10 [chk] 10 03 (BTC escaped).
	// Find the byte sequence: after DATA we must see DLE DLE (escaped BTC).
	btcIdx := 2 + 0x10 // SOM(2) + DATA(0x10, no 0x10 in content since id=0x07 and zeros)
	if btcIdx+1 >= len(out) || out[btcIdx] != DLE || out[btcIdx+1] != DLE {
		t.Fatalf("BTC not escaped: out=%X", out)
	}

	// Round-trip.
	got, _, err := Unpack(out)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if got.ID != RxMaintenance || len(got.Payload) != len(payload) {
		t.Errorf("round-trip failure: id=%#x len=%d want id=%#x len=%d",
			got.ID, len(got.Payload), RxMaintenance, len(payload))
	}
}

func TestPackEscapesDLEInCHK(t *testing.T) {
	// Find a payload whose checksum equals 0x10.  Checksum over (DATA || BTC):
	// pick ID=0x01, payload = [0xEF], DATA = 01 EF, BTC=2, chkIn = 01 EF 02
	// sum=0xF2, chk = ~0xF2+1 = 0x0E. Tweak: payload=[0xED] → sum=01+ED+02 = 0xF0 → chk=0x10.
	f := Frame{ID: 0x01, Payload: []byte{0xED}}
	out := Pack(f)
	// DATA 01 ED (no 0x10). BTC=2 (no escape). CHK=0x10 → escaped as 10 10.
	want := []byte{
		0x10, 0x02,
		0x01, 0xED,
		0x02,
		0x10, 0x10,
		0x10, 0x03,
	}
	if !bytes.Equal(out, want) {
		t.Fatalf("CHK escape\n got %X\nwant %X", out, want)
	}
	if _, _, err := Unpack(out); err != nil {
		t.Errorf("Unpack escaped-CHK frame: %v", err)
	}
}

func TestUnpackErrors(t *testing.T) {
	t.Run("bad SOM", func(t *testing.T) {
		_, _, err := Unpack([]byte{0xAA, 0xBB, 0x10, 0x03})
		if !errors.Is(err, ErrBadSOM) {
			t.Errorf("got %v want ErrBadSOM", err)
		}
	})
	t.Run("truncated", func(t *testing.T) {
		_, _, err := Unpack([]byte{0x10, 0x02, 0x01, 0x01, 0xFE})
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("got %v want ErrUnexpectedEOF", err)
		}
	})
	t.Run("bad BTC", func(t *testing.T) {
		bad := []byte{
			0x10, 0x02,
			0x07,       // DATA (1 byte)
			0x05,       // BTC claims 5 → mismatch with actual 1
			Checksum8([]byte{0x07, 0x05}),
			0x10, 0x03,
		}
		_, _, err := Unpack(bad)
		if !errors.Is(err, ErrBadBTC) {
			t.Errorf("got %v want ErrBadBTC", err)
		}
	})
	t.Run("bad checksum", func(t *testing.T) {
		bad := []byte{
			0x10, 0x02,
			0x07,
			0x01,
			0x00, // wrong checksum
			0x10, 0x03,
		}
		_, _, err := Unpack(bad)
		if !errors.Is(err, ErrBadChecksum) {
			t.Errorf("got %v want ErrBadChecksum", err)
		}
	})
}

func TestAckNak(t *testing.T) {
	if !bytes.Equal(PackACK(), []byte{0x10, 0x06}) {
		t.Errorf("PackACK = %X", PackACK())
	}
	if !bytes.Equal(PackNAK(), []byte{0x10, 0x15}) {
		t.Errorf("PackNAK = %X", PackNAK())
	}
	if !IsACK([]byte{0x10, 0x06}) {
		t.Error("IsACK(DLE ACK) = false")
	}
	if IsACK([]byte{0x10, 0x03}) {
		t.Error("IsACK(DLE ETX) = true")
	}
	if !IsNAK([]byte{0x10, 0x15}) {
		t.Error("IsNAK(DLE NAK) = false")
	}
	if IsNAK(nil) {
		t.Error("IsNAK(nil) = true")
	}
}

func TestCommandIDIsExtended(t *testing.T) {
	if RxCrosspointInterrogate.IsExtended() {
		t.Error("0x01 should not be extended")
	}
	if !RxCrosspointInterrogateExt.IsExtended() {
		t.Error("0x81 should be extended")
	}
	if !TxCrosspointTallyExt.IsExtended() {
		t.Error("0x83 should be extended")
	}
}
