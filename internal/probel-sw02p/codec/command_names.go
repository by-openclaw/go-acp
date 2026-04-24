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
	case TxCrosspointConnected:
		return "crosspoint_connected"
	case RxConnectOnGo:
		return "connect_on_go"
	case RxGo:
		return "go"
	case TxConnectOnGoAck:
		return "connect_on_go_ack"
	case TxGoDoneAck:
		return "go_done_ack"
	case RxConnectOnGoGroupSalvo:
		return "connect_on_go_group_salvo"
	case TxConnectOnGoGroupSalvoAck:
		return "connect_on_go_group_salvo_ack"
	}
	return ""
}

// CommandIDs returns every SW-P-02 command byte this codec knows. The
// order is not guaranteed; callers that need to iterate for
// registration should treat the result as a set.
func CommandIDs() []CommandID {
	return []CommandID{
		TxCrosspointConnected,
		RxConnectOnGo,
		RxGo,
		TxConnectOnGoAck,
		TxGoDoneAck,
		RxConnectOnGoGroupSalvo,
		TxConnectOnGoGroupSalvoAck,
	}
}
