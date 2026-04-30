package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendExtendedConnect emits rx 66 Extended CONNECT (§3.2.48) and waits
// for the matching tx 68 Extended CONNECTED broadcast (§3.2.50).
// Returns the decoded ExtendedConnectedParams so the caller can verify
// the matrix echoed dst / src.
func (p *Plugin) SendExtendedConnect(ctx context.Context, dst, src uint16) (codec.ExtendedConnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ExtendedConnectedParams{}, err
	}
	req := codec.EncodeExtendedConnect(codec.ExtendedConnectParams{
		Destination: dst,
		Source:      src,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		if f.ID != codec.TxExtendedConnected {
			return false
		}
		cp, derr := codec.DecodeExtendedConnected(f)
		if derr != nil {
			return false
		}
		return cp.Destination == dst && cp.Source == src
	})
	if err != nil {
		return codec.ExtendedConnectedParams{}, fmt.Errorf("probel-sw02p: SendExtendedConnect: %w", err)
	}
	cp, err := codec.DecodeExtendedConnected(reply)
	if err != nil {
		return codec.ExtendedConnectedParams{}, fmt.Errorf("probel-sw02p: decode tx 68: %w", err)
	}
	return cp, nil
}
