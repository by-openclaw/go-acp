package probel

// --- rx 029 : Master Protect Connect ---------------------------------------

// MasterProtectConnectParams is a PROTECT CONNECT variant issued by a remote
// device that claims MASTER-panel privilege: it overrides any existing
// protection set by another panel. The router replies with a regular tx 013
// PROTECT CONNECTED broadcast.
//
// General (and only) form; no extended variant. Reference: TS rx/029/command.ts.
type MasterProtectConnectParams struct {
	MatrixID      uint8
	LevelID       uint8
	DestinationID uint16
	DeviceID      uint16
}

// EncodeMasterProtectConnect builds the MASTER PROTECT CONNECT request.
//
// General form (CommandID 0x1D — 6 data bytes; uses extended-style addressing
// even in the only form — matrix/level are full u8, dest/device are u16):
//
//	| Byte | Field           | Notes                                   |
//	|------|-----------------|-----------------------------------------|
//	|  1   | Matrix          | full 8-bit                              |
//	|  2   | Level           | full 8-bit                              |
//	|  3   | Dest multiplier | Destination DIV 256                     |
//	|  4   | Dest (low 8b)   | Destination MOD 256                     |
//	|  5   | Dev  multiplier | Device DIV 256                          |
//	|  6   | Dev  (low 8b)   | Device MOD 256                          |
//
// Spec: SW-P-88 §5.31. Reference: TS rx/029/command.ts.
func EncodeMasterProtectConnect(p MasterProtectConnectParams) Frame {
	return Frame{
		ID: RxMasterProtectConnect,
		Payload: []byte{
			p.MatrixID,
			p.LevelID,
			byte(p.DestinationID / 256),
			byte(p.DestinationID % 256),
			byte(p.DeviceID / 256),
			byte(p.DeviceID % 256),
		},
	}
}

// DecodeMasterProtectConnect parses the request payload.
func DecodeMasterProtectConnect(f Frame) (MasterProtectConnectParams, error) {
	if f.ID != RxMasterProtectConnect {
		return MasterProtectConnectParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 6 {
		return MasterProtectConnectParams{}, ErrShortPayload
	}
	return MasterProtectConnectParams{
		MatrixID:      f.Payload[0],
		LevelID:       f.Payload[1],
		DestinationID: uint16(f.Payload[2])*256 + uint16(f.Payload[3]),
		DeviceID:      uint16(f.Payload[4])*256 + uint16(f.Payload[5]),
	}, nil
}
