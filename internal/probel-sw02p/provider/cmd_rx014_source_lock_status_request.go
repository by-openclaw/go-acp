package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleSourceLockStatusRequest processes rx 014 SOURCE LOCK STATUS
// REQUEST (§3.2.16). Matrix reports which HD-router input sources
// have a clean carrier signal.
//
// This plugin runs as software with no physical input cards, so the
// reply reports every declared source as "locked = true" by default.
// Per §3.2.17 note 2, a zero bit is ambiguous (card absent OR signal
// lost) — the all-ones answer matches "all cards present, all inputs
// healthy". Future HW-monitor wiring can swap the source via a
// ServerOption. Controller selector byte (LH/RH) is decoded but not
// acted on — single-controller plugin by construction.
func (s *server) handleSourceLockStatusRequest(f codec.Frame) (handlerResult, error) {
	if _, err := codec.DecodeSourceLockStatusRequest(f); err != nil {
		return handlerResult{}, err
	}
	reply := codec.EncodeSourceLockStatusResponse(codec.SourceLockStatusResponseParams{
		Locked: s.tree.sourceLockSnapshot(),
	})
	return handlerResult{reply: &reply}, nil
}
