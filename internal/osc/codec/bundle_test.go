package codec

import (
	"bytes"
	"errors"
	"testing"
)

func TestBundle_Encode_WellFormed(t *testing.T) {
	b := Bundle{
		Timetag: 0x0000000100000000, // example NTP-ish value
		Elements: []Packet{
			Message{Address: "/x", Args: []Arg{Int32(1)}},
			Message{Address: "/y", Args: []Arg{Float32(2.5)}},
		},
	}
	wire, err := b.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// First 8 bytes should be "#bundle\0"
	if !bytes.HasPrefix(wire, []byte("#bundle\x00")) {
		t.Errorf("bundle prefix wrong: % x", wire[:8])
	}
}

func TestBundle_RoundTrip_Flat(t *testing.T) {
	in := Bundle{
		Timetag: 1,
		Elements: []Packet{
			Message{Address: "/a", Args: []Arg{Int32(7)}},
			Message{Address: "/b", Args: []Arg{String("hi")}},
		},
	}
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeBundle(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Timetag != 1 || len(got.Elements) != 2 {
		t.Fatalf("bundle round-trip shape: %+v", got)
	}
	m0 := got.Elements[0].(Message)
	if m0.Address != "/a" || m0.Args[0].Int32 != 7 {
		t.Errorf("element[0] = %+v", m0)
	}
	m1 := got.Elements[1].(Message)
	if m1.Address != "/b" || m1.Args[0].String != "hi" {
		t.Errorf("element[1] = %+v", m1)
	}
}

func TestBundle_RoundTrip_Nested(t *testing.T) {
	in := Bundle{
		Timetag: 42,
		Elements: []Packet{
			Message{Address: "/outer", Args: []Arg{Int32(1)}},
			Bundle{
				Timetag: 99,
				Elements: []Packet{
					Message{Address: "/inner1", Args: []Arg{Float32(0.5)}},
					Message{Address: "/inner2", Args: []Arg{String("deep")}},
				},
			},
		},
	}
	wire, err := in.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := DecodeBundle(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Timetag != 42 || len(got.Elements) != 2 {
		t.Fatalf("outer: %+v", got)
	}
	outer := got.Elements[0].(Message)
	if outer.Address != "/outer" {
		t.Errorf("outer addr=%q", outer.Address)
	}
	inner := got.Elements[1].(Bundle)
	if inner.Timetag != 99 || len(inner.Elements) != 2 {
		t.Errorf("inner: %+v", inner)
	}
	i1 := inner.Elements[0].(Message)
	if i1.Address != "/inner1" || i1.Args[0].Float32 != 0.5 {
		t.Errorf("inner1: %+v", i1)
	}
}

func TestDecodePacket_Dispatch(t *testing.T) {
	msgWire, _ := Message{Address: "/m", Args: []Arg{Int32(1)}}.Encode()
	bndWire, _ := Bundle{Timetag: 1, Elements: []Packet{
		Message{Address: "/a", Args: []Arg{Int32(1)}},
	}}.Encode()

	mp, err := DecodePacket(msgWire)
	if err != nil {
		t.Fatalf("decode msg: %v", err)
	}
	if _, ok := mp.(Message); !ok {
		t.Errorf("expected Message, got %T", mp)
	}

	bp, err := DecodePacket(bndWire)
	if err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if _, ok := bp.(Bundle); !ok {
		t.Errorf("expected Bundle, got %T", bp)
	}
}

func TestDecodePacket_UnknownFirstByte(t *testing.T) {
	_, err := DecodePacket([]byte{'X', 0x00, 0x00, 0x00})
	if !errors.Is(err, ErrBundleNotBundle) {
		t.Errorf("want ErrBundleNotBundle, got %v", err)
	}
}

func TestDecodePacket_Empty(t *testing.T) {
	_, err := DecodePacket(nil)
	if !errors.Is(err, ErrTruncated) {
		t.Errorf("want ErrTruncated, got %v", err)
	}
}
