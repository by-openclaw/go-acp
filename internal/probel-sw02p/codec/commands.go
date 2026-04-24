package codec

// Command byte constants — one per-direction alias per §3.2 entry. Per
// SW-P-02 convention:
//
//	Rx = controller → matrix (request into the matrix)
//	Tx = matrix → controller (reply out of the matrix)
//
// A single wire byte has only one meaning in SW-P-02 (unlike SW-P-08);
// the Rx/Tx prefix in the constant name records which side initiates.
// All constants are decimal per §3 ("command byte numbers are in
// decimal" — spec §3, issue 26 change log note 4).
const (
	// RxInterrogate — §3.2.3, "INTERROGATE Message". Controller asks
	// the matrix which source is routed to a destination; the matrix
	// replies with tx 03 TALLY.
	RxInterrogate CommandID = 0x01

	// RxConnect — §3.2.4, "CONNECT Message". Controller requests a
	// route through the matrix; applied immediately (no salvo buffer)
	// and confirmed via tx 04 CROSSPOINT CONNECTED broadcast.
	RxConnect CommandID = 0x02

	// TxTally — §3.2.5, "TALLY Message". Matrix replies to rx 01
	// INTERROGATE. Source = 1023 signals "destination out of range"
	// per §3.2.5.
	TxTally CommandID = 0x03

	// TxCrosspointConnected — §3.2.6, "CONNECTED Message". Matrix
	// broadcasts on all ports after a route is set, including one per
	// slot on salvo commit (§3.2.8 "no CONNECTED" note is overridden;
	// see probel_sw02p_salvo_emitted_connected compliance event).
	TxCrosspointConnected CommandID = 0x04

	// RxSourceLockStatusRequest — §3.2.16. HD-router-only query for
	// input signal-health bits. "Source Lock" is NOT a write-
	// protection — it is a read-only indicator of carrier presence
	// on each input module, distinct from the Extended PROTECT
	// family (§3.2.60+) which guards write access. Both are
	// orthogonal per memory/project_probel_extensions.md.
	RxSourceLockStatusRequest CommandID = 0x0E // 14 dec

	// TxSourceLockStatusResponse — §3.2.17. Variable-length reply: 2
	// self-declared length bytes + N bitmap bytes (4 sources / byte,
	// low nibble only).
	TxSourceLockStatusResponse CommandID = 0x0F // 15 dec

	// RxStatusRequest — §3.2.9, "STATUS REQUEST Message". Controller
	// asks the matrix for its current hardware status (LH or RH
	// controller targeted). Matrix replies with one of the §§3.2.10-
	// 3.2.19 status-response shapes; this plugin ships tx 09 STATUS
	// RESPONSE - 2 (§3.2.11).
	RxStatusRequest CommandID = 0x07

	// TxStatusResponse2 — §3.2.11, "STATUS RESPONSE - 2 Message".
	// Issued by a 5007/5107 TDM controller card in response to rx 07.
	// Single-byte status: bit 6 idle, bit 5 bus fault, bit 4 overheat.
	TxStatusResponse2 CommandID = 0x09

	// RxConnectOnGo — §3.2.7, "CONNECT ON Go Message". Controller
	// stages one crosspoint into the matrix's pending salvo buffer.
	RxConnectOnGo CommandID = 0x05

	// RxGo — §3.2.8, "GO Message". Commits (op=00) or clears (op=01)
	// every previously received CONNECT ON GO slot.
	RxGo CommandID = 0x06

	// TxConnectOnGoAck — §3.2.14, "CONNECT ON GO ACKNOWLEDGE Message".
	// Matrix confirms that a single RxConnectOnGo was stored.
	TxConnectOnGoAck CommandID = 0x0C // 12 dec

	// TxGoDoneAck — §3.2.15, "GO DONE ACKNOWLEDGE Message". Matrix
	// confirms that a rx 06 GO was executed; emits on all ports.
	TxGoDoneAck CommandID = 0x0D // 13 dec

	// RxDualControllerStatusRequest — §3.2.45. Controller asks a
	// dual-router-controller system for the current master/slave +
	// idle-controller status. No MESSAGE bytes.
	RxDualControllerStatusRequest CommandID = 0x32 // 50 dec

	// TxDualControllerStatusResponse — §3.2.46. Active controller
	// replies with (Active Controller, Idle Controller Status) —
	// 2-byte MESSAGE.
	TxDualControllerStatusResponse CommandID = 0x33 // 51 dec

	// RxConnectOnGoGroupSalvo — §3.2.36, "CONNECT ON GO GROUP SALVO
	// Message". Like rx 05 but slot is stored under a SalvoID
	// (0-127) instead of the single unnamed buffer.
	RxConnectOnGoGroupSalvo CommandID = 0x23 // 35 dec

	// TxConnectOnGoGroupSalvoAck — §3.2.38. Ack for rx 35 — echoes
	// dst / src / SalvoID.
	TxConnectOnGoGroupSalvoAck CommandID = 0x25 // 37 dec

	// RxGoGroupSalvo — §3.2.37, "GO GROUP SALVO Message". Commits
	// (op=00) or clears (op=01) the SalvoID-keyed pending buffer.
	RxGoGroupSalvo CommandID = 0x24 // 36 dec

	// TxGoDoneGroupSalvoAck — §3.2.39, "GO DONE GROUP SALVO
	// ACKNOWLEDGE Message". Matrix confirms rx 36 execution; status
	// byte adds a 3rd value (02 = no crosspoints to set / clear)
	// over tx 13.
	TxGoDoneGroupSalvoAck CommandID = 0x26 // 38 dec

	// RxExtendedInterrogate — §3.2.47, "Extended INTERROGATE Message".
	// Destination-only query extended to 16383; matrix replies with
	// tx 67 Extended TALLY.
	RxExtendedInterrogate CommandID = 0x41 // 65 dec

	// RxExtendedConnect — §3.2.48, "Extended CONNECT Message".
	// Extended addressing (0-16383) equivalent of rx 02; applied
	// immediately and confirmed via tx 68 Extended CONNECTED broadcast.
	RxExtendedConnect CommandID = 0x42 // 66 dec

	// TxExtendedTally — §3.2.49, "Extended TALLY Message". Matrix
	// reply to rx 65; carries the extended Multiplier pair + Status byte.
	TxExtendedTally CommandID = 0x43 // 67 dec

	// TxExtendedConnected — §3.2.50, "Extended CONNECTED Message".
	// Broadcast on all ports after an extended route is made. Same
	// layout as tx 67.
	TxExtendedConnected CommandID = 0x44 // 68 dec

	// RxExtendedConnectOnGo — §3.2.51, "Extended CONNECT ON GO
	// Message". Extended-addressing counterpart to rx 005: one
	// crosspoint staged into the unnamed salvo buffer until rx 006
	// GO fires. Range per axis: 0-16383.
	RxExtendedConnectOnGo CommandID = 0x45 // 69 dec

	// TxExtendedConnectOnGoAck — §3.2.52, "Extended CONNECT ON GO
	// ACKNOWLEDGE Message". Matrix confirms rx 069 was stored; echoes
	// dst / src in extended form.
	TxExtendedConnectOnGoAck CommandID = 0x46 // 70 dec

	// RxExtendedConnectOnGoGroupSalvo — §3.2.53, "Extended CONNECT
	// ON GO GROUP SALVO Message". Like rx 35 but dst / src ranges
	// extend to 16383 via separate Destination Multiplier (§3.2.47)
	// + Source Multiplier (§3.2.48) bytes.
	RxExtendedConnectOnGoGroupSalvo CommandID = 0x47 // 71 dec

	// TxExtendedConnectOnGoGroupSalvoAck — §3.2.54. Ack for rx 71,
	// echoes the extended dst / src / SalvoID.
	TxExtendedConnectOnGoGroupSalvoAck CommandID = 0x48 // 72 dec

	// RxRouterConfigRequest — §3.2.57, "ROUTER CONFIGURATION REQUEST
	// Message". Zero-MESSAGE query; matrix replies with tx 076
	// RESPONSE-1 (§3.2.58) or tx 077 RESPONSE-2 (§3.2.59).
	RxRouterConfigRequest CommandID = 0x4B // 75 dec

	// TxRouterConfigResponse1 — §3.2.58. Variable-length response
	// carrying a 28-bit level bit map + per-level (dest count,
	// source count) entries.
	TxRouterConfigResponse1 CommandID = 0x4C // 76 dec

	// TxRouterConfigResponse2 — §3.2.59. Variable-length response
	// extending RESPONSE-1 with Start Destination / Start Source
	// (non-contiguous level addressing) + 2 reserved bytes per level.
	TxRouterConfigResponse2 CommandID = 0x4D // 77 dec

	// TxExtendedProtectTally — §3.2.60. Controller / router response
	// to EXTENDED PROTECT INTERROGATE (§3.2.65) reporting the current
	// protect status of a destination.
	TxExtendedProtectTally CommandID = 0x60 // 96 dec

	// TxExtendedProtectConnected — §3.2.61. Broadcast on all ports
	// when protect data is altered as a result of EXTENDED PROTECT
	// CONNECT (§3.2.66). Same layout as tx 96.
	TxExtendedProtectConnected CommandID = 0x61 // 97 dec

	// TxExtendedProtectDisconnected — §3.2.62. Broadcast on all ports
	// when a destination is unprotected as a result of EXTENDED
	// PROTECT DIS-CONNECT (§3.2.68). Same layout as tx 96.
	TxExtendedProtectDisconnected CommandID = 0x62 // 98 dec

	// RxExtendedProtectInterrogate — §3.2.65. Destination-only query
	// for the current protect status; router replies with tx 096.
	RxExtendedProtectInterrogate CommandID = 0x65 // 101 dec

	// RxExtendedProtectConnect — §3.2.66. Remote device asks the
	// matrix to protect a destination on behalf of a given Device
	// Number. The matrix replies with tx 097 broadcast fan-out.
	// Authority rule (owner-only): once a destination is protected
	// by Device N, only Device N (or a local-admin path) can modify
	// or clear it (§3.2.60 table). ProbelOverride is remotely
	// immutable.
	RxExtendedProtectConnect CommandID = 0x66 // 102 dec

	// TxExtendedProtectTallyDump — §3.2.64. Variable-length dump of
	// protect-tally entries. Count byte 127 signals "controller reset"
	// (no entries follow); 0-32 reports that many 4-byte entries. The
	// only SW-P-02 command with a variable MESSAGE length — the stream
	// scanner peeks at the Count byte via PayloadSize to derive the
	// total frame size.
	TxExtendedProtectTallyDump CommandID = 0x64 // 100 dec
)

// PayloadLen returns the expected MESSAGE byte count for command id.
// Returns (n, true) for a fixed-length command; (0, false) when id is
// unknown or variable-length. The session scanner uses the bool to
// decide whether it has enough bytes buffered to Unpack one frame.
//
// Every per-command file registers its length here; keep the switch
// in command-byte order.
func PayloadLen(id CommandID) (int, bool) {
	switch id {
	case RxInterrogate:
		return PayloadLenInterrogate, true
	case RxConnect:
		return PayloadLenConnect, true
	case TxTally:
		return PayloadLenTally, true
	case TxCrosspointConnected:
		return PayloadLenConnected, true
	case RxSourceLockStatusRequest:
		return PayloadLenSourceLockStatusRequest, true
	case RxStatusRequest:
		return PayloadLenStatusRequest, true
	case TxStatusResponse2:
		return PayloadLenStatusResponse2, true
	case RxConnectOnGo:
		return PayloadLenConnectOnGo, true
	case RxGo:
		return PayloadLenGo, true
	case TxConnectOnGoAck:
		return PayloadLenConnectOnGoAck, true
	case TxGoDoneAck:
		return PayloadLenGoDoneAck, true
	case RxDualControllerStatusRequest:
		return PayloadLenDualControllerStatusRequest, true
	case TxDualControllerStatusResponse:
		return PayloadLenDualControllerStatusResponse, true
	case RxConnectOnGoGroupSalvo:
		return PayloadLenConnectOnGoGroupSalvo, true
	case TxConnectOnGoGroupSalvoAck:
		return PayloadLenConnectOnGoGroupSalvoAck, true
	case RxGoGroupSalvo:
		return PayloadLenGoGroupSalvo, true
	case TxGoDoneGroupSalvoAck:
		return PayloadLenGoDoneGroupSalvoAck, true
	case RxExtendedInterrogate:
		return PayloadLenExtendedInterrogate, true
	case RxExtendedConnect:
		return PayloadLenExtendedConnect, true
	case TxExtendedTally:
		return PayloadLenExtendedTally, true
	case TxExtendedConnected:
		return PayloadLenExtendedConnected, true
	case RxExtendedConnectOnGo:
		return PayloadLenExtendedConnectOnGo, true
	case TxExtendedConnectOnGoAck:
		return PayloadLenExtendedConnectOnGoAck, true
	case RxExtendedConnectOnGoGroupSalvo:
		return PayloadLenExtendedConnectOnGoGroupSalvo, true
	case TxExtendedConnectOnGoGroupSalvoAck:
		return PayloadLenExtendedConnectOnGoGroupSalvoAck, true
	case RxRouterConfigRequest:
		return PayloadLenRouterConfigRequest, true
	case TxExtendedProtectTally:
		return PayloadLenExtendedProtectTally, true
	case TxExtendedProtectConnected:
		return PayloadLenExtendedProtectConnected, true
	case TxExtendedProtectDisconnected:
		return PayloadLenExtendedProtectDisconnected, true
	case RxExtendedProtectInterrogate:
		return PayloadLenExtendedProtectInterrogate, true
	case RxExtendedProtectConnect:
		return PayloadLenExtendedProtectConnect, true
	}
	return 0, false
}

// PayloadSize returns the MESSAGE byte count for the command at id
// given a peek at buffered MESSAGE bytes (after the COMMAND byte,
// before the CHECKSUM). Subsumes both the fixed-length PayloadLen
// registry and the variable-length tally-dump sizer.
//
// Returns:
//   - (n, true)  — MESSAGE is exactly n bytes; the scanner can peel
//     `SOM + cmd + n + checksum` off the wire.
//   - (0, false) — id unknown OR not enough bytes buffered yet for
//     a variable-length command to compute its length.
//
// Variable-length commands MUST be listed in the switch below
// alongside their fixed-length siblings; PayloadLen alone returns
// false for them.
func PayloadSize(id CommandID, peek []byte) (int, bool) {
	if n, ok := PayloadLen(id); ok {
		return n, true
	}
	switch id {
	case TxExtendedProtectTallyDump:
		return tallyDumpPayloadSize(peek)
	case TxRouterConfigResponse1:
		return routerConfigResponse1PayloadSize(peek)
	case TxRouterConfigResponse2:
		return routerConfigResponse2PayloadSize(peek)
	case TxSourceLockStatusResponse:
		return sourceLockStatusResponsePayloadSize(peek)
	}
	return 0, false
}

// isVariableLenCommand reports whether id has a MESSAGE length that
// depends on the MESSAGE content (i.e. PayloadSize consults `peek`).
// Used by the stream scanner to distinguish "unknown command" from
// "variable-length command, need more bytes".
func isVariableLenCommand(id CommandID) bool {
	switch id {
	case TxExtendedProtectTallyDump,
		TxRouterConfigResponse1,
		TxRouterConfigResponse2,
		TxSourceLockStatusResponse:
		return true
	}
	return false
}
