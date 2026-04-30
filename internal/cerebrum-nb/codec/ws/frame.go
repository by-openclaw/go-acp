package ws

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// RFC 6455 §5.2 opcodes.
const (
	OpContinuation byte = 0x0
	OpText         byte = 0x1
	OpBinary       byte = 0x2
	OpClose        byte = 0x8
	OpPing         byte = 0x9
	OpPong         byte = 0xA
)

// IsControl reports whether op is a control opcode (0x8..0xF).
func IsControl(op byte) bool { return op&0x8 != 0 }

// frame is one decoded RFC 6455 frame. payload is unmasked.
type frame struct {
	fin     bool
	opcode  byte
	masked  bool
	payload []byte
}

// readFrame reads one full frame off r. Returns io.EOF if r is exhausted
// at a frame boundary; any partial read is wrapped in an io.ErrUnexpectedEOF.
//
// Frame layout (RFC 6455 §5.2):
//
//	| Bits | Field          |
//	|------|----------------|
//	| 1    | FIN            |
//	| 3    | RSV1..RSV3 (must be 0) |
//	| 4    | opcode         |
//	| 1    | MASK           |
//	| 7    | payload len (0..125 = direct, 126 = u16, 127 = u64) |
//	| 16/64| extended length (if applicable) |
//	| 32   | masking key (if MASK = 1) |
//	| N    | payload (XOR-unmasked if MASK = 1) |
func readFrame(r io.Reader, maxPayload int64) (*frame, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	b0, b1 := hdr[0], hdr[1]

	if b0&0x70 != 0 {
		return nil, fmt.Errorf("ws: reserved bits set: %#x", b0)
	}
	f := &frame{
		fin:    b0&0x80 != 0,
		opcode: b0 & 0x0f,
		masked: b1&0x80 != 0,
	}

	plen := int64(b1 & 0x7f)
	switch plen {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, err
		}
		plen = int64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return nil, err
		}
		v := binary.BigEndian.Uint64(ext[:])
		if v > 1<<63-1 {
			return nil, errors.New("ws: 64-bit length high bit set")
		}
		plen = int64(v)
	}
	if plen < 0 {
		return nil, errors.New("ws: negative payload length")
	}
	if maxPayload > 0 && plen > maxPayload {
		return nil, fmt.Errorf("ws: frame payload %d exceeds cap %d", plen, maxPayload)
	}
	// Control frames must be <=125 and FIN=1 (RFC 6455 §5.5).
	if IsControl(f.opcode) {
		if plen > 125 {
			return nil, fmt.Errorf("ws: control frame too large (%d)", plen)
		}
		if !f.fin {
			return nil, errors.New("ws: fragmented control frame")
		}
	}

	var maskKey [4]byte
	if f.masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return nil, err
		}
	}

	if plen == 0 {
		f.payload = []byte{}
		return f, nil
	}
	f.payload = make([]byte, plen)
	if _, err := io.ReadFull(r, f.payload); err != nil {
		return nil, err
	}
	if f.masked {
		applyMask(f.payload, maskKey)
	}
	return f, nil
}

// writeFrame emits one frame. fin must be true for the final fragment;
// for client-side dhs always-single-frame TX, callers pass fin=true.
// maskKey of zeros means "no mask" (server frames); anything non-zero
// is XORed onto the payload.
func writeFrame(w io.Writer, fin bool, opcode byte, payload []byte, maskKey [4]byte, masked bool) error {
	var b0 byte
	if fin {
		b0 |= 0x80
	}
	b0 |= opcode & 0x0f

	hdr := []byte{b0}
	plen := len(payload)

	var b1 byte
	if masked {
		b1 |= 0x80
	}

	switch {
	case plen <= 125:
		b1 |= byte(plen)
		hdr = append(hdr, b1)
	case plen <= 0xffff:
		b1 |= 126
		hdr = append(hdr, b1, 0, 0)
		binary.BigEndian.PutUint16(hdr[len(hdr)-2:], uint16(plen))
	default:
		b1 |= 127
		hdr = append(hdr, b1)
		ext := make([]byte, 8)
		binary.BigEndian.PutUint64(ext, uint64(plen))
		hdr = append(hdr, ext...)
	}
	if masked {
		hdr = append(hdr, maskKey[:]...)
	}

	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if plen == 0 {
		return nil
	}
	if !masked {
		_, err := w.Write(payload)
		return err
	}
	// Mask onto a copy so we don't mutate the caller's buffer.
	out := make([]byte, plen)
	copy(out, payload)
	applyMask(out, maskKey)
	_, err := w.Write(out)
	return err
}

// applyMask XORs key cyclically onto buf. RFC 6455 §5.3.
func applyMask(buf []byte, key [4]byte) {
	for i := range buf {
		buf[i] ^= key[i&3]
	}
}
