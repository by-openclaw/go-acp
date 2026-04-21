package probel

import (
	"context"
	"fmt"

	"acp/internal/probel-sw08p/codec"
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
) (codec.TieLineTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.TieLineTallyParams{}, err
	}
	req := codec.EncodeTieLineInterrogate(codec.TieLineInterrogateParams{
		MatrixID: matrix, DestAssociationID: destAssoc,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxCrosspointTieLineTally
	})
	if err != nil {
		return codec.TieLineTallyParams{}, fmt.Errorf("probel tie-line-interrogate: %w", err)
	}
	t, derr := codec.DecodeTieLineTally(reply)
	if derr != nil {
		return codec.TieLineTallyParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return t, nil
}
