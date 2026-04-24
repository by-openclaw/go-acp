package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleDualControllerStatusRequest processes rx 050 DUAL CONTROLLER
// STATUS REQUEST (§3.2.45). Controller asks who's active + idle state.
//
// This plugin is single-controller by construction — it has no
// master/slave topology, no idle card to monitor. Per §3.2.46 the
// response is "issued by the active controller", and for a single-
// controller system that's always the master with a healthy idle
// line (idle = OK because there is no idle card to report faulty).
// Future dual-hot integration can wire a ServerOption to vary these
// flags.
func (s *server) handleDualControllerStatusRequest(f codec.Frame) (handlerResult, error) {
	if _, err := codec.DecodeDualControllerStatusRequest(f); err != nil {
		return handlerResult{}, err
	}
	reply := codec.EncodeDualControllerStatusResponse(codec.DualControllerStatusResponseParams{
		Active:     codec.ActiveControllerMaster,
		IdleStatus: codec.IdleControllerOK,
	})
	return handlerResult{reply: &reply}, nil
}
