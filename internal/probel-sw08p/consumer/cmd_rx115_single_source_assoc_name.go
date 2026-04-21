package probelsw08p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw08p/codec"
	"acp/internal/protocol"
)

// SingleSourceAssocName fetches one source-association name. Matrix
// replies with a single tx 116 frame (NumNames=1).
//
// Reference: SW-P-08 §3.2.25 / §3.3.22.
func (p *Plugin) SingleSourceAssocName(
	ctx context.Context,
	matrix uint8,
	nameLen codec.NameLength,
	srcAssoc uint16,
) (string, error) {
	cli, err := p.getClient()
	if err != nil {
		return "", err
	}
	req := codec.EncodeSingleSourceAssocNameRequest(codec.SingleSourceAssocNameRequestParams{
		MatrixID: matrix, NameLength: nameLen, SourceAssociationID: srcAssoc,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxSourceAssocNamesResponse
	})
	if err != nil {
		return "", fmt.Errorf("probel single-source-assoc-name: %w", err)
	}
	r, derr := codec.DecodeSourceAssocNamesResponse(reply)
	if derr != nil {
		return "", &protocol.TransportError{Op: "decode", Err: derr}
	}
	if len(r.Names) == 0 {
		return "", nil
	}
	return r.Names[0], nil
}
