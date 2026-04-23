package codec

// EncodeDualControllerStatusRequest builds the empty-payload request.
//
// General form (CommandID 0x08 — 1-byte DATA = ID only):
//
//	| Byte    | Field | Notes              |
//	|---------|-------|--------------------|
//	| (none)  | —     | no payload bytes   |
//
// The controller replies with tx 009 (DualControllerStatusResponse).
// Spec: SW-P-08 §3.2.9. Reference: TS rx/008/command.ts.
func EncodeDualControllerStatusRequest() Frame {
	return Frame{ID: RxDualControllerStatusRequest}
}

// DecodeDualControllerStatusRequest validates an incoming empty-payload
// request. Returns ErrWrongCommand if the command ID does not match.
func DecodeDualControllerStatusRequest(f Frame) error {
	if f.ID != RxDualControllerStatusRequest {
		return ErrWrongCommand
	}
	return nil
}
