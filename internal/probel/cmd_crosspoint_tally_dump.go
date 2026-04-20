package probel

// --- rx 021 / rx 149 : Crosspoint Tally Dump Request ------------------------

// CrosspointTallyDumpRequestParams asks the controller to dump the full tally
// table for one (matrix, level) pair. Reply arrives as one or more tx 022
// (byte form) and/or tx 023 (word form) messages depending on destination
// count and matrix size.
//
// Reference: TS rx/021/params.ts CrossPointTallyDumpRequestMessageCommandParams.
type CrosspointTallyDumpRequestParams struct {
	MatrixID uint8 // 0-15 general, 0-255 extended
	LevelID  uint8 // 0-15 general, 0-255 extended
}

func (p CrosspointTallyDumpRequestParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15
}

// EncodeCrosspointTallyDumpRequest builds the CROSSPOINT TALLY DUMP REQUEST.
//
// General form (CommandID 0x15 — 1-byte payload):
//
//	| Byte | Field          | Notes                                     |
//	|------|----------------|-------------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level     |
//
// Extended form (CommandID 0x95 — 2-byte payload):
//
//	| Byte | Field          | Notes                                     |
//	|------|----------------|-------------------------------------------|
//	|  1   | Matrix         | full 8-bit                                |
//	|  2   | Level          | full 8-bit                                |
//
// Spec: SW-P-88 §5.22. Reference: TS rx/021/command.ts.
func EncodeCrosspointTallyDumpRequest(p CrosspointTallyDumpRequestParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID:      RxCrosspointTallyDumpRequestExt,
			Payload: []byte{p.MatrixID, p.LevelID},
		}
	}
	return Frame{
		ID:      RxCrosspointTallyDumpRequest,
		Payload: []byte{(p.MatrixID << 4) | (p.LevelID & 0x0F)},
	}
}

// DecodeCrosspointTallyDumpRequest parses the general (0x15) or extended
// (0x95) request payload.
func DecodeCrosspointTallyDumpRequest(f Frame) (CrosspointTallyDumpRequestParams, error) {
	switch f.ID {
	case RxCrosspointTallyDumpRequest:
		if len(f.Payload) < 1 {
			return CrosspointTallyDumpRequestParams{}, ErrShortPayload
		}
		return CrosspointTallyDumpRequestParams{
			MatrixID: f.Payload[0] >> 4,
			LevelID:  f.Payload[0] & 0x0F,
		}, nil
	case RxCrosspointTallyDumpRequestExt:
		if len(f.Payload) < 2 {
			return CrosspointTallyDumpRequestParams{}, ErrShortPayload
		}
		return CrosspointTallyDumpRequestParams{
			MatrixID: f.Payload[0],
			LevelID:  f.Payload[1],
		}, nil
	default:
		return CrosspointTallyDumpRequestParams{}, ErrWrongCommand
	}
}

// --- tx 022 : Crosspoint Tally Dump (Byte) ---------------------------------

// CrosspointTallyDumpByteParams is the compact 1-byte-per-ID dump used when
// destination AND source IDs both fit in 7 bits (≤ 191 per Probel spec; the
// TS emulator notes "max 191"). SourceIDs[i] is the source currently routed to
// destination FirstDestinationID+i.
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
//	| Byte | Field               | Notes                                  |
//	|------|---------------------|----------------------------------------|
//	|  1   | Matrix / Level      | bits[4-7] = Matrix, bits[0-3] = Level  |
//	|  2   | Tallies returned    | N = len(SourceIDs), max 191            |
//	|  3   | First destination   | (u8)                                   |
//	|  4..N+3 | Source IDs       | one byte per destination, ascending    |
//
// Spec: SW-P-88 §5.23. Reference: TS tx/022/command.ts buildDataNormal.
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

// --- tx 023 / tx 151 : Crosspoint Tally Dump (Word) ------------------------

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
// Spec: SW-P-88 §5.24. Reference: TS tx/023/command.ts.
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
