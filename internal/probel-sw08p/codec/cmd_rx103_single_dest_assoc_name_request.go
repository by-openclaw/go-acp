package codec

// SingleDestAssocNameRequestParams: rx 103 / rx 231 — fetch one
// destination-association name. Like rx 102, byte 1 is matrix-only;
// AssocID is u16 split DIV/MOD 256 in BOTH forms, so AssocID never
// triggers extended on its own.
//
// Reference: SW-P-08 Issue 30 §3.2.21 (general) + §3.4.13 (extended).
type SingleDestAssocNameRequestParams struct {
	MatrixID          uint8
	NameLength        NameLength
	DestAssociationID uint16
}

// needsExtended fires when matrix exceeds the narrow form's 4-bit
// packing in byte 1 (§3.2.21).
func (p SingleDestAssocNameRequestParams) needsExtended() bool {
	return p.MatrixID > 15
}

// EncodeSingleDestAssocNameRequest packs cmd 103 (general, 4-byte) or
// cmd 231 (extended, 4-byte but full 8-bit matrix).
//
// General form (CommandID 0x67 — 4 data bytes, §3.2.21):
//
//	| Byte | Field            | Notes                                     |
//	|------|------------------|-------------------------------------------|
//	|  1   | Matrix           | bits[0-3] = Matrix                           |
//	|  2   | Name Length      | 0=4, 1=8, 2=12                               |
//	|  3   | Dest assoc mult  | AssocID DIV 256                              |
//	|  4   | Dest assoc num   | AssocID MOD 256                              |
//
// Extended form (CommandID 0xE7 — 4 data bytes, §3.4.13):
//
//	| Byte | Field            | Notes                                     |
//	|------|------------------|-------------------------------------------|
//	|  1   | Matrix           | full 8-bit                                |
//	|  2   | Name Length      | 0=4, 1=8, 2=12                            |
//	|  3   | Dest assoc mult  | AssocID DIV 256                           |
//	|  4   | Dest assoc num   | AssocID MOD 256                           |
func EncodeSingleDestAssocNameRequest(p SingleDestAssocNameRequestParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: RxSingleDestNameRequestExt,
			Payload: []byte{
				p.MatrixID,
				byte(p.NameLength),
				byte(p.DestAssociationID / 256),
				byte(p.DestAssociationID % 256),
			},
		}
	}
	return Frame{
		ID: RxSingleDestNameRequest,
		Payload: []byte{
			p.MatrixID & 0x0F,
			byte(p.NameLength),
			byte(p.DestAssociationID / 256),
			byte(p.DestAssociationID % 256),
		},
	}
}

// DecodeSingleDestAssocNameRequest parses cmd 103 (general 4-byte) or
// cmd 231 (extended 4-byte) into the same logical struct.
func DecodeSingleDestAssocNameRequest(f Frame) (SingleDestAssocNameRequestParams, error) {
	switch f.ID {
	case RxSingleDestNameRequest:
		if len(f.Payload) < 4 {
			return SingleDestAssocNameRequestParams{}, ErrShortPayload
		}
		n := NameLength(f.Payload[1])
		if err := validateNameLength(n); err != nil {
			return SingleDestAssocNameRequestParams{}, err
		}
		return SingleDestAssocNameRequestParams{
			MatrixID:          f.Payload[0] & 0x0F,
			NameLength:        n,
			DestAssociationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
		}, nil
	case RxSingleDestNameRequestExt:
		if len(f.Payload) < 4 {
			return SingleDestAssocNameRequestParams{}, ErrShortPayload
		}
		n := NameLength(f.Payload[1])
		if err := validateNameLength(n); err != nil {
			return SingleDestAssocNameRequestParams{}, err
		}
		return SingleDestAssocNameRequestParams{
			MatrixID:          f.Payload[0],
			NameLength:        n,
			DestAssociationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
		}, nil
	default:
		return SingleDestAssocNameRequestParams{}, ErrWrongCommand
	}
}
