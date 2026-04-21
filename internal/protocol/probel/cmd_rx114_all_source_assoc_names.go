package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// AllSourceAssocNames asks the matrix for every source-association
// name on one matrix. Returns the first tx 116 frame decoded.
//
// Reference: SW-P-08 §3.2.24 / §3.3.22.
func (p *Plugin) AllSourceAssocNames(
	ctx context.Context,
	matrix uint8,
	nameLen iprobel.NameLength,
) (iprobel.SourceAssocNamesResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.SourceAssocNamesResponseParams{}, err
	}
	req := iprobel.EncodeAllSourceAssocNamesRequest(iprobel.AllSourceAssocNamesRequestParams{
		MatrixID: matrix, NameLength: nameLen,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxSourceAssocNamesResponse
	})
	if err != nil {
		return iprobel.SourceAssocNamesResponseParams{}, fmt.Errorf("probel all-source-assoc-names: %w", err)
	}
	r, derr := iprobel.DecodeSourceAssocNamesResponse(reply)
	if derr != nil {
		return iprobel.SourceAssocNamesResponseParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r, nil
}
