package codec

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// TestEncodeExtendedProtectTallyDumpReset pins the AURORA-reset
// sentinel wire layout: MESSAGE = single Count=127 byte.
func TestEncodeExtendedProtectTallyDumpReset(t *testing.T) {
	f := EncodeExtendedProtectTallyDump(ExtendedProtectTallyDumpParams{Reset: true})
	wire := Pack(f)
	// checksum7({64, 7F}) = (-(0xE3)) & 0x7F = 0x1D.
	want := []byte{SOM, 0x64, 0x7F, 0x1D}
	if !bytes.Equal(wire, want) {
		t.Fatalf("wire\n got %X\nwant %X", wire, want)
	}
	parsed, n, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if n != len(wire) {
		t.Errorf("consumed %d; want %d", n, len(wire))
	}
	got, err := DecodeExtendedProtectTallyDump(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.Reset {
		t.Errorf("decoded.Reset = false; want true")
	}
	if len(got.Entries) != 0 {
		t.Errorf("decoded.Entries len = %d; want 0", len(got.Entries))
	}
}

// TestEncodeExtendedProtectTallyDumpEmpty pins the "no entries" layout
// (Count=0). Distinct on the wire from the reset sentinel even though
// both decode to zero entries.
func TestEncodeExtendedProtectTallyDumpEmpty(t *testing.T) {
	f := EncodeExtendedProtectTallyDump(ExtendedProtectTallyDumpParams{})
	wire := Pack(f)
	// checksum7({64, 00}) = (-(0x64)) & 0x7F = 0x1C.
	want := []byte{SOM, 0x64, 0x00, 0x1C}
	if !bytes.Equal(wire, want) {
		t.Fatalf("wire\n got %X\nwant %X", wire, want)
	}
	parsed, n, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if n != len(wire) {
		t.Errorf("consumed %d; want %d", n, len(wire))
	}
	got, err := DecodeExtendedProtectTallyDump(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Reset || len(got.Entries) != 0 {
		t.Errorf("decoded = %+v; want empty non-reset", got)
	}
}

// TestEncodeExtendedProtectTallyDumpEntriesRoundTrip exercises the
// per-entry packed (Destination, Device, Protect) layout — including
// the 3-bit DIV 128 Device field that limits this message's Device
// range to 0-1023 (§3.2.64).
func TestEncodeExtendedProtectTallyDumpEntriesRoundTrip(t *testing.T) {
	entries := []ExtendedProtectTallyDumpEntry{
		{Destination: 1, Device: 2, Protect: ProtectProBel},
		{Destination: 5000, Device: 999, Protect: ProtectOEM},
		{Destination: 16000, Device: 0, Protect: ProtectProBelOverride},
	}
	f := EncodeExtendedProtectTallyDump(ExtendedProtectTallyDumpParams{Entries: entries})
	wire := Pack(f)

	parsed, n, err := Unpack(wire)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if n != len(wire) {
		t.Errorf("consumed %d; want %d", n, len(wire))
	}
	got, err := DecodeExtendedProtectTallyDump(parsed)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Reset {
		t.Errorf("decoded.Reset = true; want false")
	}
	if len(got.Entries) != len(entries) {
		t.Fatalf("decoded %d entries; want %d", len(got.Entries), len(entries))
	}
	for i, want := range entries {
		if got.Entries[i] != want {
			t.Errorf("entry %d = %+v; want %+v", i, got.Entries[i], want)
		}
	}
}

// TestUnpackTallyDumpNeedsCountByte verifies the scanner returns
// io.ErrUnexpectedEOF when the Count byte isn't buffered yet — so
// the session keeps reading instead of fabricating ErrUnknownCommand.
func TestUnpackTallyDumpNeedsCountByte(t *testing.T) {
	// Only SOM + CMD buffered, no count byte yet.
	buf := []byte{SOM, byte(TxExtendedProtectTallyDump), 0x00}
	_, _, err := Unpack(buf[:2])
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("2 bytes: got %v; want io.ErrUnexpectedEOF", err)
	}
}

// TestUnpackTallyDumpNeedsAllEntries verifies the scanner computes the
// total frame length from the Count byte and waits for the remaining
// entry bytes + checksum before returning.
func TestUnpackTallyDumpNeedsAllEntries(t *testing.T) {
	// Build a full valid wire with 3 entries, then truncate by 1 byte
	// and confirm Unpack returns io.ErrUnexpectedEOF.
	f := EncodeExtendedProtectTallyDump(ExtendedProtectTallyDumpParams{
		Entries: []ExtendedProtectTallyDumpEntry{
			{Destination: 1, Device: 2, Protect: ProtectProBel},
			{Destination: 3, Device: 4, Protect: ProtectOEM},
			{Destination: 5, Device: 6},
		},
	})
	wire := Pack(f)
	_, _, err := Unpack(wire[:len(wire)-1])
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("truncated: got %v; want io.ErrUnexpectedEOF", err)
	}
	// Full buffer unpacks cleanly.
	if _, n, err := Unpack(wire); err != nil || n != len(wire) {
		t.Errorf("full: n=%d err=%v; want n=%d err=nil", n, err, len(wire))
	}
}

// TestEncodeExtendedProtectTallyDumpEntryBitPacking locks the per-
// entry Device-high-byte layout: bits 0-2 DIV 128, bit 3 = 0,
// bits 4-6 Protect, bit 7 = 0.
func TestEncodeExtendedProtectTallyDumpEntryBitPacking(t *testing.T) {
	entry := ExtendedProtectTallyDumpEntry{
		Destination: 3,          // DestDIV=0 DestMod=3
		Device:      512 + 17,   // DeviceDIV=4 DeviceMod=17  → high byte: bits 0-2 = 0b100
		Protect:     ProtectOEM, // value = 3 → bits 4-6 = 0b011 → 0x30
	}
	f := EncodeExtendedProtectTallyDump(ExtendedProtectTallyDumpParams{
		Entries: []ExtendedProtectTallyDumpEntry{entry},
	})
	// Payload: [Count=1, DestDIV=0, DestMod=3, DeviceLow=17, DeviceHigh=0x34]
	// DeviceHigh = (4 & 0x07) | ((3 & 0x07) << 4) = 0x04 | 0x30 = 0x34.
	want := []byte{0x01, 0x00, 0x03, 0x11, 0x34}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	// Round-trip.
	got, err := DecodeExtendedProtectTallyDump(f)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Entries[0] != entry {
		t.Errorf("round-trip = %+v; want %+v", got.Entries[0], entry)
	}
}
