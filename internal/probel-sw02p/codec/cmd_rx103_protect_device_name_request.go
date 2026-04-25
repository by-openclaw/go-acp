package codec

// ProtectDeviceNameRequestParams carries rx 103 PROTECT DEVICE NAME
// REQUEST fields — §3.2.67. Either peer issues this to resolve the
// 8-char ASCII name protecting a destination. The target peer
// replies with tx 099 PROTECT DEVICE NAME RESPONSE carrying the
// name. Note: this command is bidirectional on the wire — §3.2.67
// says "Any device issues this message". The Rx/Tx convention here
// follows the spec's primary controller→matrix direction; our
// plugin supports both roles via the codec + the provider /
// consumer helpers.
//
// Device numbering is narrow (0-1023) — packed into a 3-bit DIV 128
// field in the Multiplier byte, unlike the extended 7-bit DIV 128
// elsewhere.
//
//	| Byte | Field              | Notes                              |
//	|------|--------------------|------------------------------------|
//	|  1   | Multiplier         | bit 7 = 0                          |
//	|      |                    | bit 3-6 = unused                   |
//	|      |                    | bit 0-2 = Device DIV 128 (0-7)     |
//	|  2   | Device MOD 128     |                                    |
//
// Spec: SW-P-02 Issue 26 §3.2.67.
type ProtectDeviceNameRequestParams struct {
	Device uint16 // 0-1023
}

// PayloadLenProtectDeviceNameRequest is the fixed MESSAGE byte count
// for rx 103.
const PayloadLenProtectDeviceNameRequest = 2

// EncodeProtectDeviceNameRequest builds rx 103 wire bytes.
func EncodeProtectDeviceNameRequest(p ProtectDeviceNameRequestParams) Frame {
	return Frame{
		ID: RxProtectDeviceNameRequest,
		Payload: []byte{
			byte((p.Device / 128) & 0x07),
			byte(p.Device % 128),
		},
	}
}

// DecodeProtectDeviceNameRequest parses rx 103.
func DecodeProtectDeviceNameRequest(f Frame) (ProtectDeviceNameRequestParams, error) {
	if f.ID != RxProtectDeviceNameRequest {
		return ProtectDeviceNameRequestParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenProtectDeviceNameRequest {
		return ProtectDeviceNameRequestParams{}, ErrShortPayload
	}
	return ProtectDeviceNameRequestParams{
		Device: (uint16(f.Payload[0])&0x07)*128 + uint16(f.Payload[1]),
	}, nil
}
