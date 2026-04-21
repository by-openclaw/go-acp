package probel

import (
	iprobel "acp/internal/probel"
)

// handlerResult is what a per-command handler returns: an optional
// reply sent back to the originating session, plus zero or more
// tallies broadcast to every OTHER session (not the originator).
//
// Handlers that cannot parse their frame payload return an error; the
// session logs it and sends DLE NAK at the framing layer (the ACK has
// already gone out by the time handle() runs).
type handlerResult struct {
	reply   *iprobel.Frame
	tallies []iprobel.Frame
}

// handle is the central command dispatcher. It is a pure function of
// the server's tree state — no I/O — so per-command handlers can be
// unit-tested without a network.
//
// Cases are added by per-command PRs under #82. Unknown CMDs return a
// zero handlerResult (no reply, no error) — the session has already
// ACKed the frame at the framer layer, which is enough for most
// well-behaved controllers to consider the message delivered.
func (s *server) handle(f iprobel.Frame) (handlerResult, error) {
	switch f.ID {
	case iprobel.RxCrosspointInterrogate, iprobel.RxCrosspointInterrogateExt:
		return s.handleCrosspointInterrogate(f)
	case iprobel.RxCrosspointConnect, iprobel.RxCrosspointConnectExt:
		return s.handleCrosspointConnect(f)
	case iprobel.RxMaintenance:
		return s.handleMaintenance(f)
	case iprobel.RxDualControllerStatusRequest:
		return s.handleDualControllerStatus(f)
	case iprobel.RxCrosspointTallyDumpRequest, iprobel.RxCrosspointTallyDumpRequestExt:
		return s.handleCrosspointTallyDumpRequest(f)
	case iprobel.RxProtectInterrogate, iprobel.RxProtectInterrogateExt:
		return s.handleProtectInterrogate(f)
	case iprobel.RxProtectConnect, iprobel.RxProtectConnectExt:
		return s.handleProtectConnect(f)
	case iprobel.RxProtectDisconnect, iprobel.RxProtectDisconnectExt:
		return s.handleProtectDisconnect(f)
	case iprobel.RxProtectDeviceNameRequest:
		return s.handleProtectDeviceNameRequest(f)
	case iprobel.RxProtectTallyDumpRequest, iprobel.RxProtectTallyDumpRequestExt:
		return s.handleProtectTallyDumpRequest(f)
	case iprobel.RxMasterProtectConnect:
		return s.handleMasterProtectConnect(f)
	case iprobel.RxAllSourceNamesRequest:
		return s.handleAllSourceNames(f)
	case iprobel.RxSingleSourceNameRequest:
		return s.handleSingleSourceName(f)
	case iprobel.RxAllDestNamesRequest:
		return s.handleAllDestAssocNames(f)
	case iprobel.RxSingleDestNameRequest:
		return s.handleSingleDestAssocName(f)
	case iprobel.RxCrosspointTieLineInterrogate:
		return s.handleTieLineInterrogate(f)
	case iprobel.RxAllSourceAssocNamesRequest:
		return s.handleAllSourceAssocNames(f)
	case iprobel.RxSingleSourceAssocNameRequest:
		return s.handleSingleSourceAssocName(f)
	}
	s.profile.Note(UnsupportedCommand)
	return handlerResult{}, nil
}
