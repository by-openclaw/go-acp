package codec

// The wire is the contract. These tests are written against bytes taken
// from RFC 1157/3416 message layouts and from what the testbed's own
// peers put on the network — a Tandberg IRD answering v1 on :161 and
// Cerebrum's agent and trap receiver — so that a refactor that still
// round-trips but changes a byte is caught here rather than by a device
// that stops answering.

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"
)

// sysDescr0 is 1.3.6.1.2.1.1.1.0 — the object every walk starts from and
// the one the IRDs answer with their product name.
var sysDescr0 = MustParseOID("1.3.6.1.2.1.1.1.0")

// ---------------------------------------------------------------------
// OIDs
// ---------------------------------------------------------------------

// The first two arcs share one sub-identifier, which is the single most
// error-prone rule in the whole encoding: 1.3 is 43, and the packing
// changes shape at 40 and at 80.
func TestOIDEncodesTheFirstTwoArcsTogether(t *testing.T) {
	for _, tc := range []struct {
		oid  string
		want []byte
	}{
		{"1.3", []byte{0x2B}},
		{"0.0", []byte{0x00}},
		{"0.39", []byte{0x27}},
		{"1.0", []byte{0x28}},
		{"1.39", []byte{0x4F}},
		{"2.0", []byte{0x50}},
		// Arc values of 128 and above need the base-128 continuation
		// form, and enterprise 1773 is exactly such a value — every
		// Tandberg OID in the testbed carries it.
		{"1.3.6.1.4.1.1773", []byte{0x2B, 0x06, 0x01, 0x04, 0x01, 0x8D, 0x6D}},
		// The joint-iso-itu-t tree, where the first sub-identifier
		// exceeds 80 and the second arc is the remainder.
		{"2.100.3", []byte{0x81, 0x34, 0x03}},
	} {
		t.Run(tc.oid, func(t *testing.T) {
			got := appendOID(nil, MustParseOID(tc.oid))
			want := append([]byte{tagOID, byte(len(tc.want))}, tc.want...)
			if !bytes.Equal(got, want) {
				t.Fatalf("encoded % X, want % X", got, want)
			}
			back, err := parseOID(tc.want)
			if err != nil {
				t.Fatalf("parseOID: %v", err)
			}
			if back.String() != tc.oid {
				t.Errorf("round trip = %s, want %s", back, tc.oid)
			}
		})
	}
}

func TestParseOIDRefusals(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"empty", "", "empty OID"},
		{"a bare dot", ".", "empty OID"},
		{"one arc", "1", "fewer than two arcs"},
		{"not a number", "1.3.six", "not a number"},
		{"negative", "1.-3", "not a number"},
		{"wider than 32 bits", "1.3.4294967296", "not a number"},
		{"longer than the SMI allows", strings.Repeat("1.", MaxOIDLen+1) + "1", "exceeds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseOID(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
			if !errors.Is(err, ErrMalformed) {
				t.Errorf("error must be an ErrMalformed: %v", err)
			}
		})
	}
}

// A leading dot is written by half the world's documentation and means
// the same object.
func TestParseOIDAcceptsALeadingDot(t *testing.T) {
	a, err := ParseOID(".1.3.6.1")
	if err != nil {
		t.Fatal(err)
	}
	if a.Compare(MustParseOID("1.3.6.1")) != 0 {
		t.Errorf(".1.3.6.1 parsed as %s", a)
	}
}

func TestMustParseOIDPanicsOnOurOwnBadLiteral(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a bad OID literal in our own source must panic")
		}
	}()
	MustParseOID("not an oid")
}

// Lexicographic order by arc is the order GETNEXT walks, so it is the
// order an agent must serve its tree in. A prefix sorts before what is
// under it.
func TestOIDCompareIsTheWalkOrder(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"1.3.6.1", "1.3.6.1", 0},
		{"1.3.6.1", "1.3.6.2", -1},
		{"1.3.6.2", "1.3.6.1", 1},
		{"1.3.6.1", "1.3.6.1.0", -1},
		{"1.3.6.1.0", "1.3.6.1", 1},
		// 9 before 10: by arc, not by the string the arcs print as.
		{"1.3.6.1.9", "1.3.6.1.10", -1},
	} {
		if got := MustParseOID(tc.a).Compare(MustParseOID(tc.b)); got != tc.want {
			t.Errorf("Compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestOIDHasPrefix(t *testing.T) {
	tree := MustParseOID("1.3.6.1.4.1.1773")
	for _, tc := range []struct {
		oid  string
		want bool
	}{
		{"1.3.6.1.4.1.1773", true},
		{"1.3.6.1.4.1.1773.1.3.200", true},
		{"1.3.6.1.4.1.1774", false},
		{"1.3.6.1.4.1", false}, // shorter than the prefix
		{"1.3.6.1.2.1.1.1.0", false},
	} {
		if got := MustParseOID(tc.oid).HasPrefix(tree); got != tc.want {
			t.Errorf("%s under 1773 = %v, want %v", tc.oid, got, tc.want)
		}
	}
}

// Append must not alias: an agent that appended an instance index onto
// its own table key in place would corrupt the key for the next row.
func TestOIDAppendCopies(t *testing.T) {
	base := MustParseOID("1.3.6.1.2.1.2.2.1.2")
	a := base.Append(1)
	b := base.Append(2)
	if a[len(a)-1] != 1 || b[len(b)-1] != 2 {
		t.Fatalf("a=%s b=%s: Append aliased the base", a, b)
	}
	if base.String() != "1.3.6.1.2.1.2.2.1.2" {
		t.Errorf("the base was modified: %s", base)
	}
}

func TestOIDStringOfNothing(t *testing.T) {
	if got := OID(nil).String(); got != "" {
		t.Errorf("= %q, want the empty string", got)
	}
}

func TestParseOIDWireRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"empty", nil, "empty OID"},
		{"ends mid sub-identifier", []byte{0x2B, 0x8D}, "ends mid sub-identifier"},
		{"a padded sub-identifier", []byte{0x2B, 0x80, 0x7D}, "padded encoding"},
		{"an arc wider than 32 bits", []byte{0x2B, 0x90, 0x80, 0x80, 0x80, 0x80, 0x00}, "overflows"},
		{"more arcs than the SMI allows", append([]byte{0x2B}, bytes.Repeat([]byte{0x01}, MaxOIDLen)...), "more than"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseOID(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// An empty OID encodes to an empty element rather than to arcs that
// were never there.
func TestEncodingAnEmptyOID(t *testing.T) {
	if got := appendOID(nil, nil); !bytes.Equal(got, []byte{tagOID, 0x00}) {
		t.Errorf("= % X", got)
	}
}

// ---------------------------------------------------------------------
// lengths and integers
// ---------------------------------------------------------------------

// The short form stops at 127. Above it the length is itself a
// counted sequence of bytes, which is where a walk of a big table goes
// wrong if it is written only against small ones.
func TestLengthForms(t *testing.T) {
	// Up to the datagram ceiling; a longer one is refused, which has its
	// own test.
	for _, n := range []int{0, 1, 127, 128, 255, 256, 65507} {
		enc := appendLength(nil, n)
		got, size, err := readLength(enc, "test")
		if err != nil {
			t.Fatalf("length %d: %v", n, err)
		}
		if got != n || size != len(enc) {
			t.Errorf("length %d round-tripped as %d in %d/%d bytes", n, got, size, len(enc))
		}
	}
	// And the short form really is used where it fits.
	if enc := appendLength(nil, 127); len(enc) != 1 {
		t.Errorf("127 encoded in %d bytes, want the short form", len(enc))
	}
}

func TestLengthRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"nothing at all", nil, "truncated length"},
		{"indefinite", []byte{0x80}, "indefinite"},
		{"more bytes than a datagram could need", []byte{0x85, 1, 2, 3, 4, 5}, "exceeds any datagram"},
		{"a long form that is not all there", []byte{0x83, 0x01}, "truncated long-form"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := readLength(tc.in, "test"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// A length no datagram could carry is refused before it becomes a slice
// bound, whatever the four bytes decode to.
func TestALengthNoDatagramCouldCarry(t *testing.T) {
	_, _, err := readLength([]byte{0x84, 0xFF, 0xFF, 0xFF, 0xFF}, "test")
	if err == nil || !strings.Contains(err.Error(), "datagram limit") {
		t.Fatalf("= %v, want the size refusal", err)
	}
}

func TestIntegerRoundTrip(t *testing.T) {
	for _, v := range []int64{0, 1, -1, 127, 128, -128, -129, 255, 256,
		2147483647, -2147483648, 9223372036854775807, -9223372036854775808} {
		enc := appendInt(nil, tagInteger, v)
		e, err := readTLV(enc, "test")
		if err != nil {
			t.Fatalf("%d: %v", v, err)
		}
		got, err := parseInt(e.value, "test")
		if err != nil {
			t.Fatalf("%d: %v", v, err)
		}
		if got != v {
			t.Errorf("%d round-tripped as %d (% X)", v, got, enc)
		}
	}
}

// Minimal form: zero is one byte, and a positive value whose top bit is
// set carries the leading zero that keeps it positive.
func TestIntegerIsMinimal(t *testing.T) {
	for _, tc := range []struct {
		v    int64
		want []byte
	}{
		{0, []byte{0x00}},
		{127, []byte{0x7F}},
		{128, []byte{0x00, 0x80}},
		{-1, []byte{0xFF}},
		{-128, []byte{0x80}},
		{-129, []byte{0xFF, 0x7F}},
	} {
		got := appendInt(nil, tagInteger, tc.v)
		want := append([]byte{tagInteger, byte(len(tc.want))}, tc.want...)
		if !bytes.Equal(got, want) {
			t.Errorf("%d = % X, want % X", tc.v, got, want)
		}
	}
}

// Agents pad. A manager that refused a padded integer would refuse the
// devices it exists to poll.
func TestNonMinimalIntegersAreAccepted(t *testing.T) {
	v, err := parseInt([]byte{0x00, 0x00, 0x00, 0x2A}, "test")
	if err != nil || v != 42 {
		t.Fatalf("= %d, %v; want 42", v, err)
	}
}

func TestIntegerRefusals(t *testing.T) {
	if _, err := parseInt(nil, "test"); err == nil {
		t.Error("an empty INTEGER must be refused")
	}
	if _, err := parseInt(make([]byte, 9), "test"); err == nil {
		t.Error("a 9-byte INTEGER must be refused")
	}
}

// Counter64 with the top bit set needs a nine-byte encoding whose first
// byte is the zero that keeps it unsigned — the classic place a naive
// codec turns a large counter negative.
func TestUnsignedRoundTrip(t *testing.T) {
	for _, v := range []uint64{0, 1, 127, 128, 255, 4294967295,
		9223372036854775808, 18446744073709551615} {
		enc := appendUint(nil, tagCounter64, v)
		e, err := readTLV(enc, "test")
		if err != nil {
			t.Fatalf("%d: %v", v, err)
		}
		got, err := parseUint(e.value, "test")
		if err != nil {
			t.Fatalf("%d: %v", v, err)
		}
		if got != v {
			t.Errorf("%d round-tripped as %d (% X)", v, got, enc)
		}
	}
	if enc := appendUint(nil, tagCounter64, 18446744073709551615); enc[1] != 9 || enc[2] != 0 {
		t.Errorf("a full-width Counter64 = % X, want a zero-padded 9 bytes", enc)
	}
}

func TestUnsignedRefusals(t *testing.T) {
	if _, err := parseUint(nil, "test"); err == nil {
		t.Error("an empty unsigned must be refused")
	}
	if _, err := parseUint(append([]byte{0x01}, make([]byte, 8)...), "test"); err == nil {
		t.Error("a 9-byte unsigned without the zero pad must be refused")
	}
	if _, err := parseUint(make([]byte, 10), "test"); err == nil {
		t.Error("a 10-byte unsigned must be refused")
	}
}

func TestTLVRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"nothing", nil, "truncated element"},
		{"a tag with no length", []byte{0x02}, "truncated element"},
		{"the high-tag-number form", []byte{0x1F, 0x81, 0x00}, "high-tag-number"},
		{"a length longer than what follows", []byte{0x02, 0x08, 0x01}, "claims 8 byte"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readTLV(tc.in, "test"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

func TestExpectNamesTheTagItWanted(t *testing.T) {
	if _, err := expect([]byte{tagOctetString, 0x00}, tagInteger, "version"); err == nil ||
		!strings.Contains(err.Error(), "version") {
		t.Fatalf("= %v, want the field named", err)
	}
	if _, err := expect(nil, tagInteger, "version"); err == nil {
		t.Fatal("a truncated element must be refused before the tag is checked")
	}
}

// ---------------------------------------------------------------------
// values
// ---------------------------------------------------------------------

func TestValueRoundTrip(t *testing.T) {
	for _, v := range []Value{
		Int(-42),
		String("Tandberg TV TT1260 Professional MPEG Receiver"),
		Bytes([]byte{0x00, 0xFF}),
		Null(),
		ObjectID(MustParseOID("1.3.6.1.4.1.1773.1.3.200")),
		IPAddress(net.IPv4(10, 6, 255, 110)),
		Counter32(4294967295),
		Gauge32(1),
		TimeTicks(360000),
		Counter64(18446744073709551615),
		Opaque([]byte{0x9F, 0x78}),
		NoSuchObject(),
		NoSuchInstance(),
		EndOfMIBView(),
	} {
		t.Run(v.Type.String(), func(t *testing.T) {
			enc := appendValue(nil, v)
			e, err := readTLV(enc, "test")
			if err != nil {
				t.Fatalf("readTLV: %v", err)
			}
			got, err := parseValue(e)
			if err != nil {
				t.Fatalf("parseValue: %v", err)
			}
			if got.Type != v.Type {
				t.Fatalf("type = %s, want %s", got.Type, v.Type)
			}
			if got.String() != v.String() {
				t.Errorf("value = %s, want %s", got, v)
			}
		})
	}
}

// The three exceptions carry no contents at all: they are a tag and a
// zero length, and an agent that emitted a NULL body instead would be
// telling a manager the object exists.
func TestExceptionsAreTagOnly(t *testing.T) {
	for _, v := range []Value{NoSuchObject(), NoSuchInstance(), EndOfMIBView()} {
		enc := appendValue(nil, v)
		if len(enc) != 2 || enc[1] != 0 {
			t.Errorf("%s = % X, want a tag and a zero length", v.Type, enc)
		}
		if !v.IsException() {
			t.Errorf("%s must report as an exception", v.Type)
		}
	}
	if Int(0).IsException() {
		t.Error("an INTEGER is not an exception")
	}
}

// An IpAddress is four octets. A v6 address has no four-octet form, so
// it becomes a NULL rather than four bytes of something else.
func TestIPAddressIsAlwaysFourOctets(t *testing.T) {
	v := IPAddress(net.IPv4(10, 6, 250, 5))
	if len(v.IP) != 4 {
		t.Errorf("IP = %v (%d bytes), want 4", v.IP, len(v.IP))
	}
	if got := IPAddress(net.ParseIP("fe80::1")); got.Type != TypeNull {
		t.Errorf("a v6 address became %s, want NULL", got.Type)
	}
	// A value whose IP was cleared by hand still encodes four octets.
	enc := appendValue(nil, Value{Type: TypeIPAddress})
	if !bytes.Equal(enc, []byte{tagIPAddress, 4, 0, 0, 0, 0}) {
		t.Errorf("= % X, want four zero octets", enc)
	}
}

// The SMI caps Counter32/Gauge32/TimeTicks at 32 bits; a caller that
// overflowed one must not put a five-byte quantity on the wire.
func TestThirtyTwoBitTypesAreCapped(t *testing.T) {
	enc := appendValue(nil, Value{Type: TypeCounter32, Uint: 1 << 40})
	e, _ := readTLV(enc, "test")
	got, err := parseUint(e.value, "test")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("= %d, want the low 32 bits", got)
	}
}

// A value with no type is the NULL a request carries in its value slot.
func TestTheZeroValueIsANull(t *testing.T) {
	if enc := appendValue(nil, Value{}); !bytes.Equal(enc, []byte{tagNull, 0}) {
		t.Errorf("= % X, want a NULL", enc)
	}
	if enc := appendValue(nil, Value{Type: ValueType(0x7F)}); !bytes.Equal(enc, []byte{tagNull, 0}) {
		t.Errorf("an unknown type = % X, want a NULL", enc)
	}
}

// Binary octet strings are rendered as hex. A MAC address or a status
// bitmap written raw is how a log line eats somebody's terminal.
func TestValueRendering(t *testing.T) {
	for _, tc := range []struct {
		v    Value
		want string
	}{
		{Int(-1), "-1"},
		{Counter64(42), "42"},
		{String("sysDescr"), "sysDescr"},
		{Bytes([]byte{0x00, 0x1B, 0x21}), "001B21"},
		{ObjectID(sysDescr0), "1.3.6.1.2.1.1.1.0"},
		{IPAddress(net.IPv4(10, 6, 250, 5)), "10.6.250.5"},
		{Null(), "NULL"},
		{NoSuchObject(), "noSuchObject"},
		{Value{Type: ValueType(0x7F)}, "unknown(0x7F)"},
	} {
		if got := tc.v.String(); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.v.Type, got, tc.want)
		}
	}
}

func TestValueRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   tlv
		want string
	}{
		{"an INTEGER that is empty", tlv{tag: tagInteger}, "empty integer"},
		{"a Counter32 that is empty", tlv{tag: tagCounter32}, "empty integer"},
		{"an OID that is empty", tlv{tag: tagOID}, "empty OID"},
		{"an IpAddress of the wrong width", tlv{tag: tagIPAddress, value: []byte{1, 2, 3}}, "want 4"},
		{"a NULL with contents", tlv{tag: tagNull, value: []byte{0}}, "NULL with"},
		{"a tag from no SMI", tlv{tag: 0x45}, "unknown value tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseValue(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// A decoded OCTET STRING must not alias the datagram buffer: the read
// loop reuses it, so a varbind pointing into it changes under the caller
// between one packet and the next.
func TestDecodedStringsDoNotAliasTheDatagram(t *testing.T) {
	buf := []byte{tagOctetString, 3, 'a', 'b', 'c'}
	e, err := readTLV(buf, "test")
	if err != nil {
		t.Fatal(err)
	}
	v, err := parseValue(e)
	if err != nil {
		t.Fatal(err)
	}
	copy(buf[2:], "xyz")
	if string(v.Bytes) != "abc" {
		t.Errorf("the value followed the buffer: %q", v.Bytes)
	}
}

// ---------------------------------------------------------------------
// messages
// ---------------------------------------------------------------------

func getRequest(id int32, oids ...OID) Message {
	vbs := make([]VarBind, 0, len(oids))
	for _, o := range oids {
		vbs = append(vbs, VarBind{Name: o, Value: Null()})
	}
	return Message{
		Version: Version2c, Community: "public",
		PDU: &PDU{Type: PDUTypeGet, RequestID: id, VarBinds: vbs},
	}
}

// A v2c GET of sysDescr.0 with community "public" is the first datagram
// any manager sends, and these are its exact bytes.
func TestAGetOfSysDescrIsTheseBytes(t *testing.T) {
	got, err := Encode(getRequest(1, sysDescr0))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x30, 0x26, // SEQUENCE, 38 bytes
		0x02, 0x01, 0x01, // version 1 = v2c
		0x04, 0x06, 'p', 'u', 'b', 'l', 'i', 'c', // community
		0xA0, 0x19, // GetRequest, 25 bytes
		0x02, 0x01, 0x01, // request-id 1
		0x02, 0x01, 0x00, // error-status noError
		0x02, 0x01, 0x00, // error-index 0
		0x30, 0x0E, // varbind list
		0x30, 0x0C, // varbind
		0x06, 0x08, 0x2B, 0x06, 0x01, 0x02, 0x01, 0x01, 0x01, 0x00, // sysDescr.0
		0x05, 0x00, // NULL
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("\n got % X\nwant % X", got, want)
	}
}

func TestMessageRoundTrip(t *testing.T) {
	for _, m := range []Message{
		getRequest(1, sysDescr0),
		{Version: Version1, Community: "public",
			PDU: &PDU{Type: PDUTypeGetNext, RequestID: 2,
				VarBinds: []VarBind{{Name: MustParseOID("1.3.6.1.4.1.1773"), Value: Null()}}}},
		{Version: Version2c, Community: "public",
			PDU: &PDU{Type: PDUTypeResponse, RequestID: 3,
				VarBinds: []VarBind{{Name: sysDescr0, Value: String("TT1260")}}}},
		{Version: Version2c, Community: "private",
			PDU: &PDU{Type: PDUTypeSet, RequestID: 4,
				VarBinds: []VarBind{{Name: sysDescr0, Value: Int(7)}}}},
		{Version: Version2c, Community: "public",
			PDU: &PDU{Type: PDUTypeGetBulk, RequestID: 5,
				NonRepeaters: 1, MaxRepetitions: 20,
				VarBinds: []VarBind{{Name: sysDescr0, Value: Null()}}}},
		{Version: Version2c, Community: "public",
			PDU: &PDU{Type: PDUTypeTrapV2, RequestID: 6,
				VarBinds: []VarBind{{Name: sysDescr0, Value: TimeTicks(1)}}}},
		{Version: Version2c, Community: "public",
			PDU: &PDU{Type: PDUTypeInform, RequestID: 7}},
		{Version: Version2c, Community: "public",
			PDU: &PDU{Type: PDUTypeReport, RequestID: 8}},
		// An error response, which is what a manager actually has to
		// read most often.
		{Version: Version1, Community: "public",
			PDU: &PDU{Type: PDUTypeResponse, RequestID: 9,
				ErrorStatus: NoSuchName, ErrorIndex: 1,
				VarBinds: []VarBind{{Name: sysDescr0, Value: Null()}}}},
	} {
		t.Run(m.Version.String()+"/"+m.Type().String(), func(t *testing.T) {
			raw, err := Encode(m)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			back, err := Decode(raw)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if back.Version != m.Version || back.Community != m.Community {
				t.Fatalf("envelope = %s/%q", back.Version, back.Community)
			}
			if back.PDU == nil {
				t.Fatal("no PDU came back")
			}
			assertPDUEqual(t, *back.PDU, *m.PDU)
		})
	}
}

func assertPDUEqual(t *testing.T, got, want PDU) {
	t.Helper()
	if got.Type != want.Type || got.RequestID != want.RequestID {
		t.Errorf("= %s/%d, want %s/%d", got.Type, got.RequestID, want.Type, want.RequestID)
	}
	if want.Type == PDUTypeGetBulk {
		if got.NonRepeaters != want.NonRepeaters || got.MaxRepetitions != want.MaxRepetitions {
			t.Errorf("bulk = %d/%d, want %d/%d",
				got.NonRepeaters, got.MaxRepetitions, want.NonRepeaters, want.MaxRepetitions)
		}
	} else if got.ErrorStatus != want.ErrorStatus || got.ErrorIndex != want.ErrorIndex {
		t.Errorf("error = %s/%d, want %s/%d",
			got.ErrorStatus, got.ErrorIndex, want.ErrorStatus, want.ErrorIndex)
	}
	if len(got.VarBinds) != len(want.VarBinds) {
		t.Fatalf("%d varbinds, want %d", len(got.VarBinds), len(want.VarBinds))
	}
	for i := range want.VarBinds {
		if got.VarBinds[i].Name.Compare(want.VarBinds[i].Name) != 0 {
			t.Errorf("varbind %d name = %s, want %s", i, got.VarBinds[i].Name, want.VarBinds[i].Name)
		}
		if got.VarBinds[i].Value.String() != want.VarBinds[i].Value.String() {
			t.Errorf("varbind %d value = %s, want %s", i,
				got.VarBinds[i].Value, want.VarBinds[i].Value)
		}
	}
}

// GetBulk puts non-repeaters and max-repetitions in the SLOTS
// error-status and error-index occupy. Reading one as the other is a
// bug that looks like a working poll returning a single row, so the
// bytes are pinned.
func TestGetBulkReusesTheErrorSlots(t *testing.T) {
	raw, err := Encode(Message{Version: Version2c, Community: "public",
		PDU: &PDU{Type: PDUTypeGetBulk, RequestID: 1, NonRepeaters: 0, MaxRepetitions: 10}})
	if err != nil {
		t.Fatal(err)
	}
	// ... 0xA5 len 02 01 01 (id) 02 01 00 (non-repeaters) 02 01 0A (max-reps)
	if !bytes.Contains(raw, []byte{0x02, 0x01, 0x01, 0x02, 0x01, 0x00, 0x02, 0x01, 0x0A}) {
		t.Fatalf("% X does not carry 0/10 in the two integer slots", raw)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.PDU.MaxRepetitions != 10 || back.PDU.ErrorStatus != 0 {
		t.Errorf("decoded max-repetitions=%d error-status=%s",
			back.PDU.MaxRepetitions, back.PDU.ErrorStatus)
	}
}

// ---------------------------------------------------------------------
// v1 traps
// ---------------------------------------------------------------------

// The IRDs emit v1 traps and nothing else. The Trap-PDU has five fields
// of its own where an ordinary PDU has three, so this is a separate
// encoder and these are its bytes.
func TestV1TrapRoundTrip(t *testing.T) {
	m := Message{
		Version: Version1, Community: "public",
		TrapV1: &TrapV1{
			Enterprise: MustParseOID("1.3.6.1.4.1.1773.1.3.200"),
			AgentAddr:  net.IPv4(10, 6, 255, 110),
			Generic:    EnterpriseSpecific,
			Specific:   1, // "Signal Lock lost" in the shared alarm namespace
			Timestamp:  360000,
			VarBinds: []VarBind{
				{Name: MustParseOID("1.3.6.1.4.1.1773.1.1.3.1"), Value: Int(1)},
			},
		},
	}

	raw, err := Encode(m)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if got := pduTagOf(t, raw); got != byte(PDUTypeTrapV1) {
		t.Fatalf("PDU tag = 0x%02X, want 0xA4", got)
	}

	back, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if back.Type() != PDUTypeTrapV1 || back.TrapV1 == nil {
		t.Fatalf("came back as %s", back.Type())
	}
	tr := back.TrapV1
	if tr.Enterprise.Compare(m.TrapV1.Enterprise) != 0 {
		t.Errorf("enterprise = %s", tr.Enterprise)
	}
	if !tr.AgentAddr.Equal(m.TrapV1.AgentAddr) {
		t.Errorf("agent-addr = %s", tr.AgentAddr)
	}
	if tr.Generic != EnterpriseSpecific || tr.Specific != 1 || tr.Timestamp != 360000 {
		t.Errorf("trap = %s/%d at %d", tr.Generic, tr.Specific, tr.Timestamp)
	}
	if len(tr.VarBinds) != 1 || tr.VarBinds[0].Value.Int != 1 {
		t.Errorf("varbinds = %v", tr.VarBinds)
	}
}

// A trap with no agent address at all still carries four octets, because
// the field is an IpAddress and a manager parses it as one.
func TestV1TrapWithNoAgentAddress(t *testing.T) {
	raw, err := Encode(Message{Version: Version1, Community: "public",
		TrapV1: &TrapV1{Enterprise: MustParseOID("1.3.6.1.4.1.1773"), Generic: ColdStart}})
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !back.TrapV1.AgentAddr.Equal(net.IPv4zero) {
		t.Errorf("agent-addr = %s, want 0.0.0.0", back.TrapV1.AgentAddr)
	}
}

// A v1 Trap-PDU inside a v2c message is the commonest way a
// notification silently reaches nobody: v2c managers do not decode tag
// 0xA4 and drop it without a log line. Refused in both directions.
func TestAV1TrapCannotTravelAsV2c(t *testing.T) {
	_, err := Encode(Message{Version: Version2c, Community: "public",
		TrapV1: &TrapV1{Enterprise: MustParseOID("1.3.6.1.4.1.1773")}})
	if err == nil || !strings.Contains(err.Error(), "cannot travel") {
		t.Fatalf("encode = %v, want the refusal", err)
	}

	// Build one by hand and check the decoder refuses it too.
	raw, err := Encode(Message{Version: Version1, Community: "public",
		TrapV1: &TrapV1{Enterprise: MustParseOID("1.3.6.1.4.1.1773")}})
	if err != nil {
		t.Fatal(err)
	}
	raw[4] = 0x01 // version 0 -> 1, leaving the 0xA4 PDU in place
	if _, err := Decode(raw); err == nil || !strings.Contains(err.Error(), "Trap-PDU in a v2c") {
		t.Fatalf("decode = %v, want the refusal", err)
	}
}

// Every field of a Trap-PDU is a place a datagram can stop making sense,
// and each refusal has to name the field rather than the offset — a trap
// arrives unsolicited from a device nobody is looking at, so the log
// line is all anybody will have.
func TestV1TrapDecodeRefusals(t *testing.T) {
	oid := func(o string) []byte { return appendOID(nil, MustParseOID(o)) }
	ip := func(b ...byte) []byte { return appendTLV(nil, tagIPAddress, b) }
	i := func(v int64) []byte { return appendInt(nil, tagInteger, v) }
	ticks := func(v uint64) []byte { return appendUint(nil, tagTimeTicks, v) }
	cat := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	ent := oid("1.3.6.1.4.1.1773")

	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{"nothing at all", nil, "trap enterprise"},
		{"an enterprise that is not an OID", i(1), "trap enterprise"},
		{"an enterprise that will not parse",
			cat(appendTLV(nil, tagOID, []byte{0x2B, 0x81})), "ends mid"},
		{"no agent-addr", cat(ent), "trap agent-addr"},
		{"an agent-addr that is not an IpAddress", cat(ent, i(1)), "trap agent-addr"},
		{"an agent-addr of the wrong width", cat(ent, ip(10, 0, 0)), "want 4"},
		{"no generic-trap", cat(ent, ip(10, 0, 0, 1)), "generic-trap"},
		{"no specific-trap", cat(ent, ip(10, 0, 0, 1), i(6)), "specific-trap"},
		{"no time-stamp", cat(ent, ip(10, 0, 0, 1), i(6), i(1)), "trap time-stamp"},
		{"a time-stamp that is not TimeTicks",
			cat(ent, ip(10, 0, 0, 1), i(6), i(1), i(0)), "trap time-stamp"},
		{"a time-stamp that will not parse",
			cat(ent, ip(10, 0, 0, 1), i(6), i(1), appendTLV(nil, tagTimeTicks, nil)),
			"trap time-stamp"},
		{"no varbind list",
			cat(ent, ip(10, 0, 0, 1), i(6), i(1), ticks(1)), "variable-bindings"},
		{"a varbind list that will not parse",
			cat(ent, ip(10, 0, 0, 1), i(6), i(1), ticks(1),
				appendTLV(nil, tagSequence, []byte{0x02, 0x01, 0x00})), "variable-binding: tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTrapV1(tlv{tag: byte(PDUTypeTrapV1), value: tc.body})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}

	// And end to end, so the refusal really does travel out of Decode.
	good, err := Encode(Message{Version: Version1, Community: "p",
		TrapV1: &TrapV1{Enterprise: MustParseOID("1.3.6.1.4.1.1773"), Timestamp: 1}})
	if err != nil {
		t.Fatal(err)
	}
	good[10] = tagInteger // the enterprise's tag
	_, err = Decode(good)
	if err == nil || !strings.Contains(err.Error(), "trap enterprise") {
		t.Fatalf("= %v, want the refusal from Decode", err)
	}
	if !errors.Is(err, ErrMalformed) {
		t.Errorf("must be an ErrMalformed: %v", err)
	}
}

// envelope wraps a raw PDU element in a message, so a test can put a PDU
// on the wire that no encoder here would build.
func envelope(version Version, community string, pdu []byte) []byte {
	var body []byte
	body = appendInt(body, tagInteger, int64(version))
	body = appendTLV(body, tagOctetString, []byte(community))
	body = append(body, pdu...)
	return appendTLV(nil, tagSequence, body)
}

// The PDU element itself is the last place framing can fail, and each
// way it fails has to survive the envelope rather than panic inside it.
func TestPDUElementRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		pdu  []byte
		want string
	}{
		{"a PDU of one byte", []byte{0xA0}, "PDU: truncated element"},
		{"a PDU with an indefinite length", []byte{0xA0, 0x80}, "PDU: indefinite length"},
		{"a PDU whose body is not there", []byte{0xA0, 0x08, 0x01}, "PDU: element"},
		{"a PDU with an empty body", []byte{0xA0, 0x00}, "request-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(envelope(Version2c, "public", tc.pdu))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------
// encode refusals
// ---------------------------------------------------------------------

func TestEncodeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    Message
		want string
	}{
		{"no PDU at all", Message{Version: Version2c}, "carries no PDU"},
		{"both a PDU and a trap", Message{Version: Version1,
			PDU:    &PDU{Type: PDUTypeGet},
			TrapV1: &TrapV1{Enterprise: MustParseOID("1.3.6.1")}}, "both a PDU and a v1 trap"},
		{"a version nobody speaks", Message{Version: Version(7),
			PDU: &PDU{Type: PDUTypeGet}}, "unknown version"},
		{"v3", Message{Version: Version3, PDU: &PDU{Type: PDUTypeGet}}, "not built by this package yet"},
		{"GetBulk to a v1 agent", Message{Version: Version1,
			PDU: &PDU{Type: PDUTypeGetBulk}}, "not a v1 PDU"},
		{"an SNMPv2-Trap to a v1 manager", Message{Version: Version1,
			PDU: &PDU{Type: PDUTypeTrapV2}}, "not a v1 PDU"},
		{"a v1 trap in the wrong field", Message{Version: Version1,
			PDU: &PDU{Type: PDUTypeTrapV1}}, "belongs in Message.TrapV1"},
		{"a PDU type from no RFC", Message{Version: Version2c,
			PDU: &PDU{Type: PDUType(0xAF)}}, "unknown PDU type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Encode(tc.m); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// A message too large for a datagram is refused here rather than by a
// sendto that reports EMSGSIZE from three layers down.
func TestAMessageTooLargeForADatagram(t *testing.T) {
	huge := make([]VarBind, 0, 4096)
	for i := 0; i < 4096; i++ {
		huge = append(huge, VarBind{Name: sysDescr0, Value: Bytes(make([]byte, 32))})
	}
	_, err := Encode(Message{Version: Version2c, Community: "public",
		PDU: &PDU{Type: PDUTypeResponse, VarBinds: huge}})
	if err == nil || !strings.Contains(err.Error(), "datagram limit") {
		t.Fatalf("= %v, want the size refusal", err)
	}
}

// A message with nothing in it reports type zero rather than panicking,
// which is what a caller inspecting a half-built message gets.
func TestTypeOfAnEmptyMessage(t *testing.T) {
	if got := (Message{}).Type(); got != 0 {
		t.Errorf("= %s, want the zero type", got)
	}
}

// ---------------------------------------------------------------------
// decode refusals
// ---------------------------------------------------------------------

func TestDecodeRefusals(t *testing.T) {
	good, err := Encode(getRequest(1, sysDescr0))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"nothing", nil, "truncated element"},
		{"not a SEQUENCE", []byte{0x02, 0x01, 0x00}, "message: tag"},
		{"trailing rubbish", append(append([]byte(nil), good...), 0xFF),
			"trailing byte(s) after the message"},
		{"truncated mid-message", good[:len(good)-4], "claims"},
		{"a version that is not an integer", func() []byte {
			b := append([]byte(nil), good...)
			b[2] = tagOctetString
			return b
		}(), "version: tag"},
		{"v3", func() []byte {
			b := append([]byte(nil), good...)
			b[4] = 0x03
			return b
		}(), "not read by this package yet"},
		{"a version nobody speaks", func() []byte {
			b := append([]byte(nil), good...)
			b[4] = 0x09
			return b
		}(), "unknown version"},
		{"a community that is not an OCTET STRING", func() []byte {
			b := append([]byte(nil), good...)
			b[5] = tagInteger
			return b
		}(), "community: tag"},
		{"a PDU tag from no RFC", func() []byte {
			b := append([]byte(nil), good...)
			b[13] = 0xAF
			return b
		}(), "unknown PDU type"},
		{"a GetBulk claiming to be v1", func() []byte {
			b := append([]byte(nil), good...)
			b[4], b[13] = 0x00, byte(PDUTypeGetBulk)
			return b
		}(), "not a v1 PDU"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
			if !errors.Is(err, ErrMalformed) {
				t.Errorf("every decode failure is an ErrMalformed: %v", err)
			}
		})
	}
}

// Bytes after the PDU inside the message envelope are as suspicious as
// bytes after the message, and are refused the same way.
func TestTrailingBytesInsideTheEnvelope(t *testing.T) {
	good, err := Encode(getRequest(1, sysDescr0))
	if err != nil {
		t.Fatal(err)
	}
	// Append a stray NULL inside the outer SEQUENCE by growing its
	// length and the buffer together.
	b := append(append([]byte(nil), good...), tagNull, 0x00)
	b[1] += 2
	if _, err := Decode(b); err == nil || !strings.Contains(err.Error(), "after the PDU") {
		t.Fatalf("= %v, want the trailing-bytes refusal", err)
	}
}

// The PDU body is decoded field by field in a fixed order, and each
// field is a place a truncated datagram can stop.
func TestPDUFieldRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{"no request-id", nil, "request-id"},
		{"no error-status", []byte{0x02, 0x01, 0x01}, "error-status"},
		{"no error-index", []byte{0x02, 0x01, 0x01, 0x02, 0x01, 0x00}, "error-index"},
		{"no varbind list", []byte{0x02, 0x01, 0x01, 0x02, 0x01, 0x00, 0x02, 0x01, 0x00},
			"variable-bindings"},
		{"a request-id that is not an integer", []byte{0x04, 0x01, 0x01}, "request-id"},
		{"a request-id too wide to be one", []byte{0x02, 0x09, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			"request-id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePDU(tlv{tag: byte(PDUTypeGet), value: tc.body})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

func TestVarBindRefusals(t *testing.T) {
	vb := func(inner ...byte) []byte {
		return appendTLV(nil, tagSequence, appendTLV(nil, tagSequence, inner))
	}
	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"the list is not a SEQUENCE", []byte{0x02, 0x01, 0x00}, "variable-bindings: tag"},
		{"trailing bytes after the list",
			append(appendTLV(nil, tagSequence, nil), 0xFF, 0xFF), "trailing byte"},
		{"a binding that is not a SEQUENCE",
			appendTLV(nil, tagSequence, []byte{0x02, 0x01, 0x00}), "variable-binding: tag"},
		{"a name that is not an OID", vb(0x02, 0x01, 0x00), "name: tag"},
		{"a name that is not a valid OID", vb(0x06, 0x02, 0x2B, 0x81), "ends mid"},
		{"no value at all", vb(0x06, 0x01, 0x2B), "truncated element"},
		{"a value that will not parse", vb(0x06, 0x01, 0x2B, 0x45, 0x00), "unknown value tag"},
		{"bytes after the value", vb(0x06, 0x01, 0x2B, 0x05, 0x00, 0x05, 0x00), "after its value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseVarBinds(tc.in)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------
// naming
// ---------------------------------------------------------------------

func TestNames(t *testing.T) {
	for _, tc := range []struct {
		got, want string
	}{
		{Version1.String(), "v1"},
		{Version2c.String(), "v2c"},
		{Version3.String(), "v3"},
		{Version(9).String(), "version(9)"},
		{PDUTypeGet.String(), "GetRequest"},
		{PDUTypeGetNext.String(), "GetNextRequest"},
		{PDUTypeResponse.String(), "Response"},
		{PDUTypeSet.String(), "SetRequest"},
		{PDUTypeTrapV1.String(), "Trap"},
		{PDUTypeGetBulk.String(), "GetBulkRequest"},
		{PDUTypeInform.String(), "InformRequest"},
		{PDUTypeTrapV2.String(), "SNMPv2-Trap"},
		{PDUTypeReport.String(), "Report"},
		{PDUType(0xAF).String(), "unknown(0xAF)"},
		{NoError.String(), "noError"},
		{NoSuchName.String(), "noSuchName"},
		{InconsistentName.String(), "inconsistentName"},
		{ErrorStatus(99).String(), "errorStatus(99)"},
		{ErrorStatus(-1).String(), "errorStatus(-1)"},
		{ColdStart.String(), "coldStart"},
		{EnterpriseSpecific.String(), "enterpriseSpecific"},
		{GenericTrap(9).String(), "genericTrap(9)"},
		{GenericTrap(-1).String(), "genericTrap(-1)"},
		{TypeInteger.String(), "INTEGER"},
		{TypeOctetString.String(), "OCTET STRING"},
		{TypeNull.String(), "NULL"},
		{TypeOID.String(), "OBJECT IDENTIFIER"},
		{TypeIPAddress.String(), "IpAddress"},
		{TypeCounter32.String(), "Counter32"},
		{TypeGauge32.String(), "Gauge32"},
		{TypeTimeTicks.String(), "TimeTicks"},
		{TypeOpaque.String(), "Opaque"},
		{TypeCounter64.String(), "Counter64"},
		{TypeNoSuchObject.String(), "noSuchObject"},
		{TypeNoSuchInst.String(), "noSuchInstance"},
		{TypeEndOfMIBView.String(), "endOfMibView"},
	} {
		if tc.got != tc.want {
			t.Errorf("= %q, want %q", tc.got, tc.want)
		}
	}
}

// pduTagOf walks the message envelope to the PDU's tag byte, so a test
// can assert what a peer's parser will dispatch on without counting
// bytes by hand.
func pduTagOf(t *testing.T, raw []byte) byte {
	t.Helper()
	outer, err := readTLV(raw, "message")
	if err != nil {
		t.Fatal(err)
	}
	rest := outer.value
	for _, what := range []string{"version", "community"} {
		e, err := readTLV(rest, what)
		if err != nil {
			t.Fatal(err)
		}
		rest = rest[e.size:]
	}
	if len(rest) == 0 {
		t.Fatal("the message carries no PDU")
	}
	return rest[0]
}
