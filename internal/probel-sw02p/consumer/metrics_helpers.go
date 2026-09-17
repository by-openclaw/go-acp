package probelsw02p

import (
	"dhs/internal/metrics"
	"dhs/internal/probel-sw02p/codec"
)

// probelCmdFromBytes extracts the SW-P-02 command byte from a raw
// frame's wire bytes. Returns (cmd, true) for a well-formed frame
// (SOM + cmd + MESSAGE + checksum ≥ 3 bytes); returns (0, false) for
// anything too short or mis-framed.
//
// The raw observer callbacks (OnRx / OnTx on session.ClientConfig)
// receive framed application traffic; this helper lets the metrics
// wrapper attribute each frame to a specific command.
func probelCmdFromBytes(b []byte) (uint8, bool) {
	if len(b) < 3 || b[0] != codec.SOM {
		return 0, false
	}
	return b[1], true
}

// observeRxBytes attributes one inbound raw buffer to the metrics
// connector: per-command when the bytes form a recognisable SW-P-02 frame,
// otherwise as an un-attributed rx byte count. Factored out of the Connect
// OnRx closure so the non-frame fallback arm is directly exercisable — the
// live codec only ever delivers fully-Unpacked frames (len >= 3, SOM
// leading) to OnRx, so that arm is otherwise unreachable through a session.
func observeRxBytes(met *metrics.Connector, b []byte) {
	if id, ok := probelCmdFromBytes(b); ok {
		met.ObserveCmdRx(id, len(b))
	} else {
		met.ObserveRx(len(b))
	}
}

// observeTxBytes is the tx counterpart of observeRxBytes.
func observeTxBytes(met *metrics.Connector, b []byte) {
	if id, ok := probelCmdFromBytes(b); ok {
		met.ObserveCmdTx(id, len(b), 0)
	} else {
		met.ObserveTx(len(b), 0)
	}
}
