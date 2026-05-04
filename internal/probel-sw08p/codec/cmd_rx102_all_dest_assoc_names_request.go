package codec

// AllDestAssocNamesRequestParams: rx 102 / rx 230 — fetch every
// destination-association name on a given matrix. The narrow form
// packs Matrix into bits 0-3 of byte 1 (§3.2.20); extended uses a full
// 8-bit byte. There is no Level in either form — dest-assoc is matrix-
// scoped, not matrix+level scoped.
//
// Reference: SW-P-08 Issue 30 §3.2.20 (general) + §3.4.12 (extended).
type AllDestAssocNamesRequestParams struct {
	MatrixID   uint8 // narrow: 0-15 (bits 0-3); extended: 0-255 (full byte)
	NameLength NameLength
}

// needsExtended fires when matrix exceeds the narrow form's 4-bit
// packing in byte 1 (§3.2.20: bits 0-3 used; bits 4-7 unused).
func (p AllDestAssocNamesRequestParams) needsExtended() bool {
	return p.MatrixID > 15
}

// EncodeAllDestAssocNamesRequest packs cmd 102 (general, 2-byte) or
// cmd 230 (extended, 2-byte but full 8-bit matrix).
//
// General form (CommandID 0x66 — 2 data bytes, §3.2.20):
//
//	| Byte | Field        | Notes                                     |
//	|------|--------------|-------------------------------------------|
//	|  1   | Matrix       | bits[0-3] = Matrix; bits[4-7] unused         |
//	|  2   | Name Length  | 0=4, 1=8, 2=12                               |
//
// Extended form (CommandID 0xE6 — 2 data bytes, §3.4.12):
//
//	| Byte | Field        | Notes                                     |
//	|------|--------------|-------------------------------------------|
//	|  1   | Matrix       | full 8-bit                                |
//	|  2   | Name Length  | 0=4, 1=8, 2=12                            |
func EncodeAllDestAssocNamesRequest(p AllDestAssocNamesRequestParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID:      RxAllDestNamesRequestExt,
			Payload: []byte{p.MatrixID, byte(p.NameLength)},
		}
	}
	return Frame{
		ID:      RxAllDestNamesRequest,
		Payload: []byte{p.MatrixID & 0x0F, byte(p.NameLength)},
	}
}

// DecodeAllDestAssocNamesRequest parses cmd 102 (general 2-byte) or
// cmd 230 (extended 2-byte) into the same logical struct.
func DecodeAllDestAssocNamesRequest(f Frame) (AllDestAssocNamesRequestParams, error) {
	switch f.ID {
	case RxAllDestNamesRequest:
		if len(f.Payload) < 2 {
			return AllDestAssocNamesRequestParams{}, ErrShortPayload
		}
		n := NameLength(f.Payload[1])
		if err := validateNameLength(n); err != nil {
			return AllDestAssocNamesRequestParams{}, err
		}
		return AllDestAssocNamesRequestParams{
			MatrixID:   f.Payload[0] & 0x0F,
			NameLength: n,
		}, nil
	case RxAllDestNamesRequestExt:
		if len(f.Payload) < 2 {
			return AllDestAssocNamesRequestParams{}, ErrShortPayload
		}
		n := NameLength(f.Payload[1])
		if err := validateNameLength(n); err != nil {
			return AllDestAssocNamesRequestParams{}, err
		}
		return AllDestAssocNamesRequestParams{
			MatrixID:   f.Payload[0],
			NameLength: n,
		}, nil
	default:
		return AllDestAssocNamesRequestParams{}, ErrWrongCommand
	}
}
