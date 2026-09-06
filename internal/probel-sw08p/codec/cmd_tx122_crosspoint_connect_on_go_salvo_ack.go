package codec

// SalvoConnectOnGoAckParams: tx 122 / tx 250 CROSSPOINT CONNECT ON GO
// GROUP SALVO ACKNOWLEDGE. Matrix echoes the stored crosspoint so the
// controller can confirm the build step. The wire form pairs the rx 120
// / rx 248 form chosen by the controller.
//
// Reference: SW-P-08 Issue 30 §3.3.24 (general) + §3.5.13 (extended).
type SalvoConnectOnGoAckParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16 // narrow: 0-895; extended: 0-65535
	SourceID      uint16 // narrow: 0-1023; extended: 0-65535
	SalvoID       uint8  // 0-127
}

// needsExtended mirrors SalvoConnectOnGoParams.needsExtended — the
// ack form must match the request form so a controller waiting on
// tx 122 doesn't see tx 250 (or vice versa).
func (p SalvoConnectOnGoAckParams) needsExtended() bool {
	return p.MatrixID > 15 ||
		p.LevelID > 15 ||
		p.DestinationID > 895 ||
		p.SourceID > 1023
}

// EncodeSalvoConnectOnGoAck packs tx 122 (general) or tx 250 (extended).
// Wire layout matches rx 120 / rx 248 byte-for-byte; only the command
// ID byte differs (TxSalvoConnectOnGoAck = 0x7A general,
// TxSalvoConnectOnGoAckExt = 0xFA extended).
//
// Spec: SW-P-08 §3.3.24 + §3.5.13.
func EncodeSalvoConnectOnGoAck(p SalvoConnectOnGoAckParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: TxSalvoConnectOnGoAckExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.DestinationID / 256),
				byte(p.DestinationID % 256),
				byte(p.SourceID / 256),
				byte(p.SourceID % 256),
				p.SalvoID & 0x7F,
			},
		}
	}
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

// DecodeSalvoConnectOnGoAck parses tx 122 (general 5-byte) or tx 250
// (extended 7-byte) into the same logical struct.
func DecodeSalvoConnectOnGoAck(f Frame) (SalvoConnectOnGoAckParams, error) {
	switch f.ID {
	case TxSalvoConnectOnGoAck:
		if len(f.Payload) < 5 {
			return SalvoConnectOnGoAckParams{}, ErrShortPayload
		}
		m, l := decodeMatrixLevel(f.Payload[0])
		return SalvoConnectOnGoAckParams{
			MatrixID:      m,
			LevelID:       l,
			DestinationID: (uint16(f.Payload[1]>>4)&0x07)*128 + uint16(f.Payload[2]),
			SourceID:      (uint16(f.Payload[1])&0x07)*128 + uint16(f.Payload[3]),
			SalvoID:       f.Payload[4] & 0x7F,
		}, nil
	case TxSalvoConnectOnGoAckExt:
		if len(f.Payload) < 7 {
			return SalvoConnectOnGoAckParams{}, ErrShortPayload
		}
		return SalvoConnectOnGoAckParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
			SourceID:      uint16(f.Payload[4])*256 + uint16(f.Payload[5]),
			SalvoID:       f.Payload[6] & 0x7F,
		}, nil
	default:
		return SalvoConnectOnGoAckParams{}, ErrWrongCommand
	}
}
