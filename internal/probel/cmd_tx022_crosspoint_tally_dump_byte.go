package probel

// CrosspointTallyDumpByteParams is the compact 1-byte-per-ID dump used when
// destination AND source IDs both fit in 7 bits (≤ 191 per Probel spec).
// SourceIDs[i] is the source currently routed to destination
// FirstDestinationID+i.
//
// Reference: TS tx/022/params.ts CrossPointTallyDumpByteCommandParams.
// SW-P-08 caps one frame at 133 bytes, so callers dumping > ~128 destinations
// must split into multiple frames (each with its own FirstDestinationID).
type CrosspointTallyDumpByteParams struct {
	MatrixID           uint8
	LevelID            uint8
	FirstDestinationID uint8
	SourceIDs          []uint8
}

// EncodeCrosspointTallyDumpByte builds one TX 022 frame. Caller is
// responsible for chunking a large tally table into multiple calls.
//
// General form (CommandID 0x16 — 3 + N payload bytes, N = len(SourceIDs)):
//
//	| Byte    | Field               | Notes                               |
//	|---------|---------------------|-------------------------------------|
//	|  1      | Matrix / Level      | bits[4-7] = Matrix, bits[0-3] = Level|
//	|  2      | Tallies returned    | N = len(SourceIDs), max 191         |
//	|  3      | First destination   | (u8)                                |
//	|  4..N+3 | Source IDs          | one byte per destination, ascending |
//
// Spec: SW-P-08 §3.3.23. Reference: TS tx/022/command.ts buildDataNormal.
func EncodeCrosspointTallyDumpByte(p CrosspointTallyDumpByteParams) Frame {
	out := make([]byte, 0, 3+len(p.SourceIDs))
	out = append(out,
		(p.MatrixID<<4)|(p.LevelID&0x0F),
		byte(len(p.SourceIDs)),
		p.FirstDestinationID,
	)
	out = append(out, p.SourceIDs...)
	return Frame{ID: TxCrosspointTallyDumpByte, Payload: out}
}

// DecodeCrosspointTallyDumpByte parses a TX 022 payload.
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
