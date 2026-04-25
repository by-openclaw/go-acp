package codec

// CommandDirection captures whether a command travels controller →
// matrix (Rx) or matrix → controller (Tx). Used by the CLI catalogue
// renderers (`dhs list-commands`, `dhs help-cmd`).
type CommandDirection string

const (
	DirRx CommandDirection = "rx"
	DirTx CommandDirection = "tx"
)

// CommandSpec is the structured metadata used by the CLI catalogue
// helpers. Stdlib-only so the codec stays lift-ready.
type CommandSpec struct {
	ID         CommandID        // wire byte
	Name       string           // snake_case identifier (matches CommandName)
	Direction  CommandDirection // rx (controller → matrix) or tx (matrix → controller)
	SpecRef    string           // SW-P-02 Issue 26 section, e.g. "§3.2.4"
	Payload    string           // "fixed N bytes" / "variable" / "zero" — hint, not a schema
	Notes      string           // free-form (extended-form, owner-only auth, etc.)
	Supported  bool             // true if this codec implements the command
}

// Commands returns the full SW-P-02 command catalogue this codec
// supports — every byte covered by a cmd_rxNNN_*.go / cmd_txNNN_*.go
// file. Order: command-byte ascending. Used by `dhs list-commands
// probel-sw02p` and `dhs help-cmd probel-sw02p NN`.
func Commands() []CommandSpec {
	return []CommandSpec{
		{ID: RxInterrogate, Name: "interrogate", Direction: DirRx, SpecRef: "§3.2.3", Payload: "fixed 2 bytes", Supported: true},
		{ID: RxConnect, Name: "connect", Direction: DirRx, SpecRef: "§3.2.4", Payload: "fixed 3 bytes", Supported: true},
		{ID: TxTally, Name: "tally", Direction: DirTx, SpecRef: "§3.2.5", Payload: "fixed 3 bytes", Supported: true},
		{ID: TxCrosspointConnected, Name: "crosspoint_connected", Direction: DirTx, SpecRef: "§3.2.6", Payload: "fixed 3 bytes", Notes: "broadcast on all ports", Supported: true},
		{ID: RxConnectOnGo, Name: "connect_on_go", Direction: DirRx, SpecRef: "§3.2.7", Payload: "fixed 3 bytes", Notes: "salvo build", Supported: true},
		{ID: RxGo, Name: "go", Direction: DirRx, SpecRef: "§3.2.8", Payload: "fixed 1 byte", Notes: "salvo commit", Supported: true},
		{ID: RxStatusRequest, Name: "status_request", Direction: DirRx, SpecRef: "§3.2.9", Payload: "zero", Supported: true},
		{ID: TxStatusResponse2, Name: "status_response_2", Direction: DirTx, SpecRef: "§3.2.11", Payload: "fixed 6 bytes", Supported: true},
		{ID: TxConnectOnGoAck, Name: "connect_on_go_ack", Direction: DirTx, SpecRef: "§3.2.14", Payload: "fixed 3 bytes", Supported: true},
		{ID: TxGoDoneAck, Name: "go_done_ack", Direction: DirTx, SpecRef: "§3.2.15", Payload: "fixed 1 byte", Supported: true},
		{ID: RxSourceLockStatusRequest, Name: "source_lock_status_request", Direction: DirRx, SpecRef: "§3.2.16", Payload: "zero", Supported: true},
		{ID: TxSourceLockStatusResponse, Name: "source_lock_status_response", Direction: DirTx, SpecRef: "§3.2.17", Payload: "variable", Notes: "var-len", Supported: true},
		{ID: RxConnectOnGoGroupSalvo, Name: "connect_on_go_group_salvo", Direction: DirRx, SpecRef: "§3.2.36", Payload: "fixed 4 bytes", Notes: "group salvo build", Supported: true},
		{ID: TxConnectOnGoGroupSalvoAck, Name: "connect_on_go_group_salvo_ack", Direction: DirTx, SpecRef: "§3.2.37", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxGoGroupSalvo, Name: "go_group_salvo", Direction: DirRx, SpecRef: "§3.2.38", Payload: "fixed 2 bytes", Notes: "group salvo commit", Supported: true},
		{ID: TxGoDoneGroupSalvoAck, Name: "go_done_group_salvo_ack", Direction: DirTx, SpecRef: "§3.2.39", Payload: "fixed 2 bytes", Supported: true},
		{ID: RxDualControllerStatusRequest, Name: "dual_controller_status_request", Direction: DirRx, SpecRef: "§3.2.45", Payload: "zero", Supported: true},
		{ID: TxDualControllerStatusResponse, Name: "dual_controller_status_response", Direction: DirTx, SpecRef: "§3.2.46", Payload: "fixed 2 bytes", Supported: true},
		{ID: RxExtendedInterrogate, Name: "extended_interrogate", Direction: DirRx, SpecRef: "§3.2.47", Payload: "fixed 2 bytes", Notes: "extended addressing (dst 0-16383)", Supported: true},
		{ID: RxExtendedConnect, Name: "extended_connect", Direction: DirRx, SpecRef: "§3.2.48", Payload: "fixed 4 bytes", Notes: "extended addressing", Supported: true},
		{ID: TxExtendedTally, Name: "extended_tally", Direction: DirTx, SpecRef: "§3.2.49", Payload: "fixed 4 bytes", Supported: true},
		{ID: TxExtendedConnected, Name: "extended_connected", Direction: DirTx, SpecRef: "§3.2.50", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxExtendedConnectOnGo, Name: "extended_connect_on_go", Direction: DirRx, SpecRef: "§3.2.51", Payload: "fixed 4 bytes", Supported: true},
		{ID: TxExtendedConnectOnGoAck, Name: "extended_connect_on_go_ack", Direction: DirTx, SpecRef: "§3.2.52", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxExtendedConnectOnGoGroupSalvo, Name: "extended_connect_on_go_group_salvo", Direction: DirRx, SpecRef: "§3.2.53", Payload: "fixed 5 bytes", Supported: true},
		{ID: TxExtendedConnectOnGoGroupSalvoAck, Name: "extended_connect_on_go_group_salvo_ack", Direction: DirTx, SpecRef: "§3.2.54", Payload: "fixed 5 bytes", Supported: true},
		{ID: RxRouterConfigRequest, Name: "router_config_request", Direction: DirRx, SpecRef: "§3.2.57", Payload: "zero", Supported: true},
		{ID: TxRouterConfigResponse1, Name: "router_config_response_1", Direction: DirTx, SpecRef: "§3.2.58", Payload: "variable", Notes: "var-len, level map + per-level dst/src counts", Supported: true},
		{ID: TxRouterConfigResponse2, Name: "router_config_response_2", Direction: DirTx, SpecRef: "§3.2.59", Payload: "variable", Notes: "var-len, sparse level layout", Supported: true},
		{ID: TxExtendedProtectTally, Name: "extended_protect_tally", Direction: DirTx, SpecRef: "§3.2.60", Payload: "fixed 4 bytes", Supported: true},
		{ID: TxExtendedProtectConnected, Name: "extended_protect_connected", Direction: DirTx, SpecRef: "§3.2.61", Payload: "fixed 4 bytes", Supported: true},
		{ID: TxExtendedProtectDisconnected, Name: "extended_protect_disconnected", Direction: DirTx, SpecRef: "§3.2.62", Payload: "fixed 4 bytes", Supported: true},
		{ID: TxProtectDeviceNameResponse, Name: "protect_device_name_response", Direction: DirTx, SpecRef: "§3.2.63", Payload: "fixed 10 bytes", Supported: true},
		{ID: TxExtendedProtectTallyDump, Name: "extended_protect_tally_dump", Direction: DirTx, SpecRef: "§3.2.64", Payload: "variable", Notes: "var-len", Supported: true},
		{ID: RxExtendedProtectInterrogate, Name: "extended_protect_interrogate", Direction: DirRx, SpecRef: "§3.2.65", Payload: "fixed 2 bytes", Supported: true},
		{ID: RxExtendedProtectConnect, Name: "extended_protect_connect", Direction: DirRx, SpecRef: "§3.2.66", Payload: "fixed 4 bytes", Notes: "owner-only auth", Supported: true},
		{ID: RxProtectDeviceNameRequest, Name: "protect_device_name_request", Direction: DirRx, SpecRef: "§3.2.67", Payload: "fixed 2 bytes", Supported: true},
		{ID: RxExtendedProtectDisconnect, Name: "extended_protect_disconnect", Direction: DirRx, SpecRef: "§3.2.68", Payload: "fixed 4 bytes", Notes: "owner-only auth", Supported: true},
		{ID: RxExtendedProtectTallyDumpRequest, Name: "extended_protect_tally_dump_request", Direction: DirRx, SpecRef: "§3.2.69", Payload: "fixed 2 bytes", Supported: true},
	}
}

// CommandByID looks up a single CommandSpec by wire byte. Returns
// false when the byte isn't in this codec's catalogue.
func CommandByID(id CommandID) (CommandSpec, bool) {
	for _, c := range Commands() {
		if c.ID == id {
			return c, true
		}
	}
	return CommandSpec{}, false
}
