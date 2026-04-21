package probel

import (
	"context"
	"fmt"

	"acp/internal/probel-sw08p/codec"
	"acp/internal/protocol"
)

// SingleDestAssocName fetches one destination-association name.
// Matrix replies with a single tx 107 frame (NumNames = 1).
//
// Reference: SW-P-08 §3.2.21 / §3.3.20.
func (p *Plugin) SingleDestAssocName(
	ctx context.Context,
	matrix uint8,
	nameLen codec.NameLength,
	destAssoc uint16,
) (string, error) {
	cli, err := p.getClient()
	if err != nil {
		return "", err
	}
	req := codec.EncodeSingleDestAssocNameRequest(codec.SingleDestAssocNameRequestParams{
		MatrixID: matrix, NameLength: nameLen, DestAssociationID: destAssoc,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxDestAssocNamesResponse
	})
	if err != nil {
		return "", fmt.Errorf("probel single-dest-assoc-name: %w", err)
	}
	r, derr := codec.DecodeDestAssocNamesResponse(reply)
	if derr != nil {
		return "", &protocol.TransportError{Op: "decode", Err: derr}
	}
	if len(r.Names) == 0 {
		return "", nil
	}
	return r.Names[0], nil
}
