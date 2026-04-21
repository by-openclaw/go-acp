package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// ProtectDisconnect releases the protect on (matrix, level, dst) owned
// by device. Reply is tx 015 Protect Disconnected; tally broadcast to
// other sessions as tx 011 with state=ProtectNone.
//
// Reference: SW-P-08 §3.2 (rx 014) / §3.3 (tx 015). TS rx/014/.
func (p *Plugin) ProtectDisconnect(
	ctx context.Context,
	matrix, level uint8,
	dst, device uint16,
) (iprobel.ProtectDisconnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.ProtectDisconnectedParams{}, err
	}
	req := iprobel.EncodeProtectDisconnect(iprobel.ProtectDisconnectParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxProtectDisconnected || f.ID == iprobel.TxProtectDisconnectedExt
	})
	if err != nil {
		return iprobel.ProtectDisconnectedParams{}, fmt.Errorf("probel protect-disconnect: %w", err)
	}
	d, derr := iprobel.DecodeProtectDisconnected(reply)
	if derr != nil {
		return iprobel.ProtectDisconnectedParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return d, nil
}
