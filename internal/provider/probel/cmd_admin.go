package probel

import (
	"log/slog"

	iprobel "acp/internal/probel"
)

// --- rx 007 : Maintenance Message (fire-and-forget) ------------------------

// handleMaintenance decodes the function byte and logs it; ClearProtects
// additionally wipes the protect state on (matrix, level) — per-all
// wildcards 0xFF are honoured.
//
// SW-P-88 does not define a reply for rx 007, so the handlerResult has
// neither a reply nor tallies — the session already ACKed at the framer
// layer and that is all a well-behaved controller expects. Hard reset
// is logged but NOT executed in-process: the provider stays live so
// tests + loopback keep working.
func (s *server) handleMaintenance(f iprobel.Frame) (handlerResult, error) {
	p, err := iprobel.DecodeMaintenance(f)
	if err != nil {
		return handlerResult{}, err
	}
	switch p.Function {
	case iprobel.MaintHardReset:
		s.logger.Warn("probel: maintenance hard-reset (logged only, provider stays up)")
	case iprobel.MaintSoftReset:
		s.logger.Info("probel: maintenance soft-reset (logged only, state preserved)")
	case iprobel.MaintClearProtects:
		s.tree.clearProtects(p.MatrixID, p.LevelID)
		s.logger.Info("probel: maintenance clear-protects",
			slog.Int("matrix", int(p.MatrixID)),
			slog.Int("level", int(p.LevelID)),
		)
	case iprobel.MaintDatabaseTransfer:
		s.logger.Info("probel: maintenance database-transfer (single-controller: no-op)")
	default:
		s.logger.Warn("probel: maintenance unknown function",
			slog.Int("function", int(p.Function)),
		)
	}
	return handlerResult{}, nil
}

// --- rx 008 : Dual Controller Status Request -> tx 009 Response -------------

// handleDualControllerStatus reports the redundancy state of this
// provider. Single-controller deployments (our default) report MASTER +
// Active + IdleControllerFaulty=false. Future dual-controller support
// would read real state off the server; for now the reply is canned so
// controllers that probe this endpoint don't time out.
//
// Reference: SW-P-88 §5.9. TS tx/009/.
func (s *server) handleDualControllerStatus(f iprobel.Frame) (handlerResult, error) {
	if err := iprobel.DecodeDualControllerStatusRequest(f); err != nil {
		return handlerResult{}, err
	}
	reply := iprobel.EncodeDualControllerStatusResponse(iprobel.DualControllerStatusParams{
		SlaveActive:          false, // MASTER
		Active:               true,  // we are the active controller
		IdleControllerFaulty: false, // no idle peer; don't flag fault
	})
	return handlerResult{reply: &reply}, nil
}
