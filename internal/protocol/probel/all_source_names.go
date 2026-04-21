package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
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
// Subscribe-listener on the iprobel.Client directly. Multi-frame
// auto-collection is tracked in the Probel master issue.
//
// Reference: SW-P-08 §3.2.18 (request) / §3.3.19 (response).
func (p *Plugin) AllSourceNames(
	ctx context.Context,
	matrix, level uint8,
	nameLen iprobel.NameLength,
) (iprobel.SourceNamesResponseParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.SourceNamesResponseParams{}, err
	}
	req := iprobel.EncodeAllSourceNamesRequest(iprobel.AllSourceNamesRequestParams{
		MatrixID: matrix, LevelID: level, NameLength: nameLen,
	})
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxSourceNamesResponse
	})
	if err != nil {
		return iprobel.SourceNamesResponseParams{}, fmt.Errorf("probel all-source-names: %w", err)
	}
	r, derr := iprobel.DecodeSourceNamesResponse(reply)
	if derr != nil {
		return iprobel.SourceNamesResponseParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r, nil
}
