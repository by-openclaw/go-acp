package s101

import (
	"fmt"
	"io"
)

// Writer writes S101 frames to a TCP stream.
type Writer struct {
	w io.Writer
}

// NewWriter creates an S101 stream writer.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
}

// WriteFrame encodes and writes an S101 frame to the stream.
func (w *Writer) WriteFrame(f *Frame) error {
	data := Encode(f)
	_, err := w.w.Write(data)
	if err != nil {
		return fmt.Errorf("s101 write: %w", err)
	}
	return nil
}
