package acp2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// AN2Frame is one decoded AN2 frame as read from / written to the TCP stream.
type AN2Frame struct {
	Proto   AN2Proto
	Slot    uint8
	MTID    uint8 // AN2 mtid: 0 for data/events, 1-255 for req/reply
	Type    AN2Type
	Payload []byte
}

// Errors returned by AN2 frame decode.
var (
	ErrBadMagic    = errors.New("an2: bad magic (expected 0xC635)")
	ErrFrameTooBig = errors.New("an2: frame payload exceeds max size")
)

// EncodeAN2Frame serialises an AN2 frame into wire bytes.
//
// Wire layout (8 bytes header + payload):
//
//	offset 0-1   magic   u16 BE  0xC635
//	offset 2     proto   u8
//	offset 3     slot    u8
//	offset 4     mtid    u8
//	offset 5     type    u8
//	offset 6-7   dlen    u16 BE  len(payload)
//	offset 8+    payload
func EncodeAN2Frame(f *AN2Frame) ([]byte, error) {
	if f == nil {
		return nil, errors.New("an2: encode nil frame")
	}
	if len(f.Payload) > MaxPayload {
		return nil, fmt.Errorf("%w: %d", ErrFrameTooBig, len(f.Payload))
	}

	buf := make([]byte, AN2HeaderSize+len(f.Payload))
	binary.BigEndian.PutUint16(buf[0:2], AN2Magic)
	buf[2] = byte(f.Proto)
	buf[3] = f.Slot
	buf[4] = f.MTID
	buf[5] = byte(f.Type)
	binary.BigEndian.PutUint16(buf[6:8], uint16(len(f.Payload)))
	copy(buf[AN2HeaderSize:], f.Payload)
	return buf, nil
}

// DecodeAN2Frame decodes an AN2 frame from a byte slice that must start
// at the magic bytes. Returns the frame and the total bytes consumed.
func DecodeAN2Frame(buf []byte) (*AN2Frame, int, error) {
	if len(buf) < AN2HeaderSize {
		return nil, 0, fmt.Errorf("an2: buffer too short for header: %d < %d", len(buf), AN2HeaderSize)
	}

	magic := binary.BigEndian.Uint16(buf[0:2])
	if magic != AN2Magic {
		return nil, 0, fmt.Errorf("%w: got 0x%04X", ErrBadMagic, magic)
	}

	dlen := binary.BigEndian.Uint16(buf[6:8])
	total := AN2HeaderSize + int(dlen)
	if int(dlen) > MaxPayload {
		return nil, 0, fmt.Errorf("%w: dlen=%d", ErrFrameTooBig, dlen)
	}
	if len(buf) < total {
		return nil, 0, fmt.Errorf("an2: buffer too short for payload: need %d, have %d", total, len(buf))
	}

	payload := make([]byte, dlen)
	copy(payload, buf[AN2HeaderSize:total])

	f := &AN2Frame{
		Proto:   AN2Proto(buf[2]),
		Slot:    buf[3],
		MTID:    buf[4],
		Type:    AN2Type(buf[5]),
		Payload: payload,
	}
	return f, total, nil
}

// ReadAN2Frame reads exactly one AN2 frame from a stream reader.
// Validates magic on every receive per spec requirement.
func ReadAN2Frame(r io.Reader) (*AN2Frame, error) {
	var hdr [AN2HeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("an2 read header: %w", err)
	}

	magic := binary.BigEndian.Uint16(hdr[0:2])
	if magic != AN2Magic {
		return nil, fmt.Errorf("%w: got 0x%04X", ErrBadMagic, magic)
	}

	dlen := binary.BigEndian.Uint16(hdr[6:8])
	if int(dlen) > MaxPayload {
		return nil, fmt.Errorf("%w: dlen=%d", ErrFrameTooBig, dlen)
	}

	payload := make([]byte, dlen)
	if dlen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, fmt.Errorf("an2 read payload: %w", err)
		}
	}

	return &AN2Frame{
		Proto:   AN2Proto(hdr[2]),
		Slot:    hdr[3],
		MTID:    hdr[4],
		Type:    AN2Type(hdr[5]),
		Payload: payload,
	}, nil
}
