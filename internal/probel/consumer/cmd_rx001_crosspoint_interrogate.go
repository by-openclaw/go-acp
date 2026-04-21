package probel

import (
	"context"
	"fmt"

	"acp/internal/probel/codec"
	"acp/internal/protocol"
)

// CrosspointInterrogate queries the current source routed to one
// destination on one (matrix, level). Encodes rx 001 (general) or
// rx 0x81 (extended) automatically; matches both tx 003 / tx 0x83 on
// reply.
//
// Reference: SW-P-08 §3.2 (interrogate) / §3.3 (tally reply).
// TS reference: internal/probel/assets/smh-probelsw08p/src/rx/001/ + tx/003/.
func (p *Plugin) CrosspointInterrogate(
	ctx context.Context,
	matrix, level uint8,
	dst uint16,
) (codec.CrosspointTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.CrosspointTallyParams{}, err
	}
	req := codec.EncodeCrosspointInterrogate(codec.CrosspointInterrogateParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxCrosspointTally || f.ID == codec.TxCrosspointTallyExt
	})
	if err != nil {
		return codec.CrosspointTallyParams{}, fmt.Errorf("probel interrogate: %w", err)
	}
	t, derr := codec.DecodeCrosspointTally(reply)
	if derr != nil {
		return codec.CrosspointTallyParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return t, nil
}
