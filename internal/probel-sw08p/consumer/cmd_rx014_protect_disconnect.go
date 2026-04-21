package probel

import (
	"context"
	"fmt"

	"acp/internal/probel-sw08p/codec"
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
) (codec.ProtectDisconnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ProtectDisconnectedParams{}, err
	}
	req := codec.EncodeProtectDisconnect(codec.ProtectDisconnectParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxProtectDisconnected || f.ID == codec.TxProtectDisconnectedExt
	})
	if err != nil {
		return codec.ProtectDisconnectedParams{}, fmt.Errorf("probel protect-disconnect: %w", err)
	}
	d, derr := codec.DecodeProtectDisconnected(reply)
	if derr != nil {
		return codec.ProtectDisconnectedParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return d, nil
}
