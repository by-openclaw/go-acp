package probel

import (
	"context"
	"fmt"

	"acp/internal/protocol/probel/codec"
	"acp/internal/protocol"
)

// AllSourceNames asks the matrix for every source name on one
// (matrix, level). Returns the first tx 106 SOURCE NAMES RESPONSE
// frame's payload decoded.
//
// Multi-frame collection: the spec (§3.3.19) caps one frame at 32×4-char
// or 16×8-char or 10×12-char names; larger tables arrive as multiple
// tx 106 frames with ascending FirstSourceID. This consumer implementation
// returns the first frame only — callers needing the full table should
// paginate by issuing follow-up SingleSourceName calls, or use the
// Subscribe-listener on the codec.Client directly. Multi-frame
// auto-collection is tracked in the Probel master issue.
//
// Reference: SW-P-08 §3.2.18 (request) / §3.3.19 (response).
func (p *Plugin) AllSourceNames(
	ctx context.Context,
	matrix, level uint8,
	nameLen codec.NameLength,
) (codec.SourceNamesResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return codec.SourceNamesResponseParams{}, err
	}
	req := codec.EncodeAllSourceNamesRequest(codec.AllSourceNamesRequestParams{
		MatrixID: matrix, LevelID: level, NameLength: nameLen,
	})
	reply, err := cli.Send(ctx, req, func(f codec.Frame) bool {
		return f.ID == codec.TxSourceNamesResponse
	})
	if err != nil {
		return codec.SourceNamesResponseParams{}, fmt.Errorf("probel all-source-names: %w", err)
	}
	r, derr := codec.DecodeSourceNamesResponse(reply)
	if derr != nil {
		return codec.SourceNamesResponseParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r, nil
}
