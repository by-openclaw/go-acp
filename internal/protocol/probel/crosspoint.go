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
// Reference: SW-P-88 §5.3 (interrogate) / §5.4 (tally reply).
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

// CrosspointConnect asks the matrix to route src → dst on
// (matrix, level). Encodes rx 002 (general) or rx 0x82 (extended)
// automatically; matches both tx 004 / tx 0x84 on reply.
//
// Returns the matrix-confirmed CrosspointConnectedParams. A subsequent
// async tx 003 Tally typically fans out on other sessions — consumers
// that need live tallies should Subscribe via the Client listener API.
//
// Reference: SW-P-88 §5.4 (connect) / §5.5 (connected).
// TS reference: assets/probel/smh-probelsw08p/src/rx/002/ + tx/004/.
func (p *Plugin) CrosspointConnect(
	ctx context.Context,
	matrix, level uint8,
	dst, src uint16,
) (iprobel.CrosspointConnectedParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.CrosspointConnectedParams{}, err
	}
	req := iprobel.EncodeCrosspointConnect(iprobel.CrosspointConnectParams{
		MatrixID: matrix, LevelID: level, DestinationID: dst, SourceID: src,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxCrosspointConnected || f.ID == iprobel.TxCrosspointConnectedExt
	})
	if err != nil {
		return iprobel.CrosspointConnectedParams{}, fmt.Errorf("probel connect: %w", err)
	}
	c, derr := iprobel.DecodeCrosspointConnected(reply)
	if derr != nil {
		return iprobel.CrosspointConnectedParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return c, nil
}
