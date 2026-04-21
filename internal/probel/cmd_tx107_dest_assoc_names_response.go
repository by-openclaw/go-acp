package probel

import "fmt"

// DestAssocNamesResponseParams: tx 107 — matrix's reply to rx 102 or
// rx 103. Byte 1 is Matrix/Level per §3.1.2 (yes, asymmetric with the
// request — the response carries a level slot, the request doesn't).
//
// Reference: SW-P-08 §3.3.20.
type DestAssocNamesResponseParams struct {
	MatrixID              uint8
	LevelID               uint8
	NameLength            NameLength
	FirstDestAssociationID uint16
	Names                 []string
}

// EncodeDestAssocNamesResponse packs tx 107.
//
// | Byte     | Field            | Notes                                    |
// |----------|------------------|------------------------------------------|
// |  1       | Matrix / Level   | bits[4-7] Matrix, bits[0-3] Level (§3.1.2) |
// |  2       | Name Length      | 0=4, 1=8, 2=12                              |
// |  3       | 1st Dest mult    | FirstDestAssociationID DIV 256              |
// |  4       | 1st Dest num     | FirstDestAssociationID MOD 256              |
// |  5       | Num of names     | Names count to follow                       |
// |  6+      | N × Name (fixed) | NameLength.Bytes() bytes each, ASCII        |
//
// Spec: SW-P-08 §3.3.20.
func EncodeDestAssocNamesResponse(p DestAssocNamesResponseParams) Frame {
	width := p.NameLength.Bytes()
	cap := p.NameLength.MaxNamesPerMessage()
	n := len(p.Names)
	if n > cap {
		n = cap
	}
	payload := make([]byte, 0, 5+n*width)
	payload = append(payload, encodeMatrixLevel(p.MatrixID, p.LevelID))
	payload = append(payload, byte(p.NameLength))
	payload = append(payload, byte(p.FirstDestAssociationID/256))
	payload = append(payload, byte(p.FirstDestAssociationID%256))
	payload = append(payload, byte(n))
	for i := 0; i < n; i++ {
		payload = append(payload, packName(p.Names[i], width)...)
	}
	return Frame{ID: TxDestAssocNamesResponse, Payload: payload}
}

// DecodeDestAssocNamesResponse parses tx 107.
func DecodeDestAssocNamesResponse(f Frame) (DestAssocNamesResponseParams, error) {
	if f.ID != TxDestAssocNamesResponse {
		return DestAssocNamesResponseParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 5 {
		return DestAssocNamesResponseParams{}, ErrShortPayload
	}
	m, l := decodeMatrixLevel(f.Payload[0])
	n := NameLength(f.Payload[1])
	if err := validateNameLength(n); err != nil {
		return DestAssocNamesResponseParams{}, err
	}
	first := uint16(f.Payload[2])*256 + uint16(f.Payload[3])
	count := int(f.Payload[4])
	width := n.Bytes()
	if len(f.Payload) < 5+count*width {
		return DestAssocNamesResponseParams{}, fmt.Errorf("probel: tx 107 needs %d bytes, got %d",
			5+count*width, len(f.Payload))
	}
	names := make([]string, count)
	for i := 0; i < count; i++ {
		off := 5 + i*width
		names[i] = unpackName(f.Payload[off : off+width])
	}
	return DestAssocNamesResponseParams{
		MatrixID:              m,
		LevelID:               l,
		NameLength:            n,
		FirstDestAssociationID: first,
		Names:                 names,
	}, nil
}
