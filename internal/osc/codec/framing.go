package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Framing helpers for OSC over stream transports (TCP, serial, etc).
//
// Two distinct framings are used:
//
//  1. Length-prefix (OSC 1.0 over TCP) — int32 big-endian size + packet.
//     Simple, efficient, doesn't require byte stuffing.
//
//  2. SLIP double-END (OSC 1.1 over TCP / serial) — per RFC 1055 with
//     OSC 1.1's requirement that END appear both BEFORE and AFTER each
//     packet. Byte stuffing escapes any END or ESC bytes inside the
//     packet body.

// -----------------------------------------------------------------------------
// Length-prefix framer (OSC 1.0)
// -----------------------------------------------------------------------------

var (
	ErrLenPrefixTooLarge = errors.New("osc len-prefix: packet size exceeds max")
	ErrLenPrefixNegative = errors.New("osc len-prefix: negative size")
)

// EncodeLenPrefix returns `<int32 BE size> || packet`.
func EncodeLenPrefix(packet []byte) []byte {
	out := make([]byte, 4+len(packet))
	binary.BigEndian.PutUint32(out[:4], uint32(len(packet)))
	copy(out[4:], packet)
	return out
}

// LenPrefixReader parses back-to-back length-prefixed packets from a
// stream. Each ReadPacket returns one complete packet or io.EOF /
// io.ErrUnexpectedEOF.
type LenPrefixReader struct {
	r         io.Reader
	maxPacket int
}

// NewLenPrefixReader returns a reader with the given max packet size
// (pass 0 to accept any; reasonable cap is a few MB to guard against
// malformed streams).
func NewLenPrefixReader(r io.Reader, maxPacket int) *LenPrefixReader {
	return &LenPrefixReader{r: r, maxPacket: maxPacket}
}

// ReadPacket returns the next packet. Returns io.EOF cleanly when the
// stream ends between packets; io.ErrUnexpectedEOF if it ends
// mid-packet.
func (lp *LenPrefixReader) ReadPacket() ([]byte, error) {
	var hdr [4]byte
	n, err := io.ReadFull(lp.r, hdr[:])
	if err != nil {
		if n == 0 && err == io.EOF {
			return nil, io.EOF
		}
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	size := int32(binary.BigEndian.Uint32(hdr[:]))
	if size < 0 {
		return nil, fmt.Errorf("%w: %d", ErrLenPrefixNegative, size)
	}
	if lp.maxPacket > 0 && int(size) > lp.maxPacket {
		return nil, fmt.Errorf("%w: %d > %d", ErrLenPrefixTooLarge, size, lp.maxPacket)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(lp.r, buf); err != nil {
		if err == io.ErrUnexpectedEOF || err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf, nil
}

// -----------------------------------------------------------------------------
// SLIP framer (OSC 1.1, RFC 1055 with double-END per OSC 1.1)
// -----------------------------------------------------------------------------

const (
	SLIPEnd     = 0xC0
	SLIPEsc     = 0xDB
	SLIPEscEnd  = 0xDC // replaces an END byte in the body
	SLIPEscEsc  = 0xDD // replaces an ESC byte in the body
)

var (
	ErrSLIPTruncated = errors.New("osc slip: stream ended mid-frame")
	ErrSLIPBadEscape = errors.New("osc slip: ESC not followed by ESC_END or ESC_ESC")
)

// EncodeSLIP wraps a packet in SLIP double-END framing per OSC 1.1:
// END || stuffed-body || END. Any END or ESC byte in the body is
// escape-stuffed.
func EncodeSLIP(packet []byte) []byte {
	out := make([]byte, 0, len(packet)+4)
	out = append(out, SLIPEnd) // leading END per 1.1
	for _, b := range packet {
		switch b {
		case SLIPEnd:
			out = append(out, SLIPEsc, SLIPEscEnd)
		case SLIPEsc:
			out = append(out, SLIPEsc, SLIPEscEsc)
		default:
			out = append(out, b)
		}
	}
	out = append(out, SLIPEnd) // trailing END
	return out
}

// SLIPReader statefully de-frames SLIP packets from a stream. Tolerates
// the 1.1 double-END convention by skipping a leading END before the
// first packet and between packets.
type SLIPReader struct {
	r         io.Reader
	buf       []byte
	rpos      int
	maxPacket int
}

func NewSLIPReader(r io.Reader, maxPacket int) *SLIPReader {
	return &SLIPReader{r: r, maxPacket: maxPacket}
}

func (s *SLIPReader) ensureByte() error {
	if s.rpos < len(s.buf) {
		return nil
	}
	tmp := make([]byte, 512)
	n, err := s.r.Read(tmp)
	if n > 0 {
		s.buf = append(s.buf, tmp[:n]...)
	}
	if err != nil {
		if err == io.EOF && s.rpos == len(s.buf) {
			return io.EOF
		}
		return err
	}
	return nil
}

func (s *SLIPReader) readByte() (byte, error) {
	if err := s.ensureByte(); err != nil {
		return 0, err
	}
	b := s.buf[s.rpos]
	s.rpos++
	return b, nil
}

func (s *SLIPReader) readByteStrict() (byte, error) {
	b, err := s.readByte()
	if err == io.EOF {
		return 0, io.ErrUnexpectedEOF
	}
	return b, err
}

// ReadPacket returns the next fully-unescaped SLIP packet. Returns
// io.EOF cleanly between packets; io.ErrUnexpectedEOF if the stream
// ends mid-packet.
func (s *SLIPReader) ReadPacket() ([]byte, error) {
	// Consume any leading END(s) between packets — OSC 1.1 doubles END.
	var b byte
	var err error
	for {
		b, err = s.readByte()
		if err != nil {
			return nil, err
		}
		if b != SLIPEnd {
			break
		}
	}

	out := []byte{}
	// First non-END byte is body.
	for {
		switch b {
		case SLIPEnd:
			// End of packet.
			return out, nil
		case SLIPEsc:
			nxt, err := s.readByteStrict()
			if err != nil {
				return nil, err
			}
			switch nxt {
			case SLIPEscEnd:
				out = append(out, SLIPEnd)
			case SLIPEscEsc:
				out = append(out, SLIPEsc)
			default:
				return nil, fmt.Errorf("%w: got 0x%02x", ErrSLIPBadEscape, nxt)
			}
		default:
			out = append(out, b)
		}
		if s.maxPacket > 0 && len(out) > s.maxPacket {
			return nil, fmt.Errorf("osc slip: packet > max %d", s.maxPacket)
		}
		b, err = s.readByteStrict()
		if err != nil {
			return nil, err
		}
	}
}
