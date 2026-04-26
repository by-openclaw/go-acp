// Package s101 implements the S101 framing protocol for Ember+.
//
// S101 wraps BER-encoded Glow payloads in framed TCP messages with:
//   - BOF/EOF markers (0xFE / 0xFF)
//   - Byte escaping (0xFD prefix)
//   - CRC-CCITT16 integrity check
//   - Message type header (4 bytes)
//   - Keep-alive support
//
// Reference: S101 specification (Lawo Ember+ docs)
package s101

import "errors"

// Frame markers.
const (
	BOF byte = 0xFE // Beginning of frame
	EOF byte = 0xFF // End of frame
	ESC byte = 0xFD // Escape prefix
)

// S101 header bytes.
const (
	SlotDefault byte = 0x00 // slot 0

	MsgEmBER     byte = 0x0E // EmBER payload (Glow data)
	MsgKeepAlive byte = 0x01 // Keep-alive request/response

	CmdEmBER         byte = 0x00 // EmBER command
	CmdKeepAliveReq  byte = 0x01 // Keep-alive request
	CmdKeepAliveResp byte = 0x02 // Keep-alive response

	VersionS101 byte = 0x01 // S101 version

	// Flags for EmBER frames.
	FlagSingle byte = 0xC0 // Single-packet (first + last)
	FlagFirst  byte = 0x80 // First packet of multi
	FlagLast   byte = 0x40 // Last packet of multi
	FlagEmpty  byte = 0x20 // Empty packet

	// DTD fields.
	DTDGlow         byte = 0x01 // Glow DTD identifier
	AppBytesLen     byte = 0x02 // Length of app bytes (DTD minor + major)
	DTDMinorVersion byte = 0x1F // 31
	DTDMajorVersion byte = 0x02 // 2
)

var (
	ErrBadFrame   = errors.New("s101: invalid frame (missing BOF/EOF)")
	ErrBadCRC     = errors.New("s101: CRC mismatch")
	ErrTruncated  = errors.New("s101: truncated frame")
)

// Frame is a decoded S101 frame.
type Frame struct {
	Slot    byte   // slot (0x00)
	MsgType byte   // message type: MsgEmBER or MsgKeepAlive
	Command byte   // CmdEmBER, CmdKeepAliveReq, etc.
	Version byte   // S101 version (0x01)
	Flags   byte   // FlagSingle, FlagFirst, FlagLast
	DTD     byte   // DTD identifier (0x01 = Glow)
	Payload []byte // BER-encoded data (for EmBER frames)
}

// Encode builds an S101 wire frame: BOF + header + escaped payload + CRC + EOF.
//
// Wire layout (EmBER frame before BOF/escape/CRC/EOF wrapping):
//
//	| Offset | Field           | Width | Notes                                   |
//	|--------|-----------------|-------|-----------------------------------------|
//	|   0    | BOF             |   1   | 0xFE start-of-frame (wrapper, post-esc) |
//	|   1    | slot            |   1   | always 0x00                             |
//	|   2    | message type    |   1   | 0x0E MsgEmBER                           |
//	|   3    | command         |   1   | 0x00 EmBER / 0x01 KA req / 0x02 KA resp |
//	|   4    | version         |   1   | 0x01 S101 version                       |
//	|   5    | flags           |   1   | 0xC0 Single / 0x80 First / 0x40 Last    |
//	|   6    | DTD             |   1   | 0x01 Glow                               |
//	|   7    | appBytesLen     |   1   | 0x02 (length of following DTD version)  |
//	|   8    | DTD minor ver   |   1   | 0x1F (=31)                              |
//	|   9    | DTD major ver   |   1   | 0x02 (=2)                               |
//	|  10..  | BER payload     |   N   | Glow TLV tree                           |
//	|  N+10  | CRC-CCITT16     |   2   | little-endian, over unescaped content   |
//	|  N+12  | EOF             |   1   | 0xFF end-of-frame (wrapper, post-esc)   |
//
// Keep-alive frames compress to a 4-byte content [slot, msgType=0x0E, cmd,
// version] plus CRC; no flags/DTD/payload. After header assembly the whole
// unescaped buffer is byte-escaped (0xFD prefix, XOR 0x20) for any byte
// >= 0xF8, then wrapped with BOF/EOF markers.
//
// Spec reference: Ember+ Documentation.pdf §S101 Framing p. 94.
func Encode(f *Frame) []byte {
	var raw []byte

	if f.Command == CmdKeepAliveReq || f.Command == CmdKeepAliveResp {
		// Keep-alive: slot + msgType + command + version (4 bytes).
		raw = []byte{f.Slot, f.MsgType, f.Command, VersionS101}
	} else {
		// EmBER: 9-byte header + BER payload.
		raw = make([]byte, 0, 9+len(f.Payload))
		raw = append(raw,
			f.Slot,          // 0: slot
			MsgEmBER,        // 1: message type
			CmdEmBER,        // 2: command
			VersionS101,     // 3: version
			f.Flags,         // 4: flags
			DTDGlow,         // 5: DTD type
			AppBytesLen,     // 6: app bytes length (2)
			DTDMinorVersion, // 7: DTD minor version (31)
			DTDMajorVersion, // 8: DTD major version (2)
		)
		raw = append(raw, f.Payload...)
	}

	// Calculate CRC over raw content (inverted).
	crc := crcCCITT16(raw)

	// Append CRC (little-endian).
	raw = append(raw, byte(crc&0xFF), byte(crc>>8))

	// Escape bytes >= 0xF8 and wrap with BOF/EOF.
	out := []byte{BOF}
	for _, b := range raw {
		if b >= 0xF8 {
			out = append(out, ESC, b^0x20)
		} else {
			out = append(out, b)
		}
	}
	out = append(out, EOF)
	return out
}

// Decode parses an S101 frame from wire bytes (including BOF/EOF).
//
// Inverse of Encode — strips BOF/EOF, unescapes 0xFD sequences, verifies the
// little-endian CRC-CCITT16 trailer, then reads the header fields:
//
//	| Offset | Field         | Width | Notes                               |
//	|--------|---------------|-------|-------------------------------------|
//	|   0    | slot          |   1   | 0x00                                |
//	|   1    | message type  |   1   | 0x0E (MsgEmBER)                     |
//	|   2    | command       |   1   | 0x00 EmBER / 0x01 KA req / 0x02 resp|
//	|   3    | version       |   1   | 0x01                                |
//	|   4    | flags         |   1   | only on EmBER frames                |
//	|   5    | DTD           |   1   | 0x01 Glow                           |
//	|   6    | appBytesLen   |   1   | 0x02                                |
//	|   7    | DTD minor ver |   1   | 31                                  |
//	|   8    | DTD major ver |   1   | 2                                   |
//	|   9..  | payload       |   N   | BER Glow TLVs                       |
//
// Spec reference: Ember+ Documentation.pdf §S101 Framing p. 94.
func Decode(data []byte) (*Frame, error) {
	if len(data) < 2 || data[0] != BOF || data[len(data)-1] != EOF {
		return nil, ErrBadFrame
	}

	// Unescape content (between BOF and EOF).
	var raw []byte
	escaped := false
	for _, b := range data[1 : len(data)-1] {
		if escaped {
			raw = append(raw, b^0x20)
			escaped = false
		} else if b == ESC {
			escaped = true
		} else {
			raw = append(raw, b)
		}
	}

	// Need at least header + CRC. Minimum: keep-alive = 4 header + 2 CRC = 6.
	if len(raw) < 4 {
		return nil, ErrTruncated
	}

	// Verify CRC (last 2 bytes, little-endian).
	content := raw[:len(raw)-2]
	gotCRC := uint16(raw[len(raw)-2]) | uint16(raw[len(raw)-1])<<8
	wantCRC := crcCCITT16(content)
	if gotCRC != wantCRC {
		return nil, ErrBadCRC
	}

	f := &Frame{
		Slot:    content[0],
		MsgType: content[1],
		Command: content[2],
		Version: content[3],
	}

	// Keep-alive: msgType=0x0E but command=0x01 (req) or 0x02 (resp).
	// Only 4-byte header, no flags/DTD/payload.
	if f.Command == CmdKeepAliveReq || f.Command == CmdKeepAliveResp {
		return f, nil
	}

	// EmBER frame: need flags + DTD + app bytes (at least 9 bytes header).
	if len(content) < 9 {
		return nil, ErrTruncated
	}
	f.Flags = content[4]
	f.DTD = content[5]
	// content[6] = AppBytesLen (skip)
	// content[7] = DTD minor version (skip)
	// content[8] = DTD major version (skip)
	if len(content) > 9 {
		f.Payload = content[9:]
	}
	return f, nil
}

// crcTable is the S101 CRC lookup table from the Ember+ spec (page 94).
// Reflected CRC-CCITT, polynomial 0x8408, initial value 0xFFFF.
var crcTable = [256]uint16{
	0x0000, 0x1189, 0x2312, 0x329b, 0x4624, 0x57ad, 0x6536, 0x74bf,
	0x8c48, 0x9dc1, 0xaf5a, 0xbed3, 0xca6c, 0xdbe5, 0xe97e, 0xf8f7,
	0x1081, 0x0108, 0x3393, 0x221a, 0x56a5, 0x472c, 0x75b7, 0x643e,
	0x9cc9, 0x8d40, 0xbfdb, 0xae52, 0xdaed, 0xcb64, 0xf9ff, 0xe876,
	0x2102, 0x308b, 0x0210, 0x1399, 0x6726, 0x76af, 0x4434, 0x55bd,
	0xad4a, 0xbcc3, 0x8e58, 0x9fd1, 0xeb6e, 0xfae7, 0xc87c, 0xd9f5,
	0x3183, 0x200a, 0x1291, 0x0318, 0x77a7, 0x662e, 0x54b5, 0x453c,
	0xbdcb, 0xac42, 0x9ed9, 0x8f50, 0xfbef, 0xea66, 0xd8fd, 0xc974,
	0x4204, 0x538d, 0x6116, 0x709f, 0x0420, 0x15a9, 0x2732, 0x36bb,
	0xce4c, 0xdfc5, 0xed5e, 0xfcd7, 0x8868, 0x99e1, 0xab7a, 0xbaf3,
	0x5285, 0x430c, 0x7197, 0x601e, 0x14a1, 0x0528, 0x37b3, 0x263a,
	0xdecd, 0xcf44, 0xfddf, 0xec56, 0x98e9, 0x8960, 0xbbfb, 0xaa72,
	0x6306, 0x728f, 0x4014, 0x519d, 0x2522, 0x34ab, 0x0630, 0x17b9,
	0xef4e, 0xfec7, 0xcc5c, 0xddd5, 0xa96a, 0xb8e3, 0x8a78, 0x9bf1,
	0x7387, 0x620e, 0x5095, 0x411c, 0x35a3, 0x242a, 0x16b1, 0x0738,
	0xffcf, 0xee46, 0xdcdd, 0xcd54, 0xb9eb, 0xa862, 0x9af9, 0x8b70,
	0x8408, 0x9581, 0xa71a, 0xb693, 0xc22c, 0xd3a5, 0xe13e, 0xf0b7,
	0x0840, 0x19c9, 0x2b52, 0x3adb, 0x4e64, 0x5fed, 0x6d76, 0x7cff,
	0x9489, 0x8500, 0xb79b, 0xa612, 0xd2ad, 0xc324, 0xf1bf, 0xe036,
	0x18c1, 0x0948, 0x3bd3, 0x2a5a, 0x5ee5, 0x4f6c, 0x7df7, 0x6c7e,
	0xa50a, 0xb483, 0x8618, 0x9791, 0xe32e, 0xf2a7, 0xc03c, 0xd1b5,
	0x2942, 0x38cb, 0x0a50, 0x1bd9, 0x6f66, 0x7eef, 0x4c74, 0x5dfd,
	0xb58b, 0xa402, 0x9699, 0x8710, 0xf3af, 0xe226, 0xd0bd, 0xc134,
	0x39c3, 0x284a, 0x1ad1, 0x0b58, 0x7fe7, 0x6e6e, 0x5cf5, 0x4d7c,
	0xc60c, 0xd785, 0xe51e, 0xf497, 0x8028, 0x91a1, 0xa33a, 0xb2b3,
	0x4a44, 0x5bcd, 0x6956, 0x78df, 0x0c60, 0x1de9, 0x2f72, 0x3efb,
	0xd68d, 0xc704, 0xf59f, 0xe416, 0x90a9, 0x8120, 0xb3bb, 0xa232,
	0x5ac5, 0x4b4c, 0x79d7, 0x685e, 0x1ce1, 0x0d68, 0x3ff3, 0x2e7a,
	0xe70e, 0xf687, 0xc41c, 0xd595, 0xa12a, 0xb0a3, 0x8238, 0x93b1,
	0x6b46, 0x7acf, 0x4854, 0x59dd, 0x2d62, 0x3ceb, 0x0e70, 0x1ff9,
	0xf78f, 0xe606, 0xd49d, 0xc514, 0xb1ab, 0xa022, 0x92b9, 0x8330,
	0x7bc7, 0x6a4e, 0x58d5, 0x495c, 0x3de3, 0x2c6a, 0x1ef1, 0x0f78,
}

// crcCCITT16 computes the S101 CRC using the reflected CCITT algorithm.
// Polynomial 0x8408, initial value 0xFFFF, result inverted (~crc).
func crcCCITT16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc = (crc >> 8) ^ crcTable[(crc^uint16(b))&0xFF]
	}
	return ^crc & 0xFFFF
}

// NewEmBERFrame creates an S101 frame carrying a Glow BER payload.
func NewEmBERFrame(payload []byte) *Frame {
	return &Frame{
		Slot:    SlotDefault,
		MsgType: MsgEmBER,
		Command: CmdEmBER,
		Version: VersionS101,
		Flags:   FlagSingle,
		DTD:     DTDGlow,
		Payload: payload,
	}
}

// NewKeepAliveRequest creates an S101 keep-alive request frame.
// Uses same msgType as EmBER (0x0E) with command 0x01.
func NewKeepAliveRequest() *Frame {
	return &Frame{
		Slot:    SlotDefault,
		MsgType: MsgEmBER,
		Command: CmdKeepAliveReq,
		Version: VersionS101,
	}
}

// NewKeepAliveResponse creates an S101 keep-alive response frame.
// Uses same msgType as EmBER (0x0E) with command 0x02.
func NewKeepAliveResponse() *Frame {
	return &Frame{
		Slot:    SlotDefault,
		MsgType: MsgEmBER,
		Command: CmdKeepAliveResp,
		Version: VersionS101,
	}
}

// IsKeepAlive returns true if this frame is a keep-alive message.
func (f *Frame) IsKeepAlive() bool {
	return f.Command == CmdKeepAliveReq || f.Command == CmdKeepAliveResp
}

// IsEmBER returns true if this frame carries a Glow BER payload.
func (f *Frame) IsEmBER() bool {
	return f.MsgType == MsgEmBER
}
