package wiretrace

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

// PcapSynth synthesises a libpcap file from S101/TCP Trame frames so
// the Wireshark dissector check (R12 #473 `validate --lua`) can run
// against committed jsonl fixtures without needing a real wire
// capture.
//
// Wire-format choices:
//
//   - LINKTYPE_ETHERNET (1) — every pcap-aware tool reads it
//   - synthetic loopback IPs (127.0.0.1 client / 127.0.0.2 server)
//     so the consumer + provider resolve as distinct hosts in
//     tshark's conversation view
//   - synthetic ephemeral consumer port (50000) + caller-supplied
//     provider port (9000 for Ember+ default)
//   - per-direction TCP sequence numbers track cumulative payload
//     bytes so the segments stitch into one TCP stream from
//     tshark's perspective
//   - TCP checksum left as zero (tshark warns but accepts; avoids
//     the pseudo-header dance for every packet)
//
// Refs R12 #473.
type PcapSynth struct {
	w            io.Writer
	providerPort uint16
	consumerPort uint16
	consumerSeq  uint32
	providerSeq  uint32
}

// NewPcapSynth writes the libpcap global header to w and returns a
// synthesiser ready to receive WriteTrame calls. providerPort is the
// transport port the protocol uses on the wire (9000 for Ember+).
func NewPcapSynth(w io.Writer, providerPort uint16) (*PcapSynth, error) {
	var hdr [24]byte
	binary.LittleEndian.PutUint32(hdr[0:4], 0xa1b2c3d4) // magic — μs timestamps
	binary.LittleEndian.PutUint16(hdr[4:6], 2)          // version_major
	binary.LittleEndian.PutUint16(hdr[6:8], 4)          // version_minor
	// thiszone [8:12], sigfigs [12:16] both zero — leave as default
	binary.LittleEndian.PutUint32(hdr[16:20], 65535) // snaplen
	binary.LittleEndian.PutUint32(hdr[20:24], 1)     // LINKTYPE_ETHERNET
	if _, err := w.Write(hdr[:]); err != nil {
		return nil, fmt.Errorf("write pcap header: %w", err)
	}
	return &PcapSynth{
		w:            w,
		providerPort: providerPort,
		consumerPort: 50000,
		// Non-zero initial sequence numbers — match how real TCP starts.
		// Different bases on each side so tshark's seq-tracking renders
		// the streams distinctly.
		consumerSeq: 0x10000000,
		providerSeq: 0x20000000,
	}, nil
}

// WriteTrame writes one Trame as a TCP segment in the synthesised
// stream. Hex bytes are decoded once per call and a single TCP
// segment is emitted carrying the whole payload.
func (p *PcapSynth) WriteTrame(t Trame) error {
	payload, err := hex.DecodeString(t.Hex)
	if err != nil {
		return fmt.Errorf("hex decode (frame): %w", err)
	}
	ts := time.Time{}
	if t.Timestamp != "" {
		if parsed, perr := time.Parse(time.RFC3339Nano, t.Timestamp); perr == nil {
			ts = parsed
		}
	}
	if ts.IsZero() {
		ts = time.Now()
	}
	return p.writePacket(ts, t.Direction, payload)
}

func (p *PcapSynth) writePacket(ts time.Time, dir Direction, payload []byte) error {
	// Direction selection — `tx` = consumer→provider, `rx` = provider→consumer.
	var srcIP, dstIP [4]byte
	var srcPort, dstPort uint16
	var seq, ack uint32
	var srcMAC, dstMAC [6]byte
	consumerMAC := [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	providerMAC := [6]byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02}
	if dir == DirectionTx {
		srcIP = [4]byte{127, 0, 0, 1}
		dstIP = [4]byte{127, 0, 0, 2}
		srcPort = p.consumerPort
		dstPort = p.providerPort
		seq = p.consumerSeq
		ack = p.providerSeq
		srcMAC = consumerMAC
		dstMAC = providerMAC
		p.consumerSeq += uint32(len(payload))
	} else {
		srcIP = [4]byte{127, 0, 0, 2}
		dstIP = [4]byte{127, 0, 0, 1}
		srcPort = p.providerPort
		dstPort = p.consumerPort
		seq = p.providerSeq
		ack = p.consumerSeq
		srcMAC = providerMAC
		dstMAC = consumerMAC
		p.providerSeq += uint32(len(payload))
	}

	pkt := make([]byte, 0, 14+20+20+len(payload))

	// Ethernet header.
	pkt = append(pkt, dstMAC[:]...)
	pkt = append(pkt, srcMAC[:]...)
	pkt = append(pkt, 0x08, 0x00) // EtherType IPv4

	// IPv4 header.
	ipHdr := make([]byte, 20)
	ipHdr[0] = 0x45 // version 4, IHL 5 (20 bytes)
	// ipHdr[1] ToS = 0
	binary.BigEndian.PutUint16(ipHdr[2:4], uint16(20+20+len(payload)))
	// ipHdr[4:6] ID = 0
	binary.BigEndian.PutUint16(ipHdr[6:8], 0x4000) // DF flag, no fragment
	ipHdr[8] = 64                                  // TTL
	ipHdr[9] = 6                                   // protocol TCP
	// ipHdr[10:12] checksum — computed below
	copy(ipHdr[12:16], srcIP[:])
	copy(ipHdr[16:20], dstIP[:])
	binary.BigEndian.PutUint16(ipHdr[10:12], ipChecksum(ipHdr))
	pkt = append(pkt, ipHdr...)

	// TCP header.
	tcpHdr := make([]byte, 20)
	binary.BigEndian.PutUint16(tcpHdr[0:2], srcPort)
	binary.BigEndian.PutUint16(tcpHdr[2:4], dstPort)
	binary.BigEndian.PutUint32(tcpHdr[4:8], seq)
	binary.BigEndian.PutUint32(tcpHdr[8:12], ack)
	tcpHdr[12] = 0x50                                // data offset 5 × 4 = 20 bytes
	tcpHdr[13] = 0x18                                // PSH + ACK
	binary.BigEndian.PutUint16(tcpHdr[14:16], 0xffff) // window
	// tcpHdr[16:18] checksum = 0 (tshark accepts)
	// tcpHdr[18:20] urgent = 0
	pkt = append(pkt, tcpHdr...)

	// Payload.
	pkt = append(pkt, payload...)

	// Per-packet record header.
	var phdr [16]byte
	binary.LittleEndian.PutUint32(phdr[0:4], uint32(ts.Unix()))
	binary.LittleEndian.PutUint32(phdr[4:8], uint32(ts.Nanosecond()/1000))
	binary.LittleEndian.PutUint32(phdr[8:12], uint32(len(pkt)))
	binary.LittleEndian.PutUint32(phdr[12:16], uint32(len(pkt)))
	if _, err := p.w.Write(phdr[:]); err != nil {
		return fmt.Errorf("write packet header: %w", err)
	}
	if _, err := p.w.Write(pkt); err != nil {
		return fmt.Errorf("write packet body: %w", err)
	}
	return nil
}

// ipChecksum returns the IPv4 header checksum (16-bit one's-complement
// of one's-complement sum, big-endian).
func ipChecksum(hdr []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(hdr); i += 2 {
		sum += uint32(hdr[i])<<8 | uint32(hdr[i+1])
	}
	for sum>>16 > 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

// SynthesisePcap is the convenience function used by `validate --lua`:
// reads every Trame from r and writes a synthesised pcap to w using
// the default port for the protocol.
func SynthesisePcap(r io.Reader, w io.Writer, providerPort uint16) error {
	trames, err := ReadTrames(r)
	if err != nil {
		return err
	}
	synth, err := NewPcapSynth(w, providerPort)
	if err != nil {
		return err
	}
	for i, t := range trames {
		if err := synth.WriteTrame(t); err != nil {
			return fmt.Errorf("frame %d: %w", i, err)
		}
	}
	return nil
}
