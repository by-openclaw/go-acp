package codec

import (
	"bytes"
	"strings"
	"testing"
)

// This file pins the encode/decode arms left uncovered by the existing
// round-trip tests: extended-form encoders that only fire when an
// address field overflows the narrow ranges, the name-count truncation
// arms, name truncation/padding edge cases, and the extended-form
// decode success paths. Expected bytes follow the per-command wire
// tables in the SW-P-08 spec docs.

// --- packNameWithPad non-ASCII collapse ------------------------------------

// TestPackNameNonASCII drives the default arm of packNameWithPad: a byte
// outside printable ASCII (and not CR/LF) collapses to '?'.
func TestPackNameNonASCII(t *testing.T) {
	// 0xC3 0xA9 is UTF-8 'é' — two non-ASCII bytes, each becomes '?'.
	got := packName("\xC3\xA9", 4)
	want := []byte{'?', '?', ' ', ' '}
	if !bytes.Equal(got, want) {
		t.Errorf("packName(non-ascii) = %v; want %v", got, want)
	}
}

// --- rx 117 Update Name name-count truncation ------------------------------

// TestEncodeUpdateNameRequestTruncates feeds more names than the
// per-message cap for 16-char names (8) so the n>cap clamp arm runs.
func TestEncodeUpdateNameRequestTruncates(t *testing.T) {
	names := make([]string, 12) // cap for 16-char is 8
	for i := range names {
		names[i] = "X"
	}
	f := EncodeUpdateNameRequest(UpdateNameRequestParams{
		NameType:   UpdateNameSource,
		NameLength: NameLen16,
		Names:      names,
	})
	// Byte 7 (index 6) holds the encoded count, clamped to the cap of 8.
	if got := f.Payload[6]; got != 8 {
		t.Errorf("encoded name count = %d; want 8 (clamped)", got)
	}
	got, err := DecodeUpdateNameRequest(f)
	if err != nil {
		t.Fatalf("DecodeUpdateNameRequest: %v", err)
	}
	if len(got.Names) != 8 {
		t.Errorf("decoded %d names; want 8", len(got.Names))
	}
}

// --- tx 013 Protect Connected extended encode ------------------------------

func TestEncodeProtectConnectedExtended(t *testing.T) {
	// DeviceID > 1023 forces the extended form.
	p := ProtectConnectedParams{MatrixID: 1, LevelID: 2, DestinationID: 10, DeviceID: 2000, State: ProtectProbel}
	f := EncodeProtectConnected(p)
	if f.ID != TxProtectConnectedExt {
		t.Fatalf("ID = %#x; want TxProtectConnectedExt", byte(f.ID))
	}
	got, err := DecodeProtectConnected(f)
	if err != nil || got != p {
		t.Errorf("round-trip ext: got %+v err %v; want %+v", got, err, p)
	}
}

// --- tx 015 Protect Disconnected extended encode ---------------------------

func TestEncodeProtectDisconnectedExtended(t *testing.T) {
	p := ProtectDisconnectedParams{MatrixID: 20, LevelID: 1, DestinationID: 5, DeviceID: 3, State: ProtectNone}
	f := EncodeProtectDisconnected(p)
	if f.ID != TxProtectDisconnectedExt {
		t.Fatalf("ID = %#x; want TxProtectDisconnectedExt", byte(f.ID))
	}
	got, err := DecodeProtectDisconnected(f)
	if err != nil || got != p {
		t.Errorf("round-trip ext: got %+v err %v; want %+v", got, err, p)
	}
}

// --- tx 018 Protect Device Name Response name truncation -------------------

func TestEncodeProtectDeviceNameResponseTruncates(t *testing.T) {
	// A name longer than 8 chars exercises the truncation arm.
	f := EncodeProtectDeviceNameResponse(ProtectDeviceNameResponseParams{
		DeviceID:   42,
		DeviceName: "VERYLONGNAME",
	})
	got, err := DecodeProtectDeviceNameResponse(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.DeviceName != "VERYLONG" {
		t.Errorf("DeviceName = %q; want %q (truncated to 8)", got.DeviceName, "VERYLONG")
	}
	if got.DeviceID != 42 {
		t.Errorf("DeviceID = %d; want 42", got.DeviceID)
	}
}

// --- tx 107 Dest Assoc Names Response name-count truncation ----------------

func TestEncodeDestAssocNamesResponseTruncates(t *testing.T) {
	names := make([]string, 40) // cap for 4-char is 32
	for i := range names {
		names[i] = "AB"
	}
	f := EncodeDestAssocNamesResponse(DestAssocNamesResponseParams{
		NameLength: NameLen4,
		Names:      names,
	})
	got, err := DecodeDestAssocNamesResponse(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Names) != 32 {
		t.Errorf("decoded %d names; want 32 (clamped)", len(got.Names))
	}
}

// --- tx 116 Source Assoc Names Response name-count truncation --------------

func TestEncodeSourceAssocNamesResponseTruncates(t *testing.T) {
	names := make([]string, 40) // cap for 4-char is 32
	for i := range names {
		names[i] = "CD"
	}
	f := EncodeSourceAssocNamesResponse(SourceAssocNamesResponseParams{
		NameLength: NameLen4,
		Names:      names,
	})
	got, err := DecodeSourceAssocNamesResponse(f)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Names) != 32 {
		t.Errorf("decoded %d names; want 32 (clamped)", len(got.Names))
	}
}

// --- tx 015 Protect Disconnected narrow encode -----------------------------

func TestEncodeProtectDisconnectedNarrow(t *testing.T) {
	// All fields within narrow ranges → general form (else arm).
	p := ProtectDisconnectedParams{MatrixID: 1, LevelID: 2, DestinationID: 5, DeviceID: 3, State: ProtectNone}
	f := EncodeProtectDisconnected(p)
	if f.ID != TxProtectDisconnected {
		t.Fatalf("ID = %#x; want TxProtectDisconnected (narrow)", byte(f.ID))
	}
	got, err := DecodeProtectDisconnected(f)
	if err != nil || got != p {
		t.Errorf("round-trip narrow: got %+v err %v; want %+v", got, err, p)
	}
}

// --- tx 122 Salvo Connect On Go Ack narrow decode --------------------------

func TestDecodeSalvoConnectOnGoAckNarrow(t *testing.T) {
	// All fields within narrow ranges → general form decode arm.
	p := SalvoConnectOnGoAckParams{MatrixID: 1, LevelID: 2, DestinationID: 300, SourceID: 400, SalvoID: 7}
	f := EncodeSalvoConnectOnGoAck(p)
	if f.ID != TxSalvoConnectOnGoAck {
		t.Fatalf("ID = %#x; want TxSalvoConnectOnGoAck (narrow)", byte(f.ID))
	}
	got, err := DecodeSalvoConnectOnGoAck(f)
	if err != nil || got != p {
		t.Errorf("round-trip narrow: got %+v err %v; want %+v", got, err, p)
	}
}

// --- rx 019 Protect Tally Dump Request extended decode ---------------------

func TestDecodeProtectTallyDumpRequestExtended(t *testing.T) {
	// LevelID > 15 forces extended on encode; decode must round-trip.
	p := ProtectTallyDumpRequestParams{MatrixID: 2, LevelID: 20, DestinationID: 1000}
	f := EncodeProtectTallyDumpRequest(p)
	if f.ID != RxProtectTallyDumpRequestExt {
		t.Fatalf("ID = %#x; want RxProtectTallyDumpRequestExt", byte(f.ID))
	}
	got, err := DecodeProtectTallyDumpRequest(f)
	if err != nil || got != p {
		t.Errorf("round-trip ext: got %+v err %v; want %+v", got, err, p)
	}
}

// --- tx 122 Salvo Connect On Go Ack extended decode ------------------------

func TestDecodeSalvoConnectOnGoAckExtended(t *testing.T) {
	// SourceID > 1023 forces extended.
	p := SalvoConnectOnGoAckParams{MatrixID: 1, LevelID: 1, DestinationID: 10, SourceID: 2000, SalvoID: 5}
	f := EncodeSalvoConnectOnGoAck(p)
	if f.ID != TxSalvoConnectOnGoAckExt {
		t.Fatalf("ID = %#x; want TxSalvoConnectOnGoAckExt", byte(f.ID))
	}
	got, err := DecodeSalvoConnectOnGoAck(f)
	if err != nil || got != p {
		t.Errorf("round-trip ext: got %+v err %v; want %+v", got, err, p)
	}
}

// Guard against accidental ASCII assumptions in the helper above: a
// plain ASCII name pads with spaces, not '?'.
func TestPackNameASCIIPads(t *testing.T) {
	got := packName("OK", 4)
	if !strings.HasPrefix(string(got), "OK") || string(got) != "OK  " {
		t.Errorf("packName(ASCII) = %q; want %q", string(got), "OK  ")
	}
}
