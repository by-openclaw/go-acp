package acp1

import (
	"context"
	"testing"
	"time"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// richFuzzServer builds a server whose slot 1 carries one writable object of
// every fuzzable type, so RunFuzz exercises fuzzTick + synthesiseValue +
// fuzzableType + readIntBounds across the full type switch.
func richFuzzServer() *server {
	s := discardServer()
	add := func(grp codec.ObjGroup, id uint8, ty codec.ObjectType, p *canonical.Parameter) {
		k := objectKey{slot: 1, group: grp, id: id}
		s.tree.entries[k] = &entry{key: k, acpType: ty, param: p, access: 0x03}
	}
	add(codec.GroupControl, 0, codec.TypeInteger, &canonical.Parameter{Value: int64(0), Minimum: int64(-10), Maximum: int64(10), Step: int64(1)})
	add(codec.GroupControl, 1, codec.TypeLong, &canonical.Parameter{Value: int64(0), Minimum: int64(0), Maximum: int64(100000), Step: int64(1)})
	add(codec.GroupControl, 2, codec.TypeByte, &canonical.Parameter{Value: int64(0), Minimum: int64(0), Maximum: int64(40), Step: int64(1)})
	add(codec.GroupControl, 3, codec.TypeFloat, &canonical.Parameter{Value: float64(0), Minimum: float64(0), Maximum: float64(10)})
	add(codec.GroupControl, 4, codec.TypeIPAddr, &canonical.Parameter{Value: "0.0.0.0", Minimum: "0.0.0.0", Maximum: "255.255.255.255"})
	add(codec.GroupControl, 5, codec.TypeEnum, &canonical.Parameter{Value: int64(0),
		EnumMap: []canonical.EnumEntry{{Key: "Off", Value: 0}, {Key: "On", Value: 1}}})
	return s
}

func TestRunFuzz_AllTypesWithEdges(t *testing.T) {
	s := richFuzzServer()
	cfg := FuzzConfig{
		Seed: 9, Rate: 400, Duration: 150 * time.Millisecond,
		Slot: 1, ID: -1, IncludeEdges: true,
	}
	if err := s.RunFuzz(context.Background(), cfg); err != nil {
		t.Fatalf("RunFuzz: %v", err)
	}
}

func TestServer_SetValue_ErrorPaths(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.SetValue(context.Background(), "not-a-path", 1); err == nil {
		t.Error("bad path should error")
	}
	if _, err := s.SetValue(context.Background(), "1.9.2.0", 1); err == nil {
		t.Error("non-existent object should error")
	}
	// identity[0] (Model) is read-only → no write access.
	if _, err := s.SetValue(context.Background(), "1.2.1.0", "x"); err == nil {
		t.Error("read-only object write should error")
	}
}

// TestSession_SetValue_MutationErrorReply drives the handleRequest branch where
// applyMutation fails (short value buffer) and an error reply is returned.
func TestSession_SetValue_MutationErrorReply(t *testing.T) {
	s := newTestServer(t)
	req := &codec.Message{
		MTID: 1, MType: codec.MTypeRequest, MAddr: 1,
		MCode: byte(codec.MethodSetValue), ObjGroup: codec.GroupControl, ObjID: 0,
		Value: []byte{0x01}, // 1 byte — Integer needs 2
	}
	rep, ann := s.handleRequest(req)
	if rep == nil || rep.MType != codec.MTypeError {
		t.Fatalf("want error reply, got %+v", rep)
	}
	if ann != nil {
		t.Error("failed mutation must not announce")
	}
}

// TestEncodeObject_LongLabelTruncation drives limitLabel / limitString /
// cstrWithLimit / unitOf with over-length label + unit strings.
func TestEncodeObject_LongLabelTruncation(t *testing.T) {
	e := &entry{
		acpType: codec.TypeInteger,
		access:  codec.AccessRead,
		param: param("ThisLabelIsDefinitelyLongerThanSixteen", canonical.ParamInteger,
			withValue(int64(0)), withUnit("volts")),
	}
	b, err := encodeObject(e)
	if err != nil {
		t.Fatalf("encodeObject: %v", err)
	}
	o, err := codec.DecodeObject(b)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(o.Label) > 16 {
		t.Errorf("label not truncated to 16: %q", o.Label)
	}
	if len(o.Unit) > 4 {
		t.Errorf("unit not truncated to 4: %q", o.Unit)
	}
}

// TestEncodeObject_ValueOutOfRange drives the first error return of the numeric
// encoders (value coercion failure).
func TestEncodeObject_ValueOutOfRange(t *testing.T) {
	cases := []struct {
		ty  codec.ObjectType
		ct  string
		fmt string
		val any
	}{
		{codec.TypeInteger, canonical.ParamInteger, "", int64(99999)},      // > int16
		{codec.TypeLong, canonical.ParamInteger, "int32", int64(1) << 40},  // > int32
		{codec.TypeByte, canonical.ParamInteger, "uint8", int64(999)},      // > uint8
		{codec.TypeIPAddr, canonical.ParamString, "ipv4", "bad.ip.addr.x"}, // bad dotted quad
	}
	for _, c := range cases {
		opts := []paramOpt{withValue(c.val)}
		if c.fmt != "" {
			opts = append(opts, withFormat(c.fmt))
		}
		e := &entry{acpType: c.ty, access: codec.AccessRead, param: param("x", c.ct, opts...)}
		if _, err := encodeObject(e); err == nil {
			t.Errorf("encodeObject(type %d, val %v): expected error", c.ty, c.val)
		}
	}
}

func TestAsBoolAndFloat32Opt(t *testing.T) {
	if v, err := asBool(true, "f"); err != nil || !v {
		t.Errorf("asBool(true) = %v,%v", v, err)
	}
	if v, err := asBool(false, "f"); err != nil || v {
		t.Errorf("asBool(false) = %v,%v", v, err)
	}
	if _, err := asBool("notbool", "f"); err == nil {
		t.Error("asBool(non-bool) should error")
	}
	// asFloat32Opt: nil → default, present → parsed, bad → error.
	if v, err := asFloat32Opt(nil, "f", 2.5); err != nil || v != 2.5 {
		t.Errorf("asFloat32Opt(nil) = %v,%v want 2.5", v, err)
	}
	if v, err := asFloat32Opt(float64(1.5), "f", 0); err != nil || v != 1.5 {
		t.Errorf("asFloat32Opt(1.5) = %v,%v", v, err)
	}
	if _, err := asFloat32Opt("x", "f", 0); err == nil {
		t.Error("asFloat32Opt(bad) should error")
	}
}
