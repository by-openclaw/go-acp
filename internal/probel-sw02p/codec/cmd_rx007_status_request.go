package codec

// Controller addresses the LH or RH controller of a dual-controller
// matrix. Single-controller systems and the TDM 4-Wire Router always
// use ControllerLH per §3.2.9 "(single controller systems or TDM
// 4-Wire Router value = 0)".
type Controller uint8

// Controller values — §3.2.9.
const (
	ControllerLH Controller = 0x00
	ControllerRH Controller = 0x01
)

// StatusRequestParams carries rx 07 STATUS REQUEST fields. Controller
// asks the matrix which of its status-response commands applies to the
// current hardware; the matrix replies with one of tx 08 / 09 / 10 /
// 16 / 17 (§§3.2.10 - 3.2.19). This plugin responds with tx 09
// STATUS RESPONSE - 2 (§3.2.11) only — the other response shapes are
// outside the VSM-supported set and land per-cmd from the non-VSM
// queue. See §3.2.9.
//
//	| Byte | Field      | Notes                                        |
//	|------|------------|----------------------------------------------|
//	|  1   | Controller | 0 = LH controller, 1 = RH controller         |
//
// Spec: SW-P-02 Issue 26 §3.2.9.
type StatusRequestParams struct {
	Controller Controller
}

// PayloadLenStatusRequest is the fixed MESSAGE byte count for rx 07.
const PayloadLenStatusRequest = 1

// EncodeStatusRequest builds rx 07 wire bytes.
func EncodeStatusRequest(p StatusRequestParams) Frame {
	return Frame{
		ID:      RxStatusRequest,
		Payload: []byte{byte(p.Controller)},
	}
}

// DecodeStatusRequest parses rx 07.
func DecodeStatusRequest(f Frame) (StatusRequestParams, error) {
	if f.ID != RxStatusRequest {
		return StatusRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenStatusRequest {
		return StatusRequestParams{}, ErrShortPayload
	}
	return StatusRequestParams{Controller: Controller(f.Payload[0])}, nil
}
