package codec

// ProtectTallyParams reports the protect state of one destination, with the
// owning deviceID (whose name can be fetched via rx 017 / tx 018).
// Shared shape used by tx 011 (reply to rx 010 + broadcast), tx 013
// (reply to rx 012/029), tx 015 (reply to rx 014), and the items of
// tx 020 Protect Tally Dump.
//
// Reference: TS tx/011/params.ts + options.ts (merged into one struct here).
// The ProtectState enum lives in types.go since multiple files use it.
type ProtectTallyParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
	DeviceID      uint16
	State         ProtectState
}

func (p ProtectTallyParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15 || p.DestinationID > 895 || p.DeviceID > 1023
}

// EncodeProtectTally builds the PROTECT TALLY reply.
//
// General form (CommandID 0x0B — 5 data bytes):
//
//	| Byte | Field          | Notes                                     |
//	|------|----------------|-------------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level     |
//	|  2   | Protect        | ProtectState (0-3)                        |
//	|  3   | Multiplier     | bits[4-6] = Dest DIV 128                  |
//	|      |                | bits[0-2] = Device DIV 128                |
//	|  4   | Dest (low 7b)  | Destination MOD 128                       |
//	|  5   | Device (low 7b)| Device MOD 128                            |
//
// Extended form (CommandID 0x8B — 7 data bytes):
//
//	| Byte | Field           | Notes                                    |
//	|------|-----------------|------------------------------------------|
//	|  1   | Matrix          | full 8-bit                               |
//	|  2   | Level           | full 8-bit                               |
//	|  3   | Protect         | ProtectState (0-3)                       |
//	|  4   | Dest multiplier | Destination DIV 256                      |
//	|  5   | Dest (low 8b)   | Destination MOD 256                      |
//	|  6   | Dev  multiplier | Device DIV 256                           |
//	|  7   | Dev  (low 8b)   | Device MOD 256                           |
//
// Spec: SW-P-08 §3.3.14. Reference: TS tx/011/command.ts.
func EncodeProtectTally(p ProtectTallyParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: TxProtectTallyExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.State),
				byte(p.DestinationID / 256),
				byte(p.DestinationID % 256),
				byte(p.DeviceID / 256),
				byte(p.DeviceID % 256),
			},
		}
	}
	return Frame{
		ID: TxProtectTally,
		Payload: []byte{
			(p.MatrixID << 4) | (p.LevelID & 0x0F),
			byte(p.State),
			byte(((p.DestinationID/128)<<4)&0x70) | byte((p.DeviceID/128)&0x07),
			byte(p.DestinationID % 128),
			byte(p.DeviceID % 128),
		},
	}
}

// DecodeProtectTally parses a PROTECT TALLY payload.
func DecodeProtectTally(f Frame) (ProtectTallyParams, error) {
	switch f.ID {
	case TxProtectTally:
		if len(f.Payload) < 5 {
			return ProtectTallyParams{}, ErrShortPayload
		}
		return ProtectTallyParams{
			MatrixID:      f.Payload[0] >> 4,
			LevelID:       f.Payload[0] & 0x0F,
			State:         ProtectState(f.Payload[1]),
			DestinationID: (uint16(f.Payload[2]>>4) & 0x07) * 128 + uint16(f.Payload[3]),
			DeviceID:      (uint16(f.Payload[2]) & 0x07) * 128 + uint16(f.Payload[4]),
		}, nil
	case TxProtectTallyExt:
		if len(f.Payload) < 7 {
			return ProtectTallyParams{}, ErrShortPayload
		}
		return ProtectTallyParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			State:         ProtectState(f.Payload[2]),
			DestinationID: uint16(f.Payload[3])*256 + uint16(f.Payload[4]),
			DeviceID:      uint16(f.Payload[5])*256 + uint16(f.Payload[6]),
		}, nil
	default:
		return ProtectTallyParams{}, ErrWrongCommand
	}
}
