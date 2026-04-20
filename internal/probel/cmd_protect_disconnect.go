package probel

// --- rx 014 / rx 142 : Protect Disconnect ----------------------------------

// ProtectDisconnectParams clears protection set by the given deviceID on
// (matrix, level, destination). The router replies with tx 015 Protect
// Disconnected.
//
// Wire layout is byte-for-byte identical to rx 012 Protect Connect — only the
// CommandID differs (0x0E vs 0x0C; extended 0x8E vs 0x8C). We alias the
// params and encoder through the connect implementation to keep behaviour
// in sync.
//
// Reference: TS rx/014/params.ts ProtectDisConnectMessageCommandParams.
type ProtectDisconnectParams = ProtectConnectParams

// EncodeProtectDisconnect builds the PROTECT DIS-CONNECT request.
//
// General form (CommandID 0x0E — 4 data bytes, same layout as rx 012):
//
//	| Byte | Field           | Notes                                   |
//	|------|-----------------|-----------------------------------------|
//	|  1   | Matrix / Level  | bits[4-7] = Matrix, bits[0-3] = Level   |
//	|  2   | Multiplier      | bits[4-6] = Dest DIV 128                |
//	|      |                 | bits[0-2] = Device DIV 128              |
//	|  3   | Dest (low 7b)   | Destination MOD 128                     |
//	|  4   | Device (low 7b) | Device MOD 128                          |
//
// Extended form (CommandID 0x8E — 6 data bytes, same layout as rx 140).
//
// Spec: SW-P-88 §5.17. Reference: TS rx/014/command.ts.
func EncodeProtectDisconnect(p ProtectDisconnectParams) Frame {
	f := EncodeProtectConnect(p)
	if f.ID == RxProtectConnectExt {
		f.ID = RxProtectDisconnectExt
	} else {
		f.ID = RxProtectDisconnect
	}
	return f
}

// DecodeProtectDisconnect parses a PROTECT DIS-CONNECT payload.
func DecodeProtectDisconnect(f Frame) (ProtectDisconnectParams, error) {
	switch f.ID {
	case RxProtectDisconnect:
		f.ID = RxProtectConnect
	case RxProtectDisconnectExt:
		f.ID = RxProtectConnectExt
	default:
		return ProtectDisconnectParams{}, ErrWrongCommand
	}
	return DecodeProtectConnect(f)
}

// --- tx 015 / tx 143 : Protect Disconnected --------------------------------

// ProtectDisconnectedParams broadcasts the protect-data change after a
// PROTECT DIS-CONNECT. State should be ProtectNone on success; on failure
// State reflects the unchanged current state.
//
// Wire layout identical to tx 011 / tx 013; CommandID 0x0F (ext 0x8F).
// Reference: TS tx/015/params.ts.
type ProtectDisconnectedParams = ProtectTallyParams

// EncodeProtectDisconnected builds the PROTECT DIS-CONNECTED broadcast.
// Same layout as tx 011 (Protect Tally); see EncodeProtectTally doc table.
//
// Spec: SW-P-88 §5.18. Reference: TS tx/015/command.ts.
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
