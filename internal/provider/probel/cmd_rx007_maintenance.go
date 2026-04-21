package probel

import (
	"log/slog"

	iprobel "acp/internal/probel"
)

// handleMaintenance decodes the function byte and logs it; ClearProtects
// additionally wipes the protect state on (matrix, level) — per-all
// wildcards 0xFF are honoured.
//
// SW-P-08 does not define a reply for rx 007, so the handlerResult has
// neither a reply nor tallies — the session already ACKed at the framer
// layer and that is all a well-behaved controller expects. Hard reset
// is logged but NOT executed in-process: the provider stays live so
// tests + loopback keep working.
//
// Reference: SW-P-08 §3.2 (rx 007).
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
