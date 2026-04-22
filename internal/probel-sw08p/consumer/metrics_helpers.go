package probelsw08p

import "acp/internal/probel-sw08p/codec"

// probelCmdFromBytes extracts the SW-P-08 command byte from a raw
// frame's wire bytes. Returns (cmd, true) for a well-formed
// DLE STX cmd … frame; returns (0, false) for DLE ACK (`10 06`),
// DLE NAK (`10 15`), or anything too short / mis-framed.
//
// The raw observer callbacks (OnRx / OnTx on codec.ClientConfig)
// receive both application frames AND bare ACK/NAK signals; this
// helper lets the metrics wrapper attribute the former to a specific
// command and route the latter through the aggregate path.
func probelCmdFromBytes(b []byte) (uint8, bool) {
	if len(b) < 3 || b[0] != codec.DLE || b[1] != codec.STX {
		return 0, false
	}
	return b[2], true
}
