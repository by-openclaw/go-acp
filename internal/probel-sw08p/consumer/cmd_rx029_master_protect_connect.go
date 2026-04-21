package probel

import (
	"context"
	"fmt"

	"acp/internal/probel-sw08p/codec"
	"acp/internal/protocol"
)

// MasterProtectConnect is the override-flavoured Protect Connect used
// by master panels to seize a protect already held by another panel.
// Reply is a normal tx 013 Protect Connected broadcast.
//
// Reference: SW-P-08 §3.2 (rx 029) / §3.3 (tx 013). TS rx/029/.
func (p *Plugin) MasterProtectConnect(
	ctx context.Context,
	matrix, level uint8,
	dst, device uint16,
) (codec.ProtectConnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ProtectConnectedParams{}, err
	}
	req := codec.EncodeMasterProtectConnect(codec.MasterProtectConnectParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxProtectConnected || f.ID == codec.TxProtectConnectedExt
	})
	if err != nil {
		return codec.ProtectConnectedParams{}, fmt.Errorf("probel master-protect: %w", err)
	}
	c, derr := codec.DecodeProtectConnected(reply)
	if derr != nil {
		return codec.ProtectConnectedParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return c, nil
}
