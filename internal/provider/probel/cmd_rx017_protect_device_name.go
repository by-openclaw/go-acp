package probel

import (
	"acp/internal/protocol/probel/codec"
)

// handleProtectDeviceNameRequest resolves the device-id to a name via
// the tree's process-wide device-name map. Unknown IDs get a positional
// default ("DEV 0042") — never an error, because the wire protocol has
// no failure reply for this request.
//
// Reference: SW-P-08 §3.2 (rx 017) → §3.3 (tx 018).
func (s *server) handleProtectDeviceNameRequest(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeProtectDeviceNameRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	reply := codec.EncodeProtectDeviceNameResponse(codec.ProtectDeviceNameResponseParams{
		DeviceID:   p.DeviceID,
		DeviceName: s.tree.deviceName(p.DeviceID),
	})
	return handlerResult{reply: &reply}, nil
}
