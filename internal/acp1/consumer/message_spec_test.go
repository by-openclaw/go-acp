// Black-box spec-compliance tests for ACP1 message encode/decode.
// Every byte sequence is derived from spec v1.4 pages 9-14 and 32.
// These tests use only the exported API — no internal symbols.
package acp1_test

import (
	"bytes"
	"testing"

	"acp/internal/acp1/consumer"
)

func TestSpec_EncodeGetFrameStatus(t *testing.T) {
	m := &acp1.Message{
		MTID:     0,
		MType:    acp1.MTypeRequest,
		MAddr:    0,
		MCode:    byte(acp1.MethodGetValue),
		ObjGroup: acp1.GroupFrame,
		ObjID:    0,
	}
	want := []byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x01, 0x00, 0x00, 0x06, 0x00}
	got, err := m.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("wire:\n got=%x\nwant=%x", got, want)
	}
}

func TestSpec_DecodeRoundTrip(t *testing.T) {
	wire := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x05, 0x00, 0x02, 0x0A, 0xAA, 0xBB}
	m, err := acp1.Decode(wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	out, err := m.Encode()
	if err != nil {
		t.Fatalf("re-Encode: %v", err)
	}
	if !bytes.Equal(out, wire) {
		t.Fatalf("round-trip:\n got=%x\nwant=%x", out, wire)
	}
}
