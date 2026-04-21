package codec

import "fmt"

// SourceAssocNamesResponseParams: tx 116 — reply to rx 114 or rx 115.
// Byte 1 is Matrix/Level per §3.1.2 (response carries a level slot
// even though the request doesn't — mirrors the asymmetry in 102/107).
//
// Reference: SW-P-08 §3.3.22.
type SourceAssocNamesResponseParams struct {
	MatrixID                uint8
	LevelID                 uint8
	NameLength              NameLength
	FirstSourceAssociationID uint16
	Names                   []string
}

// EncodeSourceAssocNamesResponse packs tx 116.
//
// | Byte     | Field            | Notes                                |
// |----------|------------------|--------------------------------------|
// |  1       | Matrix / Level   | bits[4-7] Matrix, bits[0-3] Level      |
// |  2       | Name Length      | 0=4, 1=8, 2=12                         |
// |  3       | 1st Src mult     | FirstSourceAssociationID DIV 256       |
// |  4       | 1st Src num      | FirstSourceAssociationID MOD 256       |
// |  5       | Num of names     | Names count                            |
// |  6+      | N × Name (fixed) | NameLength.Bytes() bytes each          |
//
// Spec: SW-P-08 §3.3.22.
func EncodeSourceAssocNamesResponse(p SourceAssocNamesResponseParams) Frame {
	width := p.NameLength.Bytes()
	cap := p.NameLength.MaxNamesPerMessage()
	n := len(p.Names)
	if n > cap {
		n = cap
	}
	payload := make([]byte, 0, 5+n*width)
	payload = append(payload, encodeMatrixLevel(p.MatrixID, p.LevelID))
	payload = append(payload, byte(p.NameLength))
	payload = append(payload, byte(p.FirstSourceAssociationID/256))
	payload = append(payload, byte(p.FirstSourceAssociationID%256))
	payload = append(payload, byte(n))
	for i := 0; i < n; i++ {
		payload = append(payload, packName(p.Names[i], width)...)
	}
	return Frame{ID: TxSourceAssocNamesResponse, Payload: payload}
}

// DecodeSourceAssocNamesResponse parses tx 116.
func DecodeSourceAssocNamesResponse(f Frame) (SourceAssocNamesResponseParams, error) {
	if f.ID != TxSourceAssocNamesResponse {
		return SourceAssocNamesResponseParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 5 {
		return SourceAssocNamesResponseParams{}, ErrShortPayload
	}
	m, l := decodeMatrixLevel(f.Payload[0])
	n := NameLength(f.Payload[1])
	if err := validateNameLength(n); err != nil {
		return SourceAssocNamesResponseParams{}, err
	}
	first := uint16(f.Payload[2])*256 + uint16(f.Payload[3])
	count := int(f.Payload[4])
	width := n.Bytes()
	if len(f.Payload) < 5+count*width {
		return SourceAssocNamesResponseParams{}, fmt.Errorf("probel: tx 116 needs %d bytes, got %d",
			5+count*width, len(f.Payload))
	}
	names := make([]string, count)
	for i := 0; i < count; i++ {
		off := 5 + i*width
		names[i] = unpackName(f.Payload[off : off+width])
	}
	return SourceAssocNamesResponseParams{
		MatrixID:                m,
		LevelID:                 l,
		NameLength:              n,
		FirstSourceAssociationID: first,
		Names:                   names,
	}, nil
}
