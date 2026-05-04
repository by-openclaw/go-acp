package codec

// SalvoGroupInterrogateParams: rx 124 / rx 252 CROSSPOINT SALVO GROUP
// INTERROGATE. Iterating from ConnectIndex=0 fetches the salvo slots
// one-at-a-time via tx 125 / tx 253 replies; the Validity field in the
// reply tells the caller whether to continue.
//
// The encoder picks general (cmd 124, narrow 1-byte index) or
// extended (cmd 252, 2-byte index) form per the SW-P-08 §3.4.17 ranges.
//
// Reference: SW-P-08 Issue 30 §3.2.31 (general) + §3.4.17 (extended).
type SalvoGroupInterrogateParams struct {
	SalvoID      uint8  // 0-127
	ConnectIndex uint16 // narrow: 0-255 (1 byte); extended: 0-65535 (DIV 256 + MOD 256)
}

// needsExtended fires when ConnectIndex exceeds the narrow form's
// 1-byte limit (255).
func (p SalvoGroupInterrogateParams) needsExtended() bool {
	return p.ConnectIndex > 255
}

// EncodeSalvoGroupInterrogate packs cmd 124 (general, 2-byte payload)
// or cmd 252 (extended, 3-byte payload).
//
// General form (RxCrosspointSalvoGroupInterrogate = 0x7C, §3.2.31):
//
//	| Byte | Field         | Notes                                  |
//	|------|---------------|----------------------------------------|
//	|  1   | Salvo num     | 0-127                                  |
//	|  2   | Connect index | 0-based, 1 byte                        |
//
// Extended form (RxCrosspointSalvoGroupInterrogateExt = 0xFC, §3.4.17):
//
//	| Byte | Field         | Notes                                  |
//	|------|---------------|----------------------------------------|
//	|  1   | Salvo num     | 0-127                                  |
//	|  2   | Index mult    | ConnectIndex DIV 256                   |
//	|  3   | Index num     | ConnectIndex MOD 256                   |
func EncodeSalvoGroupInterrogate(p SalvoGroupInterrogateParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID: RxCrosspointSalvoGroupInterrogateExt,
			Payload: []byte{
				p.SalvoID & 0x7F,
				byte(p.ConnectIndex / 256),
				byte(p.ConnectIndex % 256),
			},
		}
	}
	return Frame{
		ID:      RxCrosspointSalvoGroupInterrogate,
		Payload: []byte{p.SalvoID & 0x7F, byte(p.ConnectIndex)},
	}
}

// DecodeSalvoGroupInterrogate parses cmd 124 (general, 2-byte payload)
// or cmd 252 (extended, 3-byte payload) into the same logical struct.
func DecodeSalvoGroupInterrogate(f Frame) (SalvoGroupInterrogateParams, error) {
	switch f.ID {
	case RxCrosspointSalvoGroupInterrogate:
		if len(f.Payload) < 2 {
			return SalvoGroupInterrogateParams{}, ErrShortPayload
		}
		return SalvoGroupInterrogateParams{
			SalvoID:      f.Payload[0] & 0x7F,
			ConnectIndex: uint16(f.Payload[1]),
		}, nil
	case RxCrosspointSalvoGroupInterrogateExt:
		if len(f.Payload) < 3 {
			return SalvoGroupInterrogateParams{}, ErrShortPayload
		}
		return SalvoGroupInterrogateParams{
			SalvoID:      f.Payload[0] & 0x7F,
			ConnectIndex: uint16(f.Payload[1])*256 + uint16(f.Payload[2]),
		}, nil
	default:
		return SalvoGroupInterrogateParams{}, ErrWrongCommand
	}
}
