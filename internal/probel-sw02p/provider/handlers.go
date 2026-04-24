package probelsw02p

import (
	"log/slog"

	"acp/internal/probel-sw02p/codec"
)

// handlerResult is what a per-command handler returns. The dispatcher
// writes `reply` back to the originating session when non-nil, then
// fan-outs every frame in `broadcast` to every connected session
// (including the originator — §3.2.6 CONNECTED is "issued on ALL
// ports", which includes the one that sent the triggering command).
type handlerResult struct {
	reply *codec.Frame // non-nil → session writes Pack(reply) to the peer

	// broadcast frames are emitted to every connected session via
	// server.fanOut. Used for tx 04 CONNECTED emissions on salvo
	// commit, tx 13 GO DONE, and future tally paths.
	broadcast []codec.Frame
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
	case codec.RxInterrogate:
		return s.handleInterrogate(f)
	case codec.RxConnect:
		return s.handleConnect(f)
	case codec.RxStatusRequest:
		return s.handleStatusRequest(f)
	case codec.RxExtendedInterrogate:
		return s.handleExtendedInterrogate(f)
	case codec.RxExtendedConnect:
		return s.handleExtendedConnect(f)
	case codec.RxDualControllerStatusRequest:
		return s.handleDualControllerStatusRequest(f)
	case codec.RxConnectOnGo:
		return s.handleConnectOnGo(f)
	case codec.RxGo:
		return s.handleGo(f)
	case codec.RxConnectOnGoGroupSalvo:
		return s.handleConnectOnGoGroupSalvo(f)
	case codec.RxGoGroupSalvo:
		return s.handleGoGroupSalvo(f)
	case codec.RxExtendedConnectOnGoGroupSalvo:
		return s.handleExtendedConnectOnGoGroupSalvo(f)
	}
	return handlerResult{}, nil
}

// fanOut writes raw wire bytes to every live session. Used by the
// dispatcher after a handler's broadcast field is populated. Write
// failures are logged but never abort the fan-out — one slow peer
// must not block broadcast delivery to healthy peers.
func (s *server) fanOut(b []byte, id codec.CommandID) {
	s.mu.Lock()
	targets := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
		targets = append(targets, sess)
	}
	s.mu.Unlock()
	s.logger.Debug("probel-sw02p fanOut",
		slog.Int("cmd", int(id)),
		slog.Int("targets", len(targets)),
		slog.Int("bytes", len(b)))
	for _, sess := range targets {
		if werr := sess.write(b); werr != nil {
			s.logger.Debug("probel-sw02p fanOut write",
				slog.String("remote", sess.remoteAddr()),
				slog.Int("cmd", int(id)),
				slog.String("err", werr.Error()))
			s.profile.Note(OutboundWriteFailed)
			continue
		}
		s.metrics.ObserveCmdTx(uint8(id), len(b), 0)
	}
}
