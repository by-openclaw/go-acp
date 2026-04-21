package probel

// CrosspointConnectedParams is the router's broadcast-style confirmation that
// (matrix M, level L, dest D) is now sourced by S. Emitted on ALL connected
// ports after a CONNECT request succeeds — not addressed to a single peer.
//
// Reference: TS tx/004/params.ts CrossPointConnectedMessageCommandParams.
// Wire layout identical to tx 003 (Crosspoint Tally); only the CommandID
// byte differs.
type CrosspointConnectedParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
	SourceID      uint16
	Status        uint8 // extended only; reserved, 0
}

func (p CrosspointConnectedParams) needsExtended() bool {
	return p.DestinationID > 895 || p.SourceID > 1023 ||
		p.MatrixID > 15 || p.LevelID > 15
}

// EncodeCrosspointConnected builds the CROSSPOINT CONNECTED broadcast frame.
//
// General form (CommandID 0x04 — 4 data bytes) — identical layout to tx 003:
//
//	| Byte | Field          | Notes                                     |
//	|------|----------------|-------------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level     |
//	|  2   | Multiplier     | bits[4-6] = Dest DIV 128                  |
//	|      |                | bits[0-2] = Source DIV 128                |
//	|  3   | Dest (low 7b)  | Destination MOD 128                       |
//	|  4   | Src  (low 7b)  | Source MOD 128                            |
//
// Extended form (CommandID 0x84 — 7 data bytes) — identical to tx 131:
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
// Spec: SW-P-08 §3.3.5. Reference: TS tx/004/command.ts.
func EncodeCrosspointConnected(p CrosspointConnectedParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: TxCrosspointConnectedExt,
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
		ID: TxCrosspointConnected,
		Payload: []byte{
			(p.MatrixID << 4) | (p.LevelID & 0x0F),
			byte(((p.DestinationID/128)<<4)&0x70) | byte((p.SourceID/128)&0x0F),
			byte(p.DestinationID % 128),
			byte(p.SourceID % 128),
		},
	}
}

// DecodeCrosspointConnected parses a general 0x04 or extended 0x84 payload.
func DecodeCrosspointConnected(f Frame) (CrosspointConnectedParams, error) {
	switch f.ID {
	case TxCrosspointConnected:
		if len(f.Payload) < 4 {
			return CrosspointConnectedParams{}, ErrShortPayload
		}
		return CrosspointConnectedParams{
			MatrixID:      f.Payload[0] >> 4,
			LevelID:       f.Payload[0] & 0x0F,
			DestinationID: (uint16(f.Payload[1]>>4) & 0x07) * 128 + uint16(f.Payload[2]),
			SourceID:      (uint16(f.Payload[1]) & 0x07) * 128 + uint16(f.Payload[3]),
		}, nil
	case TxCrosspointConnectedExt:
		if len(f.Payload) < 7 {
			return CrosspointConnectedParams{}, ErrShortPayload
		}
		return CrosspointConnectedParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
			SourceID:      uint16(f.Payload[4])*256 + uint16(f.Payload[5]),
			Status:        f.Payload[6],
		}, nil
	default:
		return CrosspointConnectedParams{}, ErrWrongCommand
	}
}
