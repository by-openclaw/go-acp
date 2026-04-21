package probel

import (
	"context"
	"fmt"

	"acp/internal/protocol/probel/codec"
	"acp/internal/protocol"
)

// ProtectInterrogate asks the matrix "what is the protect state of this
// destination?". Replies with tx 011 Protect Tally carrying the owning
// device ID + one of the four ProtectState values.
//
// Reference: SW-P-08 §3.2 (rx 010) / §3.3 (tx 011). TS rx/010/ + tx/011/.
func (p *Plugin) ProtectInterrogate(
	ctx context.Context,
	matrix, level uint8,
	dst, device uint16,
) (codec.ProtectTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ProtectTallyParams{}, err
	}
	req := codec.EncodeProtectInterrogate(codec.ProtectInterrogateParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxProtectTally || f.ID == codec.TxProtectTallyExt
	})
	if err != nil {
		return codec.ProtectTallyParams{}, fmt.Errorf("probel protect-interrogate: %w", err)
	}
	t, derr := codec.DecodeProtectTally(reply)
	if derr != nil {
		return codec.ProtectTallyParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return t, nil
}
