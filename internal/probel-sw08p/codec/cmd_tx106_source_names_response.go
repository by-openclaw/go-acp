package codec

import "fmt"

// SourceNamesResponseParams: tx 106 / tx 234 — one frame of source
// names, as issued by the matrix in reply to rx 100/101 (or
// rx 228/229). Large name tables require multiple frames; callers
// paginate with successive FirstSourceID.
//
// Names is a slice of the source labels in wire order; each entry is
// at most NameLength.Bytes() chars. Longer strings are truncated;
// shorter strings are right-padded with spaces.
//
// Reference: SW-P-08 Issue 30 §3.3.19 (general) + §3.5.10 (extended).
type SourceNamesResponseParams struct {
	MatrixID      uint8
	LevelID       uint8
	NameLength    NameLength
	FirstSourceID uint16
	Names         []string
}

// needsExtended mirrors the request-side rule (§3.1.2 4-bit packing):
// matrix > 15 OR level > 15 promotes to extended. FirstSourceID is
// 16-bit in BOTH forms, so it does not promote on its own.
func (p SourceNamesResponseParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15
}

// EncodeSourceNamesResponse packs cmd 106 (general) or cmd 234
// (extended) per the addressing range of the params.
//
// General form (CommandID 0x6A — §3.3.19):
//
//	| Byte     | Field               | Notes                                  |
//	|----------|---------------------|----------------------------------------|
//	|  1       | Matrix / Level      | bits[4-7] = Matrix, bits[0-3] = Level     |
//	|  2       | Name Length         | 0=4, 1=8, 2=12                            |
//	|  3       | 1st Src mult        | FirstSourceID DIV 256                     |
//	|  4       | 1st Src num         | FirstSourceID MOD 256                     |
//	|  5       | Num of names        | Names count to follow                     |
//	|  6+      | N × Name (fixed)    | NameLength.Bytes() bytes each             |
//
// Extended form (CommandID 0xEA — §3.5.10):
//
//	| Byte     | Field               | Notes                                  |
//	|----------|---------------------|----------------------------------------|
//	|  1       | Matrix              | full 8-bit                             |
//	|  2       | Level               | full 8-bit                             |
//	|  3       | Name Length         | 0=4, 1=8, 2=12                         |
//	|  4       | 1st Src mult        | FirstSourceID DIV 256                  |
//	|  5       | 1st Src num         | FirstSourceID MOD 256                  |
//	|  6       | Num of names        | Names count to follow                  |
//	|  7+      | N × Name (fixed)    | NameLength.Bytes() bytes each          |
//
// Caps (spec §3.3.19 / §3.5.10): "max 32 × 4-char, 16 × 8-char, or
// 10 × 12-char names" per frame. Extras are silently dropped — callers
// paginate via FirstSourceID.
func EncodeSourceNamesResponse(p SourceNamesResponseParams) Frame {
	width := p.NameLength.Bytes()
	maxN := p.NameLength.MaxNamesPerMessage()
	n := len(p.Names)
	if n > maxN {
		n = maxN
	}
	if p.needsExtended() {
		payload := make([]byte, 0, 6+n*width)
		payload = append(payload,
			p.MatrixID,
			p.LevelID,
			byte(p.NameLength),
			byte(p.FirstSourceID/256),
			byte(p.FirstSourceID%256),
			byte(n),
		)
		for i := 0; i < n; i++ {
			payload = append(payload, packName(p.Names[i], width)...)
		}
		return Frame{ID: TxSourceNamesResponseExt, Payload: payload}
	}
	payload := make([]byte, 0, 5+n*width)
	payload = append(payload,
		encodeMatrixLevel(p.MatrixID, p.LevelID),
		byte(p.NameLength),
		byte(p.FirstSourceID/256),
		byte(p.FirstSourceID%256),
		byte(n),
	)
	for i := 0; i < n; i++ {
		payload = append(payload, packName(p.Names[i], width)...)
	}
	return Frame{ID: TxSourceNamesResponse, Payload: payload}
}

// DecodeSourceNamesResponse parses tx 106 (general) or tx 234
// (extended) into the same logical struct.
func DecodeSourceNamesResponse(f Frame) (SourceNamesResponseParams, error) {
	switch f.ID {
	case TxSourceNamesResponse:
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
	case TxSourceNamesResponseExt:
		if len(f.Payload) < 6 {
			return SourceNamesResponseParams{}, ErrShortPayload
		}
		n := NameLength(f.Payload[2])
		if err := validateNameLength(n); err != nil {
			return SourceNamesResponseParams{}, err
		}
		first := uint16(f.Payload[3])*256 + uint16(f.Payload[4])
		count := int(f.Payload[5])
		width := n.Bytes()
		if len(f.Payload) < 6+count*width {
			return SourceNamesResponseParams{}, fmt.Errorf("probel: tx 234 needs %d bytes, got %d",
				6+count*width, len(f.Payload))
		}
		names := make([]string, count)
		for i := 0; i < count; i++ {
			off := 6 + i*width
			names[i] = unpackName(f.Payload[off : off+width])
		}
		return SourceNamesResponseParams{
			MatrixID:      f.Payload[0],
			LevelID:       f.Payload[1],
			NameLength:    n,
			FirstSourceID: first,
			Names:         names,
		}, nil
	default:
		return SourceNamesResponseParams{}, ErrWrongCommand
	}
}
