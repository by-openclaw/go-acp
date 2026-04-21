package probel

import (
	"context"
	"fmt"

	iprobel "acp/internal/probel"
	"acp/internal/protocol"
)

// Maintenance fires a rx 007 Maintenance Message. SW-P-88 does not
// define a reply (the matrix may reset itself or clear protects without
// echoing anything back), so this call does not wait for a response —
// it returns as soon as the frame is on the wire.
//
// Functions:
//   - MaintHardReset / MaintSoftReset — matrix (soft/hard) reset; no
//     extra params.
//   - MaintClearProtects — clears protects on (matrix, level). Pass 0xFF
//     for "all matrices" or "all levels".
//   - MaintDatabaseTransfer — dual-controller database copy; no params.
//
// Reference: SW-P-88 §5.8. TS rx/007/.
func (p *Plugin) Maintenance(
	ctx context.Context,
	fn iprobel.MaintenanceFunction,
	matrix, level uint8,
) error {
	cli, err := p.getClient()
	if err != nil {
		return err
	}
	req := iprobel.EncodeMaintenance(iprobel.MaintenanceParams{
		Function: fn, MatrixID: matrix, LevelID: level,
	})
	if _, err := cli.Send(ctx, req, nil); err != nil {
		return fmt.Errorf("probel maintenance: %w", err)
	}
	return nil
}

// DualControllerStatus queries the 1:1 redundancy state (master vs.
// slave, active flag, idle-controller health). Matches rx 008 → tx 009.
//
// Reference: SW-P-88 §5.9. TS rx/008/ + tx/009/.
func (p *Plugin) DualControllerStatus(
	ctx context.Context,
) (iprobel.DualControllerStatusParams, error) {
	cli, err := p.getClient()
	if err != nil {
		return iprobel.DualControllerStatusParams{}, err
	}
	req := iprobel.EncodeDualControllerStatusRequest()
	reply, err := cli.Send(ctx, req, func(f iprobel.Frame) bool {
		return f.ID == iprobel.TxDualControllerStatusResponse
	})
	if err != nil {
		return iprobel.DualControllerStatusParams{}, fmt.Errorf("probel dual-status: %w", err)
	}
	r, derr := iprobel.DecodeDualControllerStatusResponse(reply)
	if derr != nil {
		return iprobel.DualControllerStatusParams{}, &protocol.TransportError{Op: "decode", Err: derr}
	}
	return r, nil
}
