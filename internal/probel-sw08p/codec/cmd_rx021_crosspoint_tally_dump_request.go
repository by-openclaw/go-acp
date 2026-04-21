package codec

// CrosspointTallyDumpRequestParams asks the controller to dump the full tally
// table for one (matrix, level) pair. Reply arrives as one or more tx 022
// (byte form) and/or tx 023 (word form) messages depending on destination
// count and matrix size.
//
// Reference: TS rx/021/params.ts CrossPointTallyDumpRequestMessageCommandParams.
type CrosspointTallyDumpRequestParams struct {
	MatrixID uint8 // 0-15 general, 0-255 extended
	LevelID  uint8 // 0-15 general, 0-255 extended
}

func (p CrosspointTallyDumpRequestParams) needsExtended() bool {
	return p.MatrixID > 15 || p.LevelID > 15
}

// EncodeCrosspointTallyDumpRequest builds the CROSSPOINT TALLY DUMP REQUEST.
//
// General form (CommandID 0x15 — 1-byte payload):
//
//	| Byte | Field          | Notes                                     |
//	|------|----------------|-------------------------------------------|
//	|  1   | Matrix / Level | bits[4-7] = Matrix, bits[0-3] = Level     |
//
// Extended form (CommandID 0x95 — 2-byte payload):
//
//	| Byte | Field          | Notes                                     |
//	|------|----------------|-------------------------------------------|
//	|  1   | Matrix         | full 8-bit                                |
//	|  2   | Level          | full 8-bit                                |
//
// Spec: SW-P-08 §3.2.22. Reference: TS rx/021/command.ts.
func EncodeCrosspointTallyDumpRequest(p CrosspointTallyDumpRequestParams) Frame {
	if p.needsExtended() {
		return Frame{
			ID:      RxCrosspointTallyDumpRequestExt,
			Payload: []byte{p.MatrixID, p.LevelID},
		}
	}
	return Frame{
		ID:      RxCrosspointTallyDumpRequest,
		Payload: []byte{(p.MatrixID << 4) | (p.LevelID & 0x0F)},
	}
}

// DecodeCrosspointTallyDumpRequest parses the general (0x15) or extended
// (0x95) request payload.
func DecodeCrosspointTallyDumpRequest(f Frame) (CrosspointTallyDumpRequestParams, error) {
	switch f.ID {
	case RxCrosspointTallyDumpRequest:
		if len(f.Payload) < 1 {
			return CrosspointTallyDumpRequestParams{}, ErrShortPayload
		}
		return CrosspointTallyDumpRequestParams{
			MatrixID: f.Payload[0] >> 4,
			LevelID:  f.Payload[0] & 0x0F,
		}, nil
	case RxCrosspointTallyDumpRequestExt:
		if len(f.Payload) < 2 {
			return CrosspointTallyDumpRequestParams{}, ErrShortPayload
		}
		return CrosspointTallyDumpRequestParams{
			MatrixID: f.Payload[0],
			LevelID:  f.Payload[1],
		}, nil
	default:
		return CrosspointTallyDumpRequestParams{}, ErrWrongCommand
	}
}
