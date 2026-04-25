package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendConnect emits rx 02 CONNECT (§3.2.4) and waits for the matching
// tx 04 CROSSPOINT CONNECTED broadcast (§3.2.6). Returns the decoded
// ConnectedParams so the caller can verify the matrix echoed dst / src.
//
// Bilateral — the consumer supports initiating CONNECT even though
// VSM is normally the control surface.
func (p *Plugin) SendConnect(ctx context.Context, dst, src uint16, badSource bool) (codec.ConnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ConnectedParams{}, err
	}
	req := codec.EncodeConnect(codec.ConnectParams{
		Destination: dst,
		Source:      src,
		BadSource:   badSource,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		if f.ID != codec.TxCrosspointConnected {
			return false
		}
		cp, derr := codec.DecodeConnected(f)
		if derr != nil {
			return false
		}
		return cp.Destination == dst && cp.Source == src
	})
	if err != nil {
		return codec.ConnectedParams{}, fmt.Errorf("probel-sw02p: SendConnect: %w", err)
	}
	cp, err := codec.DecodeConnected(reply)
	if err != nil {
		return codec.ConnectedParams{}, fmt.Errorf("probel-sw02p: decode tx 04: %w", err)
	}
	return cp, nil
}
