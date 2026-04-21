package probel

import (
	"context"
	"fmt"

	"acp/internal/probel/codec"
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
	nameLen codec.NameLength,
) (codec.DestAssocNamesResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.DestAssocNamesResponseParams{}, err
	}
	req := codec.EncodeAllDestAssocNamesRequest(codec.AllDestAssocNamesRequestParams{
		MatrixID: matrix, NameLength: nameLen,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxDestAssocNamesResponse
	})
	if err != nil {
		return codec.DestAssocNamesResponseParams{}, fmt.Errorf("probel all-dest-assoc-names: %w", err)
	}
	r, derr := codec.DecodeDestAssocNamesResponse(reply)
	if derr != nil {
		return codec.DestAssocNamesResponseParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r, nil
}
