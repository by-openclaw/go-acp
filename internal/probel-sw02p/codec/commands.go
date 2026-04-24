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
	// RxConnectOnGo — §3.2.7, "CONNECT ON Go Message". Controller
	// stages one crosspoint into the matrix's pending salvo buffer.
	RxConnectOnGo CommandID = 0x05

	// TxConnectOnGoAck — §3.2.14, "CONNECT ON GO ACKNOWLEDGE Message".
	// Matrix confirms that a single RxConnectOnGo was stored.
	TxConnectOnGoAck CommandID = 0x0C // 12 dec
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
	case RxConnectOnGo:
		return PayloadLenConnectOnGo, true
	case TxConnectOnGoAck:
		return PayloadLenConnectOnGoAck, true
	}
	return 0, false
}
