package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleStatusRequest processes rx 07 STATUS REQUEST (§3.2.9).
// Controller queries the matrix's operational status. The provider
// ships tx 09 STATUS RESPONSE - 2 (§3.2.11) — the response shape a
// 5007/5107 TDM controller card would emit — with every fault flag
// clear (active system, no bus fault, no overheat). This plugin runs
// as software on a host; it has no router hardware to report. Future
// integrations with real thermometry / bus-health inputs can extend
// `*server` to carry fault flags and overwrite this default.
//
// The LH / RH controller byte on the request is preserved by the
// transport metrics but not acted on — §3.2.9 says "single controller
// systems … value = 0" and this plugin is single-controller by
// construction.
func (s *server) handleStatusRequest(f codec.Frame) (handlerResult, error) {
	if _, err := codec.DecodeStatusRequest(f); err != nil {
		return handlerResult{}, err
	}
	reply := codec.EncodeStatusResponse2(codec.StatusResponse2Params{})
	return handlerResult{reply: &reply}, nil
}
