package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
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
) (iprobel.ProtectConnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.ProtectConnectedParams{}, err
	}
	req := iprobel.EncodeMasterProtectConnect(iprobel.MasterProtectConnectParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxProtectConnected || f.ID == iprobel.TxProtectConnectedExt
	})
	if err != nil {
		return iprobel.ProtectConnectedParams{}, fmt.Errorf("probel master-protect: %w", err)
	}
	c, derr := iprobel.DecodeProtectConnected(reply)
	if derr != nil {
		return iprobel.ProtectConnectedParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return c, nil
}
