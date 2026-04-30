package codec

// SourceLockStatusResponseParams carries tx 015 SOURCE LOCK STATUS
// RESPONSE fields — §3.2.17. Matrix reports which input sources have
// a stable signal ("lock ok value = 1"). Variable-length: the first
// two bytes encode the total MESSAGE size (DIV 128 + MOD 128), then
// a bitmap with 4 sources per byte.
//
// Spec quirks:
//   - §3.2.17 byte 3 uses bits 0-3 for sources 0-3; bits 4-7 are always 0.
//   - Subsequent bytes follow the same 4-sources-per-byte layout; byte N
//     (1-indexed from byte 3) covers sources [4*(N-1), 4*(N-1)+3].
//   - "Max source DIV 4" means bytes enough to cover all declared sources.
//
// Per §3.2.17 note 2, if an input card is missing the corresponding
// bits return 0 — so a lock bit of 0 is ambiguous (card absent OR
// signal lost). Callers needing to distinguish cross-check against
// INSTALLED MODULES STATUS (§3.2.33 / §3.2.35, non-VSM queue).
//
//	| Byte | Field                          | Notes                     |
//	|------|--------------------------------|---------------------------|
//	|  1   | Bytes in message DIV 128       | total MESSAGE byte count  |
//	|  2   | Bytes in message MOD 128       |                           |
//	| 3-N  | Lock bitmap (4 sources / byte) | bit set = lock OK         |
//
// Spec: SW-P-02 Issue 26 §3.2.17.
type SourceLockStatusResponseParams struct {
	// Locked[i] == true means source i reports a stable signal.
	// Length is derived from the wire byte count on decode; encode
	// rounds up to the nearest 4 sources.
	Locked []bool
}

// sourceLockBitsPerByte is the §3.2.17 packing density — 4 sources
// per byte, low nibble only (bits 0-3).
const sourceLockBitsPerByte = 4

// EncodeSourceLockStatusResponse builds tx 015 wire bytes. Caller's
// Locked slice is packed into the bitmap; the header self-declares
// the total MESSAGE size so peers can size-validate.
func EncodeSourceLockStatusResponse(p SourceLockStatusResponseParams) Frame {
	n := len(p.Locked)
	// Number of bitmap bytes (ceil division by 4).
	bitmapLen := (n + sourceLockBitsPerByte - 1) / sourceLockBitsPerByte
	// Total MESSAGE = 2 header bytes + bitmap.
	total := 2 + bitmapLen

	payload := make([]byte, total)
	payload[0] = byte(total / 128)
	payload[1] = byte(total % 128)

	for i, ok := range p.Locked {
		if !ok {
			continue
		}
		byteIdx := 2 + i/sourceLockBitsPerByte
		bitIdx := i % sourceLockBitsPerByte
		payload[byteIdx] |= 1 << uint(bitIdx)
	}
	return Frame{ID: TxSourceLockStatusResponse, Payload: payload}
}

// DecodeSourceLockStatusResponse parses tx 015. Decoded Locked slice
// length is bitmapBytes * 4 — trailing 0 bits may reflect "source
// card absent" rather than "source lost", per §3.2.17 note 2.
func DecodeSourceLockStatusResponse(f Frame) (SourceLockStatusResponseParams, error) {
	if f.ID != TxSourceLockStatusResponse {
		return SourceLockStatusResponseParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 2 {
		return SourceLockStatusResponseParams{}, ErrShortPayload
	}
	declared := int(f.Payload[0])*128 + int(f.Payload[1])
	if declared != len(f.Payload) {
		// Header disagrees with actual MESSAGE length — short payload.
		return SourceLockStatusResponseParams{}, ErrShortPayload
	}
	bitmap := f.Payload[2:]
	locked := make([]bool, len(bitmap)*sourceLockBitsPerByte)
	for byteIdx, b := range bitmap {
		for bitIdx := 0; bitIdx < sourceLockBitsPerByte; bitIdx++ {
			if b&(1<<uint(bitIdx)) != 0 {
				locked[byteIdx*sourceLockBitsPerByte+bitIdx] = true
			}
		}
	}
	return SourceLockStatusResponseParams{Locked: locked}, nil
}

// sourceLockStatusResponsePayloadSize sizes a tx 015 MESSAGE from a
// peek at the buffered bytes — bytes 1-2 self-declare the total
// MESSAGE length. Returns (0, false) until both header bytes have
// arrived.
func sourceLockStatusResponsePayloadSize(peek []byte) (int, bool) {
	if len(peek) < 2 {
		return 0, false
	}
	return int(peek[0])*128 + int(peek[1]), true
}
