package codec

import "fmt"

// EncodeKeepaliveResponse builds an rx 0x22 APP_KEEPALIVE_RESPONSE frame —
// the controller's reply to a matrix-originated APP_KEEPALIVE_REQUEST.
// Zero-payload.
//
// | Field | Bytes | Value                     |
// |-------|-------|---------------------------|
// | ID    | 1     | RxAppKeepaliveResponse    |
//
// Reference: NOT defined in SW-P-08 §3.2/§3.3 — pairs with
// TxAppKeepaliveRequest (cmd 0x11). See the "Application keepalive"
// section of internal/probel-sw08p/CLAUDE.md for testbed evidence.
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
