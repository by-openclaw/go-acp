package probel

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
// Spec: SW-P-08 §3.2.17. Reference: TS rx/014/command.ts.
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
