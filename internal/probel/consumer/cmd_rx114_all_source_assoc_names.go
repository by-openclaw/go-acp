package probel

import (
	"context"
	"fmt"

	"acp/internal/probel/codec"
	"acp/internal/protocol"
)

// AllSourceAssocNames asks the matrix for every source-association
// name on one matrix. Returns the first tx 116 frame decoded.
//
// Reference: SW-P-08 §3.2.24 / §3.3.22.
func (p *Plugin) AllSourceAssocNames(
	ctx context.Context,
	matrix uint8,
	nameLen codec.NameLength,
) (codec.SourceAssocNamesResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.SourceAssocNamesResponseParams{}, err
	}
	req := codec.EncodeAllSourceAssocNamesRequest(codec.AllSourceAssocNamesRequestParams{
		MatrixID: matrix, NameLength: nameLen,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxSourceAssocNamesResponse
	})
	if err != nil {
		return codec.SourceAssocNamesResponseParams{}, fmt.Errorf("probel all-source-assoc-names: %w", err)
	}
	r, derr := codec.DecodeSourceAssocNamesResponse(reply)
	if derr != nil {
		return codec.SourceAssocNamesResponseParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r, nil
}
