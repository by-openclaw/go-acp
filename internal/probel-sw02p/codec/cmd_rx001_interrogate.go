package codec

// InterrogateParams carries rx 01 INTERROGATE fields. Controller sends
// one byte pair — Multiplier + Destination MOD 128 — and the matrix
// replies with tx 03 TALLY carrying the currently-routed source. See
// §3.2.3.
//
// SW-P-02 is single-matrix, single-level on the wire — there is no
// matrix or level byte. Addressing is destination-only, widened to
// 1023 via the Multiplier's DIV 128 bits (§3.2.3).
//
//	| Byte | Field       | Notes                                     |
//	|------|-------------|-------------------------------------------|
//	|  1   | Multiplier  | bit 7      reserved 0                     |
//	|      |             | bit 4-6    Destination DIV 128 (0-7)      |
//	|      |             | bit 3      BadSource / UpdateDisabled flag|
//	|      |             | bit 0-2    Source DIV 128 — always 0      |
//	|      |             |            on this command per §3.2.3     |
//	|  2   | Destination | Destination MOD 128                       |
//
// Spec: SW-P-02 Issue 26 §3.2.3.
type InterrogateParams struct {
	Destination uint16 // 0-1023
	BadSource   bool   // mirrors the Multiplier bit-3 flag
}

// PayloadLenInterrogate is the fixed MESSAGE byte count for rx 01. The
// Source DIV 128 bits (bit 0-2 of Multiplier) carry no information on
// this command (§3.2.3 "always 0 for this command"); the field is
// preserved on the wire but not exposed.
const PayloadLenInterrogate = 2

// EncodeInterrogate builds rx 01 wire bytes.
func EncodeInterrogate(p InterrogateParams) Frame {
	mult := byte(((p.Destination / 128) & 0x07) << 4)
	if p.BadSource {
		mult |= 0x08
	}
	return Frame{
		ID: RxInterrogate,
		Payload: []byte{
			mult,
			byte(p.Destination % 128),
		},
	}
}

// DecodeInterrogate parses rx 01. Rejects frames whose ID is not
// RxInterrogate or whose MESSAGE is shorter than PayloadLenInterrogate.
func DecodeInterrogate(f Frame) (InterrogateParams, error) {
	if f.ID != RxInterrogate {
		return InterrogateParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenInterrogate {
		return InterrogateParams{}, ErrShortPayload
	}
	mult := f.Payload[0]
	return InterrogateParams{
		Destination: (uint16(mult>>4) & 0x07) * 128 + uint16(f.Payload[1]),
		BadSource:   mult&0x08 != 0,
	}, nil
}
