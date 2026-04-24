package codec

// RouterConfigRequestParams carries rx 075 ROUTER CONFIGURATION
// REQUEST fields. §3.2.57 defines MESSAGE as empty — the COMMAND byte
// alone is the request. Matrix replies with tx 076 ROUTER
// CONFIGURATION RESPONSE - 1 (§3.2.58) or tx 077 RESPONSE - 2
// (§3.2.59); this plugin emits RESPONSE - 1 by default.
//
// Spec: SW-P-02 Issue 26 §3.2.57.
type RouterConfigRequestParams struct{}

// PayloadLenRouterConfigRequest is the fixed MESSAGE byte count for
// rx 075 — zero.
const PayloadLenRouterConfigRequest = 0

// EncodeRouterConfigRequest builds rx 075 wire bytes.
func EncodeRouterConfigRequest(_ RouterConfigRequestParams) Frame {
	return Frame{ID: RxRouterConfigRequest, Payload: nil}
}

// DecodeRouterConfigRequest parses rx 075.
func DecodeRouterConfigRequest(f Frame) (RouterConfigRequestParams, error) {
	if f.ID != RxRouterConfigRequest {
		return RouterConfigRequestParams{}, ErrWrongCommand
	}
	return RouterConfigRequestParams{}, nil
}
