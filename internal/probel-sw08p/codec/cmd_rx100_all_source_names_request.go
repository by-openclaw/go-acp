package codec

// AllSourceNamesRequestParams carries the inputs for rx 100 / rx 228
// ALL SOURCE NAMES REQUEST: controller asks the matrix for the names
// of every source on one (matrix, level), in the given name-length
// format. The encoder picks general (cmd 100, narrow) or extended
// (cmd 228, wide) form per the SW-P-08 §3.4.10 ranges — same logical
// struct, two wire forms.
//
// Reference: SW-P-08 Issue 30 §3.2.18 (general) + §3.4.10 (extended).
type AllSourceNamesRequestParams struct {
	MatrixID   uint8 // narrow: 0-15 (4 bits); extended: 0-255 (full byte)
	LevelID    uint8 // narrow: 0-15 (4 bits); extended: 0-255 (full byte)
	NameLength NameLength
}

// needsExtended fires when matrix or level exceeds the narrow form's
// 4-bit packing in byte 1 (§3.1.2).
func (p AllSourceNamesRequestParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15
}

// EncodeAllSourceNamesRequest packs cmd 100 (general, 2-byte) or
// cmd 228 (extended, 3-byte) per the addressing range of the params.
//
// General form (CommandID 0x64 — 2 data bytes, §3.2.18):
//
//	| Byte | Field          | Notes                                       |
//	|------|----------------|---------------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level (§3.1.2) |
//	|  2   | Name Length    | 0=4-char, 1=8-char, 2=12-char (§3.1.18)        |
//
// Extended form (CommandID 0xE4 — 3 data bytes, §3.4.10):
//
//	| Byte | Field          | Notes                                       |
//	|------|----------------|---------------------------------------------|
//	|  1   | Matrix         | full 8-bit                                   |
//	|  2   | Level          | full 8-bit                                   |
//	|  3   | Name Length    | 0=4-char, 1=8-char, 2=12-char                |
func EncodeAllSourceNamesRequest(p AllSourceNamesRequestParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: RxAllSourceNamesRequestExt,
			Payload: []byte{
				p.MatrixID,
				p.LevelID,
				byte(p.NameLength),
			},
		}
	}
	return Frame{
		ID: RxAllSourceNamesRequest,
		Payload: []byte{
			encodeMatrixLevel(p.MatrixID, p.LevelID),
			byte(p.NameLength),
		},
	}
}

// DecodeAllSourceNamesRequest parses cmd 100 (general 2-byte) or
// cmd 228 (extended 3-byte) into the same logical struct.
func DecodeAllSourceNamesRequest(f Frame) (AllSourceNamesRequestParams, error) {
	switch f.ID {
	case RxAllSourceNamesRequest:
		if len(f.Payload) < 2 {
			return AllSourceNamesRequestParams{}, ErrShortPayload
		}
		m, l := decodeMatrixLevel(f.Payload[0])
		n := NameLength(f.Payload[1])
		if err := validateNameLength(n); err != nil {
			return AllSourceNamesRequestParams{}, err
		}
		return AllSourceNamesRequestParams{MatrixID: m, LevelID: l, NameLength: n}, nil
	case RxAllSourceNamesRequestExt:
		if len(f.Payload) < 3 {
			return AllSourceNamesRequestParams{}, ErrShortPayload
		}
		n := NameLength(f.Payload[2])
		if err := validateNameLength(n); err != nil {
			return AllSourceNamesRequestParams{}, err
		}
		return AllSourceNamesRequestParams{
			MatrixID:   f.Payload[0],
			LevelID:    f.Payload[1],
			NameLength: n,
		}, nil
	default:
		return AllSourceNamesRequestParams{}, ErrWrongCommand
	}
}
