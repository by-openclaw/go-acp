package codec

// ProtectInterrogateParams asks "what is the protect state of destination D
// on (matrix M, level L)?" — DeviceID scopes the query in general form (it
// becomes part of the multiplier byte; 3-bit encoding limits to 0-1023).
//
// Reference: TS rx/010/params.ts ProtectInterrogateMessageCommandParams.
type ProtectInterrogateParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
	DeviceID      uint16 // only encoded in general form multiplier; 0 is a sensible default
}

func (p ProtectInterrogateParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15 || p.DestinationID > 895
}

// EncodeProtectInterrogate builds the PROTECT INTERROGATE request.
//
// General form (CommandID 0x0A — 3 data bytes):
//
//	| Byte | Field          | Notes                                   |
//	|------|----------------|-----------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level   |
//	|  2   | Multiplier     | bits[4-6] = Dest DIV 128                |
//	|      |                | bits[0-2] = Device DIV 128              |
//	|  3   | Dest (low 7b)  | Destination MOD 128                     |
//
// Extended form (CommandID 0x8A — 4 data bytes; device is NOT carried):
//
//	| Byte | Field           | Notes                                  |
//	|------|-----------------|----------------------------------------|
//	|  1   | Matrix          | full 8-bit                             |
//	|  2   | Level           | full 8-bit                             |
//	|  3   | Dest multiplier | Destination DIV 256                    |
//	|  4   | Dest (low 8b)   | Destination MOD 256                    |
//
// Spec: SW-P-08 §3.2.13. Reference: TS rx/010/command.ts.
func EncodeProtectInterrogate(p ProtectInterrogateParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: RxProtectInterrogateExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.DestinationID / 256),
				byte(p.DestinationID % 256),
			},
		}
	}
	return Frame{
		ID: RxProtectInterrogate,
		Payload: []byte{
			(p.MatrixID << 4) | (p.LevelID & 0x0F),
			byte(((p.DestinationID/128)<<4)&0x70) | byte((p.DeviceID/128)&0x07),
			byte(p.DestinationID % 128),
		},
	}
}

// DecodeProtectInterrogate parses a PROTECT INTERROGATE payload.
func DecodeProtectInterrogate(f Frame) (ProtectInterrogateParams, error) {
	switch f.ID {
	case RxProtectInterrogate:
		if len(f.Payload) < 3 {
			return ProtectInterrogateParams{}, ErrShortPayload
		}
		return ProtectInterrogateParams{
			MatrixID:      f.Payload[0] >> 4,
			LevelID:       f.Payload[0] & 0x0F,
			DestinationID: (uint16(f.Payload[1]>>4) & 0x07) * 128 + uint16(f.Payload[2]),
			DeviceID:      (uint16(f.Payload[1]) & 0x07) * 128,
		}, nil
	case RxProtectInterrogateExt:
		if len(f.Payload) < 4 {
			return ProtectInterrogateParams{}, ErrShortPayload
		}
		return ProtectInterrogateParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
		}, nil
	default:
		return ProtectInterrogateParams{}, ErrWrongCommand
	}
}
