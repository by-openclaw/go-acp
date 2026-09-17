package codec

import (
	"bytes"
	"reflect"
	"testing"
)

// Boundary tests for the salvo command quartet (rx 120 / rx 124 /
// tx 122 / tx 125) + their extended counterparts (rx 248 / rx 252 /
// tx 250 / tx 253). Locks the needsExtended() form-selection rules
// against SW-P-08 Issue 30 §3.1.2 (narrow ranges) + §3.4.16 + §3.4.17
// + §3.5.13 + §3.5.14 (extended forms).

// --- rx 120 / rx 248 -------------------------------------------------

func TestSalvoConnectOnGo_NarrowMin(t *testing.T) {
	in := SalvoConnectOnGoParams{MatrixID: 0, LevelID: 0, DestinationID: 0, SourceID: 0, SalvoID: 0}
	f := EncodeSalvoConnectOnGo(in)
	if f.ID != RxCrosspointConnectOnGoSalvo {
		t.Errorf("min-narrow ID = %#x; want 0x78 (general)", f.ID)
	}
	want := []byte{0x00, 0x00, 0x00, 0x00, 0x00}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("min-narrow payload = %X; want %X", f.Payload, want)
	}
	roundtrip(t, f, in, "narrow-min")
}

func TestSalvoConnectOnGo_NarrowMax(t *testing.T) {
	// Narrow ceilings per §3.1.2: matrix=15, level=15, dst=895 (7×128-1),
	// src=1023 (8×128-1). Salvo 127 (7-bit, bit 7 = 0).
	in := SalvoConnectOnGoParams{MatrixID: 15, LevelID: 15, DestinationID: 895, SourceID: 1023, SalvoID: 127}
	f := EncodeSalvoConnectOnGo(in)
	if f.ID != RxCrosspointConnectOnGoSalvo {
		t.Errorf("max-narrow ID = %#x; want 0x78 (general, no promotion)", f.ID)
	}
	// dst=895 = 6*128+127 → byte2 bits 4-6 = 6, byte3 = 127
	// src=1023 = 7*128+127 → byte2 bits 0-2 = 7, byte4 = 127
	// byte1: matrix(15)<<4 | level(15) = 0xFF
	// byte2: (6<<4)|7 = 0x67
	want := []byte{0xFF, 0x67, 0x7F, 0x7F, 0x7F}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("max-narrow payload = %X; want %X", f.Payload, want)
	}
	roundtrip(t, f, in, "narrow-max")
}

func TestSalvoConnectOnGo_PromoteOnMatrix(t *testing.T) {
	in := SalvoConnectOnGoParams{MatrixID: 16, LevelID: 0, DestinationID: 0, SourceID: 0, SalvoID: 0}
	f := EncodeSalvoConnectOnGo(in)
	if f.ID != RxCrosspointConnectOnGoSalvoExt {
		t.Errorf("matrix=16 ID = %#x; want 0xF8 (extended) — narrow only fits matrix ≤ 15", f.ID)
	}
	if len(f.Payload) != 7 {
		t.Errorf("extended payload len = %d; want 7", len(f.Payload))
	}
	roundtrip(t, f, in, "promote-matrix")
}

func TestSalvoConnectOnGo_PromoteOnLevel(t *testing.T) {
	in := SalvoConnectOnGoParams{MatrixID: 0, LevelID: 16, DestinationID: 0, SourceID: 0, SalvoID: 0}
	f := EncodeSalvoConnectOnGo(in)
	if f.ID != RxCrosspointConnectOnGoSalvoExt {
		t.Errorf("level=16 ID = %#x; want 0xF8", f.ID)
	}
	roundtrip(t, f, in, "promote-level")
}

func TestSalvoConnectOnGo_PromoteOnDest(t *testing.T) {
	in := SalvoConnectOnGoParams{MatrixID: 0, LevelID: 0, DestinationID: 896, SourceID: 0, SalvoID: 0}
	f := EncodeSalvoConnectOnGo(in)
	if f.ID != RxCrosspointConnectOnGoSalvoExt {
		t.Errorf("dst=896 ID = %#x; want 0xF8 — narrow only fits dst ≤ 895", f.ID)
	}
	roundtrip(t, f, in, "promote-dest")
}

func TestSalvoConnectOnGo_PromoteOnSource(t *testing.T) {
	in := SalvoConnectOnGoParams{MatrixID: 0, LevelID: 0, DestinationID: 0, SourceID: 1024, SalvoID: 0}
	f := EncodeSalvoConnectOnGo(in)
	if f.ID != RxCrosspointConnectOnGoSalvoExt {
		t.Errorf("src=1024 ID = %#x; want 0xF8 — narrow only fits src ≤ 1023", f.ID)
	}
	roundtrip(t, f, in, "promote-source")
}

func TestSalvoConnectOnGo_ExtendedMax(t *testing.T) {
	// Extended ceilings per §3.4.16: matrix 0-127, level 0-127 (8-bit
	// fields), dst/src 0-65535 (DIV 256 + MOD 256). Salvo 127 in both.
	in := SalvoConnectOnGoParams{MatrixID: 127, LevelID: 127, DestinationID: 65535, SourceID: 65535, SalvoID: 127}
	f := EncodeSalvoConnectOnGo(in)
	if f.ID != RxCrosspointConnectOnGoSalvoExt {
		t.Errorf("max-extended ID = %#x; want 0xF8", f.ID)
	}
	want := []byte{127, 127, 0xFF, 0xFF, 0xFF, 0xFF, 0x7F}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("max-extended payload = %X; want %X", f.Payload, want)
	}
	roundtrip(t, f, in, "extended-max")
}

func TestSalvoConnectOnGo_DecodeBothForms(t *testing.T) {
	// Same logical struct decodes from both narrow and extended frames.
	logical := SalvoConnectOnGoParams{MatrixID: 5, LevelID: 3, DestinationID: 200, SourceID: 600, SalvoID: 7}

	// Round-trip narrow.
	narrow := EncodeSalvoConnectOnGo(logical)
	if narrow.ID != RxCrosspointConnectOnGoSalvo {
		t.Fatalf("expected narrow form for in-range params")
	}
	got, err := DecodeSalvoConnectOnGo(narrow)
	if err != nil || !reflect.DeepEqual(got, logical) {
		t.Errorf("narrow decode: got %+v err %v", got, err)
	}

	// Hand-craft the equivalent extended frame (same logical values
	// but extended ID). Decoder must accept it.
	ext := Frame{
		ID: RxCrosspointConnectOnGoSalvoExt,
		Payload: []byte{
			5, 3,
			0x00, 200, // dst = 0*256+200
			0x02, 0x58, // src = 2*256+88 = 600
			7,
		},
	}
	got, err = DecodeSalvoConnectOnGo(ext)
	if err != nil || !reflect.DeepEqual(got, logical) {
		t.Errorf("extended decode: got %+v err %v", got, err)
	}
}

// --- tx 122 / tx 250 -------------------------------------------------

func TestSalvoConnectOnGoAck_PromoteOnAnyAxis(t *testing.T) {
	// Same form-selection rules as rx 120; one test per axis suffices.
	cases := []struct {
		name string
		p    SalvoConnectOnGoAckParams
	}{
		{"matrix=16", SalvoConnectOnGoAckParams{MatrixID: 16}},
		{"level=16", SalvoConnectOnGoAckParams{LevelID: 16}},
		{"dst=896", SalvoConnectOnGoAckParams{DestinationID: 896}},
		{"src=1024", SalvoConnectOnGoAckParams{SourceID: 1024}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := EncodeSalvoConnectOnGoAck(c.p)
			if f.ID != TxSalvoConnectOnGoAckExt {
				t.Errorf("ID = %#x; want 0xFA (extended)", f.ID)
			}
			back, err := DecodeSalvoConnectOnGoAck(f)
			if err != nil || !reflect.DeepEqual(back, c.p) {
				t.Errorf("round-trip: got %+v err %v want %+v", back, err, c.p)
			}
		})
	}
}

func TestSalvoConnectOnGoAck_NarrowMaxStillNarrow(t *testing.T) {
	in := SalvoConnectOnGoAckParams{MatrixID: 15, LevelID: 15, DestinationID: 895, SourceID: 1023, SalvoID: 127}
	f := EncodeSalvoConnectOnGoAck(in)
	if f.ID != TxSalvoConnectOnGoAck {
		t.Errorf("max-narrow ID = %#x; want 0x7A (general, no promotion)", f.ID)
	}
}

// --- rx 124 / rx 252 -------------------------------------------------

func TestSalvoGroupInterrogate_NarrowMaxStillNarrow(t *testing.T) {
	in := SalvoGroupInterrogateParams{SalvoID: 127, ConnectIndex: 255}
	f := EncodeSalvoGroupInterrogate(in)
	if f.ID != RxCrosspointSalvoGroupInterrogate {
		t.Errorf("ConnectIndex=255 ID = %#x; want 0x7C (general, fits 1 byte)", f.ID)
	}
	if len(f.Payload) != 2 {
		t.Errorf("narrow payload len = %d; want 2", len(f.Payload))
	}
	back, err := DecodeSalvoGroupInterrogate(f)
	if err != nil || back != in {
		t.Errorf("round-trip: got %+v err %v", back, err)
	}
}

func TestSalvoGroupInterrogate_PromoteOnIndex(t *testing.T) {
	in := SalvoGroupInterrogateParams{SalvoID: 0, ConnectIndex: 256}
	f := EncodeSalvoGroupInterrogate(in)
	if f.ID != RxCrosspointSalvoGroupInterrogateExt {
		t.Errorf("ConnectIndex=256 ID = %#x; want 0xFC (extended; narrow only fits 1 byte)", f.ID)
	}
	want := []byte{0x00, 0x01, 0x00} // salvo, index/256, index%256
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	back, err := DecodeSalvoGroupInterrogate(f)
	if err != nil || back != in {
		t.Errorf("round-trip: got %+v err %v", back, err)
	}
}

func TestSalvoGroupInterrogate_ExtendedMax(t *testing.T) {
	in := SalvoGroupInterrogateParams{SalvoID: 127, ConnectIndex: 65535}
	f := EncodeSalvoGroupInterrogate(in)
	if f.ID != RxCrosspointSalvoGroupInterrogateExt {
		t.Fatalf("ID = %#x; want 0xFC", f.ID)
	}
	want := []byte{0x7F, 0xFF, 0xFF}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
}

// --- tx 125 / tx 253 -------------------------------------------------

func TestSalvoGroupTally_PromoteOnIndex(t *testing.T) {
	in := SalvoGroupTallyParams{
		MatrixID: 0, LevelID: 0, DestinationID: 0, SourceID: 0,
		SalvoID: 0, ConnectIndex: 256, Validity: SalvoTallyValidMore,
	}
	f := EncodeSalvoGroupTally(in)
	if f.ID != TxSalvoGroupTallyExt {
		t.Errorf("ConnectIndex=256 ID = %#x; want 0xFD (extended; narrow ConnectIndex is 1 byte)", f.ID)
	}
	if len(f.Payload) != 10 {
		t.Errorf("extended payload len = %d; want 10", len(f.Payload))
	}
	back, err := DecodeSalvoGroupTally(f)
	if err != nil || !reflect.DeepEqual(back, in) {
		t.Errorf("round-trip: got %+v err %v", back, err)
	}
}

func TestSalvoGroupTally_ExtendedMaxAllAxes(t *testing.T) {
	// Hammer every field at its extended ceiling simultaneously.
	in := SalvoGroupTallyParams{
		MatrixID: 127, LevelID: 127, DestinationID: 65535, SourceID: 65535,
		SalvoID: 127, ConnectIndex: 65535, Validity: SalvoTallyValidLast,
	}
	f := EncodeSalvoGroupTally(in)
	if f.ID != TxSalvoGroupTallyExt {
		t.Fatalf("ID = %#x; want 0xFD", f.ID)
	}
	want := []byte{127, 127, 0xFF, 0xFF, 0xFF, 0xFF, 0x7F, 0xFF, 0xFF, 0x01}
	if !bytes.Equal(f.Payload, want) {
		t.Errorf("payload = %X; want %X", f.Payload, want)
	}
	back, err := DecodeSalvoGroupTally(f)
	if err != nil || !reflect.DeepEqual(back, in) {
		t.Errorf("round-trip: got %+v err %v", back, err)
	}
}

// --- mixed-form salvo (the user's worked example) --------------------

// TestMixedFormSalvo proves that a salvo group built from slots
// crossing the narrow→extended threshold emits the right wire form
// per slot. This is the "store a salvo with different mtxid, level,
// src and tgt (general, extended)" case raised during the boundary
// audit.
func TestMixedFormSalvo(t *testing.T) {
	slots := []SalvoConnectOnGoParams{
		{MatrixID: 0, LevelID: 0, DestinationID: 100, SourceID: 50, SalvoID: 3},     // narrow
		{MatrixID: 16, LevelID: 2, DestinationID: 1000, SourceID: 2000, SalvoID: 3}, // extended (mtx > 15)
		{MatrixID: 0, LevelID: 0, DestinationID: 200, SourceID: 75, SalvoID: 3},     // narrow
	}
	wantIDs := []CommandID{
		RxCrosspointConnectOnGoSalvo,
		RxCrosspointConnectOnGoSalvoExt,
		RxCrosspointConnectOnGoSalvo,
	}
	for i, s := range slots {
		f := EncodeSalvoConnectOnGo(s)
		if f.ID != wantIDs[i] {
			t.Errorf("slot %d ID = %#x; want %#x (slot=%+v)", i, f.ID, wantIDs[i], s)
		}
		// Order preserved per the salvo design discussion (PR #226).
	}
}

// --- helper ----------------------------------------------------------

func roundtrip(t *testing.T, f Frame, want SalvoConnectOnGoParams, label string) {
	t.Helper()
	got, err := DecodeSalvoConnectOnGo(f)
	if err != nil {
		t.Errorf("%s: decode: %v", label, err)
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: round-trip got %+v want %+v", label, got, want)
	}
}
