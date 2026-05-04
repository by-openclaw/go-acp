package codec

// SingleSourceNameRequestParams: rx 101 / rx 229 — fetch exactly one
// source's name.  The narrow form already carries SourceID as a 16-bit
// DIV/MOD 256 split, so SourceID alone never triggers extended; the
// promotion fires only on Matrix > 15 or Level > 15 (§3.1.2's 4-bit
// packing).
//
// Reference: SW-P-08 Issue 30 §3.2.19 (general) + §3.4.11 (extended).
type SingleSourceNameRequestParams struct {
	MatrixID   uint8
	LevelID    uint8
	NameLength NameLength
	SourceID   uint16 // 0-65535 in BOTH forms
}

// needsExtended fires when matrix or level exceeds the narrow form's
// 4-bit packing in byte 1 (§3.1.2). SourceID is 16-bit in both forms,
// so it does not promote on its own.
func (p SingleSourceNameRequestParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15
}

// EncodeSingleSourceNameRequest packs cmd 101 (general, 4-byte) or
// cmd 229 (extended, 5-byte) per the addressing range of the params.
//
// General form (CommandID 0x65 — 4 data bytes, §3.2.19):
//
//	| Byte | Field          | Notes                                       |
//	|------|----------------|---------------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level          |
//	|  2   | Name Length    | 0=4, 1=8, 2=12                                 |
//	|  3   | Src mult       | SourceID DIV 256                               |
//	|  4   | Src num        | SourceID MOD 256                               |
//
// Extended form (CommandID 0xE5 — 5 data bytes, §3.4.11):
//
//	| Byte | Field          | Notes                                       |
//	|------|----------------|---------------------------------------------|
//	|  1   | Matrix         | full 8-bit                                   |
//	|  2   | Level          | full 8-bit                                   |
//	|  3   | Name Length    | 0=4, 1=8, 2=12                              |
//	|  4   | Src mult       | SourceID DIV 256                            |
//	|  5   | Src num        | SourceID MOD 256                            |
func EncodeSingleSourceNameRequest(p SingleSourceNameRequestParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: RxSingleSourceNameRequestExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.NameLength),
				byte(p.SourceID / 256),
				byte(p.SourceID % 256),
			},
		}
	}
	return Frame{
		ID: RxSingleSourceNameRequest,
		Payload: []byte{
			encodeMatrixLevel(p.MatrixID, p.LevelID),
			byte(p.NameLength),
			byte(p.SourceID / 256),
			byte(p.SourceID % 256),
		},
	}
}

// DecodeSingleSourceNameRequest parses cmd 101 (general 4-byte) or
// cmd 229 (extended 5-byte) into the same logical struct.
func DecodeSingleSourceNameRequest(f Frame) (SingleSourceNameRequestParams, error) {
	switch f.ID {
	case RxSingleSourceNameRequest:
		if len(f.Payload) < 4 {
			return SingleSourceNameRequestParams{}, ErrShortPayload
		}
		m, l := decodeMatrixLevel(f.Payload[0])
		n := NameLength(f.Payload[1])
		if err := validateNameLength(n); err != nil {
			return SingleSourceNameRequestParams{}, err
		}
		return SingleSourceNameRequestParams{
			MatrixID:   m,
			LevelID:    l,
			NameLength: n,
			SourceID:   uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
		}, nil
	case RxSingleSourceNameRequestExt:
		if len(f.Payload) < 5 {
			return SingleSourceNameRequestParams{}, ErrShortPayload
		}
		n := NameLength(f.Payload[2])
		if err := validateNameLength(n); err != nil {
			return SingleSourceNameRequestParams{}, err
		}
		return SingleSourceNameRequestParams{
			MatrixID:   f.Payload[0],
			LevelID:    f.Payload[1],
			NameLength: n,
			SourceID:   uint16(f.Payload[3])*256 + uint16(f.Payload[4]),
		}, nil
	default:
		return SingleSourceNameRequestParams{}, ErrWrongCommand
	}
}
