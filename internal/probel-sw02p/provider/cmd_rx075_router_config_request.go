package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleRouterConfigRequest processes rx 075 ROUTER CONFIGURATION
// REQUEST (§3.2.57). Controller asks the matrix to describe its
// levels. This plugin replies with tx 076 ROUTER CONFIGURATION
// RESPONSE - 1 (§3.2.58) — the simpler variant. The canonical tree
// stores one matrix with N levels on (matrix=0, level=0..N-1); every
// level shares the same target/source counts (our canonical.Matrix
// model does not break counts out per level), so the emitted bitmap
// covers bit 0..N-1 and each entry reports the same counts.
//
// Future per-level count variance or tx 077 RESPONSE-2 (non-contiguous
// Start Dst / Start Src) can be opted into via a ServerOption.
func (s *server) handleRouterConfigRequest(f codec.Frame) (handlerResult, error) {
	if _, err := codec.DecodeRouterConfigRequest(f); err != nil {
		return handlerResult{}, err
	}
	params := s.tree.buildRouterConfigResponse1()
	reply := codec.EncodeRouterConfigResponse1(params)
	return handlerResult{reply: &reply}, nil
}
