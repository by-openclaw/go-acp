package probel

// SalvoConnectOnGoAckParams: tx 122 CROSSPOINT CONNECT ON GO GROUP
// SALVO ACKNOWLEDGE. Matrix echoes the stored crosspoint so the
// controller can confirm the build step.
//
// Reference: SW-P-08 §3.3.24.
type SalvoConnectOnGoAckParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
	SourceID      uint16
	SalvoID       uint8
}

// EncodeSalvoConnectOnGoAck packs tx 122 — same byte layout as rx 120.
//
// Spec: SW-P-08 §3.3.24.
func EncodeSalvoConnectOnGoAck(p SalvoConnectOnGoAckParams) Frame {
	return Frame{
		ID: TxSalvoConnectOnGoAck,
		Payload: []byte{
			encodeMatrixLevel(p.MatrixID, p.LevelID),
			byte(((p.DestinationID/128)<<4)&0x70) | byte((p.SourceID/128)&0x07),
			byte(p.DestinationID % 128),
			byte(p.SourceID % 128),
			p.SalvoID & 0x7F,
		},
	}
}

// DecodeSalvoConnectOnGoAck parses tx 122.
func DecodeSalvoConnectOnGoAck(f Frame) (SalvoConnectOnGoAckParams, error) {
	if f.ID != TxSalvoConnectOnGoAck {
		return SalvoConnectOnGoAckParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 5 {
		return SalvoConnectOnGoAckParams{}, ErrShortPayload
	}
	m, l := decodeMatrixLevel(f.Payload[0])
	return SalvoConnectOnGoAckParams{
		MatrixID:      m,
		LevelID:       l,
		DestinationID: (uint16(f.Payload[1]>>4) & 0x07) * 128 + uint16(f.Payload[2]),
		SourceID:      (uint16(f.Payload[1]) & 0x07) * 128 + uint16(f.Payload[3]),
		SalvoID:       f.Payload[4] & 0x7F,
	}, nil
}
