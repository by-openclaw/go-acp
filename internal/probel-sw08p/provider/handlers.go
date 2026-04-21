package probel

import (
	"acp/internal/probel-sw08p/codec"
)

// handlerResult is what a per-command handler returns: an optional
// reply sent back to the originating session, plus zero or more
// tallies broadcast to every OTHER session (not the originator).
//
// Handlers that cannot parse their frame payload return an error; the
// session logs it and sends DLE NAK at the framing layer (the ACK has
// already gone out by the time handle() runs).
type handlerResult struct {
	reply   *codec.Frame
	tallies []codec.Frame
}

// handle is the central command dispatcher. It is a pure function of
// the server's tree state — no I/O — so per-command handlers can be
// unit-tested without a network.
//
// Cases are added by per-command PRs under #82. Unknown CMDs return a
// zero handlerResult (no reply, no error) — the session has already
// ACKed the frame at the framer layer, which is enough for most
// well-behaved controllers to consider the message delivered.
func (s *server) handle(f codec.Frame) (handlerResult, error) {
	switch f.ID {
	case codec.RxCrosspointInterrogate, codec.RxCrosspointInterrogateExt:
		return s.handleCrosspointInterrogate(f)
	case codec.RxCrosspointConnect, codec.RxCrosspointConnectExt:
		return s.handleCrosspointConnect(f)
	case codec.RxMaintenance:
		return s.handleMaintenance(f)
	case codec.RxDualControllerStatusRequest:
		return s.handleDualControllerStatus(f)
	case codec.RxCrosspointTallyDumpRequest, codec.RxCrosspointTallyDumpRequestExt:
		return s.handleCrosspointTallyDumpRequest(f)
	case codec.RxProtectInterrogate, codec.RxProtectInterrogateExt:
		return s.handleProtectInterrogate(f)
	case codec.RxProtectConnect, codec.RxProtectConnectExt:
		return s.handleProtectConnect(f)
	case codec.RxProtectDisconnect, codec.RxProtectDisconnectExt:
		return s.handleProtectDisconnect(f)
	case codec.RxProtectDeviceNameRequest:
		return s.handleProtectDeviceNameRequest(f)
	case codec.RxProtectTallyDumpRequest, codec.RxProtectTallyDumpRequestExt:
		return s.handleProtectTallyDumpRequest(f)
	case codec.RxMasterProtectConnect:
		return s.handleMasterProtectConnect(f)
	case codec.RxAllSourceNamesRequest:
		return s.handleAllSourceNames(f)
	case codec.RxSingleSourceNameRequest:
		return s.handleSingleSourceName(f)
	case codec.RxAllDestNamesRequest:
		return s.handleAllDestAssocNames(f)
	case codec.RxSingleDestNameRequest:
		return s.handleSingleDestAssocName(f)
	case codec.RxCrosspointTieLineInterrogate:
		return s.handleTieLineInterrogate(f)
	case codec.RxAllSourceAssocNamesRequest:
		return s.handleAllSourceAssocNames(f)
	case codec.RxSingleSourceAssocNameRequest:
		return s.handleSingleSourceAssocName(f)
	case codec.RxUpdateNameRequest:
		return s.handleUpdateNameRequest(f)
	case codec.RxCrosspointConnectOnGoSalvo:
		return s.handleSalvoConnectOnGo(f)
	case codec.RxCrosspointGoSalvo:
		return s.handleSalvoGo(f)
	case codec.RxCrosspointSalvoGroupInterrogate:
		return s.handleSalvoGroupInterrogate(f)
	}
	s.profile.Note(UnsupportedCommand)
	return handlerResult{}, nil
}
