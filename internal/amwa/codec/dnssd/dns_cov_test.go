package dnssd

import (
	"encoding/binary"
	"errors"
	"net"
	"strings"
	"testing"
)

// The header's two mDNS-relevant bits are set and cleared, not just
// set — a responder that never clears AA answers as authoritative for
// a query it merely forwarded.
func TestHeaderFlagToggles(t *testing.T) {
	var h Header
	h.SetResponse(true)
	if !h.IsResponse() {
		t.Error("SetResponse(true) must set QR")
	}
	h.SetResponse(false)
	if h.IsResponse() {
		t.Error("SetResponse(false) must clear QR")
	}
	h.SetAuthoritative(true)
	if h.Flags&flagAA == 0 {
		t.Error("SetAuthoritative(true) must set AA")
	}
	h.SetAuthoritative(false)
	if h.Flags&flagAA != 0 {
		t.Error("SetAuthoritative(false) must clear AA")
	}
}

// The encoder refuses a record it cannot represent rather than
// emitting a malformed one onto the link.
func TestEncodeRefusesMalformedRecords(t *testing.T) {
	long := strings.Repeat("a", MaxLabelLen+1)

	for name, tc := range map[string]struct {
		msg  Message
		want string
	}{
		"a question whose label is too long": {
			Message{Questions: []Question{{Name: long + ".local", Type: TypePTR, Class: ClassIN}}},
			"63 bytes",
		},
		"a question with an empty label": {
			Message{Questions: []Question{{Name: "a..local"}}},
			"invalid label",
		},
		"a record whose owner name is too long": {
			Message{Answers: []RR{{Name: long + ".local", Type: TypePTR, PTR: "x.local"}}},
			"63 bytes",
		},
		"an A record carrying no IPv4": {
			Message{Answers: []RR{{Name: "a.local", Type: TypeA, A: net.ParseIP("2001:db8::1")}}},
			"needs IPv4",
		},
		"an AAAA record carrying no IP at all": {
			Message{Answers: []RR{{Name: "a.local", Type: TypeAAAA}}},
			"needs IPv6",
		},
		"a PTR record whose target is malformed": {
			Message{Answers: []RR{{Name: "a.local", Type: TypePTR, PTR: long + ".local"}}},
			"63 bytes",
		},
		"a TXT segment above 255 bytes": {
			Message{Answers: []RR{{
				Name: "a.local", Type: TypeTXT, TXT: []string{strings.Repeat("x", 256)},
			}}},
			"255 bytes",
		},
		"an SRV record with no data": {
			Message{Answers: []RR{{Name: "a.local", Type: TypeSRV}}},
			"missing SRVData",
		},
		"an SRV target with an empty label": {
			Message{Answers: []RR{{
				Name: "a.local", Type: TypeSRV, SRV: &SRVData{Target: "host..local"},
			}}},
			"invalid label",
		},
		"an SRV target label that is too long": {
			Message{Answers: []RR{{
				Name: "a.local", Type: TypeSRV, SRV: &SRVData{Target: long + ".local"},
			}}},
			"63 bytes",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tc.msg.Encode()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}

	// A record type the codec does not model round-trips its raw
	// rdata untouched, and an empty TXT goes out as the single
	// zero-length string RFC 6763 §6.1 requires.
	msg := Message{Answers: []RR{
		{Name: "a.local", Type: 99, RawData: []byte{1, 2, 3}},
		{Name: "b.local", Type: TypeTXT},
		{Name: "c.local", Type: TypeAAAA, AAAA: net.ParseIP("2001:db8::1")},
		{Name: "", Type: TypePTR, PTR: ""}, // the root name
	}}
	raw, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(back.Answers) != 4 {
		t.Fatalf("decoded %d answers", len(back.Answers))
	}
	if string(back.Answers[0].RawData) != "\x01\x02\x03" {
		t.Errorf("unmodelled rdata = %v", back.Answers[0].RawData)
	}
	if len(back.Answers[1].TXT) != 1 || back.Answers[1].TXT[0] != "" {
		t.Errorf("empty TXT = %q, want the one zero-length segment", back.Answers[1].TXT)
	}
	if !back.Answers[2].AAAA.Equal(net.ParseIP("2001:db8::1")) {
		t.Errorf("AAAA = %v", back.Answers[2].AAAA)
	}
}

// header builds a 12-byte DNS header with the given counts.
func header(qd, an uint16) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[4:], qd)
	binary.BigEndian.PutUint16(b[6:], an)
	return b
}

// A packet off the link is untrusted: every truncation and every
// malformed name is refused with the error that names it, never a
// half-decoded record.
func TestDecodeRefusesMalformedPackets(t *testing.T) {
	name := []byte{1, 'a', 0} // "a"

	for testName, tc := range map[string]struct {
		buf  []byte
		want error
	}{
		"a buffer shorter than the header": {make([]byte, 11), ErrTruncated},
		"a question that ends mid-name": {
			append(header(1, 0), 5, 'a'), ErrTruncated,
		},
		"a question with no type or class": {
			append(header(1, 0), name...), ErrTruncated,
		},
		"a record with no header": {
			append(header(0, 1), name...), ErrTruncated,
		},
		"a record whose rdata is short": {
			func() []byte {
				b := append(header(0, 1), name...)
				rr := make([]byte, 10)
				binary.BigEndian.PutUint16(rr[0:], TypeTXT)
				binary.BigEndian.PutUint16(rr[2:], ClassIN)
				binary.BigEndian.PutUint16(rr[8:], 8) // claims 8 bytes
				return append(b, rr...)
			}(), ErrTruncated,
		},
		"a name whose label runs past the buffer": {
			append(header(1, 0), 9, 'a'), ErrTruncated,
		},
		"a compression pointer with no second byte": {
			append(header(1, 0), 0xC0), ErrTruncated,
		},
		"a forward compression pointer": {
			append(header(1, 0), 0xC0, 0xFF), ErrBadPointer,
		},
		"a label with reserved flag bits": {
			append(header(1, 0), 0x80, 0x00), nil, // a plain error, not a sentinel
		},
	} {
		t.Run(testName, func(t *testing.T) {
			_, err := Decode(tc.buf)
			if err == nil {
				t.Fatal("was accepted")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("= %v, want %v", err, tc.want)
			}
		})
	}
}

// Each record type states its own rdata length; one that disagrees is
// a malformed record, not a short read.
func TestDecodeRefusesBadRDataLengths(t *testing.T) {
	build := func(typ uint16, rdata []byte) []byte {
		b := append(header(0, 1), 1, 'a', 0)
		rr := make([]byte, 10)
		binary.BigEndian.PutUint16(rr[0:], typ)
		binary.BigEndian.PutUint16(rr[2:], ClassIN)
		binary.BigEndian.PutUint16(rr[8:], uint16(len(rdata)))
		return append(append(b, rr...), rdata...)
	}

	for name, tc := range map[string]struct {
		buf  []byte
		want string
	}{
		"an A record that is not 4 bytes":       {build(TypeA, []byte{1, 2, 3}), "!= 4"},
		"an AAAA record that is not 16 bytes":   {build(TypeAAAA, []byte{1, 2, 3}), "!= 16"},
		"an SRV record shorter than its header": {build(TypeSRV, []byte{0, 0, 0, 0}), "< 7"},
		"a TXT segment running past the rdata":  {build(TypeTXT, []byte{9, 'a'}), "truncated"},
		"a PTR whose name is malformed":         {build(TypePTR, []byte{0xC0, 0xFF}), "pointer"},
		"an SRV whose target is malformed": {
			build(TypeSRV, []byte{0, 0, 0, 0, 0, 0, 0xC0, 0xFF}), "pointer",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(tc.buf)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Errorf("= %v, want an error mentioning %q", err, tc.want)
			}
		})
	}
}

// A name that never terminates — a pointer chain, or one long enough
// to exceed the 255-byte limit — is refused rather than looped on.
func TestDecodeRefusesPathologicalNames(t *testing.T) {
	// Two pointers aimed at each other: the decoder only follows
	// backwards, so build a chain that keeps jumping to the same
	// earlier offset.
	buf := header(1, 0)
	base := len(buf)
	buf = append(buf, 0xC0, byte(base)) // points at itself: not backwards
	if _, err := Decode(buf); !errors.Is(err, ErrBadPointer) {
		t.Errorf("a self-pointer = %v, want ErrBadPointer", err)
	}

	// A name longer than 255 bytes across many labels.
	long := header(1, 0)
	for i := 0; i < 8; i++ {
		long = append(long, MaxLabelLen)
		long = append(long, []byte(strings.Repeat("a", MaxLabelLen))...)
	}
	long = append(long, 0)
	if _, err := Decode(long); !errors.Is(err, ErrNameTooLong) {
		t.Errorf("an over-long name = %v, want ErrNameTooLong", err)
	}
}

// Compression is what keeps an mDNS response inside one datagram: a
// repeated suffix is emitted once and read back identically.
func TestNameCompressionRoundTrips(t *testing.T) {
	msg := Message{Answers: []RR{
		{Name: "a._nmos-register._tcp.local", Type: TypePTR, PTR: "one._nmos-register._tcp.local"},
		{Name: "b._nmos-register._tcp.local", Type: TypePTR, PTR: "two._nmos-register._tcp.local"},
	}}
	raw, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	uncompressed := 0
	for _, rr := range msg.Answers {
		uncompressed += len(rr.Name) + len(rr.PTR) + 12
	}
	if len(raw) >= uncompressed {
		t.Errorf("encoded %d bytes with no compression saving", len(raw))
	}

	back, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(back.Answers) != 2 ||
		back.Answers[1].Name != "b._nmos-register._tcp.local" ||
		back.Answers[1].PTR != "two._nmos-register._tcp.local" {
		t.Errorf("round-tripped %+v", back.Answers)
	}
}

// TXT keys are case-insensitive and first-one-wins (RFC 6763 §6.4),
// boolean attributes carry no value, and a key the responder cannot
// encode is refused.
func TestTXTEncodeDecode(t *testing.T) {
	segs, err := EncodeTXT(map[string]string{"api_ver": "v1.3", "pri": "100"})
	if err != nil {
		t.Fatalf("EncodeTXT: %v", err)
	}
	if len(segs) != 2 || segs[0] != "api_ver=v1.3" {
		t.Errorf("segments = %q, want them sorted", segs)
	}
	if got, _ := EncodeTXT(nil); len(got) != 1 || got[0] != "" {
		t.Errorf("an empty map = %q, want the one zero-length segment", got)
	}

	for name, kv := range map[string]map[string]string{
		"an empty key":              {"": "x"},
		"a key above 32 bytes":      {strings.Repeat("k", 33): "x"},
		"a key holding an equals":   {"a=b": "x"},
		"a key with a control byte": {"a\x01b": "x"},
		"a segment above 255 bytes": {"k": strings.Repeat("v", 300)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := EncodeTXT(kv); err == nil {
				t.Error("was accepted")
			}
		})
	}

	kv := DecodeTXT([]string{"", "API_VER=v1.3", "api_ver=v1.0", "flag", "pri=100"})
	if kv["api_ver"] != "v1.3" {
		t.Errorf("api_ver = %q, want the first segment to win", kv["api_ver"])
	}
	if v, ok := kv["flag"]; !ok || v != "" {
		t.Errorf("a boolean attribute = %q, %v", v, ok)
	}

	if n, ok := PriorityFromTXT(kv); !ok || n != 100 {
		t.Errorf("PriorityFromTXT = %d, %v", n, ok)
	}
	if _, ok := PriorityFromTXT(map[string]string{}); ok {
		t.Error("a TXT set with no pri names no priority")
	}
	if _, ok := PriorityFromTXT(map[string]string{TXTKeyPriority: "high"}); ok {
		t.Error("a pri that is not a number names no priority")
	}
}

// A record whose rdata will not fit the 16-bit length field is
// refused: the length would wrap and every reader after it would
// mis-parse the rest of the packet.
func TestEncodeRefusesOversizedRData(t *testing.T) {
	msg := Message{Answers: []RR{{
		Name: "a.local", Type: 99, Class: ClassIN,
		RawData: make([]byte, 0x10000),
	}}}
	if _, err := msg.Encode(); err == nil || !strings.Contains(err.Error(), "65535") {
		t.Errorf("= %v, want the oversized rdata refused", err)
	}
}
