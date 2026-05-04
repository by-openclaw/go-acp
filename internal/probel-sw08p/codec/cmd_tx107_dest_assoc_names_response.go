package codec

import "fmt"

// DestAssocNamesResponseParams: tx 107 / tx 235 — matrix's reply to
// rx 102/103 (or rx 230/231). Byte 1 is Matrix/Level per §3.1.2 (yes,
// asymmetric with the request — the response carries a level slot,
// the request doesn't). Both narrow and extended responses include
// the level field; the request side just doesn't.
//
// Reference: SW-P-08 Issue 30 §3.3.20 (general) + §3.5.11 (extended).
type DestAssocNamesResponseParams struct {
	MatrixID               uint8
	LevelID                uint8
	NameLength             NameLength
	FirstDestAssociationID uint16
	Names                  []string
}

// needsExtended mirrors the request-side rule: matrix > 15 OR level >
// 15 promotes to extended. FirstDestAssociationID is 16-bit in BOTH
// forms, so it does not promote on its own.
func (p DestAssocNamesResponseParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15
}

// EncodeDestAssocNamesResponse packs cmd 107 (general) or cmd 235
// (extended) per the addressing range of the params.
//
// General form (CommandID 0x6B — §3.3.20):
//
//	| Byte     | Field            | Notes                                    |
//	|----------|------------------|------------------------------------------|
//	|  1       | Matrix / Level   | bits[4-7] Matrix, bits[0-3] Level (§3.1.2) |
//	|  2       | Name Length      | 0=4, 1=8, 2=12                              |
//	|  3       | 1st Dest mult    | FirstDestAssociationID DIV 256              |
//	|  4       | 1st Dest num     | FirstDestAssociationID MOD 256              |
//	|  5       | Num of names     | Names count to follow                       |
//	|  6+      | N × Name (fixed) | NameLength.Bytes() bytes each               |
//
// Extended form (CommandID 0xEB — §3.5.11):
//
//	| Byte     | Field            | Notes                                    |
//	|----------|------------------|------------------------------------------|
//	|  1       | Matrix           | full 8-bit                               |
//	|  2       | Level            | full 8-bit                               |
//	|  3       | Name Length      | 0=4, 1=8, 2=12                           |
//	|  4       | 1st Dest mult    | FirstDestAssociationID DIV 256           |
//	|  5       | 1st Dest num     | FirstDestAssociationID MOD 256           |
//	|  6       | Num of names     | Names count to follow                    |
//	|  7+      | N × Name (fixed) | NameLength.Bytes() bytes each            |
func EncodeDestAssocNamesResponse(p DestAssocNamesResponseParams) Frame {
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
			byte(p.FirstDestAssociationID/256),
			byte(p.FirstDestAssociationID%256),
			byte(n),
		)
		for i := 0; i < n; i++ {
			payload = append(payload, packName(p.Names[i], width)...)
		}
		return Frame{ID: TxDestAssocNamesResponseExt, Payload: payload}
	}
	payload := make([]byte, 0, 5+n*width)
	payload = append(payload,
		encodeMatrixLevel(p.MatrixID, p.LevelID),
		byte(p.NameLength),
		byte(p.FirstDestAssociationID/256),
		byte(p.FirstDestAssociationID%256),
		byte(n),
	)
	for i := 0; i < n; i++ {
		payload = append(payload, packName(p.Names[i], width)...)
	}
	return Frame{ID: TxDestAssocNamesResponse, Payload: payload}
}

// DecodeDestAssocNamesResponse parses tx 107 (general) or tx 235
// (extended) into the same logical struct.
func DecodeDestAssocNamesResponse(f Frame) (DestAssocNamesResponseParams, error) {
	switch f.ID {
	case TxDestAssocNamesResponse:
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
			MatrixID:               m,
			LevelID:                l,
			NameLength:             n,
			FirstDestAssociationID: first,
			Names:                  names,
		}, nil
	case TxDestAssocNamesResponseExt:
		if len(f.Payload) < 6 {
			return DestAssocNamesResponseParams{}, ErrShortPayload
		}
		n := NameLength(f.Payload[2])
		if err := validateNameLength(n); err != nil {
			return DestAssocNamesResponseParams{}, err
		}
		first := uint16(f.Payload[3])*256 + uint16(f.Payload[4])
		count := int(f.Payload[5])
		width := n.Bytes()
		if len(f.Payload) < 6+count*width {
			return DestAssocNamesResponseParams{}, fmt.Errorf("probel: tx 235 needs %d bytes, got %d",
				6+count*width, len(f.Payload))
		}
		names := make([]string, count)
		for i := 0; i < count; i++ {
			off := 6 + i*width
			names[i] = unpackName(f.Payload[off : off+width])
		}
		return DestAssocNamesResponseParams{
			MatrixID:               f.Payload[0],
			LevelID:                f.Payload[1],
			NameLength:             n,
			FirstDestAssociationID: first,
			Names:                  names,
		}, nil
	default:
		return DestAssocNamesResponseParams{}, ErrWrongCommand
	}
}
