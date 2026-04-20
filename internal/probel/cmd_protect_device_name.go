package probel

import "strings"

// --- rx 017 : Protect Device Name Request ----------------------------------

// ProtectDeviceNameRequestParams queries the 8-character ASCII device name
// that currently holds a given deviceID in the controller's protect device
// table. This is used by a Probel device to resolve an OEM device, or vice
// versa.
//
// General form only (no extended variant). Reference: TS rx/017/params.ts.
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
// Spec: SW-P-88 §5.21. Reference: TS rx/017/command.ts.
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

// --- tx 018 : Protect Device Name Response ---------------------------------

// ProtectDeviceNameResponseParams answers rx 017 with the 8-character ASCII
// device name. Names shorter than 8 chars are left-padded with spaces on the
// wire (matching TS rx/018 behaviour); the decoder trims trailing spaces.
//
// Reference: TS tx/018/params.ts ProtectDeviceNameResponseCommandParams.
type ProtectDeviceNameResponseParams struct {
	DeviceID   uint16 // 0-1023
	DeviceName string // ≤ 8 chars, left-padded with ' ' on wire
}

// EncodeProtectDeviceNameResponse builds the PROTECT DEVICE NAME RESPONSE.
//
// General form (CommandID 0x12 — 10 data bytes):
//
//	| Byte  | Field           | Notes                                  |
//	|-------|-----------------|----------------------------------------|
//	|  1    | Multiplier      | bits[0-2] = Device DIV 128             |
//	|  2    | Device (low 7b) | Device MOD 128                         |
//	|  3..10| Name (8 bytes)  | ASCII, left-padded with SPACE (0x20)   |
//
// Spec: SW-P-88 §5.21. Reference: TS tx/018/command.ts.
func EncodeProtectDeviceNameResponse(p ProtectDeviceNameResponseParams) Frame {
	name := p.DeviceName
	if len(name) > 8 {
		name = name[:8]
	}
	padded := strings.Repeat(" ", 8-len(name)) + name
	out := make([]byte, 0, 10)
	out = append(out,
		byte((p.DeviceID/128)&0x07),
		byte(p.DeviceID%128),
	)
	out = append(out, padded...)
	return Frame{ID: TxProtectDeviceNameResponse, Payload: out}
}

// DecodeProtectDeviceNameResponse parses the response payload and trims the
// space-padding from DeviceName.
func DecodeProtectDeviceNameResponse(f Frame) (ProtectDeviceNameResponseParams, error) {
	if f.ID != TxProtectDeviceNameResponse {
		return ProtectDeviceNameResponseParams{}, ErrWrongCommand
	}
	if len(f.Payload) < 10 {
		return ProtectDeviceNameResponseParams{}, ErrShortPayload
	}
	return ProtectDeviceNameResponseParams{
		DeviceID:   (uint16(f.Payload[0]) & 0x07) * 128 + uint16(f.Payload[1]),
		DeviceName: strings.TrimLeft(string(f.Payload[2:10]), " "),
	}, nil
}
