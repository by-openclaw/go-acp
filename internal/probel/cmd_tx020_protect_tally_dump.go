package probel

// ProtectTallyItem is one (state, deviceID) pair in a protect tally dump,
// associated with consecutive destination numbers starting at the frame's
// FirstDestinationID. State occupies bits 12-14 of a u16; device bits 0-9.
type ProtectTallyItem struct {
	State    ProtectState
	DeviceID uint16 // 0-1023
}

// ProtectTallyDumpParams carries a run of protect items starting at
// FirstDestinationID. Caller chunks at the 133-byte frame limit.
//
// Reference: TS tx/020/params.ts ProtectTallyDumpCommandParams.
type ProtectTallyDumpParams struct {
	MatrixID           uint8
	LevelID            uint8
	FirstDestinationID uint16
	Items              []ProtectTallyItem
}

func (p ProtectTallyDumpParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15 || p.FirstDestinationID > 895
}

// packProtectItem packs one item as a 16-bit big-endian value:
//
//	bit[15]     = 0
//	bits[12-14] = protect state
//	bits[10-11] = 0
//	bits[0-9]   = device id
func packProtectItem(it ProtectTallyItem) (byte, byte) {
	hi := byte((uint16(it.State)&0x07)<<4) | byte((it.DeviceID/256)&0x03)
	lo := byte(it.DeviceID % 256)
	return hi, lo
}

func unpackProtectItem(hi, lo byte) ProtectTallyItem {
	return ProtectTallyItem{
		State:    ProtectState((hi >> 4) & 0x07),
		DeviceID: (uint16(hi) & 0x03) * 256 + uint16(lo),
	}
}

// EncodeProtectTallyDump builds ONE PROTECT TALLY DUMP frame. Callers must
// chunk large runs across multiple frames (SW-P-08 caps a frame at 133 bytes).
//
// General form (CommandID 0x14 — 4 + 2N payload bytes, N = len(Items)):
//
//	| Byte | Field            | Notes                                  |
//	|------|------------------|----------------------------------------|
//	|  1   | Matrix / Level   | bits[4-7] = Matrix, bits[0-3] = Level  |
//	|  2   | Num protect      | N = len(Items), max 64                 |
//	|  3   | 1st Dest mult    | FirstDestinationID DIV 256             |
//	|  4   | 1st Dest num     | FirstDestinationID MOD 256             |
//	|  5,7,…| Device/protect hi| packed byte 1 (see packProtectItem)    |
//	|  6,8,…| Device/protect lo| packed byte 2                          |
//
// Extended form (CommandID 0x94 — 5 + 2N payload bytes):
//
//	| Byte | Field            | Notes                                  |
//	|------|------------------|----------------------------------------|
//	|  1   | Matrix           | full 8-bit                             |
//	|  2   | Level            | full 8-bit                             |
//	|  3   | Num protect      | N = len(Items), max 64                 |
//	|  4   | 1st Dest mult    | FirstDestinationID DIV 256             |
//	|  5   | 1st Dest num     | FirstDestinationID MOD 256             |
//	|  6,8,…| Device/protect hi|                                        |
//	|  7,9,…| Device/protect lo|                                        |
//
// Spec: SW-P-08 §3.3.20. Reference: TS tx/020/command.ts.
func EncodeProtectTallyDump(p ProtectTallyDumpParams) Frame {
	n := len(p.Items)
	if p.needsExtended() {
		out := make([]byte, 0, 5+2*n)
		out = append(out,
			p.MatrixID,
			p.LevelID,
			byte(n),
			byte(p.FirstDestinationID/256),
			byte(p.FirstDestinationID%256),
		)
		for _, it := range p.Items {
			hi, lo := packProtectItem(it)
			out = append(out, hi, lo)
		}
		return Frame{ID: TxProtectTallyDumpExt, Payload: out}
	}
	out := make([]byte, 0, 4+2*n)
	out = append(out,
		(p.MatrixID<<4)|(p.LevelID&0x0F),
		byte(n),
		byte(p.FirstDestinationID/256),
		byte(p.FirstDestinationID%256),
	)
	for _, it := range p.Items {
		hi, lo := packProtectItem(it)
		out = append(out, hi, lo)
	}
	return Frame{ID: TxProtectTallyDump, Payload: out}
}

// DecodeProtectTallyDump parses a PROTECT TALLY DUMP payload.
func DecodeProtectTallyDump(f Frame) (ProtectTallyDumpParams, error) {
	var (
		matrix, level byte
		count         int
		firstDest     uint16
		headerLen     int
	)
	switch f.ID {
	case TxProtectTallyDump:
		if len(f.Payload) < 4 {
			return ProtectTallyDumpParams{}, ErrShortPayload
		}
		matrix = f.Payload[0] >> 4
		level = f.Payload[0] & 0x0F
		count = int(f.Payload[1])
		firstDest = uint16(f.Payload[2])*256 + uint16(f.Payload[3])
		headerLen = 4
	case TxProtectTallyDumpExt:
		if len(f.Payload) < 5 {
			return ProtectTallyDumpParams{}, ErrShortPayload
		}
		matrix = f.Payload[0]
		level = f.Payload[1]
		count = int(f.Payload[2])
		firstDest = uint16(f.Payload[3])*256 + uint16(f.Payload[4])
		headerLen = 5
	default:
		return ProtectTallyDumpParams{}, ErrWrongCommand
	}
	if len(f.Payload) < headerLen+2*count {
		return ProtectTallyDumpParams{}, ErrShortPayload
	}
	items := make([]ProtectTallyItem, count)
	for i := 0; i < count; i++ {
		hi := f.Payload[headerLen+2*i]
		lo := f.Payload[headerLen+2*i+1]
		items[i] = unpackProtectItem(hi, lo)
	}
	return ProtectTallyDumpParams{
		MatrixID:           matrix,
		LevelID:            level,
		FirstDestinationID: firstDest,
		Items:              items,
	}, nil
}
