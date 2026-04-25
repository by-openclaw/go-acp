package probelsw02p

import "acp/internal/probel-sw02p/codec"

// probelCmdFromBytes extracts the SW-P-02 command byte from a raw
// frame's wire bytes. Returns (cmd, true) for a well-formed frame
// (SOM + cmd + MESSAGE + checksum ≥ 3 bytes); returns (0, false) for
// anything too short or mis-framed.
//
// The raw observer callbacks (OnRx / OnTx on codec.ClientConfig)
// receive framed application traffic; this helper lets the metrics
// wrapper attribute each frame to a specific command.
func probelCmdFromBytes(b []byte) (uint8, bool) {
	if len(b) < 3 || b[0] != codec.SOM {
		return 0, false
	}
	return b[1], true
}
