package codec

// ActiveController identifies which card is the currently active
// controller in a dual-controller system. §3.2.46.
type ActiveController uint8

// Active controller values — §3.2.46.
const (
	ActiveControllerMaster ActiveController = 0x00
	ActiveControllerSlave  ActiveController = 0x01
)

// IdleControllerStatus reports whether the idle (standby) controller
// is healthy. §3.2.46.
type IdleControllerStatus uint8

// Idle controller status values — §3.2.46.
const (
	IdleControllerOK      IdleControllerStatus = 0x00
	IdleControllerFaulty  IdleControllerStatus = 0x01
)

// DualControllerStatusResponseParams carries tx 051 DUAL CONTROLLER
// STATUS RESPONSE fields — emitted by the active controller on
// power-up or in response to rx 050 DUAL CONTROLLER STATUS REQUEST.
// See §3.2.46.
//
//	| Byte | Field             | Notes                              |
//	|------|-------------------|------------------------------------|
//	|  1   | Active Controller | 0 = Master active, 1 = Slave active|
//	|  2   | Idle Status       | 0 = Idle OK, 1 = Idle missing/fault|
//
// Spec: SW-P-02 Issue 26 §3.2.46.
type DualControllerStatusResponseParams struct {
	Active     ActiveController
	IdleStatus IdleControllerStatus
}

// PayloadLenDualControllerStatusResponse is the fixed MESSAGE byte
// count for tx 051.
const PayloadLenDualControllerStatusResponse = 2

// EncodeDualControllerStatusResponse builds tx 051 wire bytes.
func EncodeDualControllerStatusResponse(p DualControllerStatusResponseParams) Frame {
	return Frame{
		ID: TxDualControllerStatusResponse,
		Payload: []byte{
			byte(p.Active),
			byte(p.IdleStatus),
		},
	}
}

// DecodeDualControllerStatusResponse parses tx 051.
func DecodeDualControllerStatusResponse(f Frame) (DualControllerStatusResponseParams, error) {
	if f.ID != TxDualControllerStatusResponse {
		return DualControllerStatusResponseParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenDualControllerStatusResponse {
		return DualControllerStatusResponseParams{}, ErrShortPayload
	}
	return DualControllerStatusResponseParams{
		Active:     ActiveController(f.Payload[0]),
		IdleStatus: IdleControllerStatus(f.Payload[1]),
	}, nil
}
