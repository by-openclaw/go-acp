// Package codec implements the Probel SW-P-02 wire framer (byte-level
// codec). It is consumer-agnostic and provider-agnostic: it only knows
// bytes. It has ZERO dependencies outside the Go standard library so it
// can be lifted into a separate repo without modification.
//
// Authoritative spec:
//
//	internal/probel-sw08p/assets/probel-sw02/SW-P-02_issue_26.txt
//	(General Remote Control Protocol — SW-P-02 Issue 26)
//
// Section §3.1 of that document defines the framing:
//
//	SOM  COMMAND  MESSAGE  CHECKSUM
//
//   - SOM = 0xFF (single-byte start of message; no DLE escaping).
//   - COMMAND = 1 byte.
//   - MESSAGE = 0 or more bytes depending on command.
//   - CHECKSUM = 7-bit two's-complement sum of COMMAND + MESSAGE bytes,
//     MSB forced to 0.
//
// Unlike SW-P-08, SW-P-02 does not use DLE stuffing — bytes inside the
// frame are transparent. The protocol normally rides RS-485/422 but
// this plugin runs over TCP (default port 2002).
//
// Consumer wrapper lives at internal/probel-sw02p/consumer/; provider
// wrapper lives at internal/probel-sw02p/provider/.
package codec

// SOM is the single-byte start-of-message marker used by SW-P-02 (§3.1).
// A matching end-of-message / escape mechanism does not exist: the
// receiver identifies frames by decoding COMMAND, consuming MESSAGE
// bytes per the command table, then validating CHECKSUM.
const SOM byte = 0xFF

// CommandID is a single Probel SW-P-02 command byte. The catalogue is
// populated by per-command files added in follow-up commits; the
// scaffold intentionally defines no command constants.
type CommandID byte

// DefaultPort is the TCP port this plugin binds when the caller does
// not supply one. SW-P-02 was originally serial (RS-485/422); 2002 is
// the project convention, mirroring SW-P-08 on 2008.
const DefaultPort = 2002
