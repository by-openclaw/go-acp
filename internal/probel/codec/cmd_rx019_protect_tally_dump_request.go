package codec

// ProtectTallyDumpRequestParams asks for a bulk dump of protect information
// for (matrix, level), starting at FirstDestinationID. Unlike rx 001 or rx
// 010, this request uses a FULL u16 destination in BOTH general and extended
// forms (no multiplier/mod128 split). Only matrix/level differ in encoding.
//
// Reference: TS rx/019/params.ts ProtectTallyDumpRequestMessageCommandParams.
type ProtectTallyDumpRequestParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
}

func (p ProtectTallyDumpRequestParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15 || p.DestinationID > 895
}

// EncodeProtectTallyDumpRequest builds the PROTECT TALLY DUMP REQUEST.
//
// General form (CommandID 0x13 — 3 data bytes):
//
//	| Byte | Field           | Notes                                   |
//	|------|-----------------|-----------------------------------------|
//	|  1   | Matrix / Level  | bits[4-7] = Matrix, bits[0-3] = Level   |
//	|  2   | Dest multiplier | Destination DIV 256                     |
//	|  3   | Dest (low 8b)   | Destination MOD 256                     |
//
// Extended form (CommandID 0x93 — 4 data bytes):
//
//	| Byte | Field           | Notes                                   |
//	|------|-----------------|-----------------------------------------|
//	|  1   | Matrix          | full 8-bit                              |
//	|  2   | Level           | full 8-bit                              |
//	|  3   | Dest multiplier | Destination DIV 256                     |
//	|  4   | Dest (low 8b)   | Destination MOD 256                     |
//
// Spec: SW-P-08 §3.2.19. Reference: TS rx/019/command.ts.
func EncodeProtectTallyDumpRequest(p ProtectTallyDumpRequestParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: RxProtectTallyDumpRequestExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.DestinationID / 256),
				byte(p.DestinationID % 256),
			},
		}
	}
	return Frame{
		ID: RxProtectTallyDumpRequest,
		Payload: []byte{
			(p.MatrixID << 4) | (p.LevelID & 0x0F),
			byte(p.DestinationID / 256),
			byte(p.DestinationID % 256),
		},
	}
}

// DecodeProtectTallyDumpRequest parses the request payload.
func DecodeProtectTallyDumpRequest(f Frame) (ProtectTallyDumpRequestParams, error) {
	switch f.ID {
	case RxProtectTallyDumpRequest:
		if len(f.Payload) < 3 {
			return ProtectTallyDumpRequestParams{}, ErrShortPayload
		}
		return ProtectTallyDumpRequestParams{
			MatrixID:      f.Payload[0] >> 4,
			LevelID:       f.Payload[0] & 0x0F,
			DestinationID: uint16(f.Payload[1])*256 + uint16(f.Payload[2]),
		}, nil
	case RxProtectTallyDumpRequestExt:
		if len(f.Payload) < 4 {
			return ProtectTallyDumpRequestParams{}, ErrShortPayload
		}
		return ProtectTallyDumpRequestParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
		}, nil
	default:
		return ProtectTallyDumpRequestParams{}, ErrWrongCommand
	}
}
