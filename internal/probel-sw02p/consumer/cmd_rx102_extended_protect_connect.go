package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendExtendedProtectConnect emits rx 102 Extended PROTECT CONNECT
// (§3.2.66) and waits for the matching tx 097 Extended PROTECT
// CONNECTED broadcast (§3.2.61). Returns the decoded tx 097 so the
// caller can inspect the actual state of the destination — §3.2.61
// fires on both success and failure (e.g. owner mismatch or
// ProbelOverride), so the caller compares the returned Protect
// state against what they requested to detect reject paths.
func (p *Plugin) SendExtendedProtectConnect(ctx context.Context, dst, device uint16) (codec.ExtendedProtectConnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ExtendedProtectConnectedParams{}, err
	}
	req := codec.EncodeExtendedProtectConnect(codec.ExtendedProtectConnectParams{
		Destination: dst,
		Device:      device,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		if f.ID != codec.TxExtendedProtectConnected {
			return false
		}
		c, derr := codec.DecodeExtendedProtectConnected(f)
		if derr != nil {
			return false
		}
		return c.Destination == dst
	})
	if err != nil {
		return codec.ExtendedProtectConnectedParams{}, fmt.Errorf("probel-sw02p: SendExtendedProtectConnect: %w", err)
	}
	resp, err := codec.DecodeExtendedProtectConnected(reply)
	if err != nil {
		return codec.ExtendedProtectConnectedParams{}, fmt.Errorf("probel-sw02p: decode tx 097: %w", err)
	}
	return resp, nil
}
