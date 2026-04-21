package probel

import (
	"context"
	"fmt"

	"acp/internal/protocol/probel/codec"
	"acp/internal/protocol"
)

// DualControllerStatus queries the 1:1 redundancy state (master vs.
// slave, active flag, idle-controller health). Matches rx 008 → tx 009.
//
// Reference: SW-P-08 §3.2 (rx 008) / §3.3 (tx 009). TS rx/008/ + tx/009/.
func (p *Plugin) DualControllerStatus(
	ctx context.Context,
) (codec.DualControllerStatusParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.DualControllerStatusParams{}, err
	}
	req := codec.EncodeDualControllerStatusRequest()
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxDualControllerStatusResponse
	})
	if err != nil {
		return codec.DualControllerStatusParams{}, fmt.Errorf("probel dual-status: %w", err)
	}
	r, derr := codec.DecodeDualControllerStatusResponse(reply)
	if derr != nil {
		return codec.DualControllerStatusParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r, nil
}
