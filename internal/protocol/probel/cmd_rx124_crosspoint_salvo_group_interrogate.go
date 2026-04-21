package probel

import (
	"context"
	"fmt"

	"acp/internal/protocol/probel/codec"
	"acp/internal/protocol"
)

// SalvoGroupInterrogate fetches one slot (ConnectIndex) of a salvo
// group. Callers iterate from ConnectIndex=0 and increment until the
// reply's Validity is SalvoTallyValidLast or SalvoTallyInvalid.
//
// Reference: SW-P-08 §3.2.31 / §3.3.26.
func (p *Plugin) SalvoGroupInterrogate(
	ctx context.Context,
	params codec.SalvoGroupInterrogateParams,
) (codec.SalvoGroupTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.SalvoGroupTallyParams{}, err
	}
	req := codec.EncodeSalvoGroupInterrogate(params)
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxSalvoGroupTally
	})
	if err != nil {
		return codec.SalvoGroupTallyParams{}, fmt.Errorf("probel salvo-group-interrogate: %w", err)
	}
	tally, derr := codec.DecodeSalvoGroupTally(reply)
	if derr != nil {
		return codec.SalvoGroupTallyParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return tally, nil
}
