package probel

import (
	"context"
	"fmt"

	"acp/internal/probel/codec"
)

// Maintenance fires a rx 007 Maintenance Message. SW-P-08 does not
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
// Reference: SW-P-08 §3.2 (rx 007). TS rx/007/.
func (p *Plugin) Maintenance(
	ctx context.Context,
	fn codec.MaintenanceFunction,
	matrix, level uint8,
) error {
	cli, err := p.getClient()
	if err != nil {
		return err
	}
	req := codec.EncodeMaintenance(codec.MaintenanceParams{
		Function: fn, MatrixID: matrix, LevelID: level,
	})
	if _, err := cli.Send(ctx, req, nil); err != nil {
		return fmt.Errorf("probel maintenance: %w", err)
	}
	return nil
}
