package probelsw08p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw08p/codec"
	"acp/internal/protocol"
)

// ProtectConnect requests protection on (matrix, level, dst) owned by
// device. Reply is tx 013 Protect Connected (same wire shape as tally).
// Other sessions also see a tx 011 Protect Tally broadcast.
//
// Reference: SW-P-08 §3.2 (rx 012) / §3.3 (tx 013). TS rx/012/.
func (p *Plugin) ProtectConnect(
	ctx context.Context,
	matrix, level uint8,
	dst, device uint16,
) (codec.ProtectConnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ProtectConnectedParams{}, err
	}
	req := codec.EncodeProtectConnect(codec.ProtectConnectParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxProtectConnected || f.ID == codec.TxProtectConnectedExt
	})
	if err != nil {
		return codec.ProtectConnectedParams{}, fmt.Errorf("probel protect-connect: %w", err)
	}
	c, derr := codec.DecodeProtectConnected(reply)
	if derr != nil {
		return codec.ProtectConnectedParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return c, nil
}
