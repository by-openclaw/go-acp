package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// TieLineInterrogate asks the matrix for a tie-line tally: the list
// of (matrix, level, source) currently routed to the given destination
// association across all levels. Reply is tx 113.
//
// Reference: SW-P-08 §3.2.28 / §3.3.23.
func (p *Plugin) TieLineInterrogate(
	ctx context.Context,
	matrix uint8,
	destAssoc uint16,
) (iprobel.TieLineTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.TieLineTallyParams{}, err
	}
	req := iprobel.EncodeTieLineInterrogate(iprobel.TieLineInterrogateParams{
		MatrixID: matrix, DestAssociationID: destAssoc,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxCrosspointTieLineTally
	})
	if err != nil {
		return iprobel.TieLineTallyParams{}, fmt.Errorf("probel tie-line-interrogate: %w", err)
	}
	t, derr := iprobel.DecodeTieLineTally(reply)
	if derr != nil {
		return iprobel.TieLineTallyParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return t, nil
}
