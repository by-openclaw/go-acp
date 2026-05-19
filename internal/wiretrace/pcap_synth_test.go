package wiretrace

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// TestPcapSynth_HeaderShape pins the libpcap global-header bytes:
// magic 0xa1b2c3d4 little-endian, version 2.4, linktype 1 (Ethernet),
// snaplen 65535.
func TestPcapSynth_HeaderShape(t *testing.T) {
	var buf bytes.Buffer
	_, err := NewPcapSynth(&buf, 9000)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if buf.Len() != 24 {
		t.Fatalf("header len = %d; want 24", buf.Len())
	}
	h := buf.Bytes()
	if binary.LittleEndian.Uint32(h[0:4]) != 0xa1b2c3d4 {
		t.Errorf("magic = %x; want 0xa1b2c3d4", h[0:4])
	}
	if binary.LittleEndian.Uint16(h[4:6]) != 2 {
		t.Errorf("major = %d; want 2", binary.LittleEndian.Uint16(h[4:6]))
	}
	if binary.LittleEndian.Uint16(h[6:8]) != 4 {
		t.Errorf("minor = %d; want 4", binary.LittleEndian.Uint16(h[6:8]))
	}
	if binary.LittleEndian.Uint32(h[16:20]) != 65535 {
		t.Errorf("snaplen = %d; want 65535", binary.LittleEndian.Uint32(h[16:20]))
	}
	if binary.LittleEndian.Uint32(h[20:24]) != 1 {
		t.Errorf("linktype = %d; want 1 (LINKTYPE_ETHERNET)", binary.LittleEndian.Uint32(h[20:24]))
	}
}

// TestPcapSynth_TxFrameShape pins a single tx frame: Ethernet + IPv4
// + TCP + payload. Validates that the IP header lengths + protocol +
// addresses are correct and the payload appears verbatim after the
// 14+20+20 = 54 byte network stack.
func TestPcapSynth_TxFrameShape(t *testing.T) {
	var buf bytes.Buffer
	synth, err := NewPcapSynth(&buf, 9000)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if err := synth.WriteTrame(Trame{Hex: "fe0e000c0001020100feab", Direction: DirectionTx}); err != nil {
		t.Fatalf("write: %v", err)
	}
	// header (24) + record-hdr (16) + ethernet (14) + ip (20) + tcp (20) + payload (11) = 105
	if buf.Len() != 24+16+14+20+20+11 {
		t.Fatalf("total len = %d; want %d", buf.Len(), 24+16+14+20+20+11)
	}
	body := buf.Bytes()[24+16:] // skip global + record headers

	// Ethernet ethertype at body[12:14] must be IPv4.
	if body[12] != 0x08 || body[13] != 0x00 {
		t.Errorf("ethertype = %02x%02x; want 0800", body[12], body[13])
	}
	// IP starts at 14.
	if body[14]>>4 != 4 {
		t.Errorf("ip version = %d; want 4", body[14]>>4)
	}
	if body[14]&0xf != 5 {
		t.Errorf("ip IHL = %d; want 5", body[14]&0xf)
	}
	if body[14+9] != 6 {
		t.Errorf("ip proto = %d; want 6 (TCP)", body[14+9])
	}
	// Src IP at 14+12, dst IP at 14+16.
	if !bytes.Equal(body[14+12:14+16], []byte{127, 0, 0, 1}) {
		t.Errorf("src IP = %v; want 127.0.0.1", body[14+12:14+16])
	}
	if !bytes.Equal(body[14+16:14+20], []byte{127, 0, 0, 2}) {
		t.Errorf("dst IP = %v; want 127.0.0.2", body[14+16:14+20])
	}
	// TCP starts at 14+20 = 34. dst port at 34+2.
	if binary.BigEndian.Uint16(body[34+2:34+4]) != 9000 {
		t.Errorf("dst port = %d; want 9000", binary.BigEndian.Uint16(body[34+2:34+4]))
	}
	// Payload starts at 54.
	if !bytes.Equal(body[54:], []byte{0xfe, 0x0e, 0x00, 0x0c, 0x00, 0x01, 0x02, 0x01, 0x00, 0xfe, 0xab}) {
		t.Errorf("payload mismatch: %x", body[54:])
	}
}

// TestPcapSynth_DirectionFlip ensures rx frames reverse src/dst.
func TestPcapSynth_DirectionFlip(t *testing.T) {
	var buf bytes.Buffer
	synth, err := NewPcapSynth(&buf, 9000)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if err := synth.WriteTrame(Trame{Hex: "ff", Direction: DirectionRx}); err != nil {
		t.Fatalf("write: %v", err)
	}
	body := buf.Bytes()[24+16:]
	if !bytes.Equal(body[14+12:14+16], []byte{127, 0, 0, 2}) {
		t.Errorf("rx src IP = %v; want 127.0.0.2", body[14+12:14+16])
	}
	if !bytes.Equal(body[14+16:14+20], []byte{127, 0, 0, 1}) {
		t.Errorf("rx dst IP = %v; want 127.0.0.1", body[14+16:14+20])
	}
	// src port = providerPort (9000)
	if binary.BigEndian.Uint16(body[34:34+2]) != 9000 {
		t.Errorf("rx src port = %d; want 9000", binary.BigEndian.Uint16(body[34:34+2]))
	}
}

// TestPcapSynth_SeqAdvances pins that consecutive tx frames see
// monotonically-increasing sequence numbers reflecting payload bytes.
func TestPcapSynth_SeqAdvances(t *testing.T) {
	var buf bytes.Buffer
	synth, err := NewPcapSynth(&buf, 9000)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if err := synth.WriteTrame(Trame{Hex: "aabbcc", Direction: DirectionTx}); err != nil {
		t.Fatal(err)
	}
	if err := synth.WriteTrame(Trame{Hex: "ddeeff", Direction: DirectionTx}); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()[24:]
	// First frame's record-header (16) + ethernet (14) + ip (20) + tcp = 50;
	// seq at tcp offset 4..8.
	frame1Seq := binary.BigEndian.Uint32(body[16+34+4 : 16+34+8])
	// Second frame's record-header at offset 16+14+20+20+3 = 73,
	// plus another 16 bytes record header before its content.
	off2 := 16 + 14 + 20 + 20 + 3 + 16
	frame2Seq := binary.BigEndian.Uint32(body[off2+34+4 : off2+34+8])
	if frame2Seq != frame1Seq+3 {
		t.Errorf("seq did not advance by payload size: frame1=%d frame2=%d (delta %d, want 3)",
			frame1Seq, frame2Seq, frame2Seq-frame1Seq)
	}
}

// TestSynthesisePcap_FromReader pins the convenience wrapper —
// ReadTrames + writePcap chain — for a 2-frame jsonl stream.
func TestSynthesisePcap_FromReader(t *testing.T) {
	jsonl := `{"schema_version":1,"hex":"fe000e0001020100feab","dir":"tx"}` + "\n" +
		`{"schema_version":1,"hex":"fe000e0001020200feab","dir":"rx"}` + "\n"
	var out bytes.Buffer
	if err := SynthesisePcap(strings.NewReader(jsonl), &out, 9000); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// 24 (header) + 2 × (16 + 14 + 20 + 20 + 10) = 24 + 160 = 184
	want := 24 + 2*(16+14+20+20+10)
	if out.Len() != want {
		t.Errorf("output len = %d; want %d", out.Len(), want)
	}
}

// TestPcapSynth_HexDecodeError ensures malformed hex returns an error
// rather than silently corrupting the pcap.
func TestPcapSynth_HexDecodeError(t *testing.T) {
	var buf bytes.Buffer
	synth, _ := NewPcapSynth(&buf, 9000)
	err := synth.WriteTrame(Trame{Hex: "not-hex", Direction: DirectionTx})
	if err == nil {
		t.Fatal("expected hex decode error")
	}
}
