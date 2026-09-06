package codec

import (
	"errors"
	"testing"
)

// TestRouterConfigResponse1EntriesTruncated exercises the SECOND
// ErrShortPayload guard in DecodeRouterConfigResponse1: the 4-byte
// level map declares N levels but fewer than 4*N entry bytes follow.
// §3.2.58 ties the per-level entry count to the level-map popcount, so
// a frame whose entry region is short is malformed.
func TestRouterConfigResponse1EntriesTruncated(t *testing.T) {
	// Level map with two bits set → 2 entries (8 bytes) expected, but
	// supply only the 4-byte map + 4 bytes (1 entry's worth).
	hdr := packLevelMap((1 << 0) | (1 << 1))
	payload := append(hdr[:], 0x00, 0x01, 0x00, 0x01) // only one entry follows
	f := Frame{ID: TxRouterConfigResponse1, Payload: payload}
	if _, err := DecodeRouterConfigResponse1(f); !errors.Is(err, ErrShortPayload) {
		t.Errorf("got %v; want ErrShortPayload", err)
	}
}

// TestRouterConfigResponse2EntriesTruncated exercises the second
// ErrShortPayload guard in DecodeRouterConfigResponse2 (10-byte
// entries per §3.2.59).
func TestRouterConfigResponse2EntriesTruncated(t *testing.T) {
	hdr := packLevelMap(1 << 0)                       // one level → 10 entry bytes expected
	payload := append(hdr[:], 0x00, 0x01, 0x00, 0x01) // only 4 of 10
	f := Frame{ID: TxRouterConfigResponse2, Payload: payload}
	if _, err := DecodeRouterConfigResponse2(f); !errors.Is(err, ErrShortPayload) {
		t.Errorf("got %v; want ErrShortPayload", err)
	}
}

// TestRouterConfigResponse2PayloadSizeShortHeader covers the
// (0, false) early-return arm of routerConfigResponse2PayloadSize when
// fewer than the 4 level-map bytes have been buffered — the stream
// scanner must keep reading. Driven through Unpack with a truncated
// frame so we exercise the real call path.
func TestRouterConfigResponse2PayloadSizeShortHeader(t *testing.T) {
	// Build a valid tx 077 frame, then hand Unpack only SOM + CMD + 2
	// header bytes: PayloadSize → routerConfigResponse2PayloadSize sees
	// a 2-byte peek (< 4) → (0,false) → variable-len path returns
	// io.ErrUnexpectedEOF.
	p := RouterConfigResponse2Params{
		LevelMap: 1 << 0,
		Levels:   []RouterConfigResponse2LevelEntry{{NumDestinations: 4, NumSources: 4}},
	}
	wire := Pack(EncodeRouterConfigResponse2(p))
	if _, _, err := Unpack(wire[:4]); err == nil {
		t.Errorf("truncated tx077: got nil err; want io.ErrUnexpectedEOF")
	}
}

// TestRouterConfigResponse1PayloadSizeShortHeader mirrors the above for
// tx 076's size helper.
func TestRouterConfigResponse1PayloadSizeShortHeader(t *testing.T) {
	p := RouterConfigResponse1Params{
		LevelMap: 1 << 0,
		Levels:   []RouterConfigResponse1LevelEntry{{NumDestinations: 4, NumSources: 4}},
	}
	wire := Pack(EncodeRouterConfigResponse1(p))
	if _, _, err := Unpack(wire[:4]); err == nil {
		t.Errorf("truncated tx076: got nil err; want io.ErrUnexpectedEOF")
	}
}

// TestSourceLockResponseHeaderDisagrees exercises the second
// ErrShortPayload guard in DecodeSourceLockStatusResponse: bytes 1-2
// self-declare the total MESSAGE length (§3.2.17); if that declaration
// disagrees with the actual payload length the frame is malformed.
func TestSourceLockResponseHeaderDisagrees(t *testing.T) {
	// Declare a total of 6 bytes but supply only 4 (2 header + 2 bitmap).
	f := Frame{ID: TxSourceLockStatusResponse, Payload: []byte{0x00, 0x06, 0x00, 0x00}}
	if _, err := DecodeSourceLockStatusResponse(f); !errors.Is(err, ErrShortPayload) {
		t.Errorf("got %v; want ErrShortPayload", err)
	}
}

// TestSourceLockResponsePayloadSizeShortHeader covers the (0, false)
// early-return arm of sourceLockStatusResponsePayloadSize when fewer
// than the 2 header bytes are buffered.
func TestSourceLockResponsePayloadSizeShortHeader(t *testing.T) {
	got, ok := sourceLockStatusResponsePayloadSize([]byte{0x00})
	if ok || got != 0 {
		t.Errorf("short header: got (%d,%v); want (0,false)", got, ok)
	}
}

// TestExtendedProtectTallyDumpEntriesTruncated exercises the second
// ErrShortPayload guard in DecodeExtendedProtectTallyDump: the leading
// Count byte declares N entries but fewer than 4*N entry bytes follow
// (§3.2.64).
func TestExtendedProtectTallyDumpEntriesTruncated(t *testing.T) {
	// Count=2 → 8 entry bytes expected; supply only 4.
	f := Frame{ID: TxExtendedProtectTallyDump, Payload: []byte{0x02, 0x00, 0x00, 0x00, 0x00}}
	if _, err := DecodeExtendedProtectTallyDump(f); !errors.Is(err, ErrShortPayload) {
		t.Errorf("got %v; want ErrShortPayload", err)
	}
}

// TestTallyDumpPayloadSizeShortHeader covers the (0, false)
// early-return arm of tallyDumpPayloadSize when the Count byte has not
// arrived yet.
func TestTallyDumpPayloadSizeShortHeader(t *testing.T) {
	got, ok := tallyDumpPayloadSize(nil)
	if ok || got != 0 {
		t.Errorf("empty peek: got (%d,%v); want (0,false)", got, ok)
	}
}

// TestTallyDumpPayloadSizeVariants pins the three sizing arms of
// tallyDumpPayloadSize against §3.2.64:
//   - Count=0   → 1 byte (count only, empty dump)
//   - Count=127 → 1 byte (AURORA reset sentinel, no entries)
//   - Count=N   → 1 + 4*N bytes
func TestTallyDumpPayloadSizeVariants(t *testing.T) {
	cases := []struct {
		count byte
		want  int
	}{
		{0, 1},
		{ExtendedProtectTallyDumpResetSentinel, 1},
		{1, 5},
		{32, 1 + 4*32},
	}
	for _, tc := range cases {
		got, ok := tallyDumpPayloadSize([]byte{tc.count})
		if !ok || got != tc.want {
			t.Errorf("tallyDumpPayloadSize(count=%d) = (%d,%v); want (%d,true)",
				tc.count, got, ok, tc.want)
		}
	}
}

// TestExtendedProtectTallyDumpResetRoundTrip pins the §3.2.64 reset
// sentinel (Count=127) on both encode and decode, and the empty-dump
// (Count=0) path — exercising the Reset branch of the encoder and the
// sentinel branch of the decoder.
func TestExtendedProtectTallyDumpResetRoundTrip(t *testing.T) {
	resetFrame := EncodeExtendedProtectTallyDump(ExtendedProtectTallyDumpParams{Reset: true})
	if len(resetFrame.Payload) != 1 || resetFrame.Payload[0] != ExtendedProtectTallyDumpResetSentinel {
		t.Fatalf("reset payload = %X; want single sentinel byte", resetFrame.Payload)
	}
	got, err := DecodeExtendedProtectTallyDump(resetFrame)
	if err != nil {
		t.Fatalf("Decode reset: %v", err)
	}
	if !got.Reset || len(got.Entries) != 0 {
		t.Errorf("decoded reset = %+v; want Reset=true no entries", got)
	}

	// Non-reset dump with one entry — exercises the encode + decode
	// entry loop including the packed Device/Protect byte.
	entry := ExtendedProtectTallyDumpEntry{
		Destination: 5000, Device: 300, Protect: ProtectProBel,
	}
	f := EncodeExtendedProtectTallyDump(ExtendedProtectTallyDumpParams{
		Entries: []ExtendedProtectTallyDumpEntry{entry},
	})
	dec, err := DecodeExtendedProtectTallyDump(f)
	if err != nil {
		t.Fatalf("Decode entry: %v", err)
	}
	if dec.Reset || len(dec.Entries) != 1 || dec.Entries[0] != entry {
		t.Errorf("decoded = %+v; want single entry %+v", dec, entry)
	}
}
