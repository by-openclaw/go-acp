package dnssd

import (
	"reflect"
	"testing"
)

// TestEncodeQuery_PTRMinimal pins the on-wire shape of a single-question
// PTR query for `_ember._tcp.local.`.
func TestEncodeQuery_PTRMinimal(t *testing.T) {
	got, err := EncodeQuery(0x1234, "_ember._tcp.local.")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// header (12) + name (1+6 + 1+4 + 1+5 + 1 = 19) + tail (4) = 35
	if len(got) != 35 {
		t.Fatalf("len = %d; want 35", len(got))
	}
	// ID at offset 0..1.
	if got[0] != 0x12 || got[1] != 0x34 {
		t.Errorf("ID = %02x%02x; want 1234", got[0], got[1])
	}
	// QDCOUNT at 4..6 must be 1.
	if got[4] != 0x00 || got[5] != 0x01 {
		t.Errorf("QDCOUNT = %02x%02x; want 0001", got[4], got[5])
	}
}

// TestEncodeName_TrailingDot ensures both `name` and `name.` produce
// identical bytes — DNS-SD callers vary on trailing-dot convention.
func TestEncodeName_TrailingDot(t *testing.T) {
	a, _ := encodeName("_ember._tcp.local")
	b, _ := encodeName("_ember._tcp.local.")
	if !reflect.DeepEqual(a, b) {
		t.Errorf("trailing-dot variants differ: %x vs %x", a, b)
	}
}

// TestEncodeName_Empty handles the empty-string case (root zone).
func TestEncodeName_Empty(t *testing.T) {
	out, err := encodeName("")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(out) != 1 || out[0] != 0x00 {
		t.Errorf("empty name = %x; want 00", out)
	}
}

// TestDecodeName_RoundTrip pins encode → decode preserves the name.
func TestDecodeName_RoundTrip(t *testing.T) {
	bytes, err := encodeName("dhs.local.")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, off, err := decodeName(bytes, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "dhs.local." {
		t.Errorf("name = %q; want dhs.local.", got)
	}
	if off != len(bytes) {
		t.Errorf("offset = %d; want %d", off, len(bytes))
	}
}

// TestDecodeName_Compression pins compression-pointer support: the
// pointer (0xC0 + offset) resolves the rest of the name from earlier
// in the buffer. tshark and avahi both use compression aggressively
// in real responses so this is not optional on the decode side.
func TestDecodeName_Compression(t *testing.T) {
	// First name: `dhs.local.` at offset 0.
	first, _ := encodeName("dhs.local.")
	// Second name: `srv.<pointer-to-first>`. Wire bytes:
	//   \x03 "srv" \xC0 \x00
	second := []byte{0x03, 's', 'r', 'v', 0xC0, 0x00}

	buf := append([]byte{}, first...)
	startSecond := len(buf)
	buf = append(buf, second...)
	got, off, err := decodeName(buf, startSecond)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != "srv.dhs.local." {
		t.Errorf("compressed name = %q; want srv.dhs.local.", got)
	}
	// Offset must skip past the 2-byte pointer, not return to the
	// target offset.
	if off != startSecond+len(second) {
		t.Errorf("offset = %d; want %d", off, startSecond+len(second))
	}
}

// TestEncodeRecord_TXTSorted ensures TXT keys are emitted in a stable
// order so the same TXT map produces the same bytes across runs.
func TestEncodeRecord_TXTSorted(t *testing.T) {
	rr := Record{
		Name:  "instance._ember._tcp.local.",
		Type:  TypeTXT,
		Class: ClassIN,
		TTL:   120,
		TXT:   map[string]string{"path": "/", "txtvers": "1"},
	}
	first, err := encodeRecord(rr)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := encodeRecord(rr)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("TXT encoding non-deterministic")
	}
}

// TestEncodeRecord_SRVRoundTrip pins SRV encode → decode (priority,
// weight, port, target).
func TestEncodeRecord_SRVRoundTrip(t *testing.T) {
	rr := Record{
		Name:  "instance._ember._tcp.local.",
		Type:  TypeSRV,
		Class: ClassIN,
		TTL:   120,
		SRV: SRVData{
			Priority: 0,
			Weight:   0,
			Port:     9000,
			Target:   "dhs.local.",
		},
	}
	rrBytes, err := encodeRecord(rr)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, _, err := decodeRecord(rrBytes, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SRV.Port != 9000 || got.SRV.Target != "dhs.local." {
		t.Errorf("SRV round-trip: got %+v", got.SRV)
	}
}

// TestEncodeRecord_PTR pins PTR encode → decode for a typical DNS-SD
// service-instance pointer.
func TestEncodeRecord_PTR(t *testing.T) {
	rr := Record{
		Name:  "_ember._tcp.local.",
		Type:  TypePTR,
		Class: ClassIN,
		TTL:   120,
		PTR:   "instance._ember._tcp.local.",
	}
	rrBytes, err := encodeRecord(rr)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, _, err := decodeRecord(rrBytes, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PTR != "instance._ember._tcp.local." {
		t.Errorf("PTR = %q; want instance._ember._tcp.local.", got.PTR)
	}
}

// TestEncodeRecord_A pins A-record encode → decode (IPv4 only).
func TestEncodeRecord_A(t *testing.T) {
	rr := Record{
		Name:  "dhs.local.",
		Type:  TypeA,
		Class: ClassIN,
		TTL:   120,
		A:     [4]byte{192, 168, 1, 50},
	}
	rrBytes, err := encodeRecord(rr)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, _, err := decodeRecord(rrBytes, 0)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.A != [4]byte{192, 168, 1, 50} {
		t.Errorf("A = %v; want 192.168.1.50", got.A)
	}
}

// TestEncodeResponse_Roundtrip pins full message encode → decode.
func TestEncodeResponse_Roundtrip(t *testing.T) {
	answers := []Record{
		{Name: "_ember._tcp.local.", Type: TypePTR, Class: ClassIN, TTL: 120, PTR: "dhs._ember._tcp.local."},
		{Name: "dhs._ember._tcp.local.", Type: TypeSRV, Class: ClassIN, TTL: 120,
			SRV: SRVData{Priority: 0, Weight: 0, Port: 9000, Target: "host.local."}},
		{Name: "dhs._ember._tcp.local.", Type: TypeTXT, Class: ClassIN, TTL: 120,
			TXT: map[string]string{"path": "/", "txtvers": "1"}},
		{Name: "host.local.", Type: TypeA, Class: ClassIN, TTL: 120, A: [4]byte{192, 168, 1, 50}},
	}
	msg, err := EncodeResponse(0xabcd, answers)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := Decode(msg)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !decoded.Response {
		t.Error("response flag not set")
	}
	if len(decoded.Answers) != 4 {
		t.Fatalf("answers = %d; want 4", len(decoded.Answers))
	}
}
