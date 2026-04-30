package ws

import (
	"bytes"
	"testing"
)

func TestComputeAccept(t *testing.T) {
	// RFC 6455 §1.3 worked example: key "dGhlIHNhbXBsZSBub25jZQ==" must
	// produce accept "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=".
	got := computeAccept("dGhlIHNhbXBsZSBub25jZQ==")
	want := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got != want {
		t.Fatalf("computeAccept: got %q want %q", got, want)
	}
}

func TestApplyMask(t *testing.T) {
	key := [4]byte{0x01, 0x02, 0x03, 0x04}
	in := []byte{0x10, 0x20, 0x30, 0x40, 0x50}
	want := []byte{0x11, 0x22, 0x33, 0x44, 0x51}

	buf := append([]byte(nil), in...)
	applyMask(buf, key)
	if !bytes.Equal(buf, want) {
		t.Fatalf("applyMask once: got %x want %x", buf, want)
	}
	// Mask is its own inverse.
	applyMask(buf, key)
	if !bytes.Equal(buf, in) {
		t.Fatalf("applyMask twice: got %x want %x", buf, in)
	}
}

func TestFrameRoundtrip_Short(t *testing.T) {
	// Text payload 5 bytes — fits in 7-bit length field.
	payload := []byte("hello")
	key := [4]byte{0xab, 0xcd, 0xef, 0x12}

	var w bytes.Buffer
	if err := writeFrame(&w, true, OpText, payload, key, true); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	f, err := readFrame(&w, 0)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if !f.fin || f.opcode != OpText || !f.masked {
		t.Fatalf("unexpected header: fin=%v op=%#x masked=%v", f.fin, f.opcode, f.masked)
	}
	if !bytes.Equal(f.payload, payload) {
		t.Fatalf("payload roundtrip: got %q want %q", f.payload, payload)
	}
}

func TestFrameRoundtrip_Medium(t *testing.T) {
	// 200-byte payload — uses 16-bit extended length (126 marker).
	payload := bytes.Repeat([]byte{'X'}, 200)
	var w bytes.Buffer
	if err := writeFrame(&w, true, OpText, payload, [4]byte{1, 2, 3, 4}, true); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	f, err := readFrame(&w, 0)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if !bytes.Equal(f.payload, payload) {
		t.Fatalf("payload differs (len got %d want %d)", len(f.payload), len(payload))
	}
}

func TestFrameRoundtrip_Large(t *testing.T) {
	// 70_000-byte payload — uses 64-bit extended length (127 marker).
	payload := bytes.Repeat([]byte{'Y'}, 70000)
	var w bytes.Buffer
	if err := writeFrame(&w, true, OpBinary, payload, [4]byte{0, 0, 0, 0}, false); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	f, err := readFrame(&w, 0)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if len(f.payload) != len(payload) {
		t.Fatalf("len got %d want %d", len(f.payload), len(payload))
	}
	if f.masked {
		t.Fatal("unmasked frame decoded as masked")
	}
}

func TestReadFrame_RejectsOversize(t *testing.T) {
	payload := bytes.Repeat([]byte{'Z'}, 200)
	var w bytes.Buffer
	if err := writeFrame(&w, true, OpText, payload, [4]byte{1, 1, 1, 1}, true); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	if _, err := readFrame(&w, 100); err == nil {
		t.Fatal("expected error on oversize frame, got nil")
	}
}

func TestReadFrame_RejectsControlTooLarge(t *testing.T) {
	// Build a malformed Ping with 200-byte body — control frames must be ≤125.
	payload := bytes.Repeat([]byte{0}, 200)
	var w bytes.Buffer
	if err := writeFrame(&w, true, OpPing, payload, [4]byte{0, 0, 0, 0}, false); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	if _, err := readFrame(&w, 0); err == nil {
		t.Fatal("expected error on oversized control frame")
	}
}

func TestReadFrame_RejectsFragmentedControl(t *testing.T) {
	// FIN=0 on a Ping — illegal per RFC 6455 §5.5.
	var w bytes.Buffer
	if err := writeFrame(&w, false, OpPing, []byte{1, 2, 3}, [4]byte{}, false); err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	if _, err := readFrame(&w, 0); err == nil {
		t.Fatal("expected error on fragmented control frame")
	}
}
