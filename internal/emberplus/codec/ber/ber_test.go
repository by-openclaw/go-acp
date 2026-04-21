package ber

import (
	"math"
	"testing"
)

func TestTag_ShortForm(t *testing.T) {
	cases := []struct {
		tag  Tag
		want []byte
	}{
		{Tag{ClassUniversal, false, TagBoolean}, []byte{0x01}},
		{Tag{ClassUniversal, false, TagInteger}, []byte{0x02}},
		{Tag{ClassUniversal, true, TagSequence}, []byte{0x30}},
		{Tag{ClassContext, true, 0}, []byte{0xA0}},
		{Tag{ClassContext, false, 3}, []byte{0x83}},
		{Tag{ClassApplication, true, 1}, []byte{0x61}},
	}
	for _, tc := range cases {
		got := EncodeTag(tc.tag)
		if len(got) != len(tc.want) || got[0] != tc.want[0] {
			t.Errorf("EncodeTag(%+v): got %X, want %X", tc.tag, got, tc.want)
		}
	}
}

func TestTag_LongForm(t *testing.T) {
	tag := Tag{ClassApplication, true, 31}
	got := EncodeTag(tag)
	if len(got) != 2 || got[0] != 0x7F || got[1] != 31 {
		t.Errorf("long form tag 31: got %X", got)
	}

	tag2 := Tag{ClassApplication, true, 128}
	got2 := EncodeTag(tag2)
	if len(got2) != 3 {
		t.Errorf("long form tag 128: got %X (len %d)", got2, len(got2))
	}
}

func TestTag_RoundTrip(t *testing.T) {
	tags := []Tag{
		{ClassUniversal, false, 0},
		{ClassUniversal, false, 30},
		{ClassApplication, true, 31},
		{ClassApplication, true, 127},
		{ClassContext, false, 200},
	}
	for _, orig := range tags {
		data := EncodeTag(orig)
		decoded, n, err := DecodeTag(data)
		if err != nil {
			t.Errorf("DecodeTag(%+v): %v", orig, err)
			continue
		}
		if n != len(data) {
			t.Errorf("consumed %d, want %d", n, len(data))
		}
		if decoded != orig {
			t.Errorf("round-trip: got %+v, want %+v", decoded, orig)
		}
	}
}

func TestLength_ShortForm(t *testing.T) {
	for _, v := range []int{0, 1, 127} {
		data := EncodeLength(v)
		got, n, err := DecodeLength(data)
		if err != nil {
			t.Errorf("length %d: %v", v, err)
		}
		if got != v || n != 1 {
			t.Errorf("length %d: got %d, consumed %d", v, got, n)
		}
	}
}

func TestLength_LongForm(t *testing.T) {
	for _, v := range []int{128, 255, 256, 65535} {
		data := EncodeLength(v)
		got, _, err := DecodeLength(data)
		if err != nil {
			t.Errorf("length %d: %v", v, err)
		}
		if got != v {
			t.Errorf("length %d: got %d", v, got)
		}
	}
}

func TestLength_Indefinite(t *testing.T) {
	data := EncodeLength(-1)
	got, _, err := DecodeLength(data)
	if err != nil {
		t.Fatalf("indefinite: %v", err)
	}
	if got != -1 {
		t.Errorf("indefinite: got %d, want -1", got)
	}
}

func TestInteger_RoundTrip(t *testing.T) {
	cases := []int64{0, 1, -1, 127, 128, -128, -129, 32767, -32768, 2147483647, -2147483648}
	for _, v := range cases {
		data := EncodeInteger(v)
		got, err := DecodeInteger(data)
		if err != nil {
			t.Errorf("int %d: %v", v, err)
		}
		if got != v {
			t.Errorf("int %d: got %d", v, got)
		}
	}
}

func TestBoolean_RoundTrip(t *testing.T) {
	for _, v := range []bool{true, false} {
		data := EncodeBoolean(v)
		got, err := DecodeBoolean(data)
		if err != nil {
			t.Errorf("bool %v: %v", v, err)
		}
		if got != v {
			t.Errorf("bool %v: got %v", v, got)
		}
	}
}

func TestReal_RoundTrip(t *testing.T) {
	cases := []float64{0, 1.0, -1.0, 3.14159, -273.15, math.Inf(1), math.Inf(-1)}
	for _, v := range cases {
		data := EncodeReal(v)
		got, err := DecodeReal(data)
		if err != nil {
			t.Errorf("real %v: %v", v, err)
		}
		if got != v {
			t.Errorf("real %v: got %v", v, got)
		}
	}
}

func TestTLV_PrimitiveRoundTrip(t *testing.T) {
	orig := Integer(42)
	data := EncodeTLV(orig)
	got, n, err := DecodeTLV(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if n != len(data) {
		t.Errorf("consumed %d, want %d", n, len(data))
	}
	if got.Tag != orig.Tag {
		t.Errorf("tag: got %+v, want %+v", got.Tag, orig.Tag)
	}
	v, _ := DecodeInteger(got.Value)
	if v != 42 {
		t.Errorf("value: got %d, want 42", v)
	}
}

func TestTLV_SequenceRoundTrip(t *testing.T) {
	orig := Sequence(
		Integer(1),
		UTF8("hello"),
		Boolean(true),
	)
	data := EncodeTLV(orig)
	got, _, err := DecodeTLV(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Children) != 3 {
		t.Fatalf("children: got %d, want 3", len(got.Children))
	}

	v, _ := DecodeInteger(got.Children[0].Value)
	if v != 1 {
		t.Errorf("child[0] int: got %d", v)
	}
	s := DecodeUTF8String(got.Children[1].Value)
	if s != "hello" {
		t.Errorf("child[1] str: got %q", s)
	}
	b, _ := DecodeBoolean(got.Children[2].Value)
	if !b {
		t.Errorf("child[2] bool: got false")
	}
}

func TestTLV_Nested(t *testing.T) {
	orig := Sequence(
		Integer(1),
		Sequence(
			UTF8("nested"),
			Real(3.14),
		),
	)
	data := EncodeTLV(orig)
	got, _, err := DecodeTLV(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Children) != 2 {
		t.Fatalf("top children: got %d", len(got.Children))
	}
	inner := got.Children[1]
	if len(inner.Children) != 2 {
		t.Fatalf("inner children: got %d", len(inner.Children))
	}
	s := DecodeUTF8String(inner.Children[0].Value)
	if s != "nested" {
		t.Errorf("nested str: got %q", s)
	}
}

func TestTLV_ApplicationTag(t *testing.T) {
	orig := AppConstructed(1,
		ContextConstructed(0, Integer(42)),
	)
	data := EncodeTLV(orig)
	got, _, err := DecodeTLV(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Tag.Class != ClassApplication || got.Tag.Number != 1 {
		t.Errorf("app tag: got %+v", got.Tag)
	}
	if len(got.Children) != 1 {
		t.Fatalf("children: got %d", len(got.Children))
	}
	ctx := got.Children[0]
	if ctx.Tag.Class != ClassContext || ctx.Tag.Number != 0 {
		t.Errorf("ctx tag: got %+v", ctx.Tag)
	}
}
