package codec

// DualControllerStatusRequestParams carries rx 050 DUAL CONTROLLER
// STATUS REQUEST fields. §3.2.45 defines MESSAGE as empty — the
// COMMAND byte alone is the request. The struct is intentionally
// empty so callers can pass it by value / by pointer symmetrically
// with every other *Params type in this codec.
//
//	| Bytes | Field | Notes                        |
//	|-------|-------|------------------------------|
//	|   —   |   —   | No MESSAGE bytes per §3.2.45 |
//
// Spec: SW-P-02 Issue 26 §3.2.45.
type DualControllerStatusRequestParams struct{}

// PayloadLenDualControllerStatusRequest is the fixed MESSAGE byte
// count for rx 050 — zero.
const PayloadLenDualControllerStatusRequest = 0

// EncodeDualControllerStatusRequest builds rx 050 wire bytes.
func EncodeDualControllerStatusRequest(_ DualControllerStatusRequestParams) Frame {
	return Frame{ID: RxDualControllerStatusRequest, Payload: nil}
}

// DecodeDualControllerStatusRequest parses rx 050.
func DecodeDualControllerStatusRequest(f Frame) (DualControllerStatusRequestParams, error) {
	if f.ID != RxDualControllerStatusRequest {
		return DualControllerStatusRequestParams{}, ErrWrongCommand
	}
	return DualControllerStatusRequestParams{}, nil
}
