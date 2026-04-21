package codec

// CrosspointConnectParams is the request to route source S to destination D on
// (matrix M, level L). The router replies with tx 004 (general) or 0x84
// (extended) carrying the same fields once the route is confirmed, and
// broadcasts tx 003 Crosspoint Tally to every other session.
//
// Reference: TS rx/002/params.ts CrossPointConnectMessageCommandParams.
type CrosspointConnectParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
	SourceID      uint16
}

func (p CrosspointConnectParams) needsExtended() bool {
	return p.DestinationID > 895 || p.SourceID > 1023 ||
		p.MatrixID > 15 || p.LevelID > 15
}

// EncodeCrosspointConnect builds the CROSSPOINT CONNECT request frame.
//
// General form (CommandID 0x02 — 4 data bytes):
//
//	| Byte | Field          | Notes                                     |
//	|------|----------------|-------------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level     |
//	|  2   | Multiplier     | bits[4-6] = Dest DIV 128                  |
//	|      |                | bits[0-2] = Source DIV 128                |
//	|  3   | Dest (low 7b)  | Destination MOD 128                       |
//	|  4   | Src  (low 7b)  | Source MOD 128                            |
//
// Extended form (CommandID 0x82 — 6 data bytes):
//
//	| Byte | Field           | Notes                                    |
//	|------|-----------------|------------------------------------------|
//	|  1   | Matrix          | full 8-bit                               |
//	|  2   | Level           | full 8-bit                               |
//	|  3   | Dest multiplier | Destination DIV 256                      |
//	|  4   | Dest (low 8b)   | Destination MOD 256                      |
//	|  5   | Src  multiplier | Source DIV 256                           |
//	|  6   | Src  (low 8b)   | Source MOD 256                           |
//
// Spec: SW-P-08 §3.2.4. Reference: TS rx/002/command.ts.
func EncodeCrosspointConnect(p CrosspointConnectParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: RxCrosspointConnectExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.DestinationID / 256),
				byte(p.DestinationID % 256),
				byte(p.SourceID / 256),
				byte(p.SourceID % 256),
			},
		}
	}
	return Frame{
		ID: RxCrosspointConnect,
		Payload: []byte{
			(p.MatrixID << 4) | (p.LevelID & 0x0F),
			byte(((p.DestinationID/128)<<4)&0x70) | byte((p.SourceID/128)&0x0F),
			byte(p.DestinationID % 128),
			byte(p.SourceID % 128),
		},
	}
}

// DecodeCrosspointConnect parses a general 0x02 or extended 0x82 payload.
func DecodeCrosspointConnect(f Frame) (CrosspointConnectParams, error) {
	switch f.ID {
	case RxCrosspointConnect:
		if len(f.Payload) < 4 {
			return CrosspointConnectParams{}, ErrShortPayload
		}
		return CrosspointConnectParams{
			MatrixID:      f.Payload[0] >> 4,
			LevelID:       f.Payload[0] & 0x0F,
			DestinationID: (uint16(f.Payload[1]>>4) & 0x07) * 128 + uint16(f.Payload[2]),
			SourceID:      (uint16(f.Payload[1]) & 0x07) * 128 + uint16(f.Payload[3]),
		}, nil
	case RxCrosspointConnectExt:
		if len(f.Payload) < 6 {
			return CrosspointConnectParams{}, ErrShortPayload
		}
		return CrosspointConnectParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
			SourceID:      uint16(f.Payload[4])*256 + uint16(f.Payload[5]),
		}, nil
	default:
		return CrosspointConnectParams{}, ErrWrongCommand
	}
}
