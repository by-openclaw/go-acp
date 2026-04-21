package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// AllDestAssocNames asks the matrix for every destination-association
// name on one matrix. Returns the first tx 107 DEST ASSOC NAMES RESPONSE
// frame decoded. See AllSourceNames for multi-frame pagination notes.
//
// Reference: SW-P-08 §3.2.20 / §3.3.20.
func (p *Plugin) AllDestAssocNames(
	ctx context.Context,
	matrix uint8,
	nameLen iprobel.NameLength,
) (iprobel.DestAssocNamesResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.DestAssocNamesResponseParams{}, err
	}
	req := iprobel.EncodeAllDestAssocNamesRequest(iprobel.AllDestAssocNamesRequestParams{
		MatrixID: matrix, NameLength: nameLen,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxDestAssocNamesResponse
	})
	if err != nil {
		return iprobel.DestAssocNamesResponseParams{}, fmt.Errorf("probel all-dest-assoc-names: %w", err)
	}
	r, derr := iprobel.DecodeDestAssocNamesResponse(reply)
	if derr != nil {
		return iprobel.DestAssocNamesResponseParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r, nil
}
