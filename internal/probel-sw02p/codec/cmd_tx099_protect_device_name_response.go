package codec

// ProtectDeviceNameResponseParams carries tx 099 PROTECT DEVICE NAME
// RESPONSE fields — §3.2.63. Either peer replies with this to
// resolve the 8-char ASCII name of the device protecting a given
// Device Number.
//
// Name is exactly 8 ASCII characters on the wire; callers pass any
// length and the encoder pads with space (0x20) or truncates. The
// decoder trims trailing spaces + NULs on output to give callers a
// cleaned-up string.
//
//	| Bytes | Field              | Notes                             |
//	|-------|--------------------|-----------------------------------|
//	|   1   | Multiplier         | bit 7 = 0, bit 3-6 unused,        |
//	|       |                    | bit 0-2 Device DIV 128 (0-7)      |
//	|   2   | Device MOD 128     |                                   |
//	| 3-10  | 8-char ASCII name  | space-padded to 8 bytes           |
//
// Spec: SW-P-02 Issue 26 §3.2.63.
type ProtectDeviceNameResponseParams struct {
	Device uint16 // 0-1023
	Name   string // up to 8 chars; longer → truncated, shorter → space-padded
}

// PayloadLenProtectDeviceNameResponse is the fixed MESSAGE byte count
// for tx 099 (2 header bytes + 8 ASCII bytes).
const PayloadLenProtectDeviceNameResponse = 10

// ProtectDeviceNameSize is the fixed wire width of the 8-char ASCII
// name field in tx 099 (§3.2.63).
const ProtectDeviceNameSize = 8

// EncodeProtectDeviceNameResponse builds tx 099 wire bytes. Name is
// coerced to exactly ProtectDeviceNameSize bytes — space-padded if
// shorter, truncated if longer.
func EncodeProtectDeviceNameResponse(p ProtectDeviceNameResponseParams) Frame {
	payload := make([]byte, PayloadLenProtectDeviceNameResponse)
	payload[0] = byte((p.Device / 128) & 0x07)
	payload[1] = byte(p.Device % 128)
	// Name field at bytes 3-10 (payload[2:10]).
	name := p.Name
	if len(name) > ProtectDeviceNameSize {
		name = name[:ProtectDeviceNameSize]
	}
	copy(payload[2:], name)
	// Pad the remainder with spaces per the §3.2.63 "eight character"
	// fixed-width convention.
	for i := 2 + len(name); i < PayloadLenProtectDeviceNameResponse; i++ {
		payload[i] = 0x20
	}
	return Frame{ID: TxProtectDeviceNameResponse, Payload: payload}
}

// DecodeProtectDeviceNameResponse parses tx 099. The decoded Name has
// trailing spaces and NULs trimmed for callers that want the "real"
// device name without padding artefacts.
func DecodeProtectDeviceNameResponse(f Frame) (ProtectDeviceNameResponseParams, error) {
	if f.ID != TxProtectDeviceNameResponse {
		return ProtectDeviceNameResponseParams{}, ErrWrongCommand
	}
	if len(f.Payload) < PayloadLenProtectDeviceNameResponse {
		return ProtectDeviceNameResponseParams{}, ErrShortPayload
	}
	dev := (uint16(f.Payload[0])&0x07)*128 + uint16(f.Payload[1])
	raw := f.Payload[2:PayloadLenProtectDeviceNameResponse]
	// Trim trailing spaces + NULs without allocating a second slice.
	end := len(raw)
	for end > 0 && (raw[end-1] == 0x20 || raw[end-1] == 0x00) {
		end--
	}
	return ProtectDeviceNameResponseParams{
		Device: dev,
		Name:   string(raw[:end]),
	}, nil
}
