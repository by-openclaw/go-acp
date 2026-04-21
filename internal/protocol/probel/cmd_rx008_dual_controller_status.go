package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// DualControllerStatus queries the 1:1 redundancy state (master vs.
// slave, active flag, idle-controller health). Matches rx 008 → tx 009.
//
// Reference: SW-P-08 §3.2 (rx 008) / §3.3 (tx 009). TS rx/008/ + tx/009/.
func (p *Plugin) DualControllerStatus(
	ctx context.Context,
) (iprobel.DualControllerStatusParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.DualControllerStatusParams{}, err
	}
	req := iprobel.EncodeDualControllerStatusRequest()
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxDualControllerStatusResponse
	})
	if err != nil {
		return iprobel.DualControllerStatusParams{}, fmt.Errorf("probel dual-status: %w", err)
	}
	r, derr := iprobel.DecodeDualControllerStatusResponse(reply)
	if derr != nil {
		return iprobel.DualControllerStatusParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r, nil
}
