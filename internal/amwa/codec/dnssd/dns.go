// Package dnssd implements DNS-SD (RFC 6763) over either mDNS (RFC 6762)
// or unicast DNS (RFC 1035). Stdlib-only: this layer must remain
// lift-to-own-repo ready, same rule as every other internal/<proto>/codec/.
//
// Scope: just enough DNS for service discovery — A, AAAA, PTR, SRV, TXT,
// the four record types DNS-SD needs. Name compression (0xC0 pointer) is
// supported on decode and emitted on encode for repeated suffixes.
//
// This file owns the message envelope + question + record encode/decode
// + name codec. types.go owns DNS-SD-specific helpers (Service,
// Instance). txt.go owns TXT key=value pairs.
package dnssd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
)

// RFC 1035 §3.2.2 record types. Only the DNS-SD subset.
const (
	TypeA    uint16 = 1
	TypePTR  uint16 = 12
	TypeTXT  uint16 = 16
	TypeAAAA uint16 = 28
	TypeSRV  uint16 = 33
	TypeANY  uint16 = 255 // QTYPE only
)

// RFC 1035 §3.2.4 classes; mDNS uses IN with the cache-flush bit (RFC 6762 §10.2).
const (
	ClassIN          uint16 = 1
	ClassMask        uint16 = 0x7FFF // RR class with cache-flush bit stripped
	ClassFlushBit    uint16 = 0x8000 // mDNS cache-flush bit
	ClassUnicastBit  uint16 = 0x8000 // mDNS QU bit on questions (RFC 6762 §5.4)
)

// RFC 1035 §4.1.1 header flags.
const (
	flagQR     uint16 = 1 << 15 // 0=query, 1=response
	flagOpMask uint16 = 0xF << 11
	flagAA     uint16 = 1 << 10
	flagTC     uint16 = 1 << 9
	flagRD     uint16 = 1 << 8
	flagRA     uint16 = 1 << 7
	flagRcode  uint16 = 0xF
)

// MaxLabelLen is the RFC 1035 §3.1 hard limit per label.
const MaxLabelLen = 63

// MaxNameLen is the RFC 1035 §3.1 hard limit on a fully encoded name.
const MaxNameLen = 255

// Errors returned by the decoder. Callers should errors.Is(err, ErrTruncated)
// etc. rather than string-matching.
var (
	ErrTruncated     = errors.New("dnssd: truncated message")
	ErrLabelTooLong  = errors.New("dnssd: label exceeds 63 bytes")
	ErrNameTooLong   = errors.New("dnssd: encoded name exceeds 255 bytes")
	ErrPointerLoop   = errors.New("dnssd: name compression pointer loop")
	ErrBadPointer    = errors.New("dnssd: name compression pointer out of range")
	ErrInvalidLabel  = errors.New("dnssd: invalid label byte")
)

// Header is the 12-byte RFC 1035 §4.1.1 message header.
type Header struct {
	ID      uint16
	Flags   uint16
	QDCount uint16
	ANCount uint16
	NSCount uint16
	ARCount uint16
}

// IsResponse reports whether the QR bit is set.
func (h Header) IsResponse() bool { return h.Flags&flagQR != 0 }

// SetResponse sets the QR bit.
func (h *Header) SetResponse(b bool) {
	if b {
		h.Flags |= flagQR
	} else {
		h.Flags &^= flagQR
	}
}

// SetAuthoritative sets the AA bit. mDNS responders MUST set AA on
// every response (RFC 6762 §6).
func (h *Header) SetAuthoritative(b bool) {
	if b {
		h.Flags |= flagAA
	} else {
		h.Flags &^= flagAA
	}
}

// Question is one RFC 1035 §4.1.2 question entry.
type Question struct {
	Name  string // dotted, no trailing dot e.g. "_nmos-register._tcp.local"
	Type  uint16
	Class uint16 // includes the QU bit when set
}

// RR is a Resource Record. Data carries the type-specific payload
// (already decoded for the types we care about); for unknown types
// it carries the raw rdata bytes.
type RR struct {
	Name  string
	Type  uint16
	Class uint16 // includes the cache-flush bit when set
	TTL   uint32

	// Decoded payloads for known types. Exactly one is set; the
	// others are zero values.
	A    net.IP   // TypeA
	AAAA net.IP   // TypeAAAA
	PTR  string   // TypePTR (name)
	TXT  []string // TypeTXT (raw "key=value" strings, undecoded)
	SRV  *SRVData // TypeSRV

	// RawData is the untouched rdata bytes; populated for unknown
	// types and also kept around for known types so encoders can
	// round-trip without re-marshalling.
	RawData []byte
}

// SRVData carries the four RFC 2782 fields.
type SRVData struct {
	Priority uint16
	Weight   uint16
	Port     uint16
	Target   string // dotted name
}

// Message is a complete DNS message: header + sections.
type Message struct {
	Header     Header
	Questions  []Question
	Answers    []RR
	Authority  []RR
	Additional []RR
}

// Encode serialises the message to wire bytes. Name compression
// (RFC 1035 §4.1.4) is applied where it shortens output safely.
func (m *Message) Encode() ([]byte, error) {
	enc := newEncoder()

	// Sync count fields with slice lengths so callers don't have to.
	hdr := m.Header
	hdr.QDCount = uint16(len(m.Questions))
	hdr.ANCount = uint16(len(m.Answers))
	hdr.NSCount = uint16(len(m.Authority))
	hdr.ARCount = uint16(len(m.Additional))

	enc.writeHeader(hdr)
	for _, q := range m.Questions {
		if err := enc.writeQuestion(q); err != nil {
			return nil, err
		}
	}
	for _, sec := range [][]RR{m.Answers, m.Authority, m.Additional} {
		for _, rr := range sec {
			if err := enc.writeRR(rr); err != nil {
				return nil, err
			}
		}
	}
	return enc.bytes(), nil
}

// Decode parses wire bytes into a Message. Returns ErrTruncated when
// the buffer ends mid-record, ErrPointerLoop on a malformed compressed
// name, etc.
func Decode(buf []byte) (*Message, error) {
	d := &decoder{buf: buf}
	hdr, err := d.readHeader()
	if err != nil {
		return nil, err
	}
	m := &Message{Header: hdr}
	m.Questions = make([]Question, 0, hdr.QDCount)
	for i := uint16(0); i < hdr.QDCount; i++ {
		q, err := d.readQuestion()
		if err != nil {
			return nil, err
		}
		m.Questions = append(m.Questions, q)
	}
	for _, n := range []struct {
		count uint16
		into  *[]RR
	}{
		{hdr.ANCount, &m.Answers},
		{hdr.NSCount, &m.Authority},
		{hdr.ARCount, &m.Additional},
	} {
		*n.into = make([]RR, 0, n.count)
		for i := uint16(0); i < n.count; i++ {
			rr, err := d.readRR()
			if err != nil {
				return nil, err
			}
			*n.into = append(*n.into, rr)
		}
	}
	return m, nil
}

// ---------------------------------------------------------------------
// encoder
// ---------------------------------------------------------------------

type encoder struct {
	out     []byte
	offsets map[string]int // suffix → offset for compression
}

func newEncoder() *encoder { return &encoder{out: make([]byte, 0, 256), offsets: map[string]int{}} }

func (e *encoder) bytes() []byte { return e.out }

func (e *encoder) writeHeader(h Header) {
	var b [12]byte
	binary.BigEndian.PutUint16(b[0:], h.ID)
	binary.BigEndian.PutUint16(b[2:], h.Flags)
	binary.BigEndian.PutUint16(b[4:], h.QDCount)
	binary.BigEndian.PutUint16(b[6:], h.ANCount)
	binary.BigEndian.PutUint16(b[8:], h.NSCount)
	binary.BigEndian.PutUint16(b[10:], h.ARCount)
	e.out = append(e.out, b[:]...)
}

func (e *encoder) writeQuestion(q Question) error {
	if err := e.writeName(q.Name); err != nil {
		return err
	}
	var b [4]byte
	binary.BigEndian.PutUint16(b[0:], q.Type)
	binary.BigEndian.PutUint16(b[2:], q.Class)
	e.out = append(e.out, b[:]...)
	return nil
}

func (e *encoder) writeRR(rr RR) error {
	if err := e.writeName(rr.Name); err != nil {
		return err
	}
	hdr := make([]byte, 10)
	binary.BigEndian.PutUint16(hdr[0:], rr.Type)
	binary.BigEndian.PutUint16(hdr[2:], rr.Class)
	binary.BigEndian.PutUint32(hdr[4:], rr.TTL)
	// rdlength patched in after we know the payload size
	rdlenOff := len(e.out) + 8
	e.out = append(e.out, hdr...)

	rdataStart := len(e.out)
	switch rr.Type {
	case TypeA:
		ip := rr.A.To4()
		if ip == nil {
			return fmt.Errorf("dnssd: A record needs IPv4, got %v", rr.A)
		}
		e.out = append(e.out, ip...)
	case TypeAAAA:
		ip := rr.AAAA.To16()
		if ip == nil {
			return fmt.Errorf("dnssd: AAAA record needs IPv6, got %v", rr.AAAA)
		}
		e.out = append(e.out, ip...)
	case TypePTR:
		if err := e.writeName(rr.PTR); err != nil {
			return err
		}
	case TypeTXT:
		// RFC 1035 §3.3.14 — sequence of <length-prefixed> strings.
		if len(rr.TXT) == 0 {
			// Empty TXT: per RFC 6763 §6.1 must be a single zero-length string.
			e.out = append(e.out, 0)
		}
		for _, s := range rr.TXT {
			if len(s) > 255 {
				return fmt.Errorf("dnssd: TXT segment exceeds 255 bytes")
			}
			e.out = append(e.out, byte(len(s)))
			e.out = append(e.out, []byte(s)...)
		}
	case TypeSRV:
		if rr.SRV == nil {
			return fmt.Errorf("dnssd: SRV record missing SRVData")
		}
		var sb [6]byte
		binary.BigEndian.PutUint16(sb[0:], rr.SRV.Priority)
		binary.BigEndian.PutUint16(sb[2:], rr.SRV.Weight)
		binary.BigEndian.PutUint16(sb[4:], rr.SRV.Port)
		e.out = append(e.out, sb[:]...)
		// RFC 2782 says the SRV target SHOULD NOT be compressed, but
		// most implementations do compress and DNS clients handle it
		// either way. We emit uncompressed for maximum interop.
		if err := e.writeNameUncompressed(rr.SRV.Target); err != nil {
			return err
		}
	default:
		e.out = append(e.out, rr.RawData...)
	}
	rdlen := len(e.out) - rdataStart
	if rdlen > 0xFFFF {
		return fmt.Errorf("dnssd: rdata exceeds 65535 bytes")
	}
	binary.BigEndian.PutUint16(e.out[rdlenOff:rdlenOff+2], uint16(rdlen))
	return nil
}

// writeName encodes a dotted name with backref compression. Empty
// name encodes as the single root label byte 0.
func (e *encoder) writeName(name string) error {
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		e.out = append(e.out, 0)
		return nil
	}
	for {
		if off, ok := e.offsets[name]; ok {
			// Compression pointer: 14-bit offset with top two bits 11.
			ptr := uint16(0xC000) | uint16(off&0x3FFF)
			var pb [2]byte
			binary.BigEndian.PutUint16(pb[:], ptr)
			e.out = append(e.out, pb[:]...)
			return nil
		}
		// Record this suffix's offset before consuming the head label.
		if len(e.out) < 0x4000 {
			e.offsets[name] = len(e.out)
		}
		dot := strings.IndexByte(name, '.')
		var label string
		if dot < 0 {
			label = name
			name = ""
		} else {
			label = name[:dot]
			name = name[dot+1:]
		}
		if len(label) == 0 {
			return ErrInvalidLabel
		}
		if len(label) > MaxLabelLen {
			return ErrLabelTooLong
		}
		e.out = append(e.out, byte(len(label)))
		e.out = append(e.out, []byte(label)...)
		if name == "" {
			e.out = append(e.out, 0) // root terminator
			return nil
		}
	}
}

// writeNameUncompressed forces an uncompressed name encoding (used
// for SRV target). No compression pointers are emitted.
func (e *encoder) writeNameUncompressed(name string) error {
	name = strings.TrimSuffix(name, ".")
	if name == "" {
		e.out = append(e.out, 0)
		return nil
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 {
			return ErrInvalidLabel
		}
		if len(label) > MaxLabelLen {
			return ErrLabelTooLong
		}
		e.out = append(e.out, byte(len(label)))
		e.out = append(e.out, []byte(label)...)
	}
	e.out = append(e.out, 0)
	return nil
}

// ---------------------------------------------------------------------
// decoder
// ---------------------------------------------------------------------

type decoder struct {
	buf []byte
	pos int
}

func (d *decoder) readHeader() (Header, error) {
	if len(d.buf)-d.pos < 12 {
		return Header{}, ErrTruncated
	}
	h := Header{
		ID:      binary.BigEndian.Uint16(d.buf[d.pos:]),
		Flags:   binary.BigEndian.Uint16(d.buf[d.pos+2:]),
		QDCount: binary.BigEndian.Uint16(d.buf[d.pos+4:]),
		ANCount: binary.BigEndian.Uint16(d.buf[d.pos+6:]),
		NSCount: binary.BigEndian.Uint16(d.buf[d.pos+8:]),
		ARCount: binary.BigEndian.Uint16(d.buf[d.pos+10:]),
	}
	d.pos += 12
	return h, nil
}

func (d *decoder) readQuestion() (Question, error) {
	name, err := d.readName()
	if err != nil {
		return Question{}, err
	}
	if len(d.buf)-d.pos < 4 {
		return Question{}, ErrTruncated
	}
	q := Question{
		Name:  name,
		Type:  binary.BigEndian.Uint16(d.buf[d.pos:]),
		Class: binary.BigEndian.Uint16(d.buf[d.pos+2:]),
	}
	d.pos += 4
	return q, nil
}

func (d *decoder) readRR() (RR, error) {
	name, err := d.readName()
	if err != nil {
		return RR{}, err
	}
	if len(d.buf)-d.pos < 10 {
		return RR{}, ErrTruncated
	}
	rr := RR{
		Name:  name,
		Type:  binary.BigEndian.Uint16(d.buf[d.pos:]),
		Class: binary.BigEndian.Uint16(d.buf[d.pos+2:]),
		TTL:   binary.BigEndian.Uint32(d.buf[d.pos+4:]),
	}
	rdlen := int(binary.BigEndian.Uint16(d.buf[d.pos+8:]))
	d.pos += 10
	if len(d.buf)-d.pos < rdlen {
		return RR{}, ErrTruncated
	}
	rdataEnd := d.pos + rdlen
	rr.RawData = append([]byte(nil), d.buf[d.pos:rdataEnd]...)
	switch rr.Type {
	case TypeA:
		if rdlen != 4 {
			return RR{}, fmt.Errorf("dnssd: A record rdata length %d != 4", rdlen)
		}
		rr.A = net.IPv4(d.buf[d.pos], d.buf[d.pos+1], d.buf[d.pos+2], d.buf[d.pos+3]).To4()
	case TypeAAAA:
		if rdlen != 16 {
			return RR{}, fmt.Errorf("dnssd: AAAA record rdata length %d != 16", rdlen)
		}
		rr.AAAA = append(net.IP(nil), d.buf[d.pos:rdataEnd]...)
	case TypePTR:
		// PTR rdata is a name; allow compression — read from the
		// global buffer with d.pos pointed at the rdata start.
		saved := d.pos
		ptrName, err := d.readName()
		if err != nil {
			return RR{}, err
		}
		// Some peers pad PTR rdata; rewind the cursor to rdata end.
		_ = saved
		rr.PTR = ptrName
		d.pos = rdataEnd
		return rr, nil
	case TypeTXT:
		segs, err := readTXTSegments(d.buf[d.pos:rdataEnd])
		if err != nil {
			return RR{}, err
		}
		rr.TXT = segs
	case TypeSRV:
		if rdlen < 7 {
			return RR{}, fmt.Errorf("dnssd: SRV rdata length %d < 7", rdlen)
		}
		srv := &SRVData{
			Priority: binary.BigEndian.Uint16(d.buf[d.pos:]),
			Weight:   binary.BigEndian.Uint16(d.buf[d.pos+2:]),
			Port:     binary.BigEndian.Uint16(d.buf[d.pos+4:]),
		}
		// Read target with compression — temporarily point d.pos at SRV target.
		d.pos += 6
		target, err := d.readName()
		if err != nil {
			return RR{}, err
		}
		srv.Target = target
		rr.SRV = srv
		d.pos = rdataEnd
		return rr, nil
	}
	d.pos = rdataEnd
	return rr, nil
}

func readTXTSegments(rdata []byte) ([]string, error) {
	var out []string
	for i := 0; i < len(rdata); {
		n := int(rdata[i])
		i++
		if i+n > len(rdata) {
			return nil, ErrTruncated
		}
		out = append(out, string(rdata[i:i+n]))
		i += n
	}
	return out, nil
}

// readName reads a possibly-compressed name starting at d.pos and
// advances d.pos to the byte after the name (or after the 2-byte
// pointer if compression jumps elsewhere).
func (d *decoder) readName() (string, error) {
	const maxJumps = 32
	jumps := 0
	pos := d.pos
	advanced := false // tracks whether we've followed a pointer yet
	finalPos := -1   // position to set d.pos to when we're done
	var labels []string
	totalLen := 0
	for {
		if pos >= len(d.buf) {
			return "", ErrTruncated
		}
		b := d.buf[pos]
		switch {
		case b == 0:
			pos++
			if !advanced {
				finalPos = pos
			}
			d.pos = finalPos
			return strings.Join(labels, "."), nil
		case b&0xC0 == 0xC0:
			// Pointer.
			if pos+1 >= len(d.buf) {
				return "", ErrTruncated
			}
			ptr := int(binary.BigEndian.Uint16(d.buf[pos:]) & 0x3FFF)
			if ptr >= len(d.buf) || ptr >= pos {
				// RFC 1035 §4.1.4 forbids forward pointers; allow
				// equal-or-lower offsets only.
				return "", ErrBadPointer
			}
			if !advanced {
				finalPos = pos + 2
				advanced = true
			}
			jumps++
			if jumps > maxJumps {
				return "", ErrPointerLoop
			}
			pos = ptr
		case b&0xC0 == 0:
			n := int(b)
			if pos+1+n > len(d.buf) {
				return "", ErrTruncated
			}
			labels = append(labels, string(d.buf[pos+1:pos+1+n]))
			totalLen += n + 1
			if totalLen > MaxNameLen {
				return "", ErrNameTooLong
			}
			pos += 1 + n
		default:
			// 0x80 / 0x40 reserved (RFC 6891 EDNS labels not relevant here).
			return "", fmt.Errorf("dnssd: reserved label flags 0x%x", b&0xC0)
		}
	}
}
