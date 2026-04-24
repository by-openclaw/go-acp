package codec

// CommandName returns a human-readable label for a SW-P-02 command
// byte. Empty string when the id is not known to this codec. Used by
// metrics reporting (top-commands tables, Prom label sets, CSV/MD
// exports) and by log lines that want a symbolic name instead of raw
// hex.
//
// Source of truth: §3 of SW-P-02 Issue 26. Keep the switch in
// command-byte order.
func CommandName(id CommandID) string {
	switch id {
	case RxInterrogate:
		return "interrogate"
	case RxConnect:
		return "connect"
	case TxTally:
		return "tally"
	case TxCrosspointConnected:
		return "crosspoint_connected"
	case RxSourceLockStatusRequest:
		return "source_lock_status_request"
	case TxSourceLockStatusResponse:
		return "source_lock_status_response"
	case RxStatusRequest:
		return "status_request"
	case TxStatusResponse2:
		return "status_response_2"
	case RxConnectOnGo:
		return "connect_on_go"
	case RxGo:
		return "go"
	case TxConnectOnGoAck:
		return "connect_on_go_ack"
	case TxGoDoneAck:
		return "go_done_ack"
	case RxDualControllerStatusRequest:
		return "dual_controller_status_request"
	case TxDualControllerStatusResponse:
		return "dual_controller_status_response"
	case RxConnectOnGoGroupSalvo:
		return "connect_on_go_group_salvo"
	case TxConnectOnGoGroupSalvoAck:
		return "connect_on_go_group_salvo_ack"
	case RxGoGroupSalvo:
		return "go_group_salvo"
	case TxGoDoneGroupSalvoAck:
		return "go_done_group_salvo_ack"
	case RxExtendedInterrogate:
		return "extended_interrogate"
	case RxExtendedConnect:
		return "extended_connect"
	case TxExtendedTally:
		return "extended_tally"
	case TxExtendedConnected:
		return "extended_connected"
	case RxExtendedConnectOnGo:
		return "extended_connect_on_go"
	case TxExtendedConnectOnGoAck:
		return "extended_connect_on_go_ack"
	case RxExtendedConnectOnGoGroupSalvo:
		return "extended_connect_on_go_group_salvo"
	case TxExtendedConnectOnGoGroupSalvoAck:
		return "extended_connect_on_go_group_salvo_ack"
	case RxRouterConfigRequest:
		return "router_config_request"
	case TxRouterConfigResponse1:
		return "router_config_response_1"
	case TxRouterConfigResponse2:
		return "router_config_response_2"
	case TxExtendedProtectTally:
		return "extended_protect_tally"
	case TxExtendedProtectConnected:
		return "extended_protect_connected"
	case TxExtendedProtectDisconnected:
		return "extended_protect_disconnected"
	case TxExtendedProtectTallyDump:
		return "extended_protect_tally_dump"
	case RxExtendedProtectInterrogate:
		return "extended_protect_interrogate"
	case RxExtendedProtectConnect:
		return "extended_protect_connect"
	case RxExtendedProtectDisconnect:
		return "extended_protect_disconnect"
	}
	return ""
}

// CommandIDs returns every SW-P-02 command byte this codec knows. The
// order is not guaranteed; callers that need to iterate for
// registration should treat the result as a set.
func CommandIDs() []CommandID {
	return []CommandID{
		RxInterrogate,
		RxConnect,
		TxTally,
		TxCrosspointConnected,
		RxSourceLockStatusRequest,
		TxSourceLockStatusResponse,
		RxStatusRequest,
		TxStatusResponse2,
		RxConnectOnGo,
		RxGo,
		TxConnectOnGoAck,
		TxGoDoneAck,
		RxDualControllerStatusRequest,
		TxDualControllerStatusResponse,
		RxConnectOnGoGroupSalvo,
		RxGoGroupSalvo,
		TxConnectOnGoGroupSalvoAck,
		TxGoDoneGroupSalvoAck,
		RxExtendedInterrogate,
		RxExtendedConnect,
		TxExtendedTally,
		TxExtendedConnected,
		RxExtendedConnectOnGo,
		TxExtendedConnectOnGoAck,
		RxExtendedConnectOnGoGroupSalvo,
		TxExtendedConnectOnGoGroupSalvoAck,
		RxRouterConfigRequest,
		TxRouterConfigResponse1,
		TxRouterConfigResponse2,
		TxExtendedProtectTally,
		TxExtendedProtectConnected,
		TxExtendedProtectDisconnected,
		TxExtendedProtectTallyDump,
		RxExtendedProtectInterrogate,
		RxExtendedProtectConnect,
		RxExtendedProtectDisconnect,
	}
}
