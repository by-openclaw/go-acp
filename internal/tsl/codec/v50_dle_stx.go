package codec

import (
	"errors"
	"fmt"
	"io"
)

// V5.0 TCP framing wrapper (§5.0 "Physical Layer").
//
// Packet start is delimited by DLE/STX. Any 0xFE (DLE) inside the packet
// body is byte-stuffed to 0xFE 0xFE. Byte-count fields in the packet
// (e.g. PBC) are NOT affected by the stuffing — stuffing is applied
// after the packet is serialised.
const (
	DLE = 0xFE
	STX = 0x02
)

var (
	ErrDLEStreamTruncated  = errors.New("tsl v5.0 TCP: stream ended mid-frame")
	ErrDLEMissingStart     = errors.New("tsl v5.0 TCP: expected DLE/STX start delimiter")
	ErrDLEMalformedEscape  = errors.New("tsl v5.0 TCP: malformed DLE escape (expected DLE/DLE or DLE/STX start)")
)

// EncodeDLEFrame wraps an already-encoded v5.0 packet with the DLE/STX
// prefix and byte-stuffs any 0xFE bytes inside the body.
func EncodeDLEFrame(packet []byte) []byte {
	out := make([]byte, 0, 2+len(packet)+4)
	out = append(out, DLE, STX)
	for _, b := range packet {
		if b == DLE {
			out = append(out, DLE, DLE)
			continue
		}
		out = append(out, b)
	}
	return out
}

// DLEStreamDecoder statefully reassembles framed packets from a TCP
// byte stream. Each call to ReadFrame returns the next complete packet
// (after DLE/STX start and any DLE/DLE un-stuffing) or io.EOF /
// io.ErrUnexpectedEOF at stream end.
type DLEStreamDecoder struct {
	r          io.Reader
	buf        []byte
	rpos       int
	maxPktSize int
}

// NewDLEStreamDecoder returns a stream decoder reading from r. Packets
// exceeding maxPktSize bytes are rejected to protect against malformed
// streams. Pass 0 to use the spec max of 2048.
func NewDLEStreamDecoder(r io.Reader, maxPktSize int) *DLEStreamDecoder {
	if maxPktSize <= 0 {
		maxPktSize = V50MaxPacketSize
	}
	return &DLEStreamDecoder{r: r, maxPktSize: maxPktSize}
}

// ensureBytes reads from the underlying reader until at least n bytes
// are available in buf[rpos:]. Returns io.ErrUnexpectedEOF if the
// reader closes mid-frame.
func (d *DLEStreamDecoder) ensureBytes(n int) error {
	for len(d.buf)-d.rpos < n {
		tmp := make([]byte, 512)
		k, err := d.r.Read(tmp)
		if k > 0 {
			d.buf = append(d.buf, tmp[:k]...)
		}
		if err != nil {
			if err == io.EOF {
				if len(d.buf)-d.rpos == 0 {
					return io.EOF
				}
				return io.ErrUnexpectedEOF
			}
			return err
		}
	}
	return nil
}

// readByte returns the next raw byte from the stream.
func (d *DLEStreamDecoder) readByte() (byte, error) {
	if err := d.ensureBytes(1); err != nil {
		return 0, err
	}
	b := d.buf[d.rpos]
	d.rpos++
	return b, nil
}

// readByteStrict is like readByte but promotes io.EOF to
// io.ErrUnexpectedEOF — use within a frame body where a stream close is
// never legitimate.
func (d *DLEStreamDecoder) readByteStrict() (byte, error) {
	b, err := d.readByte()
	if err == io.EOF {
		return 0, io.ErrUnexpectedEOF
	}
	return b, err
}

// ReadFrame returns the next complete v5.0 packet (already un-stuffed).
// Returns io.EOF when the stream ends cleanly between frames;
// io.ErrUnexpectedEOF if the stream closes mid-frame.
func (d *DLEStreamDecoder) ReadFrame() ([]byte, error) {
	// Look for DLE/STX. First byte EOF is clean (no frame to read).
	b, err := d.readByte()
	if err != nil {
		return nil, err
	}
	if b != DLE {
		return nil, fmt.Errorf("%w: got 0x%02x", ErrDLEMissingStart, b)
	}
	// From here on, any EOF is unexpected.
	b, err = d.readByteStrict()
	if err != nil {
		return nil, err
	}
	if b != STX {
		return nil, fmt.Errorf("%w: got DLE followed by 0x%02x", ErrDLEMissingStart, b)
	}

	// The body is everything up to the next non-stuffed DLE/STX — but
	// the spec says each packet is self-delimited by PBC. We use PBC to
	// determine the body length after un-stuffing enough bytes.
	//
	// Approach:
	//   1. Read 2 bytes (un-stuffed) → PBC.
	//   2. Total body length = 2 + PBC bytes.
	//   3. Read that many un-stuffed bytes.

	out := make([]byte, 0, 64)
	if err := d.readUnstuffedInto(&out, 2); err != nil {
		return nil, err
	}
	pbc := int(out[0]) | int(out[1])<<8
	if pbc > d.maxPktSize-2 {
		return nil, fmt.Errorf("%w: PBC=%d exceeds max %d", ErrV50PacketTooLarge, pbc, d.maxPktSize)
	}
	if err := d.readUnstuffedInto(&out, pbc); err != nil {
		return nil, err
	}
	return out, nil
}

// readUnstuffedInto appends n un-stuffed bytes to *dst. A 0xFE byte in
// the stream is a stuffed DLE and consumes two stream bytes (0xFE 0xFE)
// to yield one body byte (0xFE). A lone 0xFE followed by anything else
// is a framing error. Mid-frame EOF is promoted to ErrUnexpectedEOF.
func (d *DLEStreamDecoder) readUnstuffedInto(dst *[]byte, n int) error {
	for i := 0; i < n; i++ {
		b, err := d.readByteStrict()
		if err != nil {
			return err
		}
		if b == DLE {
			nxt, err := d.readByteStrict()
			if err != nil {
				return err
			}
			if nxt != DLE {
				return fmt.Errorf("%w: got DLE/0x%02x", ErrDLEMalformedEscape, nxt)
			}
		}
		*dst = append(*dst, b)
	}
	return nil
}
