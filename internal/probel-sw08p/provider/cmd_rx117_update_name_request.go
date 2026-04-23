package probelsw08p

import (
	"log/slog"

	"acp/internal/probel-sw08p/codec"
)

// handleUpdateNameRequest: rx 117 — fire-and-forget label push.
// Applies the names to the server tree by type:
//
//   UpdateNameSource       → update sourceLabels on (matrix, level)
//                            starting at FirstID
//   UpdateNameSourceAssoc  → update sourceLabels on (matrix, 0)
//                            (our tree doesn't model assocs separately)
//   UpdateNameDestAssoc    → update targetLabels on (matrix, 0)
//   UpdateNameUMDLabel     → logged + ignored (no UMD table in the
//                            demo tree; future scope)
//
// SW-P-08 §3.2.26 specifies NO RESPONSE, so handlerResult is empty —
// the session has already ACKed at the framer layer.
//
// Reference: SW-P-08 §3.2.26.
func (s *server) handleUpdateNameRequest(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeUpdateNameRequest(f)
	if err != nil {
		return handlerResult{}, err
	}
	switch p.NameType {
	case codec.UpdateNameSource:
		s.tree.updateSourceLabels(p.MatrixID, p.LevelID, p.FirstID, p.Names)
	case codec.UpdateNameSourceAssoc:
		s.tree.updateSourceLabels(p.MatrixID, 0, p.FirstID, p.Names)
	case codec.UpdateNameDestAssoc:
		s.tree.updateTargetLabels(p.MatrixID, 0, p.FirstID, p.Names)
	case codec.UpdateNameUMDLabel:
		s.logger.Info("probel: rx 117 UMD label update ignored (not modelled)",
			slog.Int("matrix", int(p.MatrixID)),
			slog.Int("count", len(p.Names)),
		)
	}
	return handlerResult{}, nil
}
