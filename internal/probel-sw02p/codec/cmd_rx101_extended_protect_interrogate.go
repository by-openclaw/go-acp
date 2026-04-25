package codec

// ExtendedProtectInterrogateParams carries rx 101 Extended PROTECT
// INTERROGATE fields — §3.2.65. Destination-only query for the
// current protect status of a destination; router replies with tx 096
// Extended PROTECT TALLY (§3.2.60).
//
//	| Byte | Field              | Notes                             |
//	|------|--------------------|-----------------------------------|
//	|  1   | Destination Mult.  | §3.2.47 form, bits 0-6 DIV 128    |
//	|  2   | Destination MOD 128|                                   |
//
// Spec: SW-P-02 Issue 26 §3.2.65.
type ExtendedProtectInterrogateParams struct {
	Destination uint16 // 0-16383
}

// PayloadLenExtendedProtectInterrogate is the fixed MESSAGE byte count
// for rx 101.
const PayloadLenExtendedProtectInterrogate = 2

// EncodeExtendedProtectInterrogate builds rx 101 wire bytes.
func EncodeExtendedProtectInterrogate(p ExtendedProtectInterrogateParams) Frame {
	return Frame{
		ID: RxExtendedProtectInterrogate,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
		},
	}
}

// DecodeExtendedProtectInterrogate parses rx 101.
func DecodeExtendedProtectInterrogate(f Frame) (ExtendedProtectInterrogateParams, error) {
	if f.ID != RxExtendedProtectInterrogate {
		return ExtendedProtectInterrogateParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedProtectInterrogate {
		return ExtendedProtectInterrogateParams{}, ErrShortPayload
	}
	return ExtendedProtectInterrogateParams{
		Destination: (uint16(f.Payload[0])&0x7F)*128 + uint16(f.Payload[1]),
	}, nil
}
