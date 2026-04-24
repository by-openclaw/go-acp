package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendStatusRequest emits rx 07 STATUS REQUEST (§3.2.9) targeting the
// specified controller and waits for the matrix's tx 09 STATUS
// RESPONSE - 2 reply (§3.2.11). Other response shapes (§3.2.10 tx 08,
// §3.2.12 tx 10, §3.2.18 tx 16, §3.2.19 tx 17) are out of scope for
// this VSM-supported set and return ErrAwaitFilter if emitted.
func (p *Plugin) SendStatusRequest(ctx context.Context, controller codec.Controller) (codec.StatusResponse2Params, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.StatusResponse2Params{}, err
	}
	req := codec.EncodeStatusRequest(codec.StatusRequestParams{Controller: controller})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxStatusResponse2
	})
	if err != nil {
		return codec.StatusResponse2Params{}, fmt.Errorf("probel-sw02p: SendStatusRequest: %w", err)
	}
	s, err := codec.DecodeStatusResponse2(reply)
	if err != nil {
		return codec.StatusResponse2Params{}, fmt.Errorf("probel-sw02p: decode tx 09: %w", err)
	}
	return s, nil
}
