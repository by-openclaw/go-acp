package codec

// v3 replaces the community string with a header, an opaque
// security-parameters blob and a scoped PDU. This package builds and
// reads that shape and holds no keys, so what is asserted here is the
// framing and the two offsets a security model cannot work without.

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// cat3 joins byte slices, so a test can hand-build an element from parts.
func cat3(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func v3Get(id int32) Message {
	return Message{
		Version: Version3,
		V3: &V3{
			ID: id, MaxSize: DefaultMaxSize, Flags: FlagReportable,
			SecurityModel:      SecurityModelUSM,
			SecurityParameters: []byte("opaque-to-this-package"),
			ContextEngineID:    []byte("engine"),
			ContextName:        "",
		},
		PDU: &PDU{Type: PDUTypeGet, RequestID: id,
			VarBinds: []VarBind{{Name: sysDescr0, Value: Null()}}},
	}
}

func TestV3RoundTrip(t *testing.T) {
	m := v3Get(7)
	raw, err := Encode(m)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	back, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if back.Version != Version3 || back.V3 == nil {
		t.Fatalf("came back as %s", back.Version)
	}
	v := back.V3
	if v.ID != 7 || v.MaxSize != DefaultMaxSize || v.SecurityModel != SecurityModelUSM {
		t.Errorf("header = %+v", v)
	}
	if !v.Flags.Reportable() || v.Flags.Auth() || v.Flags.Priv() {
		t.Errorf("flags = %s / reportable=%v", v.Flags.SecurityLevel(), v.Flags.Reportable())
	}
	if string(v.SecurityParameters) != "opaque-to-this-package" {
		t.Errorf("security parameters = %q", v.SecurityParameters)
	}
	if string(v.ContextEngineID) != "engine" {
		t.Errorf("contextEngineID = %q", v.ContextEngineID)
	}
	if back.PDU == nil || back.PDU.Type != PDUTypeGet || back.PDU.RequestID != 7 {
		t.Fatalf("PDU = %+v", back.PDU)
	}
	if back.Type() != PDUTypeGet {
		t.Errorf("Type() = %s", back.Type())
	}
}

// The defaults are filled in, so a caller that only knows what it is
// asking still produces a message a peer accepts.
func TestV3Defaults(t *testing.T) {
	m := Message{
		Version: Version3,
		V3:      &V3{ID: 1},
		PDU:     &PDU{Type: PDUTypeGet, RequestID: 1},
	}
	raw, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.V3.MaxSize != DefaultMaxSize {
		t.Errorf("msgMaxSize = %d, want the default", back.V3.MaxSize)
	}
	if back.V3.SecurityModel != SecurityModelUSM {
		t.Errorf("security model = %d, want USM", back.V3.SecurityModel)
	}
}

// Both offsets exist because a USM digest is computed over the whole
// message with the authentication field zeroed and then written back
// into it. If either is wrong the digest lands in the wrong place and
// every peer rejects every message.
func TestTheOffsetsPointWhereTheySay(t *testing.T) {
	params := USMParameters{
		AuthoritativeEngineID:    []byte("an-engine-id"),
		AuthoritativeEngineBoots: 3,
		AuthoritativeEngineTime:  99,
		UserName:                 "operator",
		AuthenticationParameters: make([]byte, 24),
		PrivacyParameters:        []byte("saltsalt"),
	}
	secRaw, authInSec := EncodeUSMParameters(params)

	// The offset inside the blob points at the zeroed field.
	if got := secRaw[authInSec : authInSec+24]; !bytes.Equal(got, make([]byte, 24)) {
		t.Fatalf("the offset inside the blob points at % x", got)
	}

	m := v3Get(1)
	m.V3.SecurityParameters = secRaw
	raw, secOffset, err := EncodeV3(m)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw[secOffset:secOffset+len(secRaw)], secRaw) {
		t.Fatal("the security-parameters offset does not point at them")
	}
	// And the two add up to the field in the finished datagram.
	if got := raw[secOffset+authInSec : secOffset+authInSec+24]; !bytes.Equal(got, make([]byte, 24)) {
		t.Errorf("the combined offset points at % x", got)
	}

	// Decoding reports the same place, computed from what arrived rather
	// than from a re-encode.
	back, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.V3.SecurityParametersOffset != secOffset {
		t.Errorf("decoded offset = %d, encoded %d",
			back.V3.SecurityParametersOffset, secOffset)
	}
}

// A long security-parameters blob crosses the point where BER needs a
// multi-byte length, which is where an offset computed by counting goes
// wrong.
func TestTheOffsetSurvivesALongBlob(t *testing.T) {
	m := v3Get(1)
	m.V3.SecurityParameters = bytes.Repeat([]byte{0xAB}, 300)
	raw, secOffset, err := EncodeV3(m)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw[secOffset:secOffset+300], m.V3.SecurityParameters) {
		t.Error("the offset is wrong once the length needs two bytes")
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.V3.SecurityParametersOffset != secOffset {
		t.Errorf("decoded offset = %d, encoded %d",
			back.V3.SecurityParametersOffset, secOffset)
	}
}

// An encrypted scoped PDU travels as an OCTET STRING rather than a
// SEQUENCE, which is how a receiver knows to decrypt before it parses —
// and this package stops there, because it holds no keys.
func TestAnEncryptedScopedPDUStopsAtTheCiphertext(t *testing.T) {
	m := v3Get(1)
	m.V3.Flags = FlagAuth | FlagPriv
	m.V3.EncryptedPDU = []byte("ciphertext, as far as anybody here knows")
	m.PDU = nil

	raw, _, err := EncodeV3(m)
	if err != nil {
		t.Fatalf("EncodeV3: %v", err)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if back.PDU != nil {
		t.Error("an encrypted message has no readable PDU")
	}
	if string(back.V3.EncryptedPDU) != string(m.V3.EncryptedPDU) {
		t.Errorf("ciphertext = %q", back.V3.EncryptedPDU)
	}
	if back.Type() != 0 {
		t.Errorf("Type() = %s, want nothing knowable", back.Type())
	}
}

// A scoped PDU round-trips on its own, which is the form a security
// model encrypts and decrypts.
func TestScopedPDURoundTrip(t *testing.T) {
	in := ScopedPDU{
		ContextEngineID: []byte("engine"),
		ContextName:     "bridge1",
		PDU:             &PDU{Type: PDUTypeTrapV2, RequestID: 4},
	}
	raw, err := EncodeScopedPDU(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeScopedPDU(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(out.ContextEngineID) != "engine" || out.ContextName != "bridge1" {
		t.Errorf("scope = %q/%q", out.ContextEngineID, out.ContextName)
	}
	if out.PDU == nil || out.PDU.Type != PDUTypeTrapV2 {
		t.Errorf("PDU = %+v", out.PDU)
	}
}

// Both ciphers pad to a block boundary, and the padding lands after the
// SEQUENCE with no length that describes it. Refusing it would refuse
// every encrypted message whose plaintext is not a multiple of eight.
func TestAScopedPDUToleratesCipherPadding(t *testing.T) {
	raw, err := EncodeScopedPDU(ScopedPDU{PDU: &PDU{Type: PDUTypeGet}})
	if err != nil {
		t.Fatal(err)
	}
	padded := append(append([]byte(nil), raw...), 0, 0, 0, 0, 0, 0, 0)
	if _, err := DecodeScopedPDU(padded); err != nil {
		t.Errorf("padded plaintext refused: %v", err)
	}
}

func TestScopedPDURefusals(t *testing.T) {
	if _, err := EncodeScopedPDU(ScopedPDU{}); err == nil ||
		!strings.Contains(err.Error(), "carries no PDU") {
		t.Errorf("= %v, want the refusal", err)
	}
	// A v1 Trap-PDU has no place in a v3 message.
	if _, err := EncodeScopedPDU(ScopedPDU{PDU: &PDU{Type: PDUTypeTrapV1}}); err == nil {
		t.Error("a v1 trap cannot be scoped")
	}

	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"not a SEQUENCE", []byte{0x02, 0x01, 0x00}, "scoped PDU: tag"},
		{"no contextEngineID", appendTLV(nil, tagSequence, []byte{0x02, 0x01, 0x00}),
			"contextEngineID"},
		{"no contextName", appendTLV(nil, tagSequence,
			appendTLV(nil, tagOctetString, []byte("e"))), "contextName"},
		{"no PDU", appendTLV(nil, tagSequence, append(
			appendTLV(nil, tagOctetString, []byte("e")),
			appendTLV(nil, tagOctetString, nil)...)), "PDU"},
		{"a PDU that is not a v3 PDU", appendTLV(nil, tagSequence, cat3(
			appendTLV(nil, tagOctetString, []byte("e")),
			appendTLV(nil, tagOctetString, nil),
			appendTLV(nil, byte(PDUTypeTrapV1), nil))), "belongs in Message.TrapV1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeScopedPDU(tc.in); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

func TestV3EncodeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*Message)
		want string
	}{
		{"no header at all", func(m *Message) { m.V3 = nil }, "needs its V3 header"},
		{"privacy without authentication",
			func(m *Message) { m.V3.Flags = FlagPriv }, "not a security level"},
		{"ciphertext in a message that does not claim privacy", func(m *Message) {
			m.V3.EncryptedPDU = []byte("x")
		}, "encrypted scoped PDU in a noAuthNoPriv"},
		{"privacy with no ciphertext", func(m *Message) {
			m.V3.Flags = FlagAuth | FlagPriv
		}, "no ciphertext"},
		{"a PDU the scope cannot carry", func(m *Message) {
			m.PDU = &PDU{Type: PDUTypeTrapV1}
		}, "belongs in Message.TrapV1"},
		{"no PDU at all", func(m *Message) { m.PDU = nil }, "carries no PDU"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := v3Get(1)
			tc.mut(&m)
			if _, _, err := EncodeV3(m); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}

	// And a message too large for a datagram.
	m := v3Get(1)
	for i := 0; i < 4096; i++ {
		m.PDU.VarBinds = append(m.PDU.VarBinds,
			VarBind{Name: sysDescr0, Value: Bytes(make([]byte, 32))})
	}
	if _, _, err := EncodeV3(m); err == nil || !strings.Contains(err.Error(), "datagram limit") {
		t.Errorf("= %v, want the size refusal", err)
	}
}

func TestV3DecodeRefusals(t *testing.T) {
	good, err := Encode(v3Get(1))
	if err != nil {
		t.Fatal(err)
	}

	// header is the msgGlobalData SEQUENCE, rebuilt from parts so each
	// field can be removed in turn.
	envelope := func(header, sec, data []byte) []byte {
		var body []byte
		body = appendInt(body, tagInteger, int64(Version3))
		body = appendTLV(body, tagSequence, header)
		if sec != nil {
			body = append(body, sec...)
		}
		body = append(body, data...)
		return appendTLV(nil, tagSequence, body)
	}
	i := func(v int64) []byte { return appendInt(nil, tagInteger, v) }
	octets := func(b ...byte) []byte { return appendTLV(nil, tagOctetString, b) }
	cat := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	fullHeader := cat(i(1), i(int64(DefaultMaxSize)), octets(0), i(int64(SecurityModelUSM)))

	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"no msgGlobalData", func() []byte {
			b := append([]byte(nil), good...)
			b[5] = tagOctetString
			return b
		}(), "msgGlobalData"},
		{"no msgID", envelope(nil, octets(), nil), "msgID"},
		{"no msgMaxSize", envelope(i(1), octets(), nil), "msgMaxSize"},
		{"no msgFlags", envelope(cat(i(1), i(484)), octets(), nil), "msgFlags"},
		{"msgFlags of the wrong width",
			envelope(cat(i(1), i(484), octets(0, 0), i(3)), octets(), nil), "msgFlags of 2 bytes"},
		{"no msgSecurityModel",
			envelope(cat(i(1), i(484), octets(0)), octets(), nil), "msgSecurityModel"},
		{"trailing bytes in msgGlobalData",
			envelope(cat(fullHeader, octets(9)), octets(), nil), "trailing byte(s) in msgGlobalData"},
		{"no msgSecurityParameters",
			envelope(fullHeader, i(1), nil), "msgSecurityParameters"},
		{"no msgData", envelope(fullHeader, octets(), nil), "msgData"},
		{"trailing bytes after msgData",
			envelope(fullHeader, octets(), cat(octets(1), octets(2))), "after msgData"},
		{"ciphertext in a message that does not claim privacy",
			envelope(fullHeader, octets(), octets(1, 2, 3)),
			"encrypted scoped PDU in a noAuthNoPriv"},
		{"a plaintext scoped PDU in an authPriv message",
			envelope(cat(i(1), i(484), octets(byte(FlagAuth|FlagPriv)), i(3)), octets(),
				appendTLV(nil, tagSequence, nil)), "plaintext scoped PDU"},
		{"a scoped PDU that will not parse",
			envelope(fullHeader, octets(), appendTLV(nil, tagSequence, nil)),
			"contextEngineID"},
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

func TestFlagNaming(t *testing.T) {
	for _, tc := range []struct {
		f    MsgFlags
		want string
	}{
		{0, "noAuthNoPriv"},
		{FlagReportable, "noAuthNoPriv"},
		{FlagAuth, "authNoPriv"},
		{FlagAuth | FlagPriv, "authPriv"},
	} {
		if got := tc.f.SecurityLevel(); got != tc.want {
			t.Errorf("%08b = %q, want %q", byte(tc.f), got, tc.want)
		}
	}
	if !(FlagAuth | FlagReportable).Reportable() {
		t.Error("reportable is its own bit")
	}
}

// ---------------------------------------------------------------------
// the USM parameters blob
// ---------------------------------------------------------------------

func TestUSMParametersRoundTrip(t *testing.T) {
	in := USMParameters{
		AuthoritativeEngineID:    []byte{0x80, 0x00, 0x7E, 0xD9, 0x05, 'd', 'h', 's'},
		AuthoritativeEngineBoots: 12,
		AuthoritativeEngineTime:  3600,
		UserName:                 "operator",
		AuthenticationParameters: bytes.Repeat([]byte{0xAA}, 12),
		PrivacyParameters:        []byte("saltsalt"),
	}
	raw, authOffset := EncodeUSMParameters(in)

	if !bytes.Equal(raw[authOffset:authOffset+12], in.AuthenticationParameters) {
		t.Fatalf("the encode offset points at % x", raw[authOffset:authOffset+12])
	}

	out, decodedOffset, err := DecodeUSMParameters(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decodedOffset != authOffset {
		t.Errorf("decoded offset %d, encoded %d", decodedOffset, authOffset)
	}
	if !bytes.Equal(out.AuthoritativeEngineID, in.AuthoritativeEngineID) ||
		out.AuthoritativeEngineBoots != in.AuthoritativeEngineBoots ||
		out.AuthoritativeEngineTime != in.AuthoritativeEngineTime ||
		out.UserName != in.UserName ||
		!bytes.Equal(out.AuthenticationParameters, in.AuthenticationParameters) ||
		!bytes.Equal(out.PrivacyParameters, in.PrivacyParameters) {
		t.Errorf("round trip = %+v", out)
	}
}

// The blob a noAuthNoPriv message carries is all-empty, and its offset
// still has to be right — the digest field is zero bytes long, but the
// place it would go is not nowhere.
func TestUSMParametersWithNothingInThem(t *testing.T) {
	raw, authOffset := EncodeUSMParameters(USMParameters{})
	out, decodedOffset, err := DecodeUSMParameters(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decodedOffset != authOffset || authOffset <= 0 || authOffset > len(raw) {
		t.Errorf("offsets = %d / %d in %d bytes", authOffset, decodedOffset, len(raw))
	}
	if out.UserName != "" || len(out.AuthenticationParameters) != 0 {
		t.Errorf("= %+v", out)
	}
}

func TestUSMParametersDecodeRefusals(t *testing.T) {
	octets := func(b []byte) []byte { return appendTLV(nil, tagOctetString, b) }
	i := func(v int64) []byte { return appendInt(nil, tagInteger, v) }
	cat := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	seq := func(b []byte) []byte { return appendTLV(nil, tagSequence, b) }

	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"not a SEQUENCE", []byte{0x02, 0x01, 0x00}, "usmSecurityParameters: tag"},
		{"trailing bytes after it", append(seq(nil), 0xFF, 0xFF), "trailing byte(s) after"},
		{"no engine ID", seq(i(1)), "msgAuthoritativeEngineID"},
		{"no boots", seq(octets(nil)), "msgAuthoritativeEngineBoots"},
		{"no time", seq(cat(octets(nil), i(1))), "msgAuthoritativeEngineTime"},
		{"no user name", seq(cat(octets(nil), i(1), i(2))), "msgUserName"},
		{"a user name longer than the field",
			seq(cat(octets(nil), i(1), i(2), octets(bytes.Repeat([]byte{'x'}, 33)))),
			"at most 32"},
		{"no authentication parameters",
			seq(cat(octets(nil), i(1), i(2), octets([]byte("u")))),
			"msgAuthenticationParameters"},
		{"no privacy parameters",
			seq(cat(octets(nil), i(1), i(2), octets([]byte("u")), octets(nil))),
			"msgPrivacyParameters"},
		{"trailing bytes inside it",
			seq(cat(octets(nil), i(1), i(2), octets([]byte("u")), octets(nil), octets(nil), i(9))),
			"trailing byte(s) in usmSecurityParameters"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := DecodeUSMParameters(tc.in); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// The scoped PDU's own framing has to be exact even though padding after
// it is tolerated: bytes INSIDE the SEQUENCE that no field describes are
// a different thing from a cipher's block padding outside it.
func TestAScopedPDUWithBytesItCannotAccountFor(t *testing.T) {
	inner := cat3(
		appendTLV(nil, tagOctetString, []byte("e")),
		appendTLV(nil, tagOctetString, nil),
		appendPDU(nil, PDU{Type: PDUTypeGet}),
		appendTLV(nil, tagNull, nil), // one element too many
	)
	if _, err := DecodeScopedPDU(appendTLV(nil, tagSequence, inner)); err == nil ||
		!strings.Contains(err.Error(), "after the scoped PDU's PDU") {
		t.Fatalf("= %v, want the trailing-bytes refusal", err)
	}
}

// A PDU inside the scope that will not parse is reported as such rather
// than as an empty request.
func TestAScopedPDUWhosePDUWillNotParse(t *testing.T) {
	inner := cat3(
		appendTLV(nil, tagOctetString, []byte("e")),
		appendTLV(nil, tagOctetString, nil),
		appendTLV(nil, byte(PDUTypeGet), nil), // no request-id, no anything
	)
	if _, err := DecodeScopedPDU(appendTLV(nil, tagSequence, inner)); err == nil ||
		!strings.Contains(err.Error(), "request-id") {
		t.Fatalf("= %v, want the PDU refusal", err)
	}
}
