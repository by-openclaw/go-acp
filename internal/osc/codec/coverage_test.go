package codec

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// This file closes the remaining decoder/encoder/framer error arms not
// exercised by the round-trip and happy-path tests elsewhere in the
// package. Every case drives a real production error path with crafted
// bytes or a fake io.Reader — no production code is seamed or weakened.

// -----------------------------------------------------------------------------
// primitives.go — fixed-width decode truncation arms
//
// These guards are reachable directly (white-box, same package) by calling
// the decoder at an offset past the end of the buffer. They mirror the
// per-message truncation the framer normally pre-validates, so test them
// at the unit level where the short buffer is unambiguous.
// -----------------------------------------------------------------------------

func TestDecodePrimitives_Truncated(t *testing.T) {
	// Each fixed-width decoder must reject a buffer that ends before the
	// value's width, returning ErrTruncated.
	t.Run("int32", func(t *testing.T) {
		if _, err := decodeInt32([]byte{0x00, 0x01}, 0); !errors.Is(err, ErrTruncated) {
			t.Errorf("want ErrTruncated, got %v", err)
		}
	})
	t.Run("int64", func(t *testing.T) {
		if _, err := decodeInt64([]byte{0x00, 0x01, 0x02, 0x03}, 0); !errors.Is(err, ErrTruncated) {
			t.Errorf("want ErrTruncated, got %v", err)
		}
	})
	t.Run("float32", func(t *testing.T) {
		if _, err := decodeFloat32([]byte{0x00}, 0); !errors.Is(err, ErrTruncated) {
			t.Errorf("want ErrTruncated, got %v", err)
		}
	})
	t.Run("float64", func(t *testing.T) {
		if _, err := decodeFloat64([]byte{0x00, 0x01, 0x02}, 0); !errors.Is(err, ErrTruncated) {
			t.Errorf("want ErrTruncated, got %v", err)
		}
	})
	t.Run("uint64", func(t *testing.T) {
		if _, err := decodeUint64([]byte{0xFF, 0xFF}, 0); !errors.Is(err, ErrTruncated) {
			t.Errorf("want ErrTruncated, got %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// primitives.go — decodeString edge arms
// -----------------------------------------------------------------------------

func TestDecodeString_OffsetPastEnd(t *testing.T) {
	// off >= len(b) — the leading bounds check.
	if _, _, err := decodeString([]byte{'a', 0x00}, 5); !errors.Is(err, ErrTruncated) {
		t.Errorf("want ErrTruncated, got %v", err)
	}
}

func TestDecodeString_PadRunsPastEnd(t *testing.T) {
	// A NUL terminator is present, but the 4-byte alignment pad the spec
	// requires would run past the end of the buffer. "ab\0" terminates at
	// index 2; consumed = 3 + padTo4(3)=1 = 4, but only 3 bytes exist.
	if _, _, err := decodeString([]byte{'a', 'b', 0x00}, 0); !errors.Is(err, ErrTruncated) {
		t.Errorf("want ErrTruncated, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// primitives.go — decodeBlob edge arms
// -----------------------------------------------------------------------------

func TestDecodeBlob_SizeHeaderTruncated(t *testing.T) {
	// Fewer than 4 bytes for the int32 size prefix.
	if _, _, err := decodeBlob([]byte{0x00, 0x00}, 0); !errors.Is(err, ErrTruncated) {
		t.Errorf("want ErrTruncated, got %v", err)
	}
}

func TestDecodeBlob_NegativeSize(t *testing.T) {
	// int32 size with the high bit set decodes to a negative length.
	if _, _, err := decodeBlob([]byte{0xFF, 0xFF, 0xFF, 0xFF}, 0); !errors.Is(err, ErrBlobTooLarge) {
		t.Errorf("want ErrBlobTooLarge, got %v", err)
	}
}

func TestDecodeBlob_PadRunsPastEnd(t *testing.T) {
	// size=1, one body byte present, but the pad-to-4 (3 NULs) is missing.
	// Body bounds pass (end <= len), but off+consumed > len trips the pad
	// guard. size(4) + body(1) = 5 bytes present; consumed = 4+1+3 = 8.
	b := []byte{0x00, 0x00, 0x00, 0x01, 0xAA}
	if _, _, err := decodeBlob(b, 0); !errors.Is(err, ErrTruncated) {
		t.Errorf("want ErrTruncated, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// message.go — encodeArg unknown tag arm
// -----------------------------------------------------------------------------

func TestEncodeArg_UnknownTag(t *testing.T) {
	// A tag outside the known set must fail at encode time. Encode() wraps
	// encodeArg, so drive it through the public surface as well.
	m := Message{Address: "/x", Args: []Arg{{Tag: 'Q'}}}
	_, err := m.Encode()
	if !errors.Is(err, ErrTagUnknown) {
		t.Errorf("want ErrTagUnknown, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// message.go — DecodeMessage per-arg error arms
//
// Each builds a valid address + type-tag string, then withholds the arg
// payload so the matching decode call fails. The address/tag strings are
// 4-byte aligned, so off lands exactly at the (missing) arg.
// -----------------------------------------------------------------------------

func TestDecodeMessage_AddressDecodeError(t *testing.T) {
	// Buffer with no NUL terminator at all — the address decode fails first.
	if _, err := DecodeMessage([]byte{'/', 'x'}); !errors.Is(err, ErrStringNotTerminated) {
		t.Errorf("want ErrStringNotTerminated, got %v", err)
	}
}

func TestDecodeMessage_TypeTagDecodeError(t *testing.T) {
	// Valid address, then a type-tag string that never terminates.
	wire := encodeString(nil, "/x")
	wire = append(wire, ',', 'i') // no NUL terminator for the tag string
	if _, err := DecodeMessage(wire); !errors.Is(err, ErrStringNotTerminated) {
		t.Errorf("want ErrStringNotTerminated, got %v", err)
	}
}

func TestDecodeMessage_PerArgTruncated(t *testing.T) {
	cases := []struct {
		name string
		tag  string // single type tag (without comma)
	}{
		{"float32", "f"},
		{"int64", "h"},
		{"float64", "d"},
		{"timetag", "t"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wire := encodeString(nil, "/x")
			wire = encodeString(wire, ","+c.tag)
			// No arg payload follows — the per-arg decoder must fail.
			if _, err := DecodeMessage(wire); !errors.Is(err, ErrTruncated) {
				t.Errorf("%s: want ErrTruncated, got %v", c.name, err)
			}
		})
	}
}

func TestDecodeMessage_StringArgDecodeError(t *testing.T) {
	// ,s with a non-terminated string arg.
	wire := encodeString(nil, "/x")
	wire = encodeString(wire, ",s")
	wire = append(wire, 'a', 'b', 'c') // no NUL
	if _, err := DecodeMessage(wire); !errors.Is(err, ErrStringNotTerminated) {
		t.Errorf("want ErrStringNotTerminated, got %v", err)
	}
}

func TestDecodeMessage_FixedFourByteTruncated(t *testing.T) {
	// 'r' (RGBA) / 'm' (MIDI) take a raw 4-byte payload; fewer than 4
	// bytes following the tag trips the dedicated bounds check.
	for _, tag := range []string{"r", "m"} {
		t.Run(tag, func(t *testing.T) {
			wire := encodeString(nil, "/x")
			wire = encodeString(wire, ","+tag)
			wire = append(wire, 0x01, 0x02) // only 2 of 4 bytes
			if _, err := DecodeMessage(wire); !errors.Is(err, ErrTruncated) {
				t.Errorf("%s: want ErrTruncated, got %v", tag, err)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// bundle.go — isPacket markers + encode/decode error arms
// -----------------------------------------------------------------------------

func TestIsPacket_Markers(t *testing.T) {
	// Exercise the marker methods that make Message/Bundle satisfy Packet.
	var p Packet = Message{}
	p.isPacket()
	p = Bundle{}
	p.isPacket()
}

// errPacket is an unknown Packet implementation used only to drive the
// encodePacket default arm and Bundle.Encode's element-encode error path.
type errPacket struct{}

func (errPacket) isPacket() {}

func TestEncodePacket_UnknownType(t *testing.T) {
	if _, err := encodePacket(errPacket{}); err == nil {
		t.Errorf("expected error for unknown packet type")
	}
}

func TestBundleEncode_ElementEncodeError(t *testing.T) {
	// An element that fails to encode must propagate out of Bundle.Encode.
	// An unknown packet type triggers encodePacket's default arm.
	bn := Bundle{Timetag: 1, Elements: []Packet{errPacket{}}}
	if _, err := bn.Encode(); err == nil {
		t.Errorf("expected element encode error")
	}
}

func TestDecodeBundle_MarkerDecodeError(t *testing.T) {
	// No NUL terminator — the marker OSC-string fails to decode.
	if _, err := DecodeBundle([]byte{'#', 'b'}); !errors.Is(err, ErrStringNotTerminated) {
		t.Errorf("want ErrStringNotTerminated, got %v", err)
	}
}

func TestDecodeBundle_WrongMarker(t *testing.T) {
	// A well-formed OSC-string that isn't "#bundle".
	b := encodeString(nil, "#wrong")
	b = encodeUint64(b, 0)
	if _, err := DecodeBundle(b); !errors.Is(err, ErrBundleNotBundle) {
		t.Errorf("want ErrBundleNotBundle, got %v", err)
	}
}

func TestDecodeBundle_TimetagTruncated(t *testing.T) {
	// "#bundle" present but the 8-byte timetag is missing.
	b := encodeString(nil, BundleMarker)
	b = append(b, 0x00, 0x00) // only 2 of 8 timetag bytes
	if _, err := DecodeBundle(b); !errors.Is(err, ErrTruncated) {
		t.Errorf("want ErrTruncated, got %v", err)
	}
}

func TestDecodeBundle_ElementSizeHeaderTruncated(t *testing.T) {
	// Valid marker + timetag, then only 2 bytes where a 4-byte element
	// size prefix is expected.
	b := encodeString(nil, BundleMarker)
	b = encodeUint64(b, 1)
	b = append(b, 0x00, 0x00)
	if _, err := DecodeBundle(b); !errors.Is(err, ErrTruncated) {
		t.Errorf("want ErrTruncated, got %v", err)
	}
}

func TestDecodeBundle_ElementSizeTooLarge(t *testing.T) {
	// Element size claims more bytes than remain in the bundle.
	b := encodeString(nil, BundleMarker)
	b = encodeUint64(b, 1)
	b = encodeInt32(b, 100) // claims 100-byte element, none follow
	if _, err := DecodeBundle(b); !errors.Is(err, ErrBundleElementSize) {
		t.Errorf("want ErrBundleElementSize, got %v", err)
	}
}

func TestDecodeBundle_ElementSizeNegative(t *testing.T) {
	b := encodeString(nil, BundleMarker)
	b = encodeUint64(b, 1)
	b = encodeInt32(b, -1) // negative element size
	if _, err := DecodeBundle(b); !errors.Is(err, ErrBundleElementSize) {
		t.Errorf("want ErrBundleElementSize, got %v", err)
	}
}

func TestDecodeBundle_ElementDecodeError(t *testing.T) {
	// A well-sized element whose body is not a valid packet (first byte is
	// neither '/' nor '#') must surface the inner decode error.
	bad := []byte{'X', 0x00, 0x00, 0x00} // 4-byte element, bad first byte
	b := encodeString(nil, BundleMarker)
	b = encodeUint64(b, 1)
	b = encodeInt32(b, int32(len(bad)))
	b = append(b, bad...)
	if _, err := DecodeBundle(b); !errors.Is(err, ErrBundleNotBundle) {
		t.Errorf("want ErrBundleNotBundle (inner), got %v", err)
	}
}

// -----------------------------------------------------------------------------
// framing.go — LenPrefixReader error arms via fake readers
// -----------------------------------------------------------------------------

// errReader returns a fixed error after optionally emitting some bytes.
type errReader struct {
	data []byte
	err  error
}

func (e *errReader) Read(p []byte) (int, error) {
	if len(e.data) > 0 {
		n := copy(p, e.data)
		e.data = e.data[n:]
		return n, nil
	}
	return 0, e.err
}

func TestLenPrefix_HeaderPartialThenEOF(t *testing.T) {
	// Two header bytes then EOF — io.ReadFull reports ErrUnexpectedEOF,
	// which the reader surfaces as ErrUnexpectedEOF (mid-frame).
	r := &errReader{data: []byte{0x00, 0x00}, err: io.EOF}
	if _, err := NewLenPrefixReader(r, 0).ReadPacket(); err != io.ErrUnexpectedEOF {
		t.Errorf("want io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestLenPrefix_HeaderReadError(t *testing.T) {
	// A non-EOF error reading the header is returned verbatim.
	sentinel := errors.New("boom")
	r := &errReader{err: sentinel}
	if _, err := NewLenPrefixReader(r, 0).ReadPacket(); !errors.Is(err, sentinel) {
		t.Errorf("want sentinel error, got %v", err)
	}
}

func TestLenPrefix_BodyReadError(t *testing.T) {
	// Full 4-byte header (size=4) reads fine, then the body read returns a
	// non-EOF error — returned verbatim.
	sentinel := errors.New("body boom")
	r := &errReader{data: []byte{0x00, 0x00, 0x00, 0x04}, err: sentinel}
	if _, err := NewLenPrefixReader(r, 0).ReadPacket(); !errors.Is(err, sentinel) {
		t.Errorf("want sentinel error, got %v", err)
	}
}

func TestLenPrefix_NegativeSize(t *testing.T) {
	// Header decodes to a negative int32 size.
	r := bytes.NewReader([]byte{0xFF, 0xFF, 0xFF, 0xFF})
	if _, err := NewLenPrefixReader(r, 0).ReadPacket(); !errors.Is(err, ErrLenPrefixNegative) {
		t.Errorf("want ErrLenPrefixNegative, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// framing.go — SLIPReader error arms
// -----------------------------------------------------------------------------

func TestSLIP_EnsureByteReadError(t *testing.T) {
	// A non-EOF error from the underlying reader propagates out of
	// ensureByte → readByte → ReadPacket.
	sentinel := errors.New("slip boom")
	r := &errReader{err: sentinel}
	if _, err := NewSLIPReader(r, 0).ReadPacket(); !errors.Is(err, sentinel) {
		t.Errorf("want sentinel error, got %v", err)
	}
}

func TestSLIP_TruncatedAfterEscape(t *testing.T) {
	// ESC at end of stream with no following byte — readByteStrict turns
	// the EOF into ErrUnexpectedEOF.
	stream := []byte{SLIPEnd, 0x01, SLIPEsc} // no escape-target byte, no END
	_, err := NewSLIPReader(bytes.NewReader(stream), 0).ReadPacket()
	if err != io.ErrUnexpectedEOF {
		t.Errorf("want io.ErrUnexpectedEOF, got %v", err)
	}
}

func TestSLIP_MaxPacketExceeded(t *testing.T) {
	// Body grows past maxPacket before a trailing END is seen.
	stream := []byte{SLIPEnd, 0x01, 0x02, 0x03, 0x04, 0x05}
	_, err := NewSLIPReader(bytes.NewReader(stream), 3).ReadPacket()
	if err == nil {
		t.Fatalf("expected max-packet error, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("max")) {
		t.Errorf("expected max-packet error, got %v", err)
	}
}
