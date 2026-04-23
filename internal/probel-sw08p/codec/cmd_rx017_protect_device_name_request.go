package codec

// ProtectDeviceNameRequestParams queries the 8-character ASCII device name
// that currently holds a given deviceID in the controller's protect device
// table. This is used by a Probel device to resolve an OEM device, or vice
// versa.
//
// General form only (no extended variant). Reference: TS rx/017/params.ts.
//
// NOTE: CommandID 0x11 is direction-overloaded with TxAppKeepaliveRequest
// (matrix → controller ping). Consumers decode inbound 0x11 as keepalive;
// providers decode inbound 0x11 as this ProtectDeviceNameRequest.
type ProtectDeviceNameRequestParams struct {
	DeviceID uint16 // 0-1023
}

// EncodeProtectDeviceNameRequest builds the PROTECT DEVICE NAME REQUEST.
//
// General form (CommandID 0x11 — 2 data bytes):
//
//	| Byte | Field           | Notes                                   |
//	|------|-----------------|-----------------------------------------|
//	|  1   | Multiplier      | bit[7]   = 0                            |
//	|      |                 | bits[4-6]= 0                            |
//	|      |                 | bit[3]   = 0                            |
//	|      |                 | bits[0-2]= Device DIV 128               |
//	|  2   | Device (low 7b) | Device MOD 128                          |
//
// Spec: SW-P-08 §3.2.21. Reference: TS rx/017/command.ts.
func EncodeProtectDeviceNameRequest(p ProtectDeviceNameRequestParams) Frame {
	return Frame{
		ID: RxProtectDeviceNameRequest,
		Payload: []byte{
			byte((p.DeviceID / 128) & 0x07),
			byte(p.DeviceID % 128),
		},
	}
}

// DecodeProtectDeviceNameRequest parses the request payload.
func DecodeProtectDeviceNameRequest(f Frame) (ProtectDeviceNameRequestParams, error) {
	if f.ID != RxProtectDeviceNameRequest {
		return ProtectDeviceNameRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 2 {
		return ProtectDeviceNameRequestParams{}, ErrShortPayload
	}
	return ProtectDeviceNameRequestParams{
		DeviceID: (uint16(f.Payload[0]) & 0x07) * 128 + uint16(f.Payload[1]),
	}, nil
}
