package codec

// SourceLockStatusRequestParams carries rx 014 SOURCE LOCK STATUS
// REQUEST fields — §3.2.16. "Source Lock" here is the HD Digital
// Video router's input signal-health indicator (bit=1 → clean
// carrier detected), NOT a write-protection. Only applicable to HD
// routers per §3.2.16. Matrix replies with tx 015 SOURCE LOCK STATUS
// RESPONSE (§3.2.17).
//
//	| Byte | Field      | Notes                                        |
//	|------|------------|----------------------------------------------|
//	|  1   | Controller | 0 = LH controller, 1 = RH controller         |
//	|      |            | (single-controller systems always use 0)     |
//
// Spec: SW-P-02 Issue 26 §3.2.16.
type SourceLockStatusRequestParams struct {
	Controller Controller
}

// PayloadLenSourceLockStatusRequest is the fixed MESSAGE byte count
// for rx 014.
const PayloadLenSourceLockStatusRequest = 1

// EncodeSourceLockStatusRequest builds rx 014 wire bytes.
func EncodeSourceLockStatusRequest(p SourceLockStatusRequestParams) Frame {
	return Frame{
		ID:      RxSourceLockStatusRequest,
		Payload: []byte{byte(p.Controller)},
	}
}

// DecodeSourceLockStatusRequest parses rx 014.
func DecodeSourceLockStatusRequest(f Frame) (SourceLockStatusRequestParams, error) {
	if f.ID != RxSourceLockStatusRequest {
		return SourceLockStatusRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenSourceLockStatusRequest {
		return SourceLockStatusRequestParams{}, ErrShortPayload
	}
	return SourceLockStatusRequestParams{Controller: Controller(f.Payload[0])}, nil
}
