package probelsw02p

import (
	"acp/internal/probel-sw02p/codec"
)

// handlerResult is what a per-command handler returns. The dispatcher
// writes `reply` back to the originating session when non-nil. Future
// commands (salvo GO, tally-dump) may add broadcast / streaming
// fields; keep them off this struct until the first commit that needs
// them, mirroring SW-P-08's incremental growth.
type handlerResult struct {
	reply *codec.Frame // non-nil → session writes Pack(reply) back to the peer
}

// dispatch routes a decoded Frame to its per-command handler. Unknown
// ids return (handlerResult{}, nil) and the session fires a
// compliance event for UnsupportedCommand — the SW-P-02 spec explicitly
// allows matrices to ignore commands they do not implement, so unknown
// is never fatal.
//
// Keep this switch in command-byte order so the layout matches the
// §3.2 table. Every new cmd_rxNNN_xxx.go adds one case here.
func (s *server) dispatch(f codec.Frame) (handlerResult, error) {
	switch f.ID {
	case codec.RxConnectOnGo:
		return s.handleConnectOnGo(f)
	}
	return handlerResult{}, nil
}
