package s101

import (
	"bufio"
	"fmt"
	"io"
)

// Reader reads S101 frames from a TCP stream. It scans for BOF markers
// and collects bytes until EOF marker, then decodes the frame.
type Reader struct {
	r   *bufio.Reader
	tap func([]byte) // optional; receives every frame exactly as read off the wire
}

// NewReader creates an S101 stream reader.
func NewReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReaderSize(r, 65536)}
}

// SetTap installs a callback that receives the raw bytes of every
// incoming frame (pre-Decode, including BOF/EOF/CRC). Use it for
// traffic capture.
func (r *Reader) SetTap(fn func([]byte)) {
	r.tap = fn
}

// ReadFrame reads the next complete S101 frame from the stream.
// Blocks until a complete BOF...EOF sequence is received.
func (r *Reader) ReadFrame() (*Frame, error) {
	// Scan for BOF.
	for {
		b, err := r.r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("s101 read: %w", err)
		}
		if b == BOF {
			break
		}
		// Discard bytes outside frames.
	}

	// Collect bytes until EOF.
	var buf []byte
	buf = append(buf, BOF)
	for {
		b, err := r.r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("s101 read: %w", err)
		}
		buf = append(buf, b)
		if b == EOF {
			break
		}
		if len(buf) > 65536 {
			return nil, fmt.Errorf("s101: frame too large (%d bytes)", len(buf))
		}
	}

	if r.tap != nil {
		r.tap(buf)
	}
	f, err := Decode(buf)
	if err != nil {
		// Include frame hex for debugging.
		if len(buf) <= 64 {
			return nil, fmt.Errorf("%w (hex: %x)", err, buf)
		}
		return nil, fmt.Errorf("%w (len=%d, first32: %x)", err, len(buf), buf[:32])
	}
	return f, nil
}
