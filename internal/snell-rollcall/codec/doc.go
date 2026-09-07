// Package codec implements the Snell RollCall wire format.
//
// It is stdlib-only and must stay that way (ADR-0006): it never imports
// dhs/*, so it can be lifted into its own repository unchanged.
//
// # Wire shape
//
// Every RollCall message on TCP is a 4-byte transmission header followed by a
// 14-byte address header, a 2-byte message header, and the payload:
//
//	off  size  field
//	 0    2    TxHeader.Flags   = 0x000C
//	 2    2    TxHeader.Length  = 14 + RLength
//	 4    6    Dst {Net u16, Unit u8, Port u8, Index i16}
//	10    6    Src {same}
//	16    2    RLength = 2 + len(payload)
//	18    1    Type   (0..71)
//	19    1    Flags  (0x80 back channel, 0x40 wide area)
//	20    n    payload
//
// We encode payloads within 420 bytes, the largest the vendor library will
// queue, and decode up to the 1554 the specification permits, so a legal peer
// is never rejected for sending more than we would send ourselves.
//
// All multi-byte fields are big-endian and every struct is packed with no
// padding, with one documented exception: GetNext carries a trailing pad byte
// so that it occupies 4 bytes rather than 3.
//
// # Two generations
//
// RollCall has two coexisting payload generations. The original uses 16-bit
// command and menu numbers with strings in fixed 20-byte fields. The 2014
// extension adds packet types 65..71 with 32-bit numbers and NUL-terminated
// UTF-8 strings up to 64 bytes. Which one a session uses is negotiated by the
// SvcLongStr bit in the Call service mask, not by anything in the frame
// header, so the codec decodes whichever form it is handed and leaves the
// negotiation to the session layer.
//
// This file set covers the frame, the addressing and the 16-bit payloads.
//
// # Sources
//
// Byte layouts come from the RollCall Technical Specification Revision 14 and,
// where the specification is silent, from the vendor C library under
// assets/Protocol/Source/RollCallV2. Both are quoted per struct. Test vectors
// are taken from the specification and from frames captured off a live device;
// never from this implementation.
package codec
