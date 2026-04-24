package codec

import (
	"encoding/binary"
	"fmt"
)

// BundleMarker is the OSC-string at the start of every bundle.
const BundleMarker = "#bundle"

// Packet is either a Message or a Bundle — the two top-level OSC
// payload shapes. Callers that receive a raw UDP datagram (or one
// SLIP/length-prefix frame) should call DecodePacket, which dispatches
// by the first byte of the payload (`/` = Message, `#` = Bundle).
type Packet interface {
	isPacket()
}

func (Message) isPacket() {}
func (Bundle) isPacket()  {}

// Bundle groups packets under a single OSC timetag (u64 NTP format).
// Elements are themselves Packets — either Messages or nested Bundles.
type Bundle struct {
	Timetag  uint64
	Elements []Packet

	// Notes carries deviations observed during Decode.
	Notes []ComplianceNote
}

// Encode serialises a Bundle per spec §OSC Bundle.
//
//	<OSC-string "#bundle"> <u64 timetag> (<int32 size> <element>)*
func (bn Bundle) Encode() ([]byte, error) {
	out := make([]byte, 0, 128)
	out = encodeString(out, BundleMarker)
	out = encodeUint64(out, bn.Timetag)

	for i, el := range bn.Elements {
		body, err := encodePacket(el)
		if err != nil {
			return nil, fmt.Errorf("bundle element[%d]: %w", i, err)
		}
		var sz [4]byte
		binary.BigEndian.PutUint32(sz[:], uint32(len(body)))
		out = append(out, sz[:]...)
		out = append(out, body...)
	}
	return out, nil
}

// encodePacket returns the wire form of either Message or Bundle.
func encodePacket(p Packet) ([]byte, error) {
	switch v := p.(type) {
	case Message:
		return v.Encode()
	case Bundle:
		return v.Encode()
	}
	return nil, fmt.Errorf("osc: unknown packet type %T", p)
}

// DecodeBundle parses the bytes as a Bundle. Caller must have already
// validated that the payload starts with "#bundle".
func DecodeBundle(b []byte) (Bundle, error) {
	bn := Bundle{}
	marker, n, err := decodeString(b, 0)
	if err != nil {
		return bn, fmt.Errorf("bundle marker: %w", err)
	}
	if marker != BundleMarker {
		return bn, fmt.Errorf("%w: got %q", ErrBundleNotBundle, marker)
	}
	off := n

	t, err := decodeUint64(b, off)
	if err != nil {
		return bn, fmt.Errorf("bundle timetag: %w", err)
	}
	bn.Timetag = t
	off += 8

	for off < len(b) {
		if off+4 > len(b) {
			return bn, fmt.Errorf("%w: element size at offset %d", ErrTruncated, off)
		}
		size := int32(binary.BigEndian.Uint32(b[off:]))
		off += 4
		if size < 0 || off+int(size) > len(b) {
			return bn, fmt.Errorf("%w: size=%d at offset %d", ErrBundleElementSize, size, off-4)
		}
		el, err := DecodePacket(b[off : off+int(size)])
		if err != nil {
			return bn, fmt.Errorf("bundle element at %d: %w", off, err)
		}
		bn.Elements = append(bn.Elements, el)
		off += int(size)
	}
	return bn, nil
}

// DecodePacket dispatches between Message and Bundle based on the first
// byte of the payload.
func DecodePacket(b []byte) (Packet, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("%w: empty packet", ErrTruncated)
	}
	switch b[0] {
	case '/':
		return DecodeMessage(b)
	case '#':
		return DecodeBundle(b)
	}
	return nil, fmt.Errorf("%w: first byte 0x%02x (%q)", ErrBundleNotBundle, b[0], b[0])
}
