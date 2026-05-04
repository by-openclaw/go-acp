package codec

// CrosspointTallyDumpByteParams is the compact 1-byte-per-ID dump used
// when the (matrix, level) and every dst/src ID fit in the spec's
// narrow byte-form limits. SourceIDs[i] is the source currently routed
// to destination FirstDestinationID+i.
//
// Spec narrow-byte ranges (SW-P-08 Issue 30 §3.3.10):
//   - matrix:               0-15  (4-bit packing in byte 1)
//   - level:                0-15  (4-bit packing in byte 1)
//   - FirstDestinationID:   0-191
//   - any SourceIDs[i]:     0-191
//
// When ANY of those ceilings is exceeded the encoder auto-promotes to
// the word form (cmd 23) — and the word form itself promotes to the
// extended word form (cmd 151) when matrix or level exceeds the narrow
// 4-bit packing. There is no separate extended-byte form in the spec.
//
// Per-frame tally count is bounded by the §2 128-byte DATA cap; large
// dumps split into multiple frames each with its own
// FirstDestinationID.
type CrosspointTallyDumpByteParams struct {
	MatrixID           uint8
	LevelID            uint8
	FirstDestinationID uint8
	SourceIDs          []uint8
}

// needsWordPromote returns true when any field exceeds the narrow
// byte-form's spec-defined limits (§3.3.10). The encoder then routes
// the frame through the word codec, which itself picks narrow-word
// (cmd 23) vs extended-word (cmd 151) based on matrix/level.
//
// SourceID > 255 is structurally impossible (uint8 caps at 255); the
// 192..255 range is wire-encodable but spec-disallowed for byte form,
// so we promote to word.
func (p CrosspointTallyDumpByteParams) needsWordPromote() bool {
	if p.MatrixID > 15 || p.LevelID > 15 || p.FirstDestinationID > 191 {
		return true
	}
	for _, s := range p.SourceIDs {
		if s > 191 {
			return true
		}
	}
	return false
}

// EncodeCrosspointTallyDumpByte builds one cmd 22 frame, OR auto-
// promotes to the word form (cmd 23 / cmd 151 per word-form rules)
// when the params exceed byte-form limits per §3.3.10. Caller is
// responsible for chunking a large tally table across multiple calls
// (per ADR §2 128-byte DATA cap).
//
// Narrow byte form (CommandID 0x16 — 3 + N payload bytes, N = len(SourceIDs)):
//
//	| Byte    | Field               | Notes                               |
//	|---------|---------------------|-------------------------------------|
//	|  1      | Matrix / Level      | bits[4-7] = Matrix, bits[0-3] = Level |
//	|  2      | Tallies returned    | N = len(SourceIDs)                  |
//	|  3      | First destination   | u8, range 0-191 per spec            |
//	|  4..N+3 | Source IDs          | one byte per destination, 0-191     |
//
// Spec: SW-P-08 Issue 30 §3.3.10 "This message assumes a maximum
// destination/source number of 191."
func EncodeCrosspointTallyDumpByte(p CrosspointTallyDumpByteParams) Frame {
	if p.needsWordPromote() {
		srcWord := make([]uint16, len(p.SourceIDs))
		for i, s := range p.SourceIDs {
			srcWord[i] = uint16(s)
		}
		return EncodeCrosspointTallyDumpWord(CrosspointTallyDumpWordParams{
			MatrixID:           p.MatrixID,
			LevelID:            p.LevelID,
			FirstDestinationID: uint16(p.FirstDestinationID),
			SourceIDs:          srcWord,
		})
	}
	out := make([]byte, 0, 3+len(p.SourceIDs))
	out = append(out,
		(p.MatrixID<<4)|(p.LevelID&0x0F),
		byte(len(p.SourceIDs)),
		p.FirstDestinationID,
	)
	out = append(out, p.SourceIDs...)
	return Frame{ID: TxCrosspointTallyDumpByte, Payload: out}
}

// DecodeCrosspointTallyDumpByte parses a cmd 22 payload. Use
// DecodeCrosspointTallyDumpWord for cmd 23 (narrow word) or cmd 151
// (extended word) frames.
func DecodeCrosspointTallyDumpByte(f Frame) (CrosspointTallyDumpByteParams, error) {
	if f.ID != TxCrosspointTallyDumpByte {
		return CrosspointTallyDumpByteParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 3 {
		return CrosspointTallyDumpByteParams{}, ErrShortPayload
	}
	tallies := int(f.Payload[1])
	if len(f.Payload) < 3+tallies {
		return CrosspointTallyDumpByteParams{}, ErrShortPayload
	}
	src := make([]uint8, tallies)
	copy(src, f.Payload[3:3+tallies])
	return CrosspointTallyDumpByteParams{
		MatrixID:           f.Payload[0] >> 4,
		LevelID:            f.Payload[0] & 0x0F,
		FirstDestinationID: f.Payload[2],
		SourceIDs:          src,
	}, nil
}
