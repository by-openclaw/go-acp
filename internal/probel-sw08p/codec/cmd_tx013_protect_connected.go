package codec

// ProtectConnectedParams reports protect-data state change — emitted by the
// router on all ports after a successful PROTECT CONNECT (or to signal a
// failed attempt, in which case State reflects the current unchanged state).
//
// Wire layout identical to tx 011 Protect Tally; only CommandID differs
// (0x0D vs 0x0B). Reference: TS tx/013/params.ts + options.ts.
type ProtectConnectedParams = ProtectTallyParams

// EncodeProtectConnected builds the PROTECT CONNECTED broadcast reply.
// Layout byte-for-byte the same as tx 011; see EncodeProtectTally in
// cmd_tx011_protect_tally.go. Only the CommandID differs (0x0D / 0x8D).
//
// Spec: SW-P-08 §3.3.16. Reference: TS tx/013/command.ts.
func EncodeProtectConnected(p ProtectConnectedParams) Frame {
	f := EncodeProtectTally(p)
	if f.ID == TxProtectTallyExt {
		f.ID = TxProtectConnectedExt
	} else {
		f.ID = TxProtectConnected
	}
	return f
}

// DecodeProtectConnected parses a PROTECT CONNECTED payload.
func DecodeProtectConnected(f Frame) (ProtectConnectedParams, error) {
	switch f.ID {
	case TxProtectConnected:
		f.ID = TxProtectTally
	case TxProtectConnectedExt:
		f.ID = TxProtectTallyExt
	default:
		return ProtectConnectedParams{}, ErrWrongCommand
	}
	return DecodeProtectTally(f)
}
