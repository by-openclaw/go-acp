package ber

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

// TestClass_String covers the Class.String() stringer for every class
// plus the UNKNOWN default.
func TestClass_String(t *testing.T) {
	cases := map[Class]string{
		ClassUniversal:   "UNIVERSAL",
		ClassApplication: "APPLICATION",
		ClassContext:     "CONTEXT",
		ClassPrivate:     "PRIVATE",
		Class(99):        "UNKNOWN",
	}
	for c, want := range cases {
		if got := c.String(); got != want {
			t.Errorf("Class(%d).String() = %q, want %q", c, got, want)
		}
	}
}

// TestDecodeLength_Errors covers ErrLengthTooLong (>4 length octets) and
// the truncated long-form guard, plus the empty-buffer guard.
func TestDecodeLength_Errors(t *testing.T) {
	if _, _, err := DecodeLength(nil); !errors.Is(err, ErrTruncated) {
		t.Errorf("empty: got %v, want ErrTruncated", err)
	}
	// 0x85 = long form with 5 length octets, > 4 cap.
	if _, _, err := DecodeLength([]byte{0x85, 0, 0, 0, 0, 0}); !errors.Is(err, ErrLengthTooLong) {
		t.Errorf("5-octet length: got %v, want ErrLengthTooLong", err)
	}
	// 0x83 declares 3 length octets but only 1 follows.
	if _, _, err := DecodeLength([]byte{0x83, 0x01}); !errors.Is(err, ErrTruncated) {
		t.Errorf("short long-form: got %v, want ErrTruncated", err)
	}
}

// TestDecodeTag_Errors covers the empty-buffer guard and the long-form
// base128 error propagation.
func TestDecodeTag_Errors(t *testing.T) {
	if _, _, err := DecodeTag(nil); !errors.Is(err, ErrTruncated) {
		t.Errorf("empty: got %v, want ErrTruncated", err)
	}
	// First octet 0x1F (long form) with a dangling continuation byte
	// (high bit set, no terminator) → ErrTruncated from decodeBase128.
	if _, _, err := DecodeTag([]byte{0x1F, 0x80}); !errors.Is(err, ErrTruncated) {
		t.Errorf("long-form dangling: got %v, want ErrTruncated", err)
	}
}

// TestBase128_TagTooLong covers decodeBase128's >4-byte ErrTagTooLong cap
// and encodeBase128's zero-value branch (via a round-trip of tag 0 in
// long form is not reachable, so test encodeBase128 directly through
// EncodeTag of a >30 number and the zero edge).
func TestBase128_TagTooLong(t *testing.T) {
	// Six continuation bytes (all high-bit set) exceed the 5-byte cap.
	long := []byte{0x1F, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80}
	if _, _, err := DecodeTag(long); !errors.Is(err, ErrTagTooLong) {
		t.Errorf("got %v, want ErrTagTooLong", err)
	}
}

// TestEncodeBase128_Zero pins the val==0 branch of encodeBase128. Tag 0
// never takes the long form via EncodeTag, so exercise the helper through
// a Tag whose long-form path runs encodeBase128 and verify the zero edge
// directly.
func TestEncodeBase128_Zero(t *testing.T) {
	if got := encodeBase128(0); len(got) != 1 || got[0] != 0 {
		t.Errorf("encodeBase128(0) = %X, want [00]", got)
	}
	// Multi-byte value round-trips through decode.
	enc := encodeBase128(300)
	v, n, err := decodeBase128(enc)
	if err != nil || v != 300 || n != len(enc) {
		t.Errorf("base128 round-trip 300: v=%d n=%d err=%v", v, n, err)
	}
}

// TestDecodeTLV_Indefinite covers the indefinite-length constructed path:
// children read until the EOC 0x00 0x00 sentinel.
func TestDecodeTLV_Indefinite(t *testing.T) {
	// Constructed SEQUENCE, indefinite length (0x80), one INTEGER child,
	// then EOC.
	child := EncodeTLV(Integer(7))
	buf := []byte{0x30, 0x80}
	buf = append(buf, child...)
	buf = append(buf, 0x00, 0x00) // EOC
	tlv, n, err := DecodeTLV(buf)
	if err != nil {
		t.Fatalf("indefinite decode: %v", err)
	}
	if n != len(buf) {
		t.Errorf("consumed %d, want %d", n, len(buf))
	}
	if len(tlv.Children) != 1 {
		t.Fatalf("children: got %d, want 1", len(tlv.Children))
	}
	if v, _ := DecodeInteger(tlv.Children[0].Value); v != 7 {
		t.Errorf("child value: got %d, want 7", v)
	}
}

// TestDecodeTLV_Errors covers the truncation and child-error branches.
func TestDecodeTLV_Errors(t *testing.T) {
	if _, _, err := DecodeTLV(nil); !errors.Is(err, ErrTruncated) {
		t.Errorf("empty: got %v, want ErrTruncated", err)
	}
	// Definite length claims more bytes than present.
	if _, _, err := DecodeTLV([]byte{0x02, 0x05, 0x01}); !errors.Is(err, ErrTruncated) {
		t.Errorf("short value: got %v, want ErrTruncated", err)
	}
	// Indefinite length that never reaches an EOC (runs out of buffer).
	if _, _, err := DecodeTLV([]byte{0x30, 0x80, 0x02}); !errors.Is(err, ErrTruncated) {
		t.Errorf("indefinite no-EOC: got %v, want ErrTruncated", err)
	}
	// Indefinite length whose child itself is malformed (bad child length).
	if _, _, err := DecodeTLV([]byte{0x30, 0x80, 0x02, 0x05, 0x01, 0x00, 0x00}); err == nil {
		t.Error("indefinite bad child: expected error")
	}
	// Definite constructed whose child is truncated.
	if _, _, err := DecodeTLV([]byte{0x30, 0x02, 0x02, 0x05}); !errors.Is(err, ErrTruncated) {
		t.Errorf("definite bad child: got %v, want ErrTruncated", err)
	}
	// Bad tag (long form dangling) inside DecodeTLV.
	if _, _, err := DecodeTLV([]byte{0x1F, 0x80}); !errors.Is(err, ErrTruncated) {
		t.Errorf("bad tag: got %v, want ErrTruncated", err)
	}
	// Bad length inside DecodeTLV.
	if _, _, err := DecodeTLV([]byte{0x02, 0x85, 0, 0, 0, 0, 0}); !errors.Is(err, ErrLengthTooLong) {
		t.Errorf("bad length: got %v, want ErrLengthTooLong", err)
	}
}

// TestDecodeAll covers the multi-element loop and its error path.
func TestDecodeAll(t *testing.T) {
	buf := append(EncodeTLV(Integer(1)), EncodeTLV(Integer(2))...)
	tlvs, err := DecodeAll(buf)
	if err != nil {
		t.Fatalf("DecodeAll: %v", err)
	}
	if len(tlvs) != 2 {
		t.Fatalf("got %d elements, want 2", len(tlvs))
	}
	// Error path: trailing malformed element.
	bad := append(EncodeTLV(Integer(1)), 0x02, 0x05, 0x01)
	if _, err := DecodeAll(bad); !errors.Is(err, ErrTruncated) {
		t.Errorf("DecodeAll bad tail: got %v, want ErrTruncated", err)
	}
}

// TestConstructors covers the OctetStr / Set / ContextPrimitive / RelOID
// helper constructors that produce the expected tag shapes.
func TestConstructors(t *testing.T) {
	if o := OctetStr([]byte{0xDE, 0xAD}); o.Tag.Number != TagOctetString || o.Tag.Constructed {
		t.Errorf("OctetStr tag wrong: %+v", o.Tag)
	}
	if s := Set(Integer(1)); s.Tag.Number != TagSet || !s.Tag.Constructed {
		t.Errorf("Set tag wrong: %+v", s.Tag)
	}
	if c := ContextPrimitive(3, []byte{0x01}); c.Tag.Class != ClassContext || c.Tag.Constructed || c.Tag.Number != 3 {
		t.Errorf("ContextPrimitive tag wrong: %+v", c.Tag)
	}
	if r := RelOID([]byte{0x01, 0x02}); r.Tag.Number != TagRelativeOID || r.Tag.Constructed {
		t.Errorf("RelOID tag wrong: %+v", r.Tag)
	}
}

// TestEncodeOctetString covers the OCTET STRING value encoder, including
// that it copies (does not alias) the input slice.
func TestEncodeOctetString(t *testing.T) {
	in := []byte{0x01, 0x02, 0x03}
	out := EncodeOctetString(in)
	if !bytes.Equal(out, in) {
		t.Fatalf("EncodeOctetString = %X, want %X", out, in)
	}
	in[0] = 0xFF
	if out[0] == 0xFF {
		t.Error("EncodeOctetString aliased the input slice")
	}
}

// TestEncodeReal_Specials covers the NaN / +Inf / -Inf / zero sentinels.
func TestEncodeReal_Specials(t *testing.T) {
	if got := EncodeReal(0); got != nil {
		t.Errorf("EncodeReal(0) = %X, want nil", got)
	}
	if got := EncodeReal(math.NaN()); len(got) != 1 || got[0] != 0x42 {
		t.Errorf("EncodeReal(NaN) = %X, want [42]", got)
	}
	if got := EncodeReal(math.Inf(1)); len(got) != 1 || got[0] != 0x40 {
		t.Errorf("EncodeReal(+Inf) = %X, want [40]", got)
	}
	if got := EncodeReal(math.Inf(-1)); len(got) != 1 || got[0] != 0x41 {
		t.Errorf("EncodeReal(-Inf) = %X, want [41]", got)
	}
}

// TestReal_SubnormalRoundTrip covers the subnormal mantissa branch in
// EncodeReal (expRaw == 0) and its decode mirror.
func TestReal_SubnormalRoundTrip(t *testing.T) {
	// Smallest positive subnormal double.
	v := math.SmallestNonzeroFloat64
	got, err := DecodeReal(EncodeReal(v))
	if err != nil {
		t.Fatalf("decode subnormal: %v", err)
	}
	if got != v {
		t.Errorf("subnormal round-trip: got %v, want %v", got, v)
	}
}

// TestReal_LongExponentRoundTrip covers the multi-octet exponent encoding
// branches (2- and 3-octet) and the decode side that reads them.
func TestReal_LongExponentRoundTrip(t *testing.T) {
	// Very large and very small magnitudes force >1-octet exponents.
	for _, v := range []float64{1e300, 1e-300, -1e250, 5e-200} {
		got, err := DecodeReal(EncodeReal(v))
		if err != nil {
			t.Fatalf("decode %v: %v", v, err)
		}
		// Allow tiny float rounding from the base-2 decompose.
		if math.Abs(got-v)/math.Abs(v) > 1e-12 {
			t.Errorf("round-trip %v: got %v", v, got)
		}
	}
}

// TestDecodeReal_Bases covers the base-8 and base-16 mantissa-scaling
// branches plus the scale-factor branch on the decode path. We hand-build
// the value octets since EncodeReal only emits base 2.
func TestDecodeReal_Bases(t *testing.T) {
	// Base 2, exp=1, mantissa=1 → 1 * 2^(1*1 + 0 - 0) = 2.0
	if got, err := DecodeReal([]byte{0x80, 0x01, 0x01}); err != nil || got != 2.0 {
		t.Errorf("base2: got %v err %v, want 2.0", got, err)
	}
	// Base 8 (bits 5-4 = 01 → first |= 0x10), exp=1, mant=1
	//   value = 1 * 2^(1*3 + 0 - 0) = 8.0
	if got, err := DecodeReal([]byte{0x90, 0x01, 0x01}); err != nil || got != 8.0 {
		t.Errorf("base8: got %v err %v, want 8.0", got, err)
	}
	// Base 16 (bits 5-4 = 10 → first |= 0x20), exp=1, mant=1
	//   value = 1 * 2^(1*4 + 0 - 0) = 16.0
	if got, err := DecodeReal([]byte{0xA0, 0x01, 0x01}); err != nil || got != 16.0 {
		t.Errorf("base16: got %v err %v, want 16.0", got, err)
	}
	// Scale factor (bits 3-2 = 01 → first |= 0x04), base2, exp=0, mant=1
	//   value = 1 * 2^(0 + 1 - 0) = 2.0
	if got, err := DecodeReal([]byte{0x84, 0x00, 0x01}); err != nil || got != 2.0 {
		t.Errorf("scale: got %v err %v, want 2.0", got, err)
	}
	// Reserved base (bits 5-4 = 11) is rejected.
	if _, err := DecodeReal([]byte{0xB0, 0x01, 0x01}); !errors.Is(err, ErrInvalidReal) {
		t.Errorf("reserved base: want ErrInvalidReal, got %v", err)
	}
}

// TestDecodeReal_LongExpOctet covers the long-form exponent-length branch
// (first-octet bits 1-0 = 11) on the decode path, including its guards.
func TestDecodeReal_LongExpOctet(t *testing.T) {
	// first=0x83 → long exp-len form; data[1]=expLen=1; exp byte=0x00;
	// mantissa=0x01 → 1 * 2^(0) = 1.0
	if got, err := DecodeReal([]byte{0x83, 0x01, 0x00, 0x01}); err != nil || got != 1.0 {
		t.Errorf("long exp-len: got %v err %v, want 1.0", got, err)
	}
	// Long form but missing the length octet.
	if _, err := DecodeReal([]byte{0x83}); !errors.Is(err, ErrInvalidReal) {
		t.Errorf("missing exp-len octet: want ErrInvalidReal, got %v", err)
	}
	// expLen=0 is invalid.
	if _, err := DecodeReal([]byte{0x83, 0x00, 0x01}); !errors.Is(err, ErrInvalidReal) {
		t.Errorf("zero exp-len: want ErrInvalidReal, got %v", err)
	}
}

// TestDecodeReal_Errors covers empty, decimal-form rejection, missing
// mantissa, exponent overflow, and the bit-7-clear fallthrough.
func TestDecodeReal_Errors(t *testing.T) {
	if got, err := DecodeReal(nil); err != nil || got != 0.0 {
		t.Errorf("empty: got %v err %v, want 0.0", got, err)
	}
	// NaN sentinel (first octet 0x42).
	if got, err := DecodeReal([]byte{0x42}); err != nil || !math.IsNaN(got) {
		t.Errorf("NaN sentinel: got %v err %v, want NaN", got, err)
	}
	// bit7 clear but bit6 set and not a sentinel (0x43..0x7F) → final
	// ErrInvalidReal fallthrough.
	if _, err := DecodeReal([]byte{0x43, 0x01}); !errors.Is(err, ErrInvalidReal) {
		t.Errorf("0x43 fallthrough: want ErrInvalidReal, got %v", err)
	}
	// Decimal form: bits 7-6 = 00.
	if _, err := DecodeReal([]byte{0x00, 0x01}); !errors.Is(err, ErrInvalidReal) {
		t.Errorf("decimal form: want ErrInvalidReal, got %v", err)
	}
	// Binary form, exp present, but no mantissa bytes.
	if _, err := DecodeReal([]byte{0x80, 0x01}); !errors.Is(err, ErrInvalidReal) {
		t.Errorf("no mantissa: want ErrInvalidReal, got %v", err)
	}
	// Binary form claiming a 3-octet exponent that runs past the buffer.
	if _, err := DecodeReal([]byte{0x82, 0x00, 0x00}); !errors.Is(err, ErrInvalidReal) {
		t.Errorf("exp past buffer: want ErrInvalidReal, got %v", err)
	}
}

// TestDecodeReal_ExponentDecodeError forces the DecodeInteger error inside
// DecodeReal by claiming a long exponent length wider than 8 octets.
func TestDecodeReal_ExponentDecodeError(t *testing.T) {
	// first=0x83 long form, expLen=9 (> int64 width), 9 exp bytes, 1 mant.
	data := []byte{0x83, 0x09, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01}
	if _, err := DecodeReal(data); !errors.Is(err, ErrOverflow) {
		t.Errorf("9-octet exponent: want ErrOverflow, got %v", err)
	}
}

// TestDecodeBoolean_Bad covers the length != 1 guard.
func TestDecodeBoolean_Bad(t *testing.T) {
	if _, err := DecodeBoolean(nil); !errors.Is(err, ErrTruncated) {
		t.Errorf("empty bool: got %v, want ErrTruncated", err)
	}
	if _, err := DecodeBoolean([]byte{0x01, 0x02}); !errors.Is(err, ErrTruncated) {
		t.Errorf("2-byte bool: got %v, want ErrTruncated", err)
	}
}

// TestDecodeInteger_Errors covers empty and >8-octet overflow.
func TestDecodeInteger_Errors(t *testing.T) {
	if _, err := DecodeInteger(nil); !errors.Is(err, ErrTruncated) {
		t.Errorf("empty int: got %v, want ErrTruncated", err)
	}
	if _, err := DecodeInteger([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9}); !errors.Is(err, ErrOverflow) {
		t.Errorf("9-byte int: got %v, want ErrOverflow", err)
	}
}

// TestEncodeSignedInt_Negative exercises the negative-trim branch of
// encodeSignedInt (via EncodeReal of a value whose exponent is negative
// and multi-octet) plus a direct check.
func TestEncodeSignedInt_Negative(t *testing.T) {
	// -1 trims to a single 0xFF octet.
	if got := encodeSignedInt(-1); len(got) != 1 || got[0] != 0xFF {
		t.Errorf("encodeSignedInt(-1) = %X, want [FF]", got)
	}
	// -129 needs two octets (0xFF7F).
	if got := encodeSignedInt(-129); len(got) != 2 {
		t.Errorf("encodeSignedInt(-129) = %X, want 2 octets", got)
	}
	// A large positive value keeps a leading 0x00 to stay positive.
	if got := encodeSignedInt(200); got[0]&0x80 != 0 {
		t.Errorf("encodeSignedInt(200) lost sign padding: %X", got)
	}
}
