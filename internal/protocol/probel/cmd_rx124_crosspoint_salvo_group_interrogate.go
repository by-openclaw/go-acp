package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// SalvoGroupInterrogate fetches one slot (ConnectIndex) of a salvo
// group. Callers iterate from ConnectIndex=0 and increment until the
// reply's Validity is SalvoTallyValidLast or SalvoTallyInvalid.
//
// Reference: SW-P-08 §3.2.31 / §3.3.26.
func (p *Plugin) SalvoGroupInterrogate(
	ctx context.Context,
	params iprobel.SalvoGroupInterrogateParams,
) (iprobel.SalvoGroupTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.SalvoGroupTallyParams{}, err
	}
	req := iprobel.EncodeSalvoGroupInterrogate(params)
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxSalvoGroupTally
	})
	if err != nil {
		return iprobel.SalvoGroupTallyParams{}, fmt.Errorf("probel salvo-group-interrogate: %w", err)
	}
	tally, derr := iprobel.DecodeSalvoGroupTally(reply)
	if derr != nil {
		return iprobel.SalvoGroupTallyParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return tally, nil
}
