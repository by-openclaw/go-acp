package codec

import (
	"errors"
	"io"
)

// Frame is one logical SW-P-02 command as it rides the wire. ID is the
// COMMAND byte; Payload is the MESSAGE bytes (may be empty).
type Frame struct {
	ID      CommandID
	Payload []byte
}

// Errors surfaced by the framer. Users distinguish them with errors.Is.
var (
	// ErrBadSOM is returned when the first byte is not 0xFF.
	ErrBadSOM = errors.New("probel-sw02p: bad start-of-message (expected 0xFF)")

	// ErrBadChecksum is returned when the trailing checksum byte does
	// not match the recomputed 7-bit two's-complement sum of
	// COMMAND + MESSAGE.
	ErrBadChecksum = errors.New("probel-sw02p: checksum mismatch")
)

// checksum7 returns the 7-bit two's-complement sum of b with the MSB
// forced to 0 (§3.1: "CHECKSUM = two's-complement sum … MSB = 0").
//
// | Input            | Formula                  | Result (hex) |
// |------------------|--------------------------|--------------|
// | {}               | (-0)   & 0x7F            | 0x00         |
// | {0x01}           | (-1)   & 0x7F            | 0x7F         |
// | {0x01,0x02,0x03} | (-6)   & 0x7F            | 0x7A         |
//
// Note the MSB clamp: a raw two's-complement byte can be 0x80-0xFF, but
// §3.1 asks for "MSB = 0", so we mask the result down to 7 bits.
func checksum7(b []byte) byte {
	var s byte
	for _, x := range b {
		s += x
	}
	return byte(-s) & 0x7F
}

// EncodeFrame wraps command + payload into one SW-P-02 frame per §3.1:
//
//	SOM  cmd  payload...  checksum
//
// Checksum is checksum7(cmd || payload...). Returns the complete wire
// bytes ready to write to the socket.
func EncodeFrame(cmd byte, payload []byte) []byte {
	out := make([]byte, 0, 1+1+len(payload)+1)
	out = append(out, SOM)
	out = append(out, cmd)
	out = append(out, payload...)

	sumInput := make([]byte, 0, 1+len(payload))
	sumInput = append(sumInput, cmd)
	sumInput = append(sumInput, payload...)
	out = append(out, checksum7(sumInput))
	return out
}

// DecodeFrame reads one SOM-prefixed frame from buf and returns the
// command byte, payload bytes, total bytes consumed, and a decode error.
//
// The caller is responsible for knowing how many bytes the command
// carries — SW-P-02 has no in-frame length field. Per-command decoders
// validate the MESSAGE length and treat any trailing bytes as the next
// frame's start.
//
// Signatures are indicative:
//   - (cmd, payload, consumed, nil) — well-formed frame; consumed ==
//     1 + 1 + len(payload) + 1.
//   - (0, nil, 0, io.ErrUnexpectedEOF) — buf too short to hold SOM +
//     command + checksum. Caller should accumulate more bytes.
//   - (0, nil, 0, ErrBadSOM) — leading byte is not 0xFF.
//   - (0, nil, 0, ErrBadChecksum) — trailing byte did not match.
//
// Because SW-P-02 has no framing length, this scaffold decoder assumes
// the caller supplies buf = SOM + cmd + payload + checksum with no
// extra bytes. Per-command files in follow-up commits will add length-
// aware stream decoders on top of this primitive.
func DecodeFrame(buf []byte) (cmd byte, payload []byte, consumed int, err error) {
	if len(buf) < 3 {
		return 0, nil, 0, io.ErrUnexpectedEOF
	}
	if buf[0] != SOM {
		return 0, nil, 0, ErrBadSOM
	}
	cmd = buf[1]
	// Everything between command byte and the final byte is the
	// payload; the final byte is the checksum.
	payload = append([]byte(nil), buf[2:len(buf)-1]...)
	chk := buf[len(buf)-1]

	sumInput := make([]byte, 0, 1+len(payload))
	sumInput = append(sumInput, cmd)
	sumInput = append(sumInput, payload...)
	if checksum7(sumInput) != chk {
		return 0, nil, 0, ErrBadChecksum
	}
	return cmd, payload, len(buf), nil
}

// Pack returns the wire bytes for Frame f. Convenience wrapper around
// EncodeFrame that matches the SW-P-08 codec's naming so consumer /
// provider code can treat both codecs symmetrically.
func Pack(f Frame) []byte { return EncodeFrame(byte(f.ID), f.Payload) }

// Unpack parses one complete SW-P-02 frame from the start of buf. The
// decoder is length-aware: it reads the command byte, looks the
// expected MESSAGE length up via PayloadLen, then consumes exactly
// `SOM + cmd + payload + checksum` bytes. Returns the Frame plus the
// byte count consumed so the caller can slice buf = buf[n:] and scan
// the next frame.
//
// Error paths:
//
//   - (Frame{}, 0, io.ErrUnexpectedEOF) — buf lacks the 3-byte minimum
//     OR the full frame has not arrived yet. Caller accumulates more
//     bytes and retries.
//   - (Frame{}, 0, ErrBadSOM) — leading byte is not 0xFF. Session
//     code should drop one byte and resync.
//   - (Frame{}, 0, ErrUnknownCommand) — command byte not in the
//     registry. Caller treats as a decode error (no way to peel the
//     MESSAGE without knowing its length).
//   - (Frame{}, 0, ErrBadChecksum) — checksum mismatch. Caller drops
//     the frame and resyncs.
func Unpack(buf []byte) (Frame, int, error) {
	if len(buf) < 3 {
		return Frame{}, 0, io.ErrUnexpectedEOF
	}
	if buf[0] != SOM {
		return Frame{}, 0, ErrBadSOM
	}
	id := CommandID(buf[1])
	plen, ok := PayloadLen(id)
	if !ok {
		return Frame{}, 0, ErrUnknownCommand
	}
	want := 1 + 1 + plen + 1 // SOM + cmd + payload + checksum
	if len(buf) < want {
		return Frame{}, 0, io.ErrUnexpectedEOF
	}
	payload := append([]byte(nil), buf[2:2+plen]...)
	chk := buf[2+plen]

	sumInput := make([]byte, 0, 1+plen)
	sumInput = append(sumInput, byte(id))
	sumInput = append(sumInput, payload...)
	if checksum7(sumInput) != chk {
		return Frame{}, 0, ErrBadChecksum
	}
	return Frame{ID: id, Payload: payload}, want, nil
}

// HexDump formats bytes as space-separated 2-digit lowercase hex — the
// format SW-P-02 spec examples use and the convention most byte-view
// tools recognise.
func HexDump(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 0, len(b)*3-1)
	for i, x := range b {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, hex[x>>4], hex[x&0x0F])
	}
	return string(out)
}
