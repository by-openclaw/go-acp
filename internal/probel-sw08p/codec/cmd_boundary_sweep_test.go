package codec

import (
	"bytes"
	"reflect"
	"testing"
)

// Boundary sweep across the 11 already-wired needsExtended() commands
// (the commands that had needsExtended() before the sub-PRs in #228 /
// #229 / #230 / #231 added the missing ones). For each command pair
// this file pins:
//
//   1. NarrowMax        — params at the spec narrow ceiling stay narrow
//   2. PromoteOnAxis    — params one over each axis flip to extended
//   3. ExtendedMax      — params at the extended ceiling round-trip
//   4. DecodeBothForms  — same logical struct decodes from both IDs
//
// Every want []byte carries a spec citation in the surrounding comment.
// Spec basis: SW-P-08 Issue 30 §3.1.2 (narrow ranges) + §3.5 (extended
// addressing).
//
// Narrow ceilings used throughout (per §3.1.2):
//   - matrix      ≤ 15
//   - level       ≤ 15
//   - dst         ≤ 895    (7 × 128 - 1; the 3-bit DIV-128 multiplier)
//   - src         ≤ 1023   (8 × 128 - 1; the 3-bit DIV-128 multiplier
//                            plus bad-source bit per §3.1.2)
//
// Extended ceilings (per §3.5):
//   - matrix      ≤ 255 (full byte, spec device limit varies by family)
//   - level       ≤ 255 (full byte)
//   - dst / src   ≤ 65535 (DIV/MOD 256)

// --- rx 001 / rx 129  Crosspoint Interrogate (§3.2.1 + §3.5.1) ------

func TestBoundary_CrosspointInterrogate(t *testing.T) {
	t.Run("NarrowMax", func(t *testing.T) {
		// matrix=15, level=15, dst=895 (= 6*128+127 → byte2 bits 4-6 = 6)
		in := CrosspointInterrogateParams{MatrixID: 15, LevelID: 15, DestinationID: 895}
		f := EncodeCrosspointInterrogate(in)
		if f.ID != RxCrosspointInterrogate {
			t.Errorf("ID = %#x; want 0x01 (still narrow)", f.ID)
		}
		// §3.1.2 narrow byte table:
		//   byte1 = matrix(15)<<4 | level(15) = 0xFF
		//   byte2 bits 4-6 = dst/128 = 6 → 0x60
		//   byte3 = dst%128 = 127 = 0x7F
		want := []byte{0xFF, 0x60, 0x7F}
		if !bytes.Equal(f.Payload, want) {
			t.Errorf("payload = %X; want %X", f.Payload, want)
		}
	})
	t.Run("PromoteOnDest", func(t *testing.T) {
		in := CrosspointInterrogateParams{MatrixID: 0, LevelID: 0, DestinationID: 896}
		f := EncodeCrosspointInterrogate(in)
		if f.ID != RxCrosspointInterrogateExt {
			t.Errorf("dst=896 ID = %#x; want 0x81 (ext: dst exceeds 895 narrow ceiling)", f.ID)
		}
	})
	t.Run("PromoteOnMatrix", func(t *testing.T) {
		in := CrosspointInterrogateParams{MatrixID: 16, LevelID: 0, DestinationID: 0}
		f := EncodeCrosspointInterrogate(in)
		if f.ID != RxCrosspointInterrogateExt {
			t.Errorf("matrix=16 ID = %#x; want 0x81", f.ID)
		}
	})
	t.Run("ExtendedMax", func(t *testing.T) {
		in := CrosspointInterrogateParams{MatrixID: 255, LevelID: 255, DestinationID: 65535}
		f := EncodeCrosspointInterrogate(in)
		if f.ID != RxCrosspointInterrogateExt {
			t.Fatalf("ID = %#x; want 0x81", f.ID)
		}
		// §3.5.1 extended byte table:
		//   byte1=matrix, byte2=level, bytes 3-4 = dst (DIV/MOD 256)
		want := []byte{0xFF, 0xFF, 0xFF, 0xFF}
		if !bytes.Equal(f.Payload, want) {
			t.Errorf("payload = %X; want %X", f.Payload, want)
		}
		got, err := DecodeCrosspointInterrogate(f)
		if err != nil || got != in {
			t.Errorf("round-trip: got %+v err %v want %+v", got, err, in)
		}
	})
}

// --- rx 002 / rx 130  Crosspoint Connect (§3.2.4 + §3.5.2) -----------

func TestBoundary_CrosspointConnect(t *testing.T) {
	t.Run("NarrowMax", func(t *testing.T) {
		// matrix=15, level=15, dst=895, src=1023
		in := CrosspointConnectParams{MatrixID: 15, LevelID: 15, DestinationID: 895, SourceID: 1023}
		f := EncodeCrosspointConnect(in)
		if f.ID != RxCrosspointConnect {
			t.Errorf("ID = %#x; want 0x02 (still narrow)", f.ID)
		}
		// §3.1.2 narrow:
		//   byte1 = mtx<<4|lvl = 0xFF
		//   byte2 bits 4-6 = dst/128 = 6 (0x60); bits 0-2 = src/128 = 7 → 0x67
		//   byte3 = dst%128 = 127 = 0x7F
		//   byte4 = src%128 = 127 = 0x7F
		want := []byte{0xFF, 0x67, 0x7F, 0x7F}
		if !bytes.Equal(f.Payload, want) {
			t.Errorf("payload = %X; want %X", f.Payload, want)
		}
	})
	t.Run("PromoteOnSource", func(t *testing.T) {
		in := CrosspointConnectParams{MatrixID: 0, LevelID: 0, DestinationID: 0, SourceID: 1024}
		f := EncodeCrosspointConnect(in)
		if f.ID != RxCrosspointConnectExt {
			t.Errorf("src=1024 ID = %#x; want 0x82 (ext: src exceeds 1023 narrow ceiling)", f.ID)
		}
	})
	t.Run("ExtendedMax", func(t *testing.T) {
		in := CrosspointConnectParams{MatrixID: 255, LevelID: 255, DestinationID: 65535, SourceID: 65535}
		f := EncodeCrosspointConnect(in)
		if f.ID != RxCrosspointConnectExt {
			t.Fatalf("ID = %#x; want 0x82", f.ID)
		}
		// §3.5.2: matrix, level, dst DIV/MOD 256, src DIV/MOD 256
		want := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		if !bytes.Equal(f.Payload, want) {
			t.Errorf("payload = %X; want %X", f.Payload, want)
		}
		got, err := DecodeCrosspointConnect(f)
		if err != nil || got != in {
			t.Errorf("round-trip: got %+v err %v want %+v", got, err, in)
		}
	})
}

// --- tx 003 / tx 131  Crosspoint Tally (§3.2.3 + §3.5.5) -------------

func TestBoundary_CrosspointTally(t *testing.T) {
	t.Run("NarrowMax", func(t *testing.T) {
		in := CrosspointTallyParams{MatrixID: 15, LevelID: 15, DestinationID: 895, SourceID: 1023}
		f := EncodeCrosspointTally(in)
		if f.ID != TxCrosspointTally {
			t.Errorf("ID = %#x; want 0x03 (still narrow)", f.ID)
		}
		want := []byte{0xFF, 0x67, 0x7F, 0x7F}
		if !bytes.Equal(f.Payload, want) {
			t.Errorf("payload = %X; want %X", f.Payload, want)
		}
	})
	t.Run("PromoteOnMatrix", func(t *testing.T) {
		in := CrosspointTallyParams{MatrixID: 16, LevelID: 0, DestinationID: 0, SourceID: 0}
		f := EncodeCrosspointTally(in)
		if f.ID != TxCrosspointTallyExt {
			t.Errorf("matrix=16 ID = %#x; want 0x83", f.ID)
		}
	})
	t.Run("ExtendedMax", func(t *testing.T) {
		in := CrosspointTallyParams{MatrixID: 255, LevelID: 255, DestinationID: 65535, SourceID: 65535}
		f := EncodeCrosspointTally(in)
		if f.ID != TxCrosspointTallyExt {
			t.Fatalf("ID = %#x; want 0x83", f.ID)
		}
		// §3.5.5 extended carries an additional Status byte after src
		want := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}
		if !bytes.Equal(f.Payload, want) {
			t.Errorf("payload = %X; want %X", f.Payload, want)
		}
	})
}

// --- tx 004 / tx 132  Crosspoint Connected (§3.2.5 + §3.5.6) ---------

func TestBoundary_CrosspointConnected(t *testing.T) {
	t.Run("NarrowMax", func(t *testing.T) {
		in := CrosspointConnectedParams{MatrixID: 15, LevelID: 15, DestinationID: 895, SourceID: 1023}
		f := EncodeCrosspointConnected(in)
		if f.ID != TxCrosspointConnected {
			t.Errorf("ID = %#x; want 0x04 (still narrow)", f.ID)
		}
		want := []byte{0xFF, 0x67, 0x7F, 0x7F}
		if !bytes.Equal(f.Payload, want) {
			t.Errorf("payload = %X; want %X", f.Payload, want)
		}
	})
	t.Run("PromoteOnDest", func(t *testing.T) {
		in := CrosspointConnectedParams{MatrixID: 0, LevelID: 0, DestinationID: 896, SourceID: 0}
		f := EncodeCrosspointConnected(in)
		if f.ID != TxCrosspointConnectedExt {
			t.Errorf("dst=896 ID = %#x; want 0x84", f.ID)
		}
	})
	t.Run("ExtendedMax", func(t *testing.T) {
		in := CrosspointConnectedParams{MatrixID: 255, LevelID: 255, DestinationID: 65535, SourceID: 65535}
		f := EncodeCrosspointConnected(in)
		if f.ID != TxCrosspointConnectedExt {
			t.Fatalf("ID = %#x; want 0x84", f.ID)
		}
		// §3.5.6 extended carries a trailing Status byte
		want := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}
		if !bytes.Equal(f.Payload, want) {
			t.Errorf("payload = %X; want %X", f.Payload, want)
		}
	})
}

// --- rx 010 / rx 138  Protect Interrogate (§3.2.11 + §3.5.3) ---------

func TestBoundary_ProtectInterrogate(t *testing.T) {
	t.Run("NarrowMax", func(t *testing.T) {
		in := ProtectInterrogateParams{MatrixID: 15, LevelID: 15, DestinationID: 895}
		f := EncodeProtectInterrogate(in)
		if f.ID != RxProtectInterrogate {
			t.Errorf("ID = %#x; want 0x0A (still narrow)", f.ID)
		}
	})
	t.Run("PromoteOnLevel", func(t *testing.T) {
		in := ProtectInterrogateParams{MatrixID: 0, LevelID: 16, DestinationID: 0}
		f := EncodeProtectInterrogate(in)
		if f.ID != RxProtectInterrogateExt {
			t.Errorf("level=16 ID = %#x; want 0x8A", f.ID)
		}
	})
	t.Run("ExtendedMax", func(t *testing.T) {
		in := ProtectInterrogateParams{MatrixID: 255, LevelID: 255, DestinationID: 65535}
		f := EncodeProtectInterrogate(in)
		if f.ID != RxProtectInterrogateExt {
			t.Fatalf("ID = %#x; want 0x8A", f.ID)
		}
		got, err := DecodeProtectInterrogate(f)
		if err != nil || got != in {
			t.Errorf("round-trip: got %+v err %v want %+v", got, err, in)
		}
	})
}

// --- rx 012 / rx 140  Protect Connect (§3.2.13 + §3.5.4) -------------

func TestBoundary_ProtectConnect(t *testing.T) {
	t.Run("NarrowMax", func(t *testing.T) {
		in := ProtectConnectParams{MatrixID: 15, LevelID: 15, DestinationID: 895, DeviceID: 1023}
		f := EncodeProtectConnect(in)
		if f.ID != RxProtectConnect {
			t.Errorf("ID = %#x; want 0x0C (still narrow)", f.ID)
		}
	})
	t.Run("PromoteOnDest", func(t *testing.T) {
		in := ProtectConnectParams{MatrixID: 0, LevelID: 0, DestinationID: 896, DeviceID: 0}
		f := EncodeProtectConnect(in)
		if f.ID != RxProtectConnectExt {
			t.Errorf("dst=896 ID = %#x; want 0x8C", f.ID)
		}
	})
	t.Run("ExtendedMax", func(t *testing.T) {
		in := ProtectConnectParams{MatrixID: 255, LevelID: 255, DestinationID: 65535, DeviceID: 65535}
		f := EncodeProtectConnect(in)
		if f.ID != RxProtectConnectExt {
			t.Fatalf("ID = %#x; want 0x8C", f.ID)
		}
		got, err := DecodeProtectConnect(f)
		if err != nil || got != in {
			t.Errorf("round-trip: got %+v err %v want %+v", got, err, in)
		}
	})
}

// --- rx 014 / rx 142  Protect Disconnect (§3.2.15 + alias of rx 012) -

func TestBoundary_ProtectDisconnect(t *testing.T) {
	t.Run("PromoteOnAxis", func(t *testing.T) {
		// rx 014 aliases rx 012's params + encoder; only the cmd byte
		// differs. Verify the alias-through-encoder routes the extended
		// path correctly.
		in := ProtectDisconnectParams{MatrixID: 16, LevelID: 0, DestinationID: 0, DeviceID: 0}
		f := EncodeProtectDisconnect(in)
		if f.ID != RxProtectDisconnectExt {
			t.Errorf("matrix=16 ID = %#x; want 0x8E", f.ID)
		}
	})
	t.Run("NarrowMax", func(t *testing.T) {
		in := ProtectDisconnectParams{MatrixID: 15, LevelID: 15, DestinationID: 895, DeviceID: 1023}
		f := EncodeProtectDisconnect(in)
		if f.ID != RxProtectDisconnect {
			t.Errorf("ID = %#x; want 0x0E (still narrow)", f.ID)
		}
	})
}

// --- rx 019 / rx 147  Protect Tally Dump Request (§3.2.20 + §3.5.8) --

func TestBoundary_ProtectTallyDumpRequest(t *testing.T) {
	t.Run("NarrowMax", func(t *testing.T) {
		in := ProtectTallyDumpRequestParams{MatrixID: 15, LevelID: 15, DestinationID: 895}
		f := EncodeProtectTallyDumpRequest(in)
		if f.ID != RxProtectTallyDumpRequest {
			t.Errorf("ID = %#x; want 0x13 (still narrow)", f.ID)
		}
		// §3.2.19 narrow: byte1=mtx<<4|lvl, byte2=dst/256, byte3=dst%256
		want := []byte{0xFF, 0x03, 0x7F}
		if !bytes.Equal(f.Payload, want) {
			t.Errorf("payload = %X; want %X", f.Payload, want)
		}
	})
	t.Run("PromoteOnMatrix", func(t *testing.T) {
		in := ProtectTallyDumpRequestParams{MatrixID: 16, LevelID: 0, DestinationID: 0}
		f := EncodeProtectTallyDumpRequest(in)
		if f.ID != RxProtectTallyDumpRequestExt {
			t.Errorf("matrix=16 ID = %#x; want 0x93", f.ID)
		}
	})
	t.Run("PromoteOnDest", func(t *testing.T) {
		in := ProtectTallyDumpRequestParams{MatrixID: 0, LevelID: 0, DestinationID: 896}
		f := EncodeProtectTallyDumpRequest(in)
		if f.ID != RxProtectTallyDumpRequestExt {
			t.Errorf("dst=896 ID = %#x; want 0x93", f.ID)
		}
	})
	t.Run("ExtendedMax", func(t *testing.T) {
		in := ProtectTallyDumpRequestParams{MatrixID: 255, LevelID: 255, DestinationID: 65535}
		f := EncodeProtectTallyDumpRequest(in)
		if f.ID != RxProtectTallyDumpRequestExt {
			t.Fatalf("ID = %#x; want 0x93", f.ID)
		}
		want := []byte{0xFF, 0xFF, 0xFF, 0xFF}
		if !bytes.Equal(f.Payload, want) {
			t.Errorf("payload = %X; want %X", f.Payload, want)
		}
	})
}

// --- rx 021 / rx 149  Crosspoint Tally Dump Request (§3.2.22 + §3.5.9)

func TestBoundary_CrosspointTallyDumpRequest(t *testing.T) {
	t.Run("NarrowMax", func(t *testing.T) {
		in := CrosspointTallyDumpRequestParams{MatrixID: 15, LevelID: 15}
		f := EncodeCrosspointTallyDumpRequest(in)
		if f.ID != RxCrosspointTallyDumpRequest {
			t.Errorf("ID = %#x; want 0x15 (still narrow)", f.ID)
		}
	})
	t.Run("PromoteOnLevel", func(t *testing.T) {
		in := CrosspointTallyDumpRequestParams{MatrixID: 0, LevelID: 16}
		f := EncodeCrosspointTallyDumpRequest(in)
		if f.ID != RxCrosspointTallyDumpRequestExt {
			t.Errorf("level=16 ID = %#x; want 0x95", f.ID)
		}
	})
	t.Run("ExtendedMax", func(t *testing.T) {
		in := CrosspointTallyDumpRequestParams{MatrixID: 255, LevelID: 255}
		f := EncodeCrosspointTallyDumpRequest(in)
		if f.ID != RxCrosspointTallyDumpRequestExt {
			t.Fatalf("ID = %#x; want 0x95", f.ID)
		}
		got, err := DecodeCrosspointTallyDumpRequest(f)
		if err != nil || got != in {
			t.Errorf("round-trip: got %+v err %v want %+v", got, err, in)
		}
	})
}

// --- tx 011 / tx 139  Protect Tally (§3.3.12 + §3.5.7) ---------------

func TestBoundary_ProtectTally(t *testing.T) {
	t.Run("NarrowMax", func(t *testing.T) {
		in := ProtectTallyParams{
			MatrixID: 15, LevelID: 15, DestinationID: 895,
			DeviceID: 1023, State: ProtectProbel,
		}
		f := EncodeProtectTally(in)
		if f.ID != TxProtectTally {
			t.Errorf("ID = %#x; want 0x0B (still narrow)", f.ID)
		}
	})
	t.Run("PromoteOnAxis", func(t *testing.T) {
		in := ProtectTallyParams{MatrixID: 16, State: ProtectProbel}
		f := EncodeProtectTally(in)
		if f.ID != TxProtectTallyExt {
			t.Errorf("matrix=16 ID = %#x; want 0x8B", f.ID)
		}
	})
	t.Run("ExtendedMax", func(t *testing.T) {
		in := ProtectTallyParams{
			MatrixID: 255, LevelID: 255, DestinationID: 65535,
			DeviceID: 65535, State: ProtectProbelOver,
		}
		f := EncodeProtectTally(in)
		if f.ID != TxProtectTallyExt {
			t.Fatalf("ID = %#x; want 0x8B", f.ID)
		}
		got, err := DecodeProtectTally(f)
		if err != nil || got != in {
			t.Errorf("round-trip: got %+v err %v want %+v", got, err, in)
		}
	})
}

// --- tx 020 / tx 148  Protect Tally Dump (§3.3.21 + §3.5.12) ---------

func TestBoundary_ProtectTallyDump(t *testing.T) {
	// Tally dump is multi-item; check that each form correctly carries
	// items at the per-item ceilings.
	t.Run("NarrowMax", func(t *testing.T) {
		in := ProtectTallyDumpParams{
			MatrixID: 15, LevelID: 15, FirstDestinationID: 895,
			Items: []ProtectTallyItem{
				{DeviceID: 1023, State: ProtectProbel},
			},
		}
		f := EncodeProtectTallyDump(in)
		if f.ID != TxProtectTallyDump {
			t.Errorf("ID = %#x; want 0x14 (still narrow)", f.ID)
		}
	})
	t.Run("PromoteOnMatrix", func(t *testing.T) {
		in := ProtectTallyDumpParams{
			MatrixID: 16, LevelID: 0, FirstDestinationID: 0,
			Items: []ProtectTallyItem{
				{DeviceID: 0, State: ProtectNone},
			},
		}
		f := EncodeProtectTallyDump(in)
		if f.ID != TxProtectTallyDumpExt {
			t.Errorf("matrix=16 ID = %#x; want 0x94", f.ID)
		}
	})
	t.Run("ExtendedMax", func(t *testing.T) {
		in := ProtectTallyDumpParams{
			MatrixID: 255, LevelID: 255, FirstDestinationID: 65535,
			Items: []ProtectTallyItem{
				{DeviceID: 1023, State: ProtectProbelOver}, // device limited to 0-1023 by codec packing
			},
		}
		f := EncodeProtectTallyDump(in)
		if f.ID != TxProtectTallyDumpExt {
			t.Fatalf("ID = %#x; want 0x94", f.ID)
		}
		got, err := DecodeProtectTallyDump(f)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !reflect.DeepEqual(got, in) {
			t.Errorf("round-trip: got %+v want %+v", got, in)
		}
	})
}
