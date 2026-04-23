package s101

import (
	"fmt"
	"io"
)

// Writer writes S101 frames to a TCP stream.
type Writer struct {
	w   io.Writer
	tap func([]byte) // optional; receives every frame exactly as it goes on the wire
}

// NewWriter creates an S101 stream writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// SetTap installs a callback that receives the raw bytes of every
// outgoing frame (post-Encode, including BOF/EOF/CRC). Use it for
// traffic capture, not for transformation — the bytes are already
// being written when the tap fires.
func (w *Writer) SetTap(fn func([]byte)) {
	w.tap = fn
}

// WriteFrame encodes and writes an S101 frame to the stream.
//
// Produces the wire shape of frame.Encode:
//
//	| Offset | Field       | Width | Notes                                  |
//	|--------|-------------|-------|----------------------------------------|
//	|   0    | BOF         |   1   | 0xFE                                   |
//	|   1..  | header+data |   N   | slot+msgType+cmd+ver (+flags+DTD+body) |
//	|  N+1   | CRC16       |   2   | little-endian CCITT                    |
//	|  N+3   | EOF         |   1   | 0xFF                                   |
//
// Spec reference: Ember+ Documentation.pdf §S101 Framing p. 94.
func (w *Writer) WriteFrame(f *Frame) error {
	data := Encode(f)
	if w.tap != nil {
		w.tap(data)
	}
	_, err := w.w.Write(data)
	if err != nil {
		return fmt.Errorf("s101 write: %w", err)
	}
	return nil
}
