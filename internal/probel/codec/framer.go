package codec

import (
	"errors"
	"io"
)

// Frame is one logical SW-P-08 command as it rides the wire, post-decode
// (i.e. after DLE-unstuffing). ID is DATA[0]; Payload is DATA[1:].
type Frame struct {
	ID      CommandID
	Payload []byte // zero or more bytes following the command byte
}

// Errors surfaced by the framer. Users distinguish them with errors.Is.
var (
	ErrBadSOM      = errors.New("probel: bad start-of-message (expected DLE STX)")
	ErrTruncated   = errors.New("probel: truncated frame (no DLE ETX seen)")
	ErrBadBTC      = errors.New("probel: byte-count mismatch")
	ErrBadChecksum = errors.New("probel: checksum mismatch")
	ErrEmptyFrame  = errors.New("probel: frame has no command byte")
)

// Checksum8 returns the 8-bit two's-complement checksum over b, i.e.
// (~sum + 1) & 0xFF. Applied by SW-P-08 over (DATA || BTC) pre-escape.
//
// Reference: TS BufferUtility.calculateChecksum8
// (internal/probel/assets/smh-probelsw08p/src/common/utility/buffer.utility.ts line 152).
func Checksum8(b []byte) byte {
	var s uint32
	for _, x := range b {
		s += uint32(x)
	}
	return byte((^s + 1) & 0xFF)
}

// escapeDLE returns a copy of src with every DLE byte (0x10) doubled.
// Applied to DATA, BTC and CHK during Pack; NOT applied to SOM/EOM markers.
func escapeDLE(src []byte) []byte {
	n := 0
	for _, b := range src {
		if b == DLE {
			n++
		}
	}
	if n == 0 {
		out := make([]byte, len(src))
		copy(out, src)
		return out
	}
	out := make([]byte, 0, len(src)+n)
	for _, b := range src {
		out = append(out, b)
		if b == DLE {
			out = append(out, DLE)
		}
	}
	return out
}

// Pack builds a complete framed SW-P-08 message from an unescaped Frame.
//
// | Section | Bytes                 | Notes                                       |
// |---------|-----------------------|---------------------------------------------|
// | SOM     | DLE STX               | literal, never escaped                      |
// | DATA    | ID + Payload, escaped | DLE (0x10) doubled                          |
// | BTC     | 1 byte, escaped       | byte count of pre-escape DATA               |
// | CHK     | 1 byte, escaped       | 2's-complement of (pre-escape DATA ++ BTC)  |
// | EOM     | DLE ETX               | literal, never escaped                      |
//
// Spec: SW-P-88 §3.4. Reference: TS command.base.ts::packAndBuildDataBuffer.
func Pack(f Frame) []byte {
	data := make([]byte, 1+len(f.Payload))
	data[0] = byte(f.ID)
	copy(data[1:], f.Payload)

	btc := byte(len(data))
	chkIn := make([]byte, 0, len(data)+1)
	chkIn = append(chkIn, data...)
	chkIn = append(chkIn, btc)
	chk := Checksum8(chkIn)

	escData := escapeDLE(data)
	escBTC := escapeDLE([]byte{btc})
	escCHK := escapeDLE([]byte{chk})

	out := make([]byte, 0, 2+len(escData)+len(escBTC)+len(escCHK)+2)
	out = append(out, DLE, STX)
	out = append(out, escData...)
	out = append(out, escBTC...)
	out = append(out, escCHK...)
	out = append(out, DLE, ETX)
	return out
}

// PackACK returns the 2-byte positive-acknowledge sequence DLE ACK (0x10 0x06).
// Controllers and matrices emit this immediately after every correctly-framed
// frame they accept (SW-P-08 §2).
func PackACK() []byte { return []byte{DLE, ACK} }

// PackNAK returns the 2-byte negative-acknowledge sequence DLE NAK (0x10 0x15).
// Peers emit this on bad BTC, bad checksum, or malformed framing.
func PackNAK() []byte { return []byte{DLE, NAK} }

// unescapeDLE reverses escapeDLE: every doubled DLE collapses to a single DLE.
// Assumes no unpaired DLE in src (the framer guarantees this by terminating at
// the DLE ETX boundary before reaching here).
func unescapeDLE(src []byte) []byte {
	out := make([]byte, 0, len(src))
	i := 0
	for i < len(src) {
		if src[i] == DLE && i+1 < len(src) && src[i+1] == DLE {
			out = append(out, DLE)
			i += 2
			continue
		}
		out = append(out, src[i])
		i++
	}
	return out
}

// Unpack parses one complete SW-P-08 frame from buf[start:], verifying SOM,
// BTC, checksum and EOM. Returns the decoded Frame and n, the number of bytes
// consumed from buf. If buf does not begin with DLE STX, ErrBadSOM is returned.
// If EOM is never seen, ErrTruncated.
//
// Callers driving this from a socket stream should accumulate bytes until
// Unpack succeeds; partial buffers return io.ErrUnexpectedEOF.
func Unpack(buf []byte) (Frame, int, error) {
	if len(buf) < 2 {
		return Frame{}, 0, io.ErrUnexpectedEOF
	}
	if buf[0] != DLE || buf[1] != STX {
		return Frame{}, 0, ErrBadSOM
	}

	// Scan for DLE ETX, skipping doubled DLE pairs inside DATA/BTC/CHK.
	eom := -1
	i := 2
	for i < len(buf) {
		if buf[i] != DLE {
			i++
			continue
		}
		if i+1 >= len(buf) {
			return Frame{}, 0, io.ErrUnexpectedEOF
		}
		switch buf[i+1] {
		case DLE:
			i += 2 // escaped data byte
		case ETX:
			eom = i
			i = len(buf) // break
		default:
			return Frame{}, 0, ErrTruncated // stray DLE X
		}
	}
	if eom < 0 {
		return Frame{}, 0, io.ErrUnexpectedEOF
	}

	body := unescapeDLE(buf[2:eom])
	if len(body) < 3 {
		return Frame{}, 0, ErrEmptyFrame // need at least ID + BTC + CHK
	}
	data := body[:len(body)-2]
	btc := body[len(body)-2]
	chk := body[len(body)-1]

	if int(btc) != len(data) {
		return Frame{}, 0, ErrBadBTC
	}

	chkIn := make([]byte, 0, len(data)+1)
	chkIn = append(chkIn, data...)
	chkIn = append(chkIn, btc)
	if Checksum8(chkIn) != chk {
		return Frame{}, 0, ErrBadChecksum
	}

	if len(data) == 0 {
		return Frame{}, 0, ErrEmptyFrame
	}

	f := Frame{
		ID:      CommandID(data[0]),
		Payload: append([]byte(nil), data[1:]...),
	}
	return f, eom + 2, nil // consume through DLE ETX
}

// IsACK reports whether buf begins with the 2-byte positive-acknowledge (DLE ACK).
func IsACK(buf []byte) bool { return len(buf) >= 2 && buf[0] == DLE && buf[1] == ACK }

// IsNAK reports whether buf begins with the 2-byte negative-acknowledge (DLE NAK).
func IsNAK(buf []byte) bool { return len(buf) >= 2 && buf[0] == DLE && buf[1] == NAK }
