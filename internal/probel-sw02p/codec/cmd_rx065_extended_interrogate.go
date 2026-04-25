package codec

// ExtendedInterrogateParams carries rx 65 Extended INTERROGATE fields.
// Extended form lifts dst addressing from 1023 (§3.2.3) to 16383 via a
// dedicated Destination Multiplier whose bits 0-6 encode Dest / 128.
// No Source or BadSource fields — extended INTERROGATE asks by
// destination only, same as the narrow rx 01. See §3.2.47.
//
//	| Byte | Field              | Notes                             |
//	|------|--------------------|-----------------------------------|
//	|  1   | Destination Mult.  | bits[0-6] Dst DIV 128, bit 7 = 0  |
//	|  2   | Destination number | Destination MOD 128               |
//
// Spec: SW-P-02 Issue 26 §3.2.47.
type ExtendedInterrogateParams struct {
	Destination uint16 // 0-16383
}

// PayloadLenExtendedInterrogate is the fixed MESSAGE byte count for rx 65.
const PayloadLenExtendedInterrogate = 2

// EncodeExtendedInterrogate builds rx 65 wire bytes.
func EncodeExtendedInterrogate(p ExtendedInterrogateParams) Frame {
	return Frame{
		ID: RxExtendedInterrogate,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
		},
	}
}

// DecodeExtendedInterrogate parses rx 65.
func DecodeExtendedInterrogate(f Frame) (ExtendedInterrogateParams, error) {
	if f.ID != RxExtendedInterrogate {
		return ExtendedInterrogateParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedInterrogate {
		return ExtendedInterrogateParams{}, ErrShortPayload
	}
	return ExtendedInterrogateParams{
		Destination: (uint16(f.Payload[0]) & 0x7F) * 128 + uint16(f.Payload[1]),
	}, nil
}
