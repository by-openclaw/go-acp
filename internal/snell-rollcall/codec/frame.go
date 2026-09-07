package codec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Wire sizes and limits.
const (
	// TxHeaderSize is the transmission header that precedes every RollCall
	// message on TCP (spec 10.2.1).
	TxHeaderSize = 4

	// MsgHeaderSize is MESSAGE_STR: two addresses and a length (spec 11.1.2).
	MsgHeaderSize = 14

	// RollHeaderSize is ROLLHEADER_STR: type and flags (spec 11.2.1).
	RollHeaderSize = 2

	// HeaderSize is everything before the payload.
	HeaderSize = TxHeaderSize + MsgHeaderSize + RollHeaderSize

	// MaxPayload is the largest payload the vendor library will queue
	// (MAX_QUEUED_DATA_SIZE, CoreServiceAPI.h). We encode within it.
	MaxPayload = 420

	// MaxFrame is the largest frame we will encode: 440 bytes.
	MaxFrame = HeaderSize + MaxPayload

	// SpecMaxTxLength is the largest transmission-header length the
	// specification permits (10.2.1: "within the range 1 to 1570"). We
	// decode up to this so a spec-legal peer is never rejected, while
	// encoding stays within MaxPayload.
	SpecMaxTxLength = 1570

	// MaxDecodePayload is the largest payload we will accept when decoding.
	MaxDecodePayload = SpecMaxTxLength - MsgHeaderSize - RollHeaderSize

	// MaxDecodeFrame is the largest complete frame we will accept when
	// decoding: the transmission header plus the largest length it may
	// declare. The Reader's buffer must hold one of these, or a spec-legal
	// peer could send a frame we can never assemble.
	MaxDecodeFrame = TxHeaderSize + SpecMaxTxLength
)

// TxFlagsMode3 is the only transmission-header flag value still in use. Modes
// 1 and 2 are historical and are not produced by anything shipping. The vendor
// header spells its macro TXHDR_COMPATABILITY_MODE3, misspelling included, so
// that name is searchable against RC3IP.H.
//
//nolint:misspell // vendor macro name quoted verbatim
const TxFlagsMode3 uint16 = 12

// Packet flags (spec 3.1).
const (
	// FlagBackChannel marks unsolicited data from a server to a client and
	// the replies to it.
	FlagBackChannel uint8 = 0x80

	// FlagWideArea asks bridges to forward a broadcast onto other segments.
	FlagWideArea uint8 = 0x40
)

// Frame is one decoded RollCall message.
//
// Payload aliases the caller's buffer after Decode; copy it if it must outlive
// the read. Encode never retains it.
type Frame struct {
	Dst     Address
	Src     Address
	Type    PacketType
	Flags   uint8
	Payload []byte
}

// BackChannel reports whether the frame travelled on the back channel.
func (f Frame) BackChannel() bool { return f.Flags&FlagBackChannel != 0 }

// WideArea reports whether the frame is a broadcast bridges should forward.
func (f Frame) WideArea() bool { return f.Flags&FlagWideArea != 0 }

// Size returns the number of bytes the frame occupies on the wire.
func (f Frame) Size() int { return HeaderSize + len(f.Payload) }

// String renders the frame the way the vendor's Comms Window does: direction
// is the caller's business, so this covers everything else. Used by the
// decoded trace and by test failures.
func (f Frame) String() string {
	return fmt.Sprintf("%s => %s %s flags=%s len=%d",
		f.Src, f.Dst, f.Type, flagString(f.Flags), len(f.Payload))
}

func flagString(fl uint8) string {
	switch {
	case fl&FlagBackChannel != 0 && fl&FlagWideArea != 0:
		return "BW"
	case fl&FlagBackChannel != 0:
		return "B"
	case fl&FlagWideArea != 0:
		return "W"
	default:
		return "-"
	}
}

// AppendTo appends the complete wire form of the frame to dst.
func (f Frame) AppendTo(dst []byte) ([]byte, error) {
	if len(f.Payload) > MaxPayload {
		return nil, fmt.Errorf("%w: %d > %d", ErrPayloadTooLong, len(f.Payload), MaxPayload)
	}

	rLength := RollHeaderSize + len(f.Payload)

	var tx [TxHeaderSize]byte
	binary.BigEndian.PutUint16(tx[0:2], TxFlagsMode3)
	binary.BigEndian.PutUint16(tx[2:4], uint16(MsgHeaderSize+rLength))
	dst = append(dst, tx[:]...)

	dst = f.Dst.AppendTo(dst)
	dst = f.Src.AppendTo(dst)

	var tail [RollHeaderSize + 2]byte
	binary.BigEndian.PutUint16(tail[0:2], uint16(rLength))
	tail[2] = byte(f.Type)
	tail[3] = f.Flags
	dst = append(dst, tail[:]...)

	return append(dst, f.Payload...), nil
}

// Encode returns the complete wire form of the frame.
func (f Frame) Encode() ([]byte, error) {
	return f.AppendTo(make([]byte, 0, f.Size()))
}

// DecodeFrame decodes one complete frame from the front of b and reports how
// many bytes it consumed.
//
// It validates the two independent length fields against each other. The
// vendor library drops a frame whose lengths disagree without reporting it
// (IPShClient.c); we return ErrLengthMismatch so the caller can raise a
// compliance event instead of losing traffic silently.
func DecodeFrame(b []byte) (Frame, int, error) {
	if err := need(b, TxHeaderSize, "TxHeader", ""); err != nil {
		return Frame{}, 0, err
	}

	flags := binary.BigEndian.Uint16(b[0:2])
	if flags != TxFlagsMode3 {
		return Frame{}, 0, fmt.Errorf("%w: 0x%04X, want 0x%04X", ErrBadTxFlags, flags, TxFlagsMode3)
	}

	txLen := int(binary.BigEndian.Uint16(b[2:4]))
	if txLen < MsgHeaderSize+RollHeaderSize || txLen > SpecMaxTxLength {
		return Frame{}, 0, fmt.Errorf("%w: %d", ErrBadTxLength, txLen)
	}

	total := TxHeaderSize + txLen
	if len(b) < total {
		return Frame{}, 0, decodeErr("Frame", "", len(b),
			fmt.Errorf("%w: need %d bytes, have %d", ErrShortBuffer, total, len(b)))
	}

	body := b[TxHeaderSize:total]

	// txLen was checked against MsgHeaderSize+RollHeaderSize above, so the
	// two addresses and the length word are present; and once rLength is
	// known to agree with txLen it is necessarily at least RollHeaderSize.
	rLength := int(binary.BigEndian.Uint16(body[12:14]))
	if rLength+MsgHeaderSize != txLen {
		return Frame{}, 0, fmt.Errorf("%w: rLength %d + %d != txLength %d",
			ErrLengthMismatch, rLength, MsgHeaderSize, txLen)
	}

	return Frame{
		Dst:     addressAt(body[0:6]),
		Src:     addressAt(body[6:12]),
		Type:    PacketType(body[14]),
		Flags:   body[15],
		Payload: body[16:],
	}, total, nil
}

// Reader decodes frames from a byte stream.
//
// On a malformed transmission header it resynchronises by discarding a single
// byte and retrying. The vendor library discards four at a time, which can
// never re-align a stream that slipped by an odd number of bytes; one byte
// always can. Resyncs are counted so a caller can raise a compliance event.
type Reader struct {
	r    io.Reader
	buf  []byte
	n    int // bytes buffered
	off  int // read offset into buf
	sync uint64
}

// NewReader wraps r.
//
// The buffer holds two maximum-size frames. It is sized from MaxDecodeFrame
// rather than from MaxFrame, because what we may receive is bounded by the
// specification's limit and not by the smaller one we encode within: a peer
// that sends a legal 1570-byte message must still be readable.
func NewReader(r io.Reader) *Reader {
	return newReaderSize(r, 2*MaxDecodeFrame)
}

// newReaderSize wraps r with an explicit buffer size. It exists so the
// buffer-full guard in fill can be exercised; NewReader always supplies a
// buffer large enough that the guard cannot fire.
func newReaderSize(r io.Reader, size int) *Reader {
	if size < HeaderSize {
		size = HeaderSize
	}
	return &Reader{r: r, buf: make([]byte, 0, size)}
}

// Resyncs reports how many bytes have been discarded to regain framing.
func (r *Reader) Resyncs() uint64 { return r.sync }

// ReadFrame returns the next frame.
//
// The returned Payload aliases the reader's buffer and is only valid until the
// next call; copy it if it must outlive that.
func (r *Reader) ReadFrame() (Frame, error) {
	for {
		if r.n-r.off > 0 {
			f, used, err := DecodeFrame(r.buf[r.off:r.n])
			switch {
			case err == nil:
				r.off += used
				return f, nil
			case errors.Is(err, ErrShortBuffer):
				// Need more bytes; fall through to fill.
			default:
				// Any other decode failure means framing is lost:
				// drop one byte and try again.
				r.off++
				r.sync++
				continue
			}
		}
		if err := r.fill(); err != nil {
			return Frame{}, err
		}
	}
}

// fill compacts the buffer and reads at least one more byte.
func (r *Reader) fill() error {
	if r.off > 0 {
		copy(r.buf[:cap(r.buf)], r.buf[r.off:r.n])
		r.n -= r.off
		r.off = 0
	}
	if r.n == cap(r.buf) {
		// Unreachable through NewReader, whose buffer holds more than the
		// largest frame DecodeFrame will accept. Kept as a guard so an
		// undersized buffer fails loudly instead of spinning.
		return fmt.Errorf("%w: buffer full at %d bytes", ErrBadTxLength, r.n)
	}

	buf := r.buf[:cap(r.buf)]
	n, err := r.r.Read(buf[r.n:])
	r.n += n
	if n > 0 {
		r.buf = buf[:r.n]
		return nil
	}
	if err == nil {
		err = io.ErrNoProgress
	}
	return err
}
