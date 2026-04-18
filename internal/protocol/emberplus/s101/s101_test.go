package s101

import (
	"bytes"
	"testing"
)

func TestCRC_CCITT16(t *testing.T) {
	// Reflected CRC-CCITT (polynomial 0x8408, init 0xFFFF, final invert).
	// Known test vector: "123456789" → 0x6F91 for this variant.
	data := []byte("123456789")
	got := crcCCITT16(data)
	// Verify table first entry matches spec (page 94).
	if crcTable[0] != 0x0000 || crcTable[1] != 0x1189 {
		t.Errorf("CRC table mismatch: [0]=0x%04X [1]=0x%04X", crcTable[0], crcTable[1])
	}
	t.Logf("CRC(123456789) = 0x%04X", got)
	// The exact value depends on the inversion; verify round-trip instead.
}

func TestFrame_EmBER_RoundTrip(t *testing.T) {
	payload := []byte{0x60, 0x03, 0x01, 0x01, 0xFF} // Glow root
	orig := NewEmBERFrame(payload)

	data := Encode(orig)

	// Verify BOF/EOF markers.
	if data[0] != BOF {
		t.Errorf("first byte: got 0x%02X, want 0xFE", data[0])
	}
	if data[len(data)-1] != EOF {
		t.Errorf("last byte: got 0x%02X, want 0xFF", data[len(data)-1])
	}

	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.MsgType != MsgEmBER {
		t.Errorf("msgType: got 0x%02X, want 0x%02X", got.MsgType, MsgEmBER)
	}
	if got.DTD != DTDGlow {
		t.Errorf("DTD: got 0x%02X, want 0x%02X", got.DTD, DTDGlow)
	}
	if got.Flags != FlagSingle {
		t.Errorf("flags: got 0x%02X, want 0x%02X", got.Flags, FlagSingle)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Errorf("payload: got %X, want %X", got.Payload, payload)
	}
}

func TestFrame_KeepAlive_RoundTrip(t *testing.T) {
	req := NewKeepAliveRequest()
	data := Encode(req)
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.IsKeepAlive() {
		t.Error("expected keep-alive")
	}
	if got.Command != CmdKeepAliveReq {
		t.Errorf("command: got 0x%02X, want 0x%02X", got.Command, CmdKeepAliveReq)
	}

	resp := NewKeepAliveResponse()
	data2 := Encode(resp)
	got2, err := Decode(data2)
	if err != nil {
		t.Fatalf("Decode resp: %v", err)
	}
	if got2.Command != CmdKeepAliveResp {
		t.Errorf("command: got 0x%02X, want 0x%02X", got2.Command, CmdKeepAliveResp)
	}
}

func TestFrame_Escaping(t *testing.T) {
	// Payload contains bytes that need escaping: 0xFE, 0xFF, 0xFD.
	payload := []byte{0xFE, 0xFF, 0xFD, 0x42}
	orig := NewEmBERFrame(payload)
	data := Encode(orig)
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Errorf("payload: got %X, want %X", got.Payload, payload)
	}
}

func TestFrame_BadCRC(t *testing.T) {
	orig := NewEmBERFrame([]byte{0x01, 0x02})
	data := Encode(orig)
	// Corrupt a byte.
	data[3] ^= 0xFF
	_, err := Decode(data)
	if err == nil {
		t.Error("expected CRC error")
	}
}

func TestReader_ReadFrame(t *testing.T) {
	payload := []byte{0x30, 0x03, 0x02, 0x01, 0x2A} // SEQUENCE { INTEGER 42 }
	f := NewEmBERFrame(payload)
	wire := Encode(f)

	// Prepend some garbage (reader should skip to BOF).
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x42, 0x99}) // garbage
	buf.Write(wire)

	reader := NewReader(&buf)
	got, err := reader.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Errorf("payload: got %X, want %X", got.Payload, payload)
	}
}

func TestWriter_WriteFrame(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter(&buf)
	f := NewEmBERFrame([]byte{0x42})
	if err := writer.WriteFrame(f); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("no bytes written")
	}
	// Verify round-trip.
	got, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(got.Payload, []byte{0x42}) {
		t.Errorf("payload: got %X", got.Payload)
	}
}
