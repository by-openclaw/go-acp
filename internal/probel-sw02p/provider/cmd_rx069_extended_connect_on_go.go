package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handleExtendedConnectOnGo processes rx 069 Extended CONNECT ON GO
// (§3.2.51). Extended-addressing counterpart to handleConnectOnGo:
// one crosspoint staged into the shared (matrix=0, level=0) unnamed
// pending buffer — rx 006 GO later commits / clears both narrow and
// extended stages together, matching the rx 005 / rx 069 pairing
// behaviour of real routers.
//
// Matrix replies with tx 070 Extended CONNECT ON GO ACKNOWLEDGE to
// confirm the slot was stored.
func (s *server) handleExtendedConnectOnGo(f codec.Frame) (handlerResult, error) {
	p, err := codec.DecodeExtendedConnectOnGo(f)
	if err != nil {
		return handlerResult{}, err
	}
	s.tree.appendPending(0, 0, pendingSlot{
		Destination: p.Destination,
		Source:      p.Source,
		Extended:    true,
	})
	// Request and ack share an identical layout (§3.2.51 == §3.2.52):
	// same 4 bytes, same field widths, different cmd ID. Direct struct
	// conversion avoids hand-copying each field — staticcheck's
	// preferred idiom, mirroring the rx 035 / rx 071 handlers.
	ack := codec.EncodeExtendedConnectOnGoAck(codec.ExtendedConnectOnGoAckParams(p))
	return handlerResult{reply: &ack}, nil
}
