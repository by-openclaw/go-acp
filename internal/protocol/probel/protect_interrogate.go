package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
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
) (iprobel.ProtectTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.ProtectTallyParams{}, err
	}
	req := iprobel.EncodeProtectInterrogate(iprobel.ProtectInterrogateParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, DeviceID: device,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxProtectTally || f.ID == iprobel.TxProtectTallyExt
	})
	if err != nil {
		return iprobel.ProtectTallyParams{}, fmt.Errorf("probel protect-interrogate: %w", err)
	}
	t, derr := iprobel.DecodeProtectTally(reply)
	if derr != nil {
		return iprobel.ProtectTallyParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return t, nil
}
