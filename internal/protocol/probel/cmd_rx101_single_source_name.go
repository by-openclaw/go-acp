package probel

import (
	"context"
	"fmt"

	"acp/internal/protocol/probel/codec"
	"acp/internal/protocol"
)

// SingleSourceName fetches one source's name. Per spec §3.3.19 Note 5,
// the matrix replies with a single tx 106 SOURCE NAMES RESPONSE carrying
// exactly one name (NumNames = 1).
//
// Returns the decoded name string, or empty on an unknown source.
//
// Reference: SW-P-08 §3.2.19 / §3.3.19.
func (p *Plugin) SingleSourceName(
	ctx context.Context,
	matrix, level uint8,
	nameLen codec.NameLength,
	src uint16,
) (string, error) {
	cli, err := p.getClient()
	if err != nil {
		return "", err
	}
	req := codec.EncodeSingleSourceNameRequest(codec.SingleSourceNameRequestParams{
		MatrixID: matrix, LevelID: level, NameLength: nameLen, SourceID: src,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxSourceNamesResponse
	})
	if err != nil {
		return "", fmt.Errorf("probel single-source-name: %w", err)
	}
	r, derr := codec.DecodeSourceNamesResponse(reply)
	if derr != nil {
		return "", &protocol.TransportError{Op: "decode", Err: derr}
	}
	if len(r.Names) == 0 {
		return "", nil
	}
	return r.Names[0], nil
}
