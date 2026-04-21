package probel

import (
	"acp/internal/probel/codec"
)

// handleDualControllerStatus reports the redundancy state of this
// provider. Single-controller deployments (our default) report MASTER +
// Active + IdleControllerFaulty=false. Future dual-controller support
// would read real state off the server; for now the reply is canned so
// controllers that probe this endpoint don't time out.
//
// Reference: SW-P-08 §3.2 (rx 008) → §3.3 (tx 009). TS tx/009/.
func (s *server) handleDualControllerStatus(f codec.Frame) (handlerResult, error) {
	if err := codec.DecodeDualControllerStatusRequest(f); err != nil {
		return handlerResult{}, err
	}
	reply := codec.EncodeDualControllerStatusResponse(codec.DualControllerStatusParams{
		SlaveActive:          false, // MASTER
		Active:               true,  // we are the active controller
		IdleControllerFaulty: false, // no idle peer; don't flag fault
	})
	return handlerResult{reply: &reply}, nil
}
