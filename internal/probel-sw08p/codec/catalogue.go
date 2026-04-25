package codec

// CommandDirection captures whether a command travels controller →
// matrix (Rx), matrix → controller (Tx), or both (one byte means
// different things by direction). Used by the CLI catalogue
// renderers (`dhs list-commands`, `dhs help-cmd`).
type CommandDirection string

const (
	DirRx   CommandDirection = "rx"
	DirTx   CommandDirection = "tx"
	DirBoth CommandDirection = "both"
)

// CommandSpec is the structured metadata used by the CLI catalogue
// helpers. Stdlib-only so the codec stays lift-ready.
type CommandSpec struct {
	ID         CommandID
	Name       string           // matches CommandName output verbatim
	Direction  CommandDirection // rx / tx / both (0x11 is rx 17 OR tx 17)
	SpecRef    string           // SW-P-08 Issue 30 section reference
	Payload    string           // "fixed N bytes" / "variable" / "zero"
	Notes      string
	Supported  bool
}

// Commands returns the full SW-P-08 command catalogue this codec
// supports. Used by `dhs list-commands probel-sw08p` and
// `dhs help-cmd probel-sw08p NN`.
func Commands() []CommandSpec {
	return []CommandSpec{
		// RX general
		{ID: RxCrosspointInterrogate, Name: "rx 001 Crosspoint Interrogate", Direction: DirRx, SpecRef: "§3.2.1", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxCrosspointConnect, Name: "rx 002 Crosspoint Connect", Direction: DirRx, SpecRef: "§3.2.2", Payload: "fixed 6 bytes", Supported: true},
		{ID: TxCrosspointTally, Name: "tx 003 Crosspoint Tally", Direction: DirTx, SpecRef: "§3.2.3", Payload: "fixed 6 bytes", Supported: true},
		{ID: TxCrosspointConnected, Name: "tx 004 Crosspoint Connected", Direction: DirTx, SpecRef: "§3.2.4", Payload: "fixed 6 bytes", Supported: true},
		{ID: RxMaintenance, Name: "rx 007 Maintenance", Direction: DirRx, SpecRef: "§3.2.7", Payload: "fixed 1 byte", Supported: true},
		{ID: RxDualControllerStatusRequest, Name: "rx 008 Dual Controller Status Request", Direction: DirRx, SpecRef: "§3.2.8", Payload: "zero", Supported: true},
		{ID: TxDualControllerStatusResponse, Name: "tx 009 Dual Controller Status Response", Direction: DirTx, SpecRef: "§3.2.9", Payload: "fixed 2 bytes", Supported: true},
		{ID: RxProtectInterrogate, Name: "rx 010 Protect Interrogate", Direction: DirRx, SpecRef: "§3.2.10", Payload: "fixed 4 bytes", Supported: true},
		{ID: TxProtectTally, Name: "tx 011 Protect Tally", Direction: DirTx, SpecRef: "§3.2.11", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxProtectConnect, Name: "rx 012 Protect Connect", Direction: DirRx, SpecRef: "§3.2.12", Payload: "fixed 6 bytes", Supported: true},
		{ID: TxProtectConnected, Name: "tx 013 Protect Connected", Direction: DirTx, SpecRef: "§3.2.13", Payload: "fixed 6 bytes", Supported: true},
		{ID: RxProtectDisconnect, Name: "rx 014 Protect Disconnect", Direction: DirRx, SpecRef: "§3.2.14", Payload: "fixed 4 bytes", Supported: true},
		{ID: TxProtectDisconnected, Name: "tx 015 Protect Disconnected", Direction: DirTx, SpecRef: "§3.2.15", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxProtectDeviceNameRequest, Name: "rx 017 Protect Device Name Req / tx 017 App Keepalive Req", Direction: DirBoth, SpecRef: "§3.2.17 (rx) / §3.2.17 (tx)", Payload: "zero (tx) / fixed 2 bytes (rx)", Notes: "byte 0x11 overloaded by direction", Supported: true},
		{ID: TxProtectDeviceNameResponse, Name: "tx 018 Protect Device Name Response", Direction: DirTx, SpecRef: "§3.2.18", Payload: "variable", Notes: "var-len device name", Supported: true},
		{ID: RxProtectTallyDumpRequest, Name: "rx 019 Protect Tally Dump Request", Direction: DirRx, SpecRef: "§3.2.19", Payload: "fixed 2 bytes", Supported: true},
		{ID: TxProtectTallyDump, Name: "tx 020 Protect Tally Dump", Direction: DirTx, SpecRef: "§3.2.20", Payload: "variable", Notes: "var-len, streamed", Supported: true},
		{ID: RxCrosspointTallyDumpRequest, Name: "rx 021 Crosspoint Tally Dump Request", Direction: DirRx, SpecRef: "§3.2.21", Payload: "fixed 2 bytes", Supported: true},
		{ID: TxCrosspointTallyDumpByte, Name: "tx 022 Crosspoint Tally Dump (byte)", Direction: DirTx, SpecRef: "§3.2.22", Payload: "variable", Notes: "var-len byte form", Supported: true},
		{ID: TxCrosspointTallyDumpWord, Name: "tx 023 Crosspoint Tally Dump (word)", Direction: DirTx, SpecRef: "§3.2.23", Payload: "variable", Notes: "var-len word form", Supported: true},
		{ID: RxMasterProtectConnect, Name: "rx 029 Master Protect Connect", Direction: DirRx, SpecRef: "§3.2.29", Payload: "fixed 6 bytes", Supported: true},
		{ID: RxAppKeepaliveResponse, Name: "rx 034 App Keepalive Response", Direction: DirRx, SpecRef: "§3.2.34", Payload: "zero", Supported: true},
		{ID: RxAllSourceNamesRequest, Name: "rx 100 All Source Names Request", Direction: DirRx, SpecRef: "§3.2.100", Payload: "fixed 2 bytes", Supported: true},
		{ID: RxSingleSourceNameRequest, Name: "rx 101 Single Source Name Request", Direction: DirRx, SpecRef: "§3.2.101", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxAllDestNamesRequest, Name: "rx 102 All Dest Assoc Names Request", Direction: DirRx, SpecRef: "§3.2.102", Payload: "fixed 2 bytes", Supported: true},
		{ID: RxSingleDestNameRequest, Name: "rx 103 Single Dest Assoc Name Request", Direction: DirRx, SpecRef: "§3.2.103", Payload: "fixed 4 bytes", Supported: true},
		{ID: TxSourceNamesResponse, Name: "tx 106 Source Names Response", Direction: DirTx, SpecRef: "§3.2.106", Payload: "variable", Notes: "var-len", Supported: true},
		{ID: TxDestAssocNamesResponse, Name: "tx 107 Dest Assoc Names Response", Direction: DirTx, SpecRef: "§3.2.107", Payload: "variable", Supported: true},
		{ID: RxCrosspointTieLineInterrogate, Name: "rx 112 Crosspoint Tie-Line Interrogate", Direction: DirRx, SpecRef: "§3.2.112", Payload: "fixed 4 bytes", Supported: true},
		{ID: TxCrosspointTieLineTally, Name: "tx 113 Crosspoint Tie-Line Tally", Direction: DirTx, SpecRef: "§3.2.113", Payload: "fixed 6 bytes", Supported: true},
		{ID: RxAllSourceAssocNamesRequest, Name: "rx 114 All Source Assoc Names Request", Direction: DirRx, SpecRef: "§3.2.114", Payload: "fixed 2 bytes", Supported: true},
		{ID: RxSingleSourceAssocNameRequest, Name: "rx 115 Single Source Assoc Name Request", Direction: DirRx, SpecRef: "§3.2.115", Payload: "fixed 4 bytes", Supported: true},
		{ID: TxSourceAssocNamesResponse, Name: "tx 116 Source Assoc Names Response", Direction: DirTx, SpecRef: "§3.2.116", Payload: "variable", Supported: true},
		{ID: RxUpdateNameRequest, Name: "rx 117 Update Name Request", Direction: DirRx, SpecRef: "§3.2.117", Payload: "variable", Supported: true},
		{ID: RxCrosspointConnectOnGoSalvo, Name: "rx 120 Crosspoint Connect-On-Go Salvo", Direction: DirRx, SpecRef: "§3.2.120", Payload: "variable", Notes: "salvo build (var-len)", Supported: true},
		{ID: RxCrosspointGoSalvo, Name: "rx 121 Crosspoint Go Salvo", Direction: DirRx, SpecRef: "§3.2.121", Payload: "fixed 2 bytes", Notes: "salvo commit", Supported: true},
		{ID: TxSalvoConnectOnGoAck, Name: "tx 122 Salvo Connect-On-Go Ack", Direction: DirTx, SpecRef: "§3.2.122", Payload: "fixed 2 bytes", Supported: true},
		{ID: RxCrosspointSalvoGroupInterrogate, Name: "rx 124 Crosspoint Salvo Group Interrogate", Direction: DirRx, SpecRef: "§3.2.124", Payload: "fixed 2 bytes", Supported: true},

		// RX/TX extended
		{ID: RxCrosspointInterrogateExt, Name: "rx 129 Crosspoint Interrogate (ext)", Direction: DirRx, SpecRef: "§3.4.1", Payload: "fixed 6 bytes", Notes: "extended addressing", Supported: true},
		{ID: RxCrosspointConnectExt, Name: "rx 130 Crosspoint Connect (ext)", Direction: DirRx, SpecRef: "§3.4.2", Payload: "fixed 8 bytes", Notes: "extended addressing", Supported: true},
		{ID: RxProtectInterrogateExt, Name: "rx 138 Protect Interrogate (ext)", Direction: DirRx, SpecRef: "§3.4.10", Payload: "fixed 6 bytes", Supported: true},
		{ID: RxProtectConnectExt, Name: "rx 140 Protect Connect (ext)", Direction: DirRx, SpecRef: "§3.4.12", Payload: "fixed 8 bytes", Supported: true},
		{ID: RxProtectDisconnectExt, Name: "rx 142 Protect Disconnect (ext)", Direction: DirRx, SpecRef: "§3.4.14", Payload: "fixed 6 bytes", Supported: true},
		{ID: RxProtectTallyDumpRequestExt, Name: "rx 147 Protect Tally Dump Request (ext)", Direction: DirRx, SpecRef: "§3.4.19", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxCrosspointTallyDumpRequestExt, Name: "rx 149 Crosspoint Tally Dump Request (ext)", Direction: DirRx, SpecRef: "§3.4.21", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxAllSourceNamesRequestExt, Name: "rx 228 All Source Names Request (ext)", Direction: DirRx, SpecRef: "§3.4.100", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxSingleSourceNameRequestExt, Name: "rx 229 Single Source Name Request (ext)", Direction: DirRx, SpecRef: "§3.4.101", Payload: "fixed 6 bytes", Supported: true},
		{ID: RxAllDestNamesRequestExt, Name: "rx 230 All Dest Assoc Names Request (ext)", Direction: DirRx, SpecRef: "§3.4.102", Payload: "fixed 4 bytes", Supported: true},
		{ID: RxSingleDestNameRequestExt, Name: "rx 231 Single Dest Assoc Name Request (ext)", Direction: DirRx, SpecRef: "§3.4.103", Payload: "fixed 6 bytes", Supported: true},
		{ID: RxCrosspointConnectOnGoSalvoExt, Name: "rx 248 Crosspoint Connect-On-Go Salvo (ext)", Direction: DirRx, SpecRef: "§3.4.120", Payload: "variable", Supported: true},
		{ID: RxCrosspointSalvoGroupInterrogateExt, Name: "rx 252 Crosspoint Salvo Group Interrogate (ext)", Direction: DirRx, SpecRef: "§3.4.124", Payload: "fixed 4 bytes", Supported: true},
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
