package probelsw02p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw02p/codec"
)

// SendInterrogate emits rx 01 INTERROGATE (§3.2.3) and waits for the
// matching tx 03 TALLY (§3.2.5). Returns the decoded TallyParams so the
// caller can distinguish a real route from the §3.2.5 sentinel (Source
// == codec.DestOutOfRangeSource = 1023 = "destination out of range").
//
// Bilateral by construction — the consumer supports initiating the
// query even though VSM normally owns this direction.
func (p *Plugin) SendInterrogate(ctx context.Context, dst uint16) (codec.TallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.TallyParams{}, err
	}
	req := codec.EncodeInterrogate(codec.InterrogateParams{
		Destination: dst,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		if f.ID != codec.TxTally {
			return false
		}
		tally, derr := codec.DecodeTally(f)
		if derr != nil {
			return false
		}
		return tally.Destination == dst
	})
	if err != nil {
		return codec.TallyParams{}, fmt.Errorf("probel-sw02p: SendInterrogate: %w", err)
	}
	tally, err := codec.DecodeTally(reply)
	if err != nil {
		return codec.TallyParams{}, fmt.Errorf("probel-sw02p: decode tx 03: %w", err)
	}
	return tally, nil
}
