package probel

// AllSourceNamesRequestParams carries the inputs for rx 100 ALL SOURCE
// NAMES REQUEST: controller asks the matrix for the names of every
// source on one (matrix, level), in the given name-length format.
//
// Reference: SW-P-08 §3.2.18.
type AllSourceNamesRequestParams struct {
	MatrixID   uint8 // 0-15
	LevelID    uint8 // 0-15
	NameLength NameLength
}

// EncodeAllSourceNamesRequest packs rx 100 ALL SOURCE NAMES REQUEST.
//
// | Byte | Field          | Notes                                       |
// |------|----------------|---------------------------------------------|
// |  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level (§3.1.2) |
// |  2   | Name Length    | 0=4-char, 1=8-char, 2=12-char (§3.1.18)        |
//
// Spec: SW-P-08 §3.2.18.
func EncodeAllSourceNamesRequest(p AllSourceNamesRequestParams) Frame {
	return Frame{
		ID: RxAllSourceNamesRequest,
		Payload: []byte{
			encodeMatrixLevel(p.MatrixID, p.LevelID),
			byte(p.NameLength),
		},
	}
}

// DecodeAllSourceNamesRequest parses an rx 100 ALL SOURCE NAMES REQUEST.
func DecodeAllSourceNamesRequest(f Frame) (AllSourceNamesRequestParams, error) {
	if f.ID != RxAllSourceNamesRequest {
		return AllSourceNamesRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 2 {
		return AllSourceNamesRequestParams{}, ErrShortPayload
	}
	m, l := decodeMatrixLevel(f.Payload[0])
	n := NameLength(f.Payload[1])
	if err := validateNameLength(n); err != nil {
		return AllSourceNamesRequestParams{}, err
	}
	return AllSourceNamesRequestParams{MatrixID: m, LevelID: l, NameLength: n}, nil
}
