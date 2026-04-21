package codec

// ProtectDisconnectedParams broadcasts the protect-data change after a
// PROTECT DIS-CONNECT. State should be ProtectNone on success; on failure
// State reflects the unchanged current state.
//
// Wire layout identical to tx 011 / tx 013; CommandID 0x0F (ext 0x8F).
// Reference: TS tx/015/params.ts.
type ProtectDisconnectedParams = ProtectTallyParams

// EncodeProtectDisconnected builds the PROTECT DIS-CONNECTED broadcast.
// Same layout as tx 011 (Protect Tally); see EncodeProtectTally in
// cmd_tx011_protect_tally.go.
//
// Spec: SW-P-08 §3.3.18. Reference: TS tx/015/command.ts.
func EncodeProtectDisconnected(p ProtectDisconnectedParams) Frame {
	f := EncodeProtectTally(p)
	if f.ID == TxProtectTallyExt {
		f.ID = TxProtectDisconnectedExt
	} else {
		f.ID = TxProtectDisconnected
	}
	return f
}

// DecodeProtectDisconnected parses a PROTECT DIS-CONNECTED payload.
func DecodeProtectDisconnected(f Frame) (ProtectDisconnectedParams, error) {
	switch f.ID {
	case TxProtectDisconnected:
		f.ID = TxProtectTally
	case TxProtectDisconnectedExt:
		f.ID = TxProtectTallyExt
	default:
		return ProtectDisconnectedParams{}, ErrWrongCommand
	}
	return DecodeProtectTally(f)
}
