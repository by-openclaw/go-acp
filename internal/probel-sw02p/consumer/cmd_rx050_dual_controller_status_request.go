package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendDualControllerStatusRequest emits rx 050 DUAL CONTROLLER STATUS
// REQUEST (§3.2.45) and waits for the matching tx 051 DUAL
// CONTROLLER STATUS RESPONSE (§3.2.46). The request carries no
// MESSAGE bytes — only the COMMAND byte itself.
func (p *Plugin) SendDualControllerStatusRequest(ctx context.Context) (codec.DualControllerStatusResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.DualControllerStatusResponseParams{}, err
	}
	req := codec.EncodeDualControllerStatusRequest(codec.DualControllerStatusRequestParams{})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxDualControllerStatusResponse
	})
	if err != nil {
		return codec.DualControllerStatusResponseParams{}, fmt.Errorf("probel-sw02p: SendDualControllerStatusRequest: %w", err)
	}
	resp, err := codec.DecodeDualControllerStatusResponse(reply)
	if err != nil {
		return codec.DualControllerStatusResponseParams{}, fmt.Errorf("probel-sw02p: decode tx 051: %w", err)
	}
	return resp, nil
}
