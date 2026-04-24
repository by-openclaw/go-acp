package codec

// ExtendedConnectedParams carries tx 68 Extended CONNECTED fields —
// the broadcast emitted on all ports after a route is made (§3.2.50),
// usually in response to rx 66 Extended CONNECT. Same layout as tx 67
// Extended TALLY (§3.2.49) — including the Status byte — which is why
// the struct shape matches ExtendedTallyParams exactly.
//
//	| Byte | Field              | Notes                              |
//	|------|--------------------|------------------------------------|
//	|  1   | Destination Mult.  | bits[0-6] Dst DIV 128, bit 7 = 0   |
//	|  2   | Destination number | Destination MOD 128                |
//	|  3   | Source Multiplier  | bits[0-6] Src DIV 128, bit 7 = 0   |
//	|  4   | Source number      | Source MOD 128                     |
//	|  5   | Status             | bit 0 = Crosspoint update disabled |
//	|      |                    | bit 1 = Bad source                 |
//
// Spec: SW-P-02 Issue 26 §3.2.50.
type ExtendedConnectedParams struct {
	Destination uint16 // 0-16383
	Source      uint16 // 0-16383
	UpdateOff   bool   // Status bit 0
	BadSource   bool   // Status bit 1
}

// PayloadLenExtendedConnected is the fixed MESSAGE byte count for tx 68.
const PayloadLenExtendedConnected = 5

// EncodeExtendedConnected builds tx 68 wire bytes.
func EncodeExtendedConnected(p ExtendedConnectedParams) Frame {
	var status byte
	if p.UpdateOff {
		status |= 1 << 0
	}
	if p.BadSource {
		status |= 1 << 1
	}
	return Frame{
		ID: TxExtendedConnected,
		Payload: []byte{
			byte((p.Destination / 128) & 0x7F),
			byte(p.Destination % 128),
			byte((p.Source / 128) & 0x7F),
			byte(p.Source % 128),
			status,
		},
	}
}

// DecodeExtendedConnected parses tx 68.
func DecodeExtendedConnected(f Frame) (ExtendedConnectedParams, error) {
	if f.ID != TxExtendedConnected {
		return ExtendedConnectedParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedConnected {
		return ExtendedConnectedParams{}, ErrShortPayload
	}
	s := f.Payload[4]
	return ExtendedConnectedParams{
		Destination: (uint16(f.Payload[0]) & 0x7F) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(f.Payload[2]) & 0x7F) * 128 + uint16(f.Payload[3]),
		UpdateOff:   s&(1<<0) != 0,
		BadSource:   s&(1<<1) != 0,
	}, nil
}
