// Package probel implements the Probel SW-P-08 wire protocol (framer + codec).
//
// Authoritative spec:
//
//	assets/probel/probel-sw08p/SW-P-08 Issue 30.doc  (General Remote Control Protocol)
//
// Section §2 of that document defines the transmission protocol (ACK/NAK
// flow, retry semantics, 10ms response target, 128-byte DATA cap).
//
// Secondary reference for byte layouts (NOT authoritative for flow):
//
//	assets/probel/smh-probelsw08p/  (TypeScript matrix-side emulator — useful as
//	                                 a decode/layout aid; the Go implementation
//	                                 follows the spec, not the TS code)
//
// This package is consumer-agnostic and provider-agnostic: it only knows
// bytes. It has ZERO dependencies outside the Go standard library so it
// can be lifted into a separate repo without modification. Consumer
// wrapper lives at internal/protocol/probel/; provider wrapper lives at
// internal/provider/probel/.
package probel

// Control symbols from ASCII control set (SW-P-88 §3.2).
//
// | Symbol | Byte | Meaning                                              |
// |--------|------|------------------------------------------------------|
// | DLE    | 0x10 | Data Link Escape — frame delimiter and escape char   |
// | STX    | 0x02 | Start of Text (paired with DLE = SOM)                |
// | ETX    | 0x03 | End of Text   (paired with DLE = EOM)                |
// | ACK    | 0x06 | Positive acknowledge (paired with DLE)               |
// | NAK    | 0x15 | Negative acknowledge (paired with DLE)               |
const (
	DLE byte = 0x10
	STX byte = 0x02
	ETX byte = 0x03
	ACK byte = 0x06
	NAK byte = 0x15
)

// CommandID is a single Probel SW-P-08 command byte (rx or tx, general or extended).
// General rx+tx codes are < 128 (0x00–0x7F). Extended codes are >= 128 (0x80–0xFF).
type CommandID byte

// RX general command IDs (controller → matrix). Source: SW-P-88 §5 and TS
// assets/probel/smh-probelsw08p/src/command/command-contract.ts RX_GENERAL.
const (
	RxCrosspointInterrogate          CommandID = 0x01 // 001 dest status request
	RxCrosspointConnect              CommandID = 0x02 // 002 connect (route)
	RxMaintenance                    CommandID = 0x07 // 007 keepalive
	RxDualControllerStatusRequest    CommandID = 0x08 // 008 dual-ctl status req
	RxProtectInterrogate             CommandID = 0x0A // 010 protect status req
	RxProtectConnect                 CommandID = 0x0C // 012 protect on
	RxProtectDisconnect              CommandID = 0x0E // 014 protect off
	RxProtectDeviceNameRequest       CommandID = 0x11 // 017 owner name req
	RxProtectTallyDumpRequest        CommandID = 0x13 // 019 protect dump req
	RxCrosspointTallyDumpRequest     CommandID = 0x15 // 021 tally dump req
	RxMasterProtectConnect           CommandID = 0x1D // 029 master protect
	RxAllSourceNamesRequest          CommandID = 0x64 // 100 names all srcs
	RxSingleSourceNameRequest        CommandID = 0x65 // 101 name single src
	RxAllDestNamesRequest            CommandID = 0x66 // 102 names all dsts
	RxSingleDestNameRequest          CommandID = 0x67 // 103 name single dst
	RxCrosspointTieLineInterrogate   CommandID = 0x70 // 112 tie-line status
	RxAllSourceAssocNamesRequest     CommandID = 0x72 // 114 assoc all srcs
	RxSingleSourceAssocNameRequest   CommandID = 0x73 // 115 assoc single src
	RxUpdateNameRequest              CommandID = 0x75 // 117 rename notify
	RxCrosspointConnectOnGoSalvo     CommandID = 0x78 // 120 salvo build
	RxCrosspointGoSalvo              CommandID = 0x79 // 121 salvo fire
	RxCrosspointSalvoGroupInterrogate CommandID = 0x7C // 124 salvo query
)

// Application keepalive (TS "application-keep-alive" module, #91). The
// matrix periodically emits TxAppKeepaliveRequest; the controller is
// expected to reply with RxAppKeepaliveResponse. Both are zero-payload.
//
// NOTE: the byte 0x11 on the rx side is RxProtectDeviceNameRequest
// (above); the same byte on the tx side is TxAppKeepaliveRequest below.
// The bytes are direction-overloaded per the TS command catalogue —
// consumers decode inbound 0x11 as TxAppKeepaliveRequest; providers
// decode inbound 0x11 as RxProtectDeviceNameRequest.
const (
	TxAppKeepaliveRequest  CommandID = 0x11 // matrix → controller: ping
	RxAppKeepaliveResponse CommandID = 0x22 // controller → matrix: pong
)

// RX extended command IDs (controller → matrix, wide addressing). Addresses above
// the general-command range flip the MSB: general ID | 0x80 = extended ID.
const (
	RxCrosspointInterrogateExt       CommandID = 0x81
	RxCrosspointConnectExt           CommandID = 0x82
	RxProtectInterrogateExt          CommandID = 0x8A
	RxProtectConnectExt              CommandID = 0x8C
	RxProtectDisconnectExt           CommandID = 0x8E
	RxProtectTallyDumpRequestExt     CommandID = 0x93
	RxCrosspointTallyDumpRequestExt  CommandID = 0x95
	RxAllSourceNamesRequestExt       CommandID = 0xE4
	RxSingleSourceNameRequestExt     CommandID = 0xE5
	RxAllDestNamesRequestExt         CommandID = 0xE6
	RxSingleDestNameRequestExt       CommandID = 0xE7
	RxCrosspointConnectOnGoSalvoExt  CommandID = 0xF8
	RxCrosspointSalvoGroupInterrogateExt CommandID = 0xFC
)

// TX general command IDs (matrix → controller). Source: SW-P-88 §5 and TS
// assets/probel/smh-probelsw08p/src/command/command-contract.ts TX_GENERAL.
const (
	TxCrosspointTally              CommandID = 0x03 // 003 async tally
	TxCrosspointConnected          CommandID = 0x04 // 004 connect ack
	TxDualControllerStatusResponse CommandID = 0x09 // 009
	TxProtectTally                 CommandID = 0x0B // 011
	TxProtectConnected             CommandID = 0x0D // 013
	TxProtectDisconnected          CommandID = 0x0F // 015
	TxProtectDeviceNameResponse    CommandID = 0x12 // 018
	TxProtectTallyDump             CommandID = 0x14 // 020
	TxCrosspointTallyDumpByte      CommandID = 0x16 // 022
	TxCrosspointTallyDumpWord      CommandID = 0x17 // 023
	TxSourceNamesResponse          CommandID = 0x6A // 106
	TxDestAssocNamesResponse       CommandID = 0x6B // 107
	TxCrosspointTieLineTally       CommandID = 0x71 // 113
	TxSourceAssocNamesResponse     CommandID = 0x74 // 116
	TxSalvoConnectOnGoAck          CommandID = 0x7A // 122
	TxSalvoGoDoneAck               CommandID = 0x7B // 123
	TxSalvoGroupTally              CommandID = 0x7D // 125
)

// TX extended command IDs (matrix → controller, wide addressing).
const (
	TxCrosspointTallyExt         CommandID = 0x83
	TxCrosspointConnectedExt     CommandID = 0x84
	TxProtectTallyExt            CommandID = 0x8B
	TxProtectConnectedExt        CommandID = 0x8D
	TxProtectDisconnectedExt     CommandID = 0x8F
	TxProtectTallyDumpExt        CommandID = 0x94
	TxCrosspointTallyDumpWordExt CommandID = 0x97
	TxSourceNamesResponseExt     CommandID = 0xEA
	TxDestAssocNamesResponseExt  CommandID = 0xEB
	TxSalvoConnectOnGoAckExt     CommandID = 0xFA
	TxSalvoGroupTallyExt         CommandID = 0xFD
)

// IsExtended reports whether a command ID is in the extended range (bit 7 set).
// Extended commands use wider address fields than their general counterparts.
func (id CommandID) IsExtended() bool { return id&0x80 != 0 }

// DefaultPort is the TCP port commonly used by Probel SW-P-08 matrices
// when exposed over IP. Serial mode is the original transport and
// carries no port.
const DefaultPort = 2008

// ProtectState is the 4-value enum carried in tx 011 / 013 / 015 / 020
// byte 3 (or byte 3 of the header for tx 020 items) to describe a
// destination's protect disposition. Defined in SW-P-08 §3.1.5.
//
// | Value | Meaning                                             |
// |-------|-----------------------------------------------------|
// |   0   | Not Protected                                       |
// |   1   | Pro-Bel Protected                                   |
// |   2   | Pro-Bel override Protected (cannot be altered rem.) |
// |   3   | OEM Protected                                       |
type ProtectState uint8

const (
	ProtectNone       ProtectState = 0
	ProtectProbel     ProtectState = 1
	ProtectProbelOver ProtectState = 2
	ProtectOEM        ProtectState = 3
)
