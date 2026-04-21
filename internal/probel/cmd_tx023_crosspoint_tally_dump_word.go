package probel

// CrosspointTallyDumpWordParams carries the wide-address tally dump
// (destinations and sources addressed with u16). SourceIDs[i] is the source
// routed to destination FirstDestinationID+i.
//
// Reference: TS tx/023/params.ts CrossPointTallyDumpWordCommandParams.
type CrosspointTallyDumpWordParams struct {
	MatrixID           uint8
	LevelID            uint8
	FirstDestinationID uint16
	SourceIDs          []uint16
}

func (p CrosspointTallyDumpWordParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15 || p.FirstDestinationID > 895
}

// EncodeCrosspointTallyDumpWord builds one TX 023 / TX 151 frame.
//
// General form (CommandID 0x17 — 4 + 2N payload bytes, N = len(SourceIDs)):
//
//	| Byte | Field                | Notes                                 |
//	|------|----------------------|---------------------------------------|
//	|  1   | Matrix / Level       | bits[4-7] = Matrix, bits[0-3] = Level |
//	|  2   | Tallies returned     | N = len(SourceIDs), max 64            |
//	|  3   | 1st Dest multiplier  | FirstDestinationID DIV 256            |
//	|  4   | 1st Dest number      | FirstDestinationID MOD 256            |
//	|  5,7,... | Src multiplier   | SourceIDs[i] DIV 256                  |
//	|  6,8,... | Src number       | SourceIDs[i] MOD 256                  |
//
// Extended form (CommandID 0x97 — 5 + 2N payload bytes):
//
//	| Byte | Field                | Notes                                 |
//	|------|----------------------|---------------------------------------|
//	|  1   | Matrix               | full 8-bit                            |
//	|  2   | Level                | full 8-bit                            |
//	|  3   | Tallies returned     | N = len(SourceIDs), max 64            |
//	|  4   | 1st Dest multiplier  | FirstDestinationID DIV 256            |
//	|  5   | 1st Dest number      | FirstDestinationID MOD 256            |
//	|  6,8,... | Src multiplier   | SourceIDs[i] DIV 256                  |
//	|  7,9,... | Src number       | SourceIDs[i] MOD 256                  |
//
// Spec: SW-P-08 §3.3.24. Reference: TS tx/023/command.ts.
func EncodeCrosspointTallyDumpWord(p CrosspointTallyDumpWordParams) Frame {
	n := len(p.SourceIDs)
	if p.needsExtended() {
		out := make([]byte, 0, 5+2*n)
		out = append(out,
			p.MatrixID,
			p.LevelID,
			byte(n),
			byte(p.FirstDestinationID/256),
			byte(p.FirstDestinationID%256),
		)
		for _, s := range p.SourceIDs {
			out = append(out, byte(s/256), byte(s%256))
		}
		return Frame{ID: TxCrosspointTallyDumpWordExt, Payload: out}
	}
	out := make([]byte, 0, 4+2*n)
	out = append(out,
		(p.MatrixID<<4)|(p.LevelID&0x0F),
		byte(n),
		byte(p.FirstDestinationID/256),
		byte(p.FirstDestinationID%256),
	)
	for _, s := range p.SourceIDs {
		out = append(out, byte(s/256), byte(s%256))
	}
	return Frame{ID: TxCrosspointTallyDumpWord, Payload: out}
}

// DecodeCrosspointTallyDumpWord parses a TX 023 (general) or TX 151
// (extended) payload.
func DecodeCrosspointTallyDumpWord(f Frame) (CrosspointTallyDumpWordParams, error) {
	var (
		matrix, level byte
		headerLen     int
		tallies       int
		firstDest     uint16
	)
	switch f.ID {
	case TxCrosspointTallyDumpWord:
		if len(f.Payload) < 4 {
			return CrosspointTallyDumpWordParams{}, ErrShortPayload
		}
		matrix = f.Payload[0] >> 4
		level = f.Payload[0] & 0x0F
		tallies = int(f.Payload[1])
		firstDest = uint16(f.Payload[2])*256 + uint16(f.Payload[3])
		headerLen = 4
	case TxCrosspointTallyDumpWordExt:
		if len(f.Payload) < 5 {
			return CrosspointTallyDumpWordParams{}, ErrShortPayload
		}
		matrix = f.Payload[0]
		level = f.Payload[1]
		tallies = int(f.Payload[2])
		firstDest = uint16(f.Payload[3])*256 + uint16(f.Payload[4])
		headerLen = 5
	default:
		return CrosspointTallyDumpWordParams{}, ErrWrongCommand
	}
	if len(f.Payload) < headerLen+2*tallies {
		return CrosspointTallyDumpWordParams{}, ErrShortPayload
	}
	src := make([]uint16, tallies)
	for i := 0; i < tallies; i++ {
		hi := uint16(f.Payload[headerLen+2*i])
		lo := uint16(f.Payload[headerLen+2*i+1])
		src[i] = hi*256 + lo
	}
	return CrosspointTallyDumpWordParams{
		MatrixID:           matrix,
		LevelID:            level,
		FirstDestinationID: firstDest,
		SourceIDs:          src,
	}, nil
}
