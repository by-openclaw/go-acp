package probel

// ProtectConnectParams requests protection of (matrix, level, destination) on
// behalf of the given deviceID (the panel/device that will own the protect).
// The router replies with tx 013 Protect Connected broadcast on success.
//
// Reference: TS rx/012/params.ts ProtectConnectMessageCommandParams.
type ProtectConnectParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
	DeviceID      uint16
}

func (p ProtectConnectParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15 || p.DestinationID > 895
}

// EncodeProtectConnect builds the PROTECT CONNECT request.
//
// General form (CommandID 0x0C — 4 data bytes):
//
//	| Byte | Field           | Notes                                   |
//	|------|-----------------|-----------------------------------------|
//	|  1   | Matrix / Level  | bits[4-7] = Matrix, bits[0-3] = Level   |
//	|  2   | Multiplier      | bits[4-6] = Dest DIV 128                |
//	|      |                 | bits[0-2] = Device DIV 128              |
//	|  3   | Dest (low 7b)   | Destination MOD 128                     |
//	|  4   | Device (low 7b) | Device MOD 128                          |
//
// Extended form (CommandID 0x8C — 6 data bytes):
//
//	| Byte | Field           | Notes                                   |
//	|------|-----------------|-----------------------------------------|
//	|  1   | Matrix          | full 8-bit                              |
//	|  2   | Level           | full 8-bit                              |
//	|  3   | Dest multiplier | Destination DIV 256                     |
//	|  4   | Dest (low 8b)   | Destination MOD 256                     |
//	|  5   | Dev  multiplier | Device DIV 256                          |
//	|  6   | Dev  (low 8b)   | Device MOD 256                          |
//
// Spec: SW-P-08 §3.2.15. Reference: TS rx/012/command.ts.
func EncodeProtectConnect(p ProtectConnectParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: RxProtectConnectExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.DestinationID / 256),
				byte(p.DestinationID % 256),
				byte(p.DeviceID / 256),
				byte(p.DeviceID % 256),
			},
		}
	}
	return Frame{
		ID: RxProtectConnect,
		Payload: []byte{
			(p.MatrixID << 4) | (p.LevelID & 0x0F),
			byte(((p.DestinationID/128)<<4)&0x70) | byte((p.DeviceID/128)&0x07),
			byte(p.DestinationID % 128),
			byte(p.DeviceID % 128),
		},
	}
}

// DecodeProtectConnect parses a PROTECT CONNECT payload.
func DecodeProtectConnect(f Frame) (ProtectConnectParams, error) {
	switch f.ID {
	case RxProtectConnect:
		if len(f.Payload) < 4 {
			return ProtectConnectParams{}, ErrShortPayload
		}
		return ProtectConnectParams{
			MatrixID:      f.Payload[0] >> 4,
			LevelID:       f.Payload[0] & 0x0F,
			DestinationID: (uint16(f.Payload[1]>>4) & 0x07) * 128 + uint16(f.Payload[2]),
			DeviceID:      (uint16(f.Payload[1]) & 0x07) * 128 + uint16(f.Payload[3]),
		}, nil
	case RxProtectConnectExt:
		if len(f.Payload) < 6 {
			return ProtectConnectParams{}, ErrShortPayload
		}
		return ProtectConnectParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
			DeviceID:      uint16(f.Payload[4])*256 + uint16(f.Payload[5]),
		}, nil
	default:
		return ProtectConnectParams{}, ErrWrongCommand
	}
}
