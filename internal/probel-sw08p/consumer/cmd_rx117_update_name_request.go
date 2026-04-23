package probelsw08p

import (
	"context"
	"fmt"

	"acp/internal/probel-sw08p/codec"
)

// UpdateNameRequest issues rx 117 UPDATE NAME REQUEST — a fire-and-
// forget label push. Per SW-P-08 §3.2.26 the matrix does not reply;
// this call returns as soon as the frame is ACKed on the wire.
//
// A NOTE from the spec worth passing on: "Using this command on an
// Aurora or similar controller will result in a database mismatch
// when the system configuration editor is next connected online."
// Caller's responsibility to decide whether pushing labels remotely
// is acceptable for the target deployment.
//
// Reference: SW-P-08 §3.2.26.
func (p *Plugin) UpdateNameRequest(
	ctx context.Context,
	params codec.UpdateNameRequestParams,
) error {
	cli, err := p.getClient()
	if err != nil {
		return err
	}
	req := codec.EncodeUpdateNameRequest(params)
	if _, err := cli.Send(ctx, req, nil); err != nil {
		return fmt.Errorf("probel update-name: %w", err)
	}
	return nil
}
