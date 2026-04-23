package codec

import "fmt"

// EncodeKeepaliveRequest builds a tx 0x11 APP_KEEPALIVE_REQUEST frame —
// the matrix's periodic ping to its controller session. Zero-payload.
//
// | Field | Bytes | Value                     |
// |-------|-------|---------------------------|
// | ID    | 1     | TxAppKeepaliveRequest     |
//
// Reference: TS internal/probel-sw08p/assets/smh-probelsw08p/src/command/application-keep-alive/
// application-keepalive-request.ts. Not defined in SW-P-08 §3.2/§3.3 —
// the TS module documents it as a custom application-layer liveness probe.
func EncodeKeepaliveRequest() Frame {
	return Frame{ID: TxAppKeepaliveRequest}
}

// DecodeKeepaliveRequest validates an inbound APP_KEEPALIVE_REQUEST frame
// (consumer side, matrix → controller). Returns an error for non-empty
// payload — the spec/TS mandate a zero-byte body.
func DecodeKeepaliveRequest(f Frame) error {
	if f.ID != TxAppKeepaliveRequest {
		return fmt.Errorf("probel: expected TxAppKeepaliveRequest (0x11); got %#x", byte(f.ID))
	}
	if len(f.Payload) != 0 {
		return fmt.Errorf("probel: TxAppKeepaliveRequest must have zero payload; got %d bytes", len(f.Payload))
	}
	return nil
}
