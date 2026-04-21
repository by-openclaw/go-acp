package probel

// SalvoConnectOnGoParams: rx 120 CROSSPOINT CONNECT ON GO GROUP SALVO.
// Builds up a salvo group by adding one crosspoint per frame. Routing
// information is held by the controller until rx 121 fires.
//
// Reference: SW-P-08 §3.2.29.
type SalvoConnectOnGoParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16 // narrow encoding: 0-895
	SourceID      uint16 // narrow encoding: 0-1023
	SalvoID       uint8  // 0-127
}

// EncodeSalvoConnectOnGo packs rx 120 in the general (narrow) form used
// by SW-P-08 §3.2.29. Extended form is out of scope — the spec gates
// group-salvo to the "XD and ECLIPSE router ranges" only.
//
// | Byte | Field          | Notes                                   |
// |------|----------------|-----------------------------------------|
// |  1   | Matrix / Level | bits[4-7] Matrix, bits[0-3] Level         |
// |  2   | Multiplier     | bits[4-6] Dest/128, bits[0-2] Src/128     |
// |  3   | Dest num       | Dest MOD 128                              |
// |  4   | Src num        | Src MOD 128                               |
// |  5   | Salvo num      | bit 7 reserved = 0; bits[0-6] SalvoID     |
//
// Spec: SW-P-08 §3.2.29.
func EncodeSalvoConnectOnGo(p SalvoConnectOnGoParams) Frame {
	return Frame{
		ID: RxCrosspointConnectOnGoSalvo,
		Payload: []byte{
			encodeMatrixLevel(p.MatrixID, p.LevelID),
			byte(((p.DestinationID/128)<<4)&0x70) | byte((p.SourceID/128)&0x07),
			byte(p.DestinationID % 128),
			byte(p.SourceID % 128),
			p.SalvoID & 0x7F,
		},
	}
}

// DecodeSalvoConnectOnGo parses rx 120.
func DecodeSalvoConnectOnGo(f Frame) (SalvoConnectOnGoParams, error) {
	if f.ID != RxCrosspointConnectOnGoSalvo {
		return SalvoConnectOnGoParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 5 {
		return SalvoConnectOnGoParams{}, ErrShortPayload
	}
	m, l := decodeMatrixLevel(f.Payload[0])
	return SalvoConnectOnGoParams{
		MatrixID:      m,
		LevelID:       l,
		DestinationID: (uint16(f.Payload[1]>>4) & 0x07) * 128 + uint16(f.Payload[2]),
		SourceID:      (uint16(f.Payload[1]) & 0x07) * 128 + uint16(f.Payload[3]),
		SalvoID:       f.Payload[4] & 0x7F,
	}, nil
}
