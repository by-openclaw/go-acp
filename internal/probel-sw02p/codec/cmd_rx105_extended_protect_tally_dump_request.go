package codec

// ExtendedProtectTallyDumpRequestParams carries rx 105 Extended
// PROTECT TALLY DUMP REQUEST fields — §3.2.69. A router asks a
// controller for all protect information (e.g. on router-side
// reinitialisation); the controller replies with one or more tx 100
// EXTENDED PROTECT TALLY DUMP messages covering the requested count
// starting at StartDestination.
//
// Since §3.2.64 caps each tx 100 at 32 entries, a request for more
// than 32 entries fans out into multiple tx 100 broadcasts — the
// provider helper handles the split.
//
//	| Byte | Field                  | Notes                         |
//	|------|------------------------|-------------------------------|
//	|  1   | Number of Protect      | how many entries to report    |
//	|      | Tallies required       |                               |
//	|  2   | Start Destination DIV  | §3.2.47 form, bits 0-6 DIV 128|
//	|      | 128                    |                               |
//	|  3   | Start Destination MOD  |                               |
//	|      | 128                    |                               |
//
// Spec: SW-P-02 Issue 26 §3.2.69.
type ExtendedProtectTallyDumpRequestParams struct {
	Count            uint8  // 1-255, spec examples imply up to however many are needed
	StartDestination uint16 // 0-16383
}

// PayloadLenExtendedProtectTallyDumpRequest is the fixed MESSAGE byte
// count for rx 105.
const PayloadLenExtendedProtectTallyDumpRequest = 3

// EncodeExtendedProtectTallyDumpRequest builds rx 105 wire bytes.
func EncodeExtendedProtectTallyDumpRequest(p ExtendedProtectTallyDumpRequestParams) Frame {
	return Frame{
		ID: RxExtendedProtectTallyDumpRequest,
		Payload: []byte{
			p.Count,
			byte((p.StartDestination / 128) & 0x7F),
			byte(p.StartDestination % 128),
		},
	}
}

// DecodeExtendedProtectTallyDumpRequest parses rx 105.
func DecodeExtendedProtectTallyDumpRequest(f Frame) (ExtendedProtectTallyDumpRequestParams, error) {
	if f.ID != RxExtendedProtectTallyDumpRequest {
		return ExtendedProtectTallyDumpRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenExtendedProtectTallyDumpRequest {
		return ExtendedProtectTallyDumpRequestParams{}, ErrShortPayload
	}
	return ExtendedProtectTallyDumpRequestParams{
		Count:            f.Payload[0],
		StartDestination: (uint16(f.Payload[1])&0x7F)*128 + uint16(f.Payload[2]),
	}, nil
}
