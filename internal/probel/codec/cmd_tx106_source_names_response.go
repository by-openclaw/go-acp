package codec

import "fmt"

// SourceNamesResponseParams: tx 106 — one frame of source names, as
// issued by the matrix in reply to rx 100 or rx 101. Large name tables
// require multiple frames; callers paginate with successive FirstSourceID.
//
// Names is a slice of the source labels in wire order; each entry is at
// most NameLength.Bytes() chars. Longer strings are truncated; shorter
// strings are right-padded with spaces.
//
// Reference: SW-P-08 §3.3.19.
type SourceNamesResponseParams struct {
	MatrixID      uint8
	LevelID       uint8
	NameLength    NameLength
	FirstSourceID uint16
	Names         []string
}

// EncodeSourceNamesResponse packs tx 106 SOURCE NAMES RESPONSE.
//
// | Byte     | Field               | Notes                                  |
// |----------|---------------------|----------------------------------------|
// |  1       | Matrix / Level      | bits[4-7] = Matrix, bits[0-3] = Level     |
// |  2       | Name Length         | 0=4, 1=8, 2=12                            |
// |  3       | 1st Src mult        | FirstSourceID DIV 256                     |
// |  4       | 1st Src num         | FirstSourceID MOD 256                     |
// |  5       | Num of names        | Names count to follow                     |
// |  6+      | N × Name (fixed)    | NameLength.Bytes() bytes each, ASCII      |
//
// Caps (spec §3.3.19): "max 32 × 4-char, 16 × 8-char, or 10 × 12-char
// names" per frame. Extras are silently dropped — callers paginate.
//
// Spec: SW-P-08 §3.3.19.
func EncodeSourceNamesResponse(p SourceNamesResponseParams) Frame {
	width := p.NameLength.Bytes()
	cap := p.NameLength.MaxNamesPerMessage()
	n := len(p.Names)
	if n > cap {
		n = cap
	}
	payload := make([]byte, 0, 5+n*width)
	payload = append(payload, encodeMatrixLevel(p.MatrixID, p.LevelID))
	payload = append(payload, byte(p.NameLength))
	payload = append(payload, byte(p.FirstSourceID/256))
	payload = append(payload, byte(p.FirstSourceID%256))
	payload = append(payload, byte(n))
	for i := 0; i < n; i++ {
		payload = append(payload, packName(p.Names[i], width)...)
	}
	return Frame{ID: TxSourceNamesResponse, Payload: payload}
}

// DecodeSourceNamesResponse parses tx 106.
func DecodeSourceNamesResponse(f Frame) (SourceNamesResponseParams, error) {
	if f.ID != TxSourceNamesResponse {
		return SourceNamesResponseParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 5 {
		return SourceNamesResponseParams{}, ErrShortPayload
	}
	m, l := decodeMatrixLevel(f.Payload[0])
	n := NameLength(f.Payload[1])
	if err := validateNameLength(n); err != nil {
		return SourceNamesResponseParams{}, err
	}
	first := uint16(f.Payload[2])*256 + uint16(f.Payload[3])
	count := int(f.Payload[4])
	width := n.Bytes()
	if len(f.Payload) < 5+count*width {
		return SourceNamesResponseParams{}, fmt.Errorf("probel: tx 106 needs %d bytes, got %d",
			5+count*width, len(f.Payload))
	}
	names := make([]string, count)
	for i := 0; i < count; i++ {
		off := 5 + i*width
		names[i] = unpackName(f.Payload[off : off+width])
	}
	return SourceNamesResponseParams{
		MatrixID:      m,
		LevelID:       l,
		NameLength:    n,
		FirstSourceID: first,
		Names:         names,
	}, nil
}
