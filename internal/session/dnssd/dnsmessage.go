package dnssd

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Minimal pure-Go DNS message codec for the mDNS browser + announcer
// (R18 #477). Covers exactly what DNS-SD over mDNS needs:
//
//   - Question / Answer header
//   - Resource records: A (1), PTR (12), TXT (16), SRV (33)
//   - Name encoding with on-write full-label form
//   - Name decoding with compression-pointer support (peer-supplied
//     responses routinely use it; we only need to READ compressed
//     names, never write them)
//
// References:
//
//   - RFC 1035 §3 (DNS wire format)
//   - RFC 6762 (Multicast DNS)
//   - RFC 6763 (DNS-Based Service Discovery)

// DNS record types we handle.
const (
	TypeA   uint16 = 1
	TypePTR uint16 = 12
	TypeTXT uint16 = 16
	TypeSRV uint16 = 33
	TypeANY uint16 = 255

	ClassIN uint16 = 1
	// Cache-flush bit OR'd into class on responses (RFC 6762 §10.2).
	ClassFlushBit uint16 = 0x8000
)

// Message is one DNS frame on the wire.
type Message struct {
	ID        uint16
	Response  bool
	Questions []Question
	Answers   []Record
	// Additional is the additional-section records the responder
	// includes opportunistically (SRV usually arrives here on a PTR
	// query, A/AAAA on an SRV query). Decode merges both Answer and
	// Additional into Records for caller convenience.
	Additional []Record
}

// Question is one entry in a DNS message's question section.
type Question struct {
	Name  string
	Type  uint16
	Class uint16
}

// Record is one resource record. Concrete RDATA lives in the
// type-specific fields; unused fields stay at their zero value.
type Record struct {
	Name  string
	Type  uint16
	Class uint16
	TTL   uint32

	// RDATA per type.
	A    [4]byte           // type=A
	PTR  string            // type=PTR
	TXT  map[string]string // type=TXT
	SRV  SRVData           // type=SRV
}

// SRVData is the RDATA for a SRV record.
type SRVData struct {
	Priority uint16
	Weight   uint16
	Port     uint16
	Target   string
}

// EncodeQuery builds a DNS query message with one PTR question against
// the named service type (e.g. `_ember._tcp.local.`). Used by the
// browser to kick off a DNS-SD discovery round.
func EncodeQuery(id uint16, serviceType string) ([]byte, error) {
	buf := make([]byte, 0, 64)
	// Header.
	var hdr [12]byte
	binary.BigEndian.PutUint16(hdr[0:2], id)
	// Flags: standard query, recursion not requested.
	binary.BigEndian.PutUint16(hdr[2:4], 0)
	binary.BigEndian.PutUint16(hdr[4:6], 1) // QDCOUNT
	binary.BigEndian.PutUint16(hdr[6:8], 0)
	binary.BigEndian.PutUint16(hdr[8:10], 0)
	binary.BigEndian.PutUint16(hdr[10:12], 0)
	buf = append(buf, hdr[:]...)
	// Question.
	name, err := encodeName(serviceType)
	if err != nil {
		return nil, fmt.Errorf("encode question name: %w", err)
	}
	buf = append(buf, name...)
	var qtail [4]byte
	binary.BigEndian.PutUint16(qtail[0:2], TypePTR)
	binary.BigEndian.PutUint16(qtail[2:4], ClassIN)
	buf = append(buf, qtail[:]...)
	return buf, nil
}

// EncodeResponse builds a DNS answer message containing every record
// in `answers`. Used by the announcer to publish our service.
func EncodeResponse(id uint16, answers []Record) ([]byte, error) {
	buf := make([]byte, 0, 256)
	var hdr [12]byte
	binary.BigEndian.PutUint16(hdr[0:2], id)
	binary.BigEndian.PutUint16(hdr[2:4], 0x8400) // response, authoritative
	binary.BigEndian.PutUint16(hdr[4:6], 0)      // no questions echoed
	binary.BigEndian.PutUint16(hdr[6:8], uint16(len(answers)))
	binary.BigEndian.PutUint16(hdr[8:10], 0)
	binary.BigEndian.PutUint16(hdr[10:12], 0)
	buf = append(buf, hdr[:]...)
	for _, rr := range answers {
		rrBytes, err := encodeRecord(rr)
		if err != nil {
			return nil, fmt.Errorf("encode record %q (%d): %w", rr.Name, rr.Type, err)
		}
		buf = append(buf, rrBytes...)
	}
	return buf, nil
}

// Decode parses a DNS message off the wire.
func Decode(b []byte) (*Message, error) {
	if len(b) < 12 {
		return nil, errors.New("dnssd: message shorter than DNS header")
	}
	m := &Message{
		ID:       binary.BigEndian.Uint16(b[0:2]),
		Response: (binary.BigEndian.Uint16(b[2:4]) & 0x8000) != 0,
	}
	qdCount := binary.BigEndian.Uint16(b[4:6])
	anCount := binary.BigEndian.Uint16(b[6:8])
	nsCount := binary.BigEndian.Uint16(b[8:10])
	arCount := binary.BigEndian.Uint16(b[10:12])
	off := 12

	for i := uint16(0); i < qdCount; i++ {
		name, n, err := decodeName(b, off)
		if err != nil {
			return nil, fmt.Errorf("question %d name: %w", i, err)
		}
		off = n
		if off+4 > len(b) {
			return nil, errors.New("question section truncated")
		}
		q := Question{
			Name:  name,
			Type:  binary.BigEndian.Uint16(b[off : off+2]),
			Class: binary.BigEndian.Uint16(b[off+2 : off+4]),
		}
		off += 4
		m.Questions = append(m.Questions, q)
	}

	// nsCount records — skipped (authority section is rare on mDNS).
	skipRRs := func(n uint16) error {
		for i := uint16(0); i < n; i++ {
			_, no, err := decodeName(b, off)
			if err != nil {
				return err
			}
			off = no
			if off+10 > len(b) {
				return errors.New("RR header truncated")
			}
			rdlen := int(binary.BigEndian.Uint16(b[off+8 : off+10]))
			off += 10 + rdlen
		}
		return nil
	}

	readRRs := func(count uint16, into *[]Record) error {
		for i := uint16(0); i < count; i++ {
			rr, no, err := decodeRecord(b, off)
			if err != nil {
				return fmt.Errorf("record %d: %w", i, err)
			}
			off = no
			*into = append(*into, rr)
		}
		return nil
	}

	if err := readRRs(anCount, &m.Answers); err != nil {
		return nil, err
	}
	if err := skipRRs(nsCount); err != nil {
		return nil, err
	}
	if err := readRRs(arCount, &m.Additional); err != nil {
		return nil, err
	}
	return m, nil
}

// encodeName writes a fully-qualified domain name (must end in "." or
// trailing dot will be added) in the on-wire <len><label>...<0> form.
// No compression is applied — every name is written in full.
func encodeName(s string) ([]byte, error) {
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return []byte{0x00}, nil
	}
	parts := strings.Split(s, ".")
	var out []byte
	for _, p := range parts {
		if len(p) == 0 {
			return nil, errors.New("empty label in name")
		}
		if len(p) > 63 {
			return nil, fmt.Errorf("label %q exceeds 63 bytes", p)
		}
		out = append(out, byte(len(p)))
		out = append(out, p...)
	}
	out = append(out, 0x00)
	return out, nil
}

// decodeName decodes a name starting at b[off], handling RFC 1035
// compression pointers (0xC0 | high-6-bits + low-8-bits offset). The
// returned int is the offset AFTER the name in the original buffer
// (compression pointers terminate the local name even though they
// reference earlier bytes).
func decodeName(b []byte, off int) (string, int, error) {
	var parts []string
	jumped := false
	originalOff := off
	jumpsBudget := 16 // bound on pointer chase to avoid loops
	for {
		if off >= len(b) {
			return "", 0, errors.New("name truncated")
		}
		ln := int(b[off])
		if ln == 0 {
			off++
			if !jumped {
				originalOff = off
			}
			return strings.Join(parts, ".") + ".", originalOff, nil
		}
		if ln&0xC0 == 0xC0 {
			if off+1 >= len(b) {
				return "", 0, errors.New("pointer truncated")
			}
			ptr := int(binary.BigEndian.Uint16(b[off:off+2])) & 0x3FFF
			if !jumped {
				originalOff = off + 2
				jumped = true
			}
			jumpsBudget--
			if jumpsBudget <= 0 {
				return "", 0, errors.New("pointer-chase budget exhausted")
			}
			off = ptr
			continue
		}
		if ln > 63 {
			return "", 0, fmt.Errorf("label length %d exceeds 63", ln)
		}
		if off+1+ln > len(b) {
			return "", 0, errors.New("label truncated")
		}
		parts = append(parts, string(b[off+1:off+1+ln]))
		off += 1 + ln
	}
}

// encodeRecord serialises one RR. Caller passes the full TTL + class.
func encodeRecord(rr Record) ([]byte, error) {
	nameBytes, err := encodeName(rr.Name)
	if err != nil {
		return nil, err
	}
	rdata, err := encodeRDATA(rr)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(nameBytes)+10+len(rdata))
	out = append(out, nameBytes...)
	var tail [10]byte
	binary.BigEndian.PutUint16(tail[0:2], rr.Type)
	binary.BigEndian.PutUint16(tail[2:4], rr.Class)
	binary.BigEndian.PutUint32(tail[4:8], rr.TTL)
	binary.BigEndian.PutUint16(tail[8:10], uint16(len(rdata)))
	out = append(out, tail[:]...)
	out = append(out, rdata...)
	return out, nil
}

func encodeRDATA(rr Record) ([]byte, error) {
	switch rr.Type {
	case TypeA:
		return rr.A[:], nil
	case TypePTR:
		return encodeName(rr.PTR)
	case TypeTXT:
		// One <len><value> per key=value pair. Empty TXT records carry
		// a single zero-length label per RFC 6763 — never emit a fully
		// empty RDATA (some parsers reject it).
		if len(rr.TXT) == 0 {
			return []byte{0x00}, nil
		}
		// Stable ordering — keys sorted ASC so wire bytes are
		// reproducible across runs / instances.
		keys := make([]string, 0, len(rr.TXT))
		for k := range rr.TXT {
			keys = append(keys, k)
		}
		// Stable sort via simple insertion (no external import).
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
				keys[j], keys[j-1] = keys[j-1], keys[j]
			}
		}
		var out []byte
		for _, k := range keys {
			entry := k + "=" + rr.TXT[k]
			if len(entry) > 255 {
				return nil, fmt.Errorf("TXT entry %q exceeds 255 bytes", entry)
			}
			out = append(out, byte(len(entry)))
			out = append(out, entry...)
		}
		return out, nil
	case TypeSRV:
		nameBytes, err := encodeName(rr.SRV.Target)
		if err != nil {
			return nil, err
		}
		var hdr [6]byte
		binary.BigEndian.PutUint16(hdr[0:2], rr.SRV.Priority)
		binary.BigEndian.PutUint16(hdr[2:4], rr.SRV.Weight)
		binary.BigEndian.PutUint16(hdr[4:6], rr.SRV.Port)
		return append(hdr[:], nameBytes...), nil
	}
	return nil, fmt.Errorf("unsupported RR type %d", rr.Type)
}

func decodeRecord(b []byte, off int) (Record, int, error) {
	name, n, err := decodeName(b, off)
	if err != nil {
		return Record{}, 0, fmt.Errorf("RR name: %w", err)
	}
	off = n
	if off+10 > len(b) {
		return Record{}, 0, errors.New("RR header truncated")
	}
	rr := Record{
		Name:  name,
		Type:  binary.BigEndian.Uint16(b[off : off+2]),
		Class: binary.BigEndian.Uint16(b[off+2 : off+4]),
		TTL:   binary.BigEndian.Uint32(b[off+4 : off+8]),
	}
	rdlen := int(binary.BigEndian.Uint16(b[off+8 : off+10]))
	off += 10
	if off+rdlen > len(b) {
		return Record{}, 0, errors.New("RR rdata truncated")
	}
	switch rr.Type & 0x7FFF {
	case TypeA:
		if rdlen != 4 {
			return Record{}, 0, fmt.Errorf("a-record rdata length %d (want 4)", rdlen)
		}
		copy(rr.A[:], b[off:off+4])
	case TypePTR:
		ptrName, _, perr := decodeName(b, off)
		if perr != nil {
			return Record{}, 0, fmt.Errorf("PTR rdata: %w", perr)
		}
		rr.PTR = ptrName
	case TypeTXT:
		rr.TXT = map[string]string{}
		pos := off
		end := off + rdlen
		for pos < end {
			ln := int(b[pos])
			pos++
			if pos+ln > end {
				break
			}
			entry := string(b[pos : pos+ln])
			pos += ln
			eq := strings.IndexByte(entry, '=')
			if eq <= 0 {
				continue
			}
			rr.TXT[entry[:eq]] = entry[eq+1:]
		}
	case TypeSRV:
		if rdlen < 7 {
			return Record{}, 0, fmt.Errorf("SRV rdata length %d (want >= 7)", rdlen)
		}
		rr.SRV.Priority = binary.BigEndian.Uint16(b[off : off+2])
		rr.SRV.Weight = binary.BigEndian.Uint16(b[off+2 : off+4])
		rr.SRV.Port = binary.BigEndian.Uint16(b[off+4 : off+6])
		target, _, terr := decodeName(b, off+6)
		if terr != nil {
			return Record{}, 0, fmt.Errorf("SRV target: %w", terr)
		}
		rr.SRV.Target = target
	}
	off += rdlen
	return rr, off, nil
}

// Type & 0x7FFF mask in decodeRecord drops the cache-flush bit per RFC 6762.
// Mask is identical to ClassFlushBit but flipped for the high bit on the
// type field in mDNS responses — explicit constant kept for legibility.
const _ = "decode strips cache-flush bit via & 0x7FFF"
