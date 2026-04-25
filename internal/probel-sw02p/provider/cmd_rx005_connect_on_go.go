package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleConnectOnGo processes rx 05 CONNECT ON GO (§3.2.7). Each frame
// stages one crosspoint into the matrix's pending salvo buffer; rx 06
// GO later commits or clears the whole list. The matrix replies with
// tx 12 CONNECT ON GO ACKNOWLEDGE to confirm that the slot was stored.
//
// SW-P-02 has no matrix / level byte — we store every pending slot on
// (matrix=0, level=0). The BadSource / Crosspoint-Update-Disabled flag
// from §3.2.3 is preserved on the ingress wire and decoded into
// ConnectOnGoParams.BadSource but not acted on here; the flag is
// informational and §3.2.14 requires the ack to always clear it.
func (s *server) handleConnectOnGo(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeConnectOnGo(f)
	if err != nil {
		return handlerResult{}, err
	}
	s.tree.appendPending(0, 0, pendingSlot{
		Destination: p.Destination,
		Source:      p.Source,
	})
	ack := codec.EncodeConnectOnGoAck(codec.ConnectOnGoAckParams{
		Destination: p.Destination,
		Source:      p.Source,
	})
	return handlerResult{reply: &ack}, nil
}
