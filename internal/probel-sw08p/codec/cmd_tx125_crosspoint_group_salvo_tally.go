package codec

import "fmt"

// SalvoGroupTallyValidity enumerates the tx 125 / tx 253 validity-flag
// values.
type SalvoGroupTallyValidity uint8

const (
	SalvoTallyValidMore SalvoGroupTallyValidity = 0x00 // More data available
	SalvoTallyValidLast SalvoGroupTallyValidity = 0x01 // Last row in queue
	SalvoTallyInvalid   SalvoGroupTallyValidity = 0x02 // No data in salvo
)

// SalvoGroupTallyParams: tx 125 / tx 253 CROSSPOINT GROUP SALVO TALLY —
// one row per query. Callers iterate by bumping ConnectIndex on rx 124
// / rx 252 until Validity != SalvoTallyValidMore.
//
// The encoder picks general (cmd 125, narrow) or extended (cmd 253,
// wide) form per the SW-P-08 §3.5.14 ranges. ConnectIndex widens from
// 1 byte (narrow, 0-255) to 2 bytes (extended, 0-65535).
//
// Reference: SW-P-08 Issue 30 §3.3.26 (general) + §3.5.14 (extended).
type SalvoGroupTallyParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16 // narrow: 0-895; extended: 0-65535
	SourceID      uint16 // narrow: 0-1023; extended: 0-65535
	SalvoID       uint8  // 0-127 (bit 7 = 0 in both forms)
	ConnectIndex  uint16 // narrow: 0-255 (1 byte); extended: 0-65535 (DIV 256 + MOD 256)
	Validity      SalvoGroupTallyValidity
}

// needsExtended fires when any addressing field exceeds the narrow
// ranges OR ConnectIndex exceeds 255 (narrow form's 1-byte limit).
func (p SalvoGroupTallyParams) needsExtended() bool {
	return p.MatrixID > 15 ||
		p.LevelID > 15 ||
		p.DestinationID > 895 ||
		p.SourceID > 1023 ||
		p.ConnectIndex > 255
}

// EncodeSalvoGroupTally packs cmd 125 (general, 7-byte) or cmd 253
// (extended, 10-byte) per the addressing range of the params.
//
// General form (TxSalvoGroupTally = 0x7D, §3.3.26):
//
//	| Byte | Field          | Notes                                   |
//	|------|----------------|-----------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] Matrix, bits[0-3] Level       |
//	|  2   | Multiplier     | bits[4-6] Dest/128, bits[0-2] Src/128   |
//	|  3   | Dest num       | Dest MOD 128                            |
//	|  4   | Src num        | Src MOD 128                             |
//	|  5   | Salvo group    | 0-127                                   |
//	|  6   | Connect index  | 0-based, 1 byte                         |
//	|  7   | Validity       | 0=more, 1=last, 2=invalid               |
//
// Extended form (TxSalvoGroupTallyExt = 0xFD, §3.5.14):
//
//	| Byte | Field          | Notes                                   |
//	|------|----------------|-----------------------------------------|
//	|  1   | Matrix         | full 8-bit                              |
//	|  2   | Level          | full 8-bit                              |
//	|  3   | Dest mult      | Destination DIV 256                     |
//	|  4   | Dest num       | Destination MOD 256                     |
//	|  5   | Src mult       | Source DIV 256                          |
//	|  6   | Src num        | Source MOD 256                          |
//	|  7   | Salvo group    | 0-127                                   |
//	|  8   | Index mult     | ConnectIndex DIV 256                    |
//	|  9   | Index num      | ConnectIndex MOD 256                    |
//	|  10  | Validity       | 0=more, 1=last, 2=invalid               |
func EncodeSalvoGroupTally(p SalvoGroupTallyParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: TxSalvoGroupTallyExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.DestinationID / 256),
				byte(p.DestinationID % 256),
				byte(p.SourceID / 256),
				byte(p.SourceID % 256),
				p.SalvoID & 0x7F,
				byte(p.ConnectIndex / 256),
				byte(p.ConnectIndex % 256),
				byte(p.Validity),
			},
		}
	}
	return Frame{
		ID: TxSalvoGroupTally,
		Payload: []byte{
			encodeMatrixLevel(p.MatrixID, p.LevelID),
			byte(((p.DestinationID/128)<<4)&0x70) | byte((p.SourceID/128)&0x07),
			byte(p.DestinationID % 128),
			byte(p.SourceID % 128),
			p.SalvoID & 0x7F,
			byte(p.ConnectIndex), // narrow: 1 byte; needsExtended() guards overflow
			byte(p.Validity),
		},
	}
}

// DecodeSalvoGroupTally parses cmd 125 (general 7-byte) or cmd 253
// (extended 10-byte) into the same logical struct.
func DecodeSalvoGroupTally(f Frame) (SalvoGroupTallyParams, error) {
	switch f.ID {
	case TxSalvoGroupTally:
		if len(f.Payload) < 7 {
			return SalvoGroupTallyParams{}, ErrShortPayload
		}
		m, l := decodeMatrixLevel(f.Payload[0])
		v := SalvoGroupTallyValidity(f.Payload[6])
		if v != SalvoTallyValidMore && v != SalvoTallyValidLast && v != SalvoTallyInvalid {
			return SalvoGroupTallyParams{}, fmt.Errorf("probel: tx 125 unknown validity %#x", byte(v))
		}
		return SalvoGroupTallyParams{
			MatrixID:      m,
			LevelID:       l,
			DestinationID: (uint16(f.Payload[1]>>4)&0x07)*128 + uint16(f.Payload[2]),
			SourceID:      (uint16(f.Payload[1])&0x07)*128 + uint16(f.Payload[3]),
			SalvoID:       f.Payload[4] & 0x7F,
			ConnectIndex:  uint16(f.Payload[5]),
			Validity:      v,
		}, nil
	case TxSalvoGroupTallyExt:
		if len(f.Payload) < 10 {
			return SalvoGroupTallyParams{}, ErrShortPayload
		}
		v := SalvoGroupTallyValidity(f.Payload[9])
		if v != SalvoTallyValidMore && v != SalvoTallyValidLast && v != SalvoTallyInvalid {
			return SalvoGroupTallyParams{}, fmt.Errorf("probel: tx 253 unknown validity %#x", byte(v))
		}
		return SalvoGroupTallyParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
			SourceID:      uint16(f.Payload[4])*256 + uint16(f.Payload[5]),
			SalvoID:       f.Payload[6] & 0x7F,
			ConnectIndex:  uint16(f.Payload[7])*256 + uint16(f.Payload[8]),
			Validity:      v,
		}, nil
	default:
		return SalvoGroupTallyParams{}, ErrWrongCommand
	}
}
