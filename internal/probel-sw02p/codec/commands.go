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

	// TxTally — §3.2.5, "TALLY Message". Matrix replies to rx 01
	// INTERROGATE. Source = 1023 signals "destination out of range"
	// per §3.2.5.
	TxTally CommandID = 0x03

	// TxCrosspointConnected — §3.2.6, "CONNECTED Message". Matrix
	// broadcasts on all ports after a route is set, including one per
	// slot on salvo commit (§3.2.8 "no CONNECTED" note is overridden;
	// see probel_sw02p_salvo_emitted_connected compliance event).
	TxCrosspointConnected CommandID = 0x04

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

	// RxExtendedConnectOnGoGroupSalvo — §3.2.53, "Extended CONNECT
	// ON GO GROUP SALVO Message". Like rx 35 but dst / src ranges
	// extend to 16383 via separate Destination Multiplier (§3.2.47)
	// + Source Multiplier (§3.2.48) bytes.
	RxExtendedConnectOnGoGroupSalvo CommandID = 0x47 // 71 dec

	// TxExtendedConnectOnGoGroupSalvoAck — §3.2.54. Ack for rx 71,
	// echoes the extended dst / src / SalvoID.
	TxExtendedConnectOnGoGroupSalvoAck CommandID = 0x48 // 72 dec
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
	case TxTally:
		return PayloadLenTally, true
	case TxCrosspointConnected:
		return PayloadLenConnected, true
	case RxConnectOnGo:
		return PayloadLenConnectOnGo, true
	case RxGo:
		return PayloadLenGo, true
	case TxConnectOnGoAck:
		return PayloadLenConnectOnGoAck, true
	case TxGoDoneAck:
		return PayloadLenGoDoneAck, true
	case RxConnectOnGoGroupSalvo:
		return PayloadLenConnectOnGoGroupSalvo, true
	case TxConnectOnGoGroupSalvoAck:
		return PayloadLenConnectOnGoGroupSalvoAck, true
	case RxGoGroupSalvo:
		return PayloadLenGoGroupSalvo, true
	case TxGoDoneGroupSalvoAck:
		return PayloadLenGoDoneGroupSalvoAck, true
	case RxExtendedConnectOnGoGroupSalvo:
		return PayloadLenExtendedConnectOnGoGroupSalvo, true
	case TxExtendedConnectOnGoGroupSalvoAck:
		return PayloadLenExtendedConnectOnGoGroupSalvoAck, true
	}
	return 0, false
}
