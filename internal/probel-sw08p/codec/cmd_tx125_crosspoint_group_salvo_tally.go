package codec

import "fmt"

// SalvoGroupTallyValidity enumerates the tx 125 byte-7 values.
type SalvoGroupTallyValidity uint8

const (
	SalvoTallyValidMore SalvoGroupTallyValidity = 0x00 // More data available
	SalvoTallyValidLast SalvoGroupTallyValidity = 0x01 // Last row in queue
	SalvoTallyInvalid   SalvoGroupTallyValidity = 0x02 // No data in salvo
)

// SalvoGroupTallyParams: tx 125 CROSSPOINT GROUP SALVO TALLY — one row
// per query, callers iterate by bumping ConnectIndex on rx 124 until
// Validity != SalvoTallyValidMore.
//
// Reference: SW-P-08 §3.3.26.
type SalvoGroupTallyParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
	SourceID      uint16
	SalvoID       uint8
	ConnectIndex  uint8
	Validity      SalvoGroupTallyValidity
}

// EncodeSalvoGroupTally packs tx 125.
//
// | Byte | Field          | Notes                                   |
// |------|----------------|-----------------------------------------|
// |  1   | Matrix / Level | bits[4-7] Matrix, bits[0-3] Level         |
// |  2   | Multiplier     | bits[4-6] Dest/128, bits[0-2] Src/128     |
// |  3   | Dest num       | Dest MOD 128                              |
// |  4   | Src num        | Src MOD 128                               |
// |  5   | Salvo group    | 0-127                                     |
// |  6   | Connect index  | 0-based                                   |
// |  7   | Validity       | 0=more, 1=last, 2=invalid                 |
//
// Spec: SW-P-08 §3.3.26.
func EncodeSalvoGroupTally(p SalvoGroupTallyParams) Frame {
	return Frame{
		ID: TxSalvoGroupTally,
		Payload: []byte{
			encodeMatrixLevel(p.MatrixID, p.LevelID),
			byte(((p.DestinationID/128)<<4)&0x70) | byte((p.SourceID/128)&0x07),
			byte(p.DestinationID % 128),
			byte(p.SourceID % 128),
			p.SalvoID & 0x7F,
			p.ConnectIndex,
			byte(p.Validity),
		},
	}
}

// DecodeSalvoGroupTally parses tx 125.
func DecodeSalvoGroupTally(f Frame) (SalvoGroupTallyParams, error) {
	if f.ID != TxSalvoGroupTally {
		return SalvoGroupTallyParams{}, ErrWrongCommand
	}
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
		DestinationID: (uint16(f.Payload[1]>>4) & 0x07) * 128 + uint16(f.Payload[2]),
		SourceID:      (uint16(f.Payload[1]) & 0x07) * 128 + uint16(f.Payload[3]),
		SalvoID:       f.Payload[4] & 0x7F,
		ConnectIndex:  f.Payload[5],
		Validity:      v,
	}, nil
}
