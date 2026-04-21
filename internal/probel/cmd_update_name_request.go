package probel

import "fmt"

// UpdateNameType enumerates the four name classes rx 117 can update.
// Matches the NameType byte in SW-P-08 §3.2.26.
type UpdateNameType uint8

const (
	UpdateNameSource       UpdateNameType = 0x00
	UpdateNameSourceAssoc  UpdateNameType = 0x01
	UpdateNameDestAssoc    UpdateNameType = 0x02
	UpdateNameUMDLabel     UpdateNameType = 0x03
)

// UpdateNameRequestParams: rx 117 UPDATE NAME REQUEST. No response.
//
// Reference: SW-P-08 §3.2.26.
type UpdateNameRequestParams struct {
	NameType   UpdateNameType
	NameLength NameLength
	MatrixID   uint8
	LevelID    uint8 // only meaningful when NameType == UpdateNameSource
	FirstID    uint16
	Names      []string
}

// EncodeUpdateNameRequest packs rx 117.
//
// | Byte    | Field          | Notes                                  |
// |---------|----------------|----------------------------------------|
// |  1      | Name Type      | 0=Src, 1=SrcAssoc, 2=DestAssoc, 3=UMD     |
// |  2      | Name Length    | 0=4, 1=8, 2=12, 3=16                      |
// |  3      | Matrix         |                                           |
// |  4      | Level          | Source-name only (other types: pass 0)    |
// |  5      | 1st name mult  | FirstID DIV 256                           |
// |  6      | 1st name num   | FirstID MOD 256                           |
// |  7      | Num of names   |                                           |
// |  8+     | N × Name       | NameLength.Bytes() bytes each             |
//
// Spec: SW-P-08 §3.2.26.
func EncodeUpdateNameRequest(p UpdateNameRequestParams) Frame {
	width := p.NameLength.Bytes()
	cap := p.NameLength.MaxNamesPerMessage()
	n := len(p.Names)
	if n > cap {
		n = cap
	}
	payload := make([]byte, 0, 7+n*width)
	payload = append(payload, byte(p.NameType))
	payload = append(payload, byte(p.NameLength))
	payload = append(payload, p.MatrixID)
	payload = append(payload, p.LevelID)
	payload = append(payload, byte(p.FirstID/256))
	payload = append(payload, byte(p.FirstID%256))
	payload = append(payload, byte(n))
	for i := 0; i < n; i++ {
		payload = append(payload, packName(p.Names[i], width)...)
	}
	return Frame{ID: RxUpdateNameRequest, Payload: payload}
}

// DecodeUpdateNameRequest parses rx 117.
func DecodeUpdateNameRequest(f Frame) (UpdateNameRequestParams, error) {
	if f.ID != RxUpdateNameRequest {
		return UpdateNameRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 7 {
		return UpdateNameRequestParams{}, ErrShortPayload
	}
	p := UpdateNameRequestParams{
		NameType:   UpdateNameType(f.Payload[0]),
		NameLength: NameLength(f.Payload[1]),
		MatrixID:   f.Payload[2],
		LevelID:    f.Payload[3],
		FirstID:    uint16(f.Payload[4])*256 + uint16(f.Payload[5]),
	}
	if err := validateNameLengthExt(p.NameLength); err != nil {
		return UpdateNameRequestParams{}, err
	}
	switch p.NameType {
	case UpdateNameSource, UpdateNameSourceAssoc, UpdateNameDestAssoc, UpdateNameUMDLabel:
	default:
		return UpdateNameRequestParams{}, fmt.Errorf("probel: unknown UpdateNameType %#x", byte(p.NameType))
	}
	count := int(f.Payload[6])
	width := p.NameLength.Bytes()
	if len(f.Payload) < 7+count*width {
		return UpdateNameRequestParams{}, fmt.Errorf("probel: rx 117 needs %d bytes for %d names, got %d",
			7+count*width, count, len(f.Payload))
	}
	p.Names = make([]string, count)
	for i := 0; i < count; i++ {
		off := 7 + i*width
		p.Names[i] = unpackName(f.Payload[off : off+width])
	}
	return p, nil
}
