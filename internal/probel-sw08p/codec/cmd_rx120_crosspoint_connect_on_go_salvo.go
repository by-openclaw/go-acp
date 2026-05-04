package codec

// SalvoConnectOnGoParams: rx 120 / rx 248 CROSSPOINT CONNECT ON GO
// GROUP SALVO. Builds up a salvo group by adding one crosspoint per
// frame. Routing information is held by the controller until rx 121
// CROSSPOINT GO GROUP SALVO fires.
//
// The encoder picks general (cmd 120, narrow) or extended (cmd 248,
// wide) form per the SW-P-08 §3.4.16 ranges — same logical struct,
// two wire forms. The decoder accepts both IDs and produces the same
// struct.
//
// Reference: SW-P-08 Issue 30 §3.2.29 (general) + §3.4.16 (extended).
// Spec note (both sections): "The group salvo commands are only
// implemented on the XD and ECLIPSE router ranges."
type SalvoConnectOnGoParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16 // narrow: 0-895; extended: 0-65535
	SourceID      uint16 // narrow: 0-1023; extended: 0-65535
	SalvoID       uint8  // 0-127 (bit 7 = 0 in both forms)
}

// needsExtended returns true when any addressing field exceeds the
// narrow ranges encodable by cmd 120 (§3.2.29). Narrow ceilings
// (per §3.1.2): matrix > 15, level > 15, dst > 895, src > 1023.
//
// SalvoID is bounded to 0-127 in BOTH narrow and extended forms,
// so it does not promote to extended on its own.
func (p SalvoConnectOnGoParams) needsExtended() bool {
	return p.MatrixID > 15 ||
		p.LevelID > 15 ||
		p.DestinationID > 895 ||
		p.SourceID > 1023
}

// EncodeSalvoConnectOnGo packs cmd 120 (general) or cmd 248 (extended)
// per the addressing range of the params.
//
// General form (CommandID 0x78 — 5 data bytes, §3.2.29):
//
//	| Byte | Field          | Notes                                     |
//	|------|----------------|-------------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level     |
//	|  2   | Multiplier     | bits[4-6] = Dest DIV 128                  |
//	|      |                | bits[0-2] = Source DIV 128                |
//	|  3   | Dest num       | Dest MOD 128                              |
//	|  4   | Src num        | Source MOD 128                            |
//	|  5   | Salvo num      | bit 7 reserved = 0; bits[0-6] SalvoID     |
//
// Extended form (CommandID 0xF8 — 7 data bytes, §3.4.16):
//
//	| Byte | Field           | Notes                                    |
//	|------|-----------------|------------------------------------------|
//	|  1   | Matrix          | full 8-bit                               |
//	|  2   | Level           | full 8-bit                               |
//	|  3   | Dest mult       | Destination DIV 256                      |
//	|  4   | Dest num        | Destination MOD 256                      |
//	|  5   | Src mult        | Source DIV 256                           |
//	|  6   | Src num         | Source MOD 256                           |
//	|  7   | Salvo num       | bit 7 = 0; bits[0-6] SalvoID             |
func EncodeSalvoConnectOnGo(p SalvoConnectOnGoParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: RxCrosspointConnectOnGoSalvoExt,
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

// DecodeSalvoConnectOnGo parses cmd 120 (general 5-byte) or cmd 248
// (extended 7-byte) and produces the same logical struct.
func DecodeSalvoConnectOnGo(f Frame) (SalvoConnectOnGoParams, error) {
	switch f.ID {
	case RxCrosspointConnectOnGoSalvo:
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
	case RxCrosspointConnectOnGoSalvoExt:
		if len(f.Payload) < 7 {
			return SalvoConnectOnGoParams{}, ErrShortPayload
		}
		return SalvoConnectOnGoParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
			SourceID:      uint16(f.Payload[4])*256 + uint16(f.Payload[5]),
			SalvoID:       f.Payload[6] & 0x7F,
		}, nil
	default:
		return SalvoConnectOnGoParams{}, ErrWrongCommand
	}
}
