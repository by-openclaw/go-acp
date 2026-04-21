package probel

import (
	"context"
	"fmt"

	"acp/internal/protocol/probel/codec"
	"acp/internal/protocol"
)

// ProtectDeviceName resolves a deviceID (0-1023) to its 8-char ASCII
// name as held in the matrix's device table. Wire form is the left-
// space-padded 8-byte name trimmed by the decoder.
//
// Reference: SW-P-08 §3.2 (rx 017) / §3.3 (tx 018). TS rx/017/ + tx/018/.
func (p *Plugin) ProtectDeviceName(
	ctx context.Context,
	device uint16,
) (string, error) {
	cli, err := p.getClient()
	if err != nil {
		return "", err
	}
	req := codec.EncodeProtectDeviceNameRequest(codec.ProtectDeviceNameRequestParams{DeviceID: device})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxProtectDeviceNameResponse
	})
	if err != nil {
		return "", fmt.Errorf("probel protect-name: %w", err)
	}
	r, derr := codec.DecodeProtectDeviceNameResponse(reply)
	if derr != nil {
		return "", &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r.DeviceName, nil
}
