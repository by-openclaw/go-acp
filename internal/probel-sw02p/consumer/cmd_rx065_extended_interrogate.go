package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendExtendedInterrogate emits rx 65 Extended INTERROGATE (§3.2.47)
// and waits for the matching tx 67 Extended TALLY (§3.2.49). Returns
// the decoded ExtendedTallyParams — unrouted destinations echo
// Source = codec.DestOutOfRangeSource (1023) per this plugin's
// convention (§3.2.49 itself does not spec a sentinel).
func (p *Plugin) SendExtendedInterrogate(ctx context.Context, dst uint16) (codec.ExtendedTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.ExtendedTallyParams{}, err
	}
	req := codec.EncodeExtendedInterrogate(codec.ExtendedInterrogateParams{Destination: dst})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		if f.ID != codec.TxExtendedTally {
			return false
		}
		t, derr := codec.DecodeExtendedTally(f)
		if derr != nil {
			return false
		}
		return t.Destination == dst
	})
	if err != nil {
		return codec.ExtendedTallyParams{}, fmt.Errorf("probel-sw02p: SendExtendedInterrogate: %w", err)
	}
	t, err := codec.DecodeExtendedTally(reply)
	if err != nil {
		return codec.ExtendedTallyParams{}, fmt.Errorf("probel-sw02p: decode tx 67: %w", err)
	}
	return t, nil
}
