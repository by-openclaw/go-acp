package probel

// DualControllerStatusParams describes the active/idle controller state in
// a dual-controller system (1:1 redundancy).
//
// Reference: TS tx/009/options.ts DualControllerStatusResponseMessageCommandOptions.
type DualControllerStatusParams struct {
	// SlaveActive is true when the SLAVE controller is the active one.
	// Byte 1 bit 0: 0 = MASTER active, 1 = SLAVE active.
	SlaveActive bool
	// Active is the ACTIVE flag of the queried controller. Bit 1 of byte 1.
	// Semantics per spec: 0 = Inactive, 1 = Active.
	Active bool
	// IdleControllerFaulty reports whether the idle peer is missing/faulty.
	// Byte 2: 0 = OK, 1 = missing/faulty.
	IdleControllerFaulty bool
}

// EncodeDualControllerStatusResponse builds the tx 009 reply.
//
// General form (CommandID 0x09 — 2-byte payload):
//
//	| Byte | Field      | Notes                                           |
//	|------|------------|-------------------------------------------------|
//	|  1   | Status     | bit[0] = SlaveActive (0=MASTER, 1=SLAVE)        |
//	|      |            | bit[1] = Active     (0=Inactive, 1=Active)      |
//	|      |            | bits[2-7] reserved, 0                           |
//	|  2   | Idle Card  | 0 = idle controller OK, 1 = missing/faulty      |
//
// Spec: SW-P-08 §3.3.10. Reference: TS tx/009/command.ts buildDataNormal.
func EncodeDualControllerStatusResponse(p DualControllerStatusParams) Frame {
	var status byte
	if p.SlaveActive {
		status |= 0x01
	}
	if p.Active {
		status |= 0x02
	}
	var idle byte
	if p.IdleControllerFaulty {
		idle = 1
	}
	return Frame{
		ID:      TxDualControllerStatusResponse,
		Payload: []byte{status, idle},
	}
}

// DecodeDualControllerStatusResponse parses the 2-byte tx 009 payload.
func DecodeDualControllerStatusResponse(f Frame) (DualControllerStatusParams, error) {
	if f.ID != TxDualControllerStatusResponse {
		return DualControllerStatusParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 2 {
		return DualControllerStatusParams{}, ErrShortPayload
	}
	status := f.Payload[0]
	return DualControllerStatusParams{
		SlaveActive:          status&0x01 != 0,
		Active:               status&0x02 != 0,
		IdleControllerFaulty: f.Payload[1] != 0,
	}, nil
}
