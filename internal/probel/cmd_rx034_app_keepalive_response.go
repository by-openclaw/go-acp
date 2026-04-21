package probel

import "fmt"

// EncodeKeepaliveResponse builds an rx 0x22 APP_KEEPALIVE_RESPONSE frame —
// the controller's reply to a matrix-originated APP_KEEPALIVE_REQUEST.
// Zero-payload.
//
// | Field | Bytes | Value                     |
// |-------|-------|---------------------------|
// | ID    | 1     | RxAppKeepaliveResponse    |
//
// Reference: TS assets/probel/smh-probelsw08p/src/command/application-keep-alive/
// application-keepalive-response.ts.
func EncodeKeepaliveResponse() Frame {
	return Frame{ID: RxAppKeepaliveResponse}
}

// DecodeKeepaliveResponse validates an inbound APP_KEEPALIVE_RESPONSE
// frame (provider side, controller → matrix). Rejects non-empty payload.
func DecodeKeepaliveResponse(f Frame) error {
	if f.ID != RxAppKeepaliveResponse {
		return fmt.Errorf("probel: expected RxAppKeepaliveResponse (0x22); got %#x", byte(f.ID))
	}
	if len(f.Payload) != 0 {
		return fmt.Errorf("probel: RxAppKeepaliveResponse must have zero payload; got %d bytes", len(f.Payload))
	}
	return nil
}
