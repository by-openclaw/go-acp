package codec

// SalvoGroupInterrogateParams: rx 124 CROSSPOINT SALVO GROUP
// INTERROGATE. Iterating from ConnectIndex=0 fetches the salvo slots
// one-at-a-time via tx 125 replies; the Validity field in the reply
// tells the caller whether to continue.
//
// Reference: SW-P-08 §3.2.31.
type SalvoGroupInterrogateParams struct {
	SalvoID      uint8 // 0-127
	ConnectIndex uint8 // 0-based slot index
}

// EncodeSalvoGroupInterrogate packs rx 124.
//
// | Byte | Field         | Notes                                   |
// |------|---------------|-----------------------------------------|
// |  1   | Salvo num     | 0-127                                   |
// |  2   | Connect index | 0-based                                 |
//
// Spec: SW-P-08 §3.2.31.
func EncodeSalvoGroupInterrogate(p SalvoGroupInterrogateParams) Frame {
	return Frame{
		ID:      RxCrosspointSalvoGroupInterrogate,
		Payload: []byte{p.SalvoID & 0x7F, p.ConnectIndex},
	}
}

// DecodeSalvoGroupInterrogate parses rx 124.
func DecodeSalvoGroupInterrogate(f Frame) (SalvoGroupInterrogateParams, error) {
	if f.ID != RxCrosspointSalvoGroupInterrogate {
		return SalvoGroupInterrogateParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 2 {
		return SalvoGroupInterrogateParams{}, ErrShortPayload
	}
	return SalvoGroupInterrogateParams{
		SalvoID:      f.Payload[0] & 0x7F,
		ConnectIndex: f.Payload[1],
	}, nil
}
