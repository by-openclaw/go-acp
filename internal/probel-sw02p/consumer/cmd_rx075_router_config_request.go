package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// RouterConfigResponse captures either tx 076 RESPONSE-1 (§3.2.58)
// or tx 077 RESPONSE-2 (§3.2.59). Exactly one of Response1 / Response2
// is non-nil on success — the matrix chooses which variant to emit,
// and real routers vary (older HD cards ship RESPONSE-1, newer XD /
// Eclipse controllers emit RESPONSE-2 to report non-contiguous level
// addressing).
type RouterConfigResponse struct {
	Response1 *codec.RouterConfigResponse1Params
	Response2 *codec.RouterConfigResponse2Params
}

// SendRouterConfigRequest emits rx 075 ROUTER CONFIGURATION REQUEST
// (§3.2.57) and waits for whichever of tx 076 / tx 077 the matrix
// chooses to emit. Returns the decoded payload as a RouterConfig
// Response tagged-union.
func (p *Plugin) SendRouterConfigRequest(ctx context.Context) (RouterConfigResponse, error) {
	cli, err := p.getClient()
	if err != nil {
		return RouterConfigResponse{}, err
	}
	req := codec.EncodeRouterConfigRequest(codec.RouterConfigRequestParams{})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxRouterConfigResponse1 || f.ID == codec.TxRouterConfigResponse2
	})
	if err != nil {
		return RouterConfigResponse{}, fmt.Errorf("probel-sw02p: SendRouterConfigRequest: %w", err)
	}
	switch reply.ID {
	case codec.TxRouterConfigResponse1:
		r1, derr := codec.DecodeRouterConfigResponse1(reply)
		if derr != nil {
			return RouterConfigResponse{}, fmt.Errorf("probel-sw02p: decode tx 076: %w", derr)
		}
		return RouterConfigResponse{Response1: &r1}, nil
	case codec.TxRouterConfigResponse2:
		r2, derr := codec.DecodeRouterConfigResponse2(reply)
		if derr != nil {
			return RouterConfigResponse{}, fmt.Errorf("probel-sw02p: decode tx 077: %w", derr)
		}
		return RouterConfigResponse{Response2: &r2}, nil
	}
	return RouterConfigResponse{}, fmt.Errorf("probel-sw02p: unexpected reply ID %#x", reply.ID)
}
