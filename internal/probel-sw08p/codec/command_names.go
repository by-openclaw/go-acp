package codec

// CommandName returns a human-readable label for a SW-P-08 command
// byte. Empty string when the id is not known to this codec. Used by
// metrics reporting (top-commands tables, Prom label sets, CSV/MD
// exports) and by log lines that want a symbolic name instead of
// raw hex.
//
// Source of truth: §3.2 (general) and §3.4/§3.5 (extended) of
// SW-P-08 Issue 30, mirrored here from types.go constants.
func CommandName(id CommandID) string {
	switch id {
	// RX general (controller → matrix)
	case RxCrosspointInterrogate:
		return "rx 001 Crosspoint Interrogate"
	case RxCrosspointConnect:
		return "rx 002 Crosspoint Connect"
	case RxMaintenance:
		return "rx 007 Maintenance"
	case RxDualControllerStatusRequest:
		return "rx 008 Dual Controller Status Request"
	case RxProtectInterrogate:
		return "rx 010 Protect Interrogate"
	case RxProtectConnect:
		return "rx 012 Protect Connect"
	case RxProtectDisconnect:
		return "rx 014 Protect Disconnect"
	case RxProtectDeviceNameRequest:
		// Byte 0x11 is direction-overloaded per SW-P-08 catalogue:
		// controller→matrix reads as "rx 017 Protect Device Name
		// Request"; matrix→controller reads as "tx 017 App Keepalive
		// Request". The switch only sees the byte value — combined
		// label captures both meanings since metrics distinguish
		// direction via rxHits vs txHits.
		return "rx 017 Protect Device Name Req / tx 017 App Keepalive Req"
	case RxProtectTallyDumpRequest:
		return "rx 019 Protect Tally Dump Request"
	case RxCrosspointTallyDumpRequest:
		return "rx 021 Crosspoint Tally Dump Request"
	case RxMasterProtectConnect:
		return "rx 029 Master Protect Connect"
	case RxAppKeepaliveResponse:
		return "rx 034 App Keepalive Response"
	case RxAllSourceNamesRequest:
		return "rx 100 All Source Names Request"
	case RxSingleSourceNameRequest:
		return "rx 101 Single Source Name Request"
	case RxAllDestNamesRequest:
		return "rx 102 All Dest Assoc Names Request"
	case RxSingleDestNameRequest:
		return "rx 103 Single Dest Assoc Name Request"
	case RxCrosspointTieLineInterrogate:
		return "rx 112 Crosspoint Tie-Line Interrogate"
	case RxAllSourceAssocNamesRequest:
		return "rx 114 All Source Assoc Names Request"
	case RxSingleSourceAssocNameRequest:
		return "rx 115 Single Source Assoc Name Request"
	case RxUpdateNameRequest:
		return "rx 117 Update Name Request"
	case RxCrosspointConnectOnGoSalvo:
		return "rx 120 Crosspoint Connect-On-Go Salvo"
	case RxCrosspointGoSalvo:
		return "rx 121 Crosspoint Go Salvo"
	case RxCrosspointSalvoGroupInterrogate:
		return "rx 124 Crosspoint Salvo Group Interrogate"

	// RX extended
	case RxCrosspointInterrogateExt:
		return "rx 129 Crosspoint Interrogate (ext)"
	case RxCrosspointConnectExt:
		return "rx 130 Crosspoint Connect (ext)"
	case RxProtectInterrogateExt:
		return "rx 138 Protect Interrogate (ext)"
	case RxProtectConnectExt:
		return "rx 140 Protect Connect (ext)"
	case RxProtectDisconnectExt:
		return "rx 142 Protect Disconnect (ext)"
	case RxProtectTallyDumpRequestExt:
		return "rx 147 Protect Tally Dump Request (ext)"
	case RxCrosspointTallyDumpRequestExt:
		return "rx 149 Crosspoint Tally Dump Request (ext)"
	case RxAllSourceNamesRequestExt:
		return "rx 228 All Source Names Request (ext)"
	case RxSingleSourceNameRequestExt:
		return "rx 229 Single Source Name Request (ext)"
	case RxAllDestNamesRequestExt:
		return "rx 230 All Dest Assoc Names Request (ext)"
	case RxSingleDestNameRequestExt:
		return "rx 231 Single Dest Assoc Name Request (ext)"
	case RxCrosspointConnectOnGoSalvoExt:
		return "rx 248 Crosspoint Connect-On-Go Salvo (ext)"
	case RxCrosspointSalvoGroupInterrogateExt:
		return "rx 252 Crosspoint Salvo Group Interrogate (ext)"

	// TX general (matrix → controller)
	case TxCrosspointTally:
		return "tx 003 Crosspoint Tally"
	case TxCrosspointConnected:
		return "tx 004 Crosspoint Connected"
	case TxDualControllerStatusResponse:
		return "tx 009 Dual Controller Status Response"
	case TxProtectTally:
		return "tx 011 Protect Tally"
	case TxProtectConnected:
		return "tx 013 Protect Connected"
	case TxProtectDisconnected:
		return "tx 015 Protect Disconnected"
	// NOTE: TxAppKeepaliveRequest (0x11) collides with
	// RxProtectDeviceNameRequest — handled in the rx block above with
	// a combined label.
	case TxProtectDeviceNameResponse:
		return "tx 018 Protect Device Name Response"
	case TxProtectTallyDump:
		return "tx 020 Protect Tally Dump"
	case TxCrosspointTallyDumpByte:
		return "tx 022 Crosspoint Tally Dump (byte)"
	case TxCrosspointTallyDumpWord:
		return "tx 023 Crosspoint Tally Dump (word)"
	case TxSourceNamesResponse:
		return "tx 106 Source Names Response"
	case TxDestAssocNamesResponse:
		return "tx 107 Dest Assoc Names Response"
	case TxCrosspointTieLineTally:
		return "tx 113 Crosspoint Tie-Line Tally"
	case TxSourceAssocNamesResponse:
		return "tx 116 Source Assoc Names Response"
	case TxSalvoConnectOnGoAck:
		return "tx 122 Salvo Connect-On-Go Ack"
	}
	return ""
}

// CommandIDs returns every SW-P-08 command byte this codec knows. The
// order is not guaranteed; callers that need to iterate for
// registration should treat the result as a set.
func CommandIDs() []CommandID {
	return []CommandID{
		RxCrosspointInterrogate, RxCrosspointConnect, RxMaintenance,
		RxDualControllerStatusRequest, RxProtectInterrogate,
		RxProtectConnect, RxProtectDisconnect, RxProtectDeviceNameRequest,
		RxProtectTallyDumpRequest, RxCrosspointTallyDumpRequest,
		RxMasterProtectConnect, RxAppKeepaliveResponse,
		RxAllSourceNamesRequest, RxSingleSourceNameRequest,
		RxAllDestNamesRequest, RxSingleDestNameRequest,
		RxCrosspointTieLineInterrogate, RxAllSourceAssocNamesRequest,
		RxSingleSourceAssocNameRequest, RxUpdateNameRequest,
		RxCrosspointConnectOnGoSalvo, RxCrosspointGoSalvo,
		RxCrosspointSalvoGroupInterrogate,

		RxCrosspointInterrogateExt, RxCrosspointConnectExt,
		RxProtectInterrogateExt, RxProtectConnectExt, RxProtectDisconnectExt,
		RxProtectTallyDumpRequestExt, RxCrosspointTallyDumpRequestExt,
		RxAllSourceNamesRequestExt, RxSingleSourceNameRequestExt,
		RxAllDestNamesRequestExt, RxSingleDestNameRequestExt,
		RxCrosspointConnectOnGoSalvoExt, RxCrosspointSalvoGroupInterrogateExt,

		TxCrosspointTally, TxCrosspointConnected,
		TxDualControllerStatusResponse, TxProtectTally,
		TxProtectConnected, TxProtectDisconnected,
		// TxAppKeepaliveRequest (0x11) collides with
		// RxProtectDeviceNameRequest already in the rx block above —
		// single entry covers both directions.
		TxProtectDeviceNameResponse,
		TxProtectTallyDump, TxCrosspointTallyDumpByte,
		TxCrosspointTallyDumpWord, TxSourceNamesResponse,
		TxDestAssocNamesResponse, TxCrosspointTieLineTally,
		TxSourceAssocNamesResponse, TxSalvoConnectOnGoAck,
	}
}
