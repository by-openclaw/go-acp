package codec

import "strings"

// ProtectDeviceNameResponseParams answers rx 017 with the 8-character ASCII
// device name. Names shorter than 8 chars are left-padded with spaces on the
// wire (matching TS tx/018 behaviour); the decoder trims trailing spaces.
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
// Spec: SW-P-08 §3.3.21. Reference: TS tx/018/command.ts.
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
