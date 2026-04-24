package codec

// ExtendedConnectOnGoParams carries rx 069 Extended CONNECT ON GO
// fields. Extended-addressing equivalent of rx 005 CONNECT ON GO: one
// crosspoint per message, staged into the matrix's unnamed pending
// salvo buffer until rx 006 GO commits or clears the whole list.
// See §3.2.51.
//
// Extended addressing replaces the narrow §3.2.3 Multiplier (one byte
// packing dst/src high bits + bad-source flag) with TWO dedicated
// multiplier bytes per §3.2.47 + §3.2.48, stretching the per-axis
// range from 1023 to 16383. No bad-source bit in this form (§3.2.49
// re-exposes it on the Status byte, but §3.2.51 does not carry one).
//
//	| Byte | Field              | Notes                          |
//	|------|--------------------|--------------------------------|
//	|  1   | Destination Mult.  | §3.2.47 form, bits 0-6 DIV 128 |
//	|  2   | Destination MOD 128|                                |
//	|  3   | Source Multiplier  | §3.2.48 form, bits 0-6 DIV 128 |
//	|  4   | Source MOD 128     |                                |
//
// Spec: SW-P-02 Issue 26 §3.2.51 + §3.2.47 + §3.2.48.
type ExtendedConnectOnGoParams struct {
	Destination uint16 // 0-16383
	Source      uint16 // 0-16383
}

// PayloadLenExtendedConnectOnGo is the fixed MESSAGE byte count for
// rx 069.
const PayloadLenExtendedConnectOnGo = 4

// EncodeExtendedConnectOnGo builds rx 069 wire bytes.
func EncodeExtendedConnectOnGo(p ExtendedConnectOnGoParams) Frame {
	return Frame{
		ID: RxExtendedConnectOnGo,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Source / 128) & 0x7F),
			byte(p.Source % 128),
		},
	}
}

// DecodeExtendedConnectOnGo parses rx 069.
func DecodeExtendedConnectOnGo(f Frame) (ExtendedConnectOnGoParams, error) {
	if f.ID != RxExtendedConnectOnGo {
		return ExtendedConnectOnGoParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedConnectOnGo {
		return ExtendedConnectOnGoParams{}, ErrShortPayload
	}
	return ExtendedConnectOnGoParams{
		Destination: (uint16(f.Payload[0]) & 0x7F) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(f.Payload[2]) & 0x7F) * 128 + uint16(f.Payload[3]),
	}, nil
}
