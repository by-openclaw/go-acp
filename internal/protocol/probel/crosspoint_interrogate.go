package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// CrosspointInterrogate queries the current source routed to one
// destination on one (matrix, level). Encodes rx 001 (general) or
// rx 0x81 (extended) automatically; matches both tx 003 / tx 0x83 on
// reply.
//
// Reference: SW-P-08 §3.2 (interrogate) / §3.3 (tally reply).
// TS reference: assets/probel/smh-probelsw08p/src/rx/001/ + tx/003/.
func (p *Plugin) CrosspointInterrogate(
	ctx context.Context,
	matrix, level uint8,
	dst uint16,
) (iprobel.CrosspointTallyParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.CrosspointTallyParams{}, err
	}
	req := iprobel.EncodeCrosspointInterrogate(iprobel.CrosspointInterrogateParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxCrosspointTally || f.ID == iprobel.TxCrosspointTallyExt
	})
	if err != nil {
		return iprobel.CrosspointTallyParams{}, fmt.Errorf("probel interrogate: %w", err)
	}
	t, derr := iprobel.DecodeCrosspointTally(reply)
	if derr != nil {
		return iprobel.CrosspointTallyParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return t, nil
}
