package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// EncodeAN2Frame: nil frame -> error.
func TestEncodeAN2Frame_Nil(t *testing.T) {
	if _, err := EncodeAN2Frame(nil); err == nil {
		t.Fatal("expected error encoding nil frame")
	}
}

// EncodeAN2Frame: payload over MaxPayload -> ErrFrameTooBig.
func TestEncodeAN2Frame_TooBig(t *testing.T) {
	f := &AN2Frame{
		Proto:   AN2ProtoACP2,
		Type:    AN2TypeData,
		Payload: make([]byte, MaxPayload+1),
	}
	_, err := EncodeAN2Frame(f)
	if !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("got %v, want ErrFrameTooBig", err)
	}
}

// DecodeAN2Frame: dlen field exceeding MaxPayload -> ErrFrameTooBig,
// even before the buffer is inspected for the payload.
func TestDecodeAN2Frame_DlenExceedsMax(t *testing.T) {
	// MaxPayload is 65536 which does not fit in the u16 dlen field
	// (max 65535), so set dlen to its max and lower MaxPayload is not
	// possible; instead build a header whose declared dlen still trips
	// the cap by being > MaxPayload is impossible via u16. Verify the
	// boundary another way: dlen == 65535 must NOT trip the cap.
	hdr := make([]byte, AN2HeaderSize)
	binary.BigEndian.PutUint16(hdr[0:2], AN2Magic)
	binary.BigEndian.PutUint16(hdr[6:8], 65535) // max u16, < MaxPayload(65536)
	// Buffer too short for the 65535-byte payload -> short-payload error,
	// proving the dlen>MaxPayload branch was NOT taken at u16 max.
	_, _, err := DecodeAN2Frame(hdr)
	if err == nil {
		t.Fatal("expected short-payload error")
	}
	if errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("dlen=65535 (<MaxPayload) must not be ErrFrameTooBig: %v", err)
	}
}

// DecodeAN2Frame: header declares a payload longer than the buffer holds.
func TestDecodeAN2Frame_ShortPayload(t *testing.T) {
	buf := make([]byte, AN2HeaderSize+2)
	binary.BigEndian.PutUint16(buf[0:2], AN2Magic)
	buf[2] = byte(AN2ProtoACP2)
	buf[5] = byte(AN2TypeData)
	binary.BigEndian.PutUint16(buf[6:8], 10) // claims 10 payload bytes, only 2 present
	_, _, err := DecodeAN2Frame(buf)
	if err == nil {
		t.Fatal("expected short-payload error")
	}
	if errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("unexpected ErrFrameTooBig: %v", err)
	}
}

// ReadAN2Frame: header read fails (empty stream) -> error.
func TestReadAN2Frame_HeaderReadError(t *testing.T) {
	_, err := ReadAN2Frame(bytes.NewReader(nil))
	if err == nil {
		t.Fatal("expected header read error")
	}
}

// ReadAN2Frame: dlen exceeds MaxPayload is unreachable at u16 max; verify
// instead that a valid header with a payload-read failure propagates.
func TestReadAN2Frame_PayloadReadError(t *testing.T) {
	hdr := make([]byte, AN2HeaderSize)
	binary.BigEndian.PutUint16(hdr[0:2], AN2Magic)
	hdr[2] = byte(AN2ProtoACP2)
	hdr[5] = byte(AN2TypeData)
	binary.BigEndian.PutUint16(hdr[6:8], 8) // claims 8 payload bytes
	// Stream has the full header but only 3 of the 8 payload bytes.
	stream := append(append([]byte{}, hdr...), 0x01, 0x02, 0x03)
	_, err := ReadAN2Frame(bytes.NewReader(stream))
	if err == nil {
		t.Fatal("expected payload read error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("got %v, want io.ErrUnexpectedEOF", err)
	}
}

// ReadAN2Frame: zero-length payload path (dlen=0) reads no body.
func TestReadAN2Frame_ZeroPayload(t *testing.T) {
	hdr := make([]byte, AN2HeaderSize)
	binary.BigEndian.PutUint16(hdr[0:2], AN2Magic)
	hdr[2] = byte(AN2ProtoInternal)
	hdr[5] = byte(AN2TypeReply)
	f, err := ReadAN2Frame(bytes.NewReader(hdr))
	if err != nil {
		t.Fatalf("ReadAN2Frame: %v", err)
	}
	if len(f.Payload) != 0 {
		t.Fatalf("payload: got %d bytes, want 0", len(f.Payload))
	}
}
