package probel

import "errors"

// Errors shared by per-command codecs. Kept in the same file pattern as TS
// (each command file surfaces just the errors it can produce).
var (
	ErrShortPayload = errors.New("probel: frame payload too short for this command")
	ErrWrongCommand = errors.New("probel: frame command ID does not match the expected command")
)

// --- rx 001 / rx 129 : Crosspoint Interrogate Message -----------------------

// CrosspointInterrogateParams carries the inputs for a crosspoint interrogate
// request (rx 001 general or rx 129/0x81 extended).  The encoder picks general
// vs. extended automatically based on the field ranges.
//
// Reference: TS params.ts CrossPointInterrogateMessageCommandParams.
type CrosspointInterrogateParams struct {
	MatrixID      uint8  // 0-15 for general, 0-255 for extended
	LevelID       uint8  // 0-15 for general, 0-255 for extended
	DestinationID uint16 // 0-895 for general, 0-65535 for extended
}

// needsExtendedCrosspointInterrogate returns true when any param exceeds the
// ranges encodable in the general (3-byte) form, forcing the extended (4-byte)
// encoding with CommandID 0x81.
//
// Mirror of TS CrossPointInterrogateMessageCommand.isExtended — 895 DIV 128 < 7
// means the 3-bit multiplier slot still fits.
func (p CrosspointInterrogateParams) needsExtended() bool {
	return p.DestinationID > 895 || p.MatrixID > 15 || p.LevelID > 15
}

// EncodeCrosspointInterrogate builds a Frame for the CROSSPOINT INTERROGATE
// message (request tally for one destination).
//
// General form (CommandID 0x01 — 3 data bytes):
//
//	| Byte | Field           | Notes                                    |
//	|------|-----------------|------------------------------------------|
//	|  1   | Matrix / Level  | bits[4-7] = Matrix, bits[0-3] = Level    |
//	|  2   | Multiplier      | bits[4-6] = Dest DIV 128 (3 bits)        |
//	|      |                 | bits[0-2] = Source DIV 128 (here = 0)    |
//	|  3   | Dest (low 7b)   | Destination MOD 128                      |
//
// Extended form (CommandID 0x81 — 4 data bytes):
//
//	| Byte | Field           | Notes                                    |
//	|------|-----------------|------------------------------------------|
//	|  1   | Matrix          | full 8-bit                               |
//	|  2   | Level           | full 8-bit                               |
//	|  3   | Dest multiplier | Destination DIV 256                      |
//	|  4   | Dest (low 8b)   | Destination MOD 256                      |
//
// Spec: SW-P-88 §5.3.  Reference: TS rx/001/command.ts buildDataNormal /
// buildDataExtended.
func EncodeCrosspointInterrogate(p CrosspointInterrogateParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: RxCrosspointInterrogateExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.DestinationID / 256),
				byte(p.DestinationID % 256),
			},
		}
	}
	return Frame{
		ID: RxCrosspointInterrogate,
		Payload: []byte{
			(p.MatrixID << 4) | (p.LevelID & 0x0F),
			byte(((p.DestinationID / 128) << 4) & 0x70),
			byte(p.DestinationID % 128),
		},
	}
}

// DecodeCrosspointInterrogate parses the payload of a CROSSPOINT INTERROGATE
// frame (general 0x01 or extended 0x81) back into its params.
func DecodeCrosspointInterrogate(f Frame) (CrosspointInterrogateParams, error) {
	switch f.ID {
	case RxCrosspointInterrogate:
		if len(f.Payload) < 3 {
			return CrosspointInterrogateParams{}, ErrShortPayload
		}
		return CrosspointInterrogateParams{
			MatrixID:      f.Payload[0] >> 4,
			LevelID:       f.Payload[0] & 0x0F,
			DestinationID: (uint16(f.Payload[1]>>4) & 0x07) * 128 + uint16(f.Payload[2]),
		}, nil
	case RxCrosspointInterrogateExt:
		if len(f.Payload) < 4 {
			return CrosspointInterrogateParams{}, ErrShortPayload
		}
		return CrosspointInterrogateParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
		}, nil
	default:
		return CrosspointInterrogateParams{}, ErrWrongCommand
	}
}

// --- tx 003 / tx 131 : Crosspoint Tally Message ----------------------------

// CrosspointTallyParams carries the router's reply to a CROSSPOINT INTERROGATE
// request: "destination D on matrix M level L is currently sourced by S".
// Reference: TS tx/003/params.ts CrossPointTallyMessageCommandParams.
type CrosspointTallyParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
	SourceID      uint16
	Status        uint8 // extended only; "for future use", 0 in current spec
}

// needsExtendedCrosspointTally returns true when any param exceeds the ranges
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
// Spec: SW-P-88 §5.4.  Reference: TS tx/003/command.ts buildDataNormal /
// buildDataExtended.
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
