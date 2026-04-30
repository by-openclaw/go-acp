package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendExtendedProtectInterrogate emits rx 101 Extended PROTECT
// INTERROGATE (§3.2.65) and waits for the matching tx 096 Extended
// PROTECT TALLY (§3.2.60). Returns the decoded tally so the caller
// can inspect the current protect state of the destination — a
// zero-value State + Device means "not protected" by convention.
func (p *Plugin) SendExtendedProtectInterrogate(ctx context.Context, dst uint16) (codec.ExtendedProtectTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ExtendedProtectTallyParams{}, err
	}
	req := codec.EncodeExtendedProtectInterrogate(codec.ExtendedProtectInterrogateParams{Destination: dst})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		if f.ID != codec.TxExtendedProtectTally {
			return false
		}
		t, derr := codec.DecodeExtendedProtectTally(f)
		if derr != nil {
			return false
		}
		return t.Destination == dst
	})
	if err != nil {
		return codec.ExtendedProtectTallyParams{}, fmt.Errorf("probel-sw02p: SendExtendedProtectInterrogate: %w", err)
	}
	t, err := codec.DecodeExtendedProtectTally(reply)
	if err != nil {
		return codec.ExtendedProtectTallyParams{}, fmt.Errorf("probel-sw02p: decode tx 096: %w", err)
	}
	return t, nil
}
