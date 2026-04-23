package codec

// cmd_tx003_crosspoint_tally.go — tx 003 CROSSPOINT TALLY message, the
// reply to rx 001 CROSSPOINT INTERROGATE and an async broadcast echoing
// rx 002 CROSSPOINT CONNECT state changes on every other session.
//
// Exported symbols are shared across crosspoint commands; errors
// (ErrShortPayload / ErrWrongCommand) live in cmd_rx001_crosspoint_interrogate.go.

// CrosspointTallyParams is the router's reply to a CROSSPOINT INTERROGATE
// request: "destination D on matrix M level L is currently sourced by S".
// Reference: TS tx/003/params.ts CrossPointTallyMessageCommandParams.
type CrosspointTallyParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
	SourceID      uint16
	Status        uint8 // extended only; "for future use", 0 in current spec
}

// needsExtended returns true when any param exceeds the ranges
// encodable in the general (4-byte) form.
//
// Mirror of TS CrossPointTallyMessageCommand.isExtended.
func (p CrosspointTallyParams) needsExtended() bool {
	return p.DestinationID > 895 || p.SourceID > 1023 ||
		p.MatrixID > 15 || p.LevelID > 15
}

// EncodeCrosspointTally builds a Frame for the CROSSPOINT TALLY reply.
//
// General form (CommandID 0x03 — 4 data bytes):
//
//	| Byte | Field          | Notes                                     |
//	|------|----------------|-------------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level     |
//	|  2   | Multiplier     | bits[4-6] = Dest DIV 128                  |
//	|      |                | bits[0-2] = Source DIV 128                |
//	|  3   | Dest (low 7b)  | Destination MOD 128                       |
//	|  4   | Src  (low 7b)  | Source MOD 128                            |
//
// Extended form (CommandID 0x83 — 7 data bytes):
//
//	| Byte | Field           | Notes                                    |
//	|------|-----------------|------------------------------------------|
//	|  1   | Matrix          | full 8-bit                               |
//	|  2   | Level           | full 8-bit                               |
//	|  3   | Dest multiplier | Destination DIV 256                      |
//	|  4   | Dest (low 8b)   | Destination MOD 256                      |
//	|  5   | Src  multiplier | Source DIV 256                           |
//	|  6   | Src  (low 8b)   | Source MOD 256                           |
//	|  7   | Status          | reserved, 0                              |
//
// Spec: SW-P-08 §3.3.4. Reference: TS tx/003/command.ts.
func EncodeCrosspointTally(p CrosspointTallyParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: TxCrosspointTallyExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.DestinationID / 256),
				byte(p.DestinationID % 256),
				byte(p.SourceID / 256),
				byte(p.SourceID % 256),
				p.Status,
			},
		}
	}
	return Frame{
		ID: TxCrosspointTally,
		Payload: []byte{
			(p.MatrixID << 4) | (p.LevelID & 0x0F),
			byte(((p.DestinationID/128)<<4)&0x70) | byte((p.SourceID/128)&0x0F),
			byte(p.DestinationID % 128),
			byte(p.SourceID % 128),
		},
	}
}

// DecodeCrosspointTally parses the payload of a CROSSPOINT TALLY frame
// (general 0x03 or extended 0x83).
func DecodeCrosspointTally(f Frame) (CrosspointTallyParams, error) {
	switch f.ID {
	case TxCrosspointTally:
		if len(f.Payload) < 4 {
			return CrosspointTallyParams{}, ErrShortPayload
		}
		return CrosspointTallyParams{
			MatrixID:      f.Payload[0] >> 4,
			LevelID:       f.Payload[0] & 0x0F,
			DestinationID: (uint16(f.Payload[1]>>4) & 0x07) * 128 + uint16(f.Payload[2]),
			SourceID:      (uint16(f.Payload[1]) & 0x07) * 128 + uint16(f.Payload[3]),
		}, nil
	case TxCrosspointTallyExt:
		if len(f.Payload) < 7 {
			return CrosspointTallyParams{}, ErrShortPayload
		}
		return CrosspointTallyParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
			SourceID:      uint16(f.Payload[4])*256 + uint16(f.Payload[5]),
			Status:        f.Payload[6],
		}, nil
	default:
		return CrosspointTallyParams{}, ErrWrongCommand
	}
}
