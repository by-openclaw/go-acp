package codec

// TallyParams carries tx 03 TALLY fields. Matrix emits this in reply to
// rx 01 INTERROGATE, reporting which source is currently routed to the
// queried destination. See §3.2.5.
//
// Per §3.2.5 note: "Source number 1023 is reserved to indicate
// destination out of range" — callers encoding a tally for a dst not
// declared in the tree set Source = DestOutOfRangeSource.
//
//	| Byte | Field       | Notes                                     |
//	|------|-------------|-------------------------------------------|
//	|  1   | Multiplier  | same layout as rx 01 §3.2.3               |
//	|  2   | Destination | Destination MOD 128                       |
//	|  3   | Source      | Source MOD 128                            |
//
// Spec: SW-P-02 Issue 26 §3.2.5 + §3.2.3 (Multiplier layout).
type TallyParams struct {
	Destination uint16 // 0-1023
	Source      uint16 // 0-1023; 1023 = destination out of range (§3.2.5)
	BadSource   bool   // mirrors the Multiplier bit-3 flag
}

// PayloadLenTally is the fixed MESSAGE byte count for tx 03.
const PayloadLenTally = 3

// DestOutOfRangeSource is the reserved Source value (§3.2.5 note) the
// matrix reports when the queried destination is out of range.
const DestOutOfRangeSource uint16 = 1023

// EncodeTally builds tx 03 wire bytes.
func EncodeTally(p TallyParams) Frame {
	mult := byte(((p.Destination / 128) & 0x07) << 4)
	mult |= byte((p.Source / 128) & 0x07)
	if p.BadSource {
		mult |= 0x08
	}
	return Frame{
		ID: TxTally,
		Payload: []byte{
			mult,
			byte(p.Destination % 128),
			byte(p.Source % 128),
		},
	}
}

// DecodeTally parses tx 03.
func DecodeTally(f Frame) (TallyParams, error) {
	if f.ID != TxTally {
		return TallyParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenTally {
		return TallyParams{}, ErrShortPayload
	}
	mult := f.Payload[0]
	return TallyParams{
		Destination: (uint16(mult>>4) & 0x07) * 128 + uint16(f.Payload[1]),
		Source:      (uint16(mult) & 0x07) * 128 + uint16(f.Payload[2]),
		BadSource:   mult&0x08 != 0,
	}, nil
}
