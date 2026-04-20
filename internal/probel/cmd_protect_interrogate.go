package probel

// ProtectState is the 4-value enum carried in tx 011 / 013 / 015 / 020 byte 3
// (or byte 3 of the header for tx 020 items) to describe a destination's
// protect disposition. Defined in SW-P-88 §3.2.5.
//
// | Value | Meaning                                             |
// |-------|-----------------------------------------------------|
// |   0   | Not Protected                                       |
// |   1   | Pro-Bel Protected                                   |
// |   2   | Pro-Bel override Protected (cannot be altered rem.) |
// |   3   | OEM Protected                                       |
type ProtectState uint8

const (
	ProtectNone        ProtectState = 0
	ProtectProbel      ProtectState = 1
	ProtectProbelOver  ProtectState = 2
	ProtectOEM         ProtectState = 3
)

// --- rx 010 / rx 138 : Protect Interrogate ---------------------------------

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
// Spec: SW-P-88 §5.13. Reference: TS rx/010/command.ts.
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

// --- tx 011 / tx 139 : Protect Tally ---------------------------------------

// ProtectTallyParams reports the protect state of one destination, with the
// owning deviceID (whose name can be fetched via rx 017 / tx 018).
//
// Reference: TS tx/011/params.ts + options.ts (merged into one struct here).
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
// Spec: SW-P-88 §5.14. Reference: TS tx/011/command.ts.
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
