package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendSourceLockStatusRequest emits rx 014 SOURCE LOCK STATUS REQUEST
// (§3.2.16) and waits for the matching tx 015 SOURCE LOCK STATUS
// RESPONSE (§3.2.17). Only applicable against HD Digital Video
// routers — other matrix types typically NAK or silently ignore this
// command (the Plugin layer absorbs that via compliance events).
func (p *Plugin) SendSourceLockStatusRequest(ctx context.Context, controller codec.Controller) (codec.SourceLockStatusResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.SourceLockStatusResponseParams{}, err
	}
	req := codec.EncodeSourceLockStatusRequest(codec.SourceLockStatusRequestParams{Controller: controller})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxSourceLockStatusResponse
	})
	if err != nil {
		return codec.SourceLockStatusResponseParams{}, fmt.Errorf("probel-sw02p: SendSourceLockStatusRequest: %w", err)
	}
	resp, err := codec.DecodeSourceLockStatusResponse(reply)
	if err != nil {
		return codec.SourceLockStatusResponseParams{}, fmt.Errorf("probel-sw02p: decode tx 015: %w", err)
	}
	return resp, nil
}
