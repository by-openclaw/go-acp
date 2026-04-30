// Package codec is the stdlib-only byte codec for TSL UMD (v3.1, v4.0,
// v5.0). It is lift-to-own-repo ready: no imports outside stdlib. Every
// wire frame type is one file (v31_frame.go, v40_xdata.go, v50_packet.go,
// etc.) with table-driven unit tests alongside.
//
// Layering:
//
//	v31 : HEADER(1) | CTRL(1) | DATA(16)
//	v40 : v31 + CHKSUM(1) + VBC(1) + XDATA(N)
//	v50 : PBC(2LE) | VER(1) | FLAGS(1) | SCREEN(2LE) | (DMSG+ | SCONTROL)
//
// v5 TCP adds a DLE(0xFE)/STX(0x02) wrapper + 0xFE byte stuffing around
// the packet — handled by a separate DLE/STX framer.
//
// Spec source: internal/tsl/assets/tsl-umd-protocol.txt (pdftotext
// extract of the TSL UMD Protocol PDF).
package codec
