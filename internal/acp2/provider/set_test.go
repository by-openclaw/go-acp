package acp2

import (
	"encoding/binary"
	"math"
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// set_test.go covers the applySet* family that the loopback serve test
// reaches only partially: the numeric clamp branches per NumberType, the
// IPv4 + string write paths, and the applySet dispatch + guard rejects.

func rwParam(typ string, format string, val any) *canonical.Parameter {
	p := &canonical.Parameter{
		Header: canonical.Header{Number: 1, Identifier: "p",
			Access: canonical.AccessReadWrite},
		Type:  typ,
		Value: val,
	}
	if format != "" {
		p.Format = &format
	}
	return p
}

func numEntry(t *testing.T, p *canonical.Parameter) *entry {
	t.Helper()
	objType, numType, err := deriveACP2Type(p)
	if err != nil {
		t.Fatalf("deriveACP2Type: %v", err)
	}
	return &entry{objID: 1, label: "p", access: 0x03,
		objType: objType, numType: numType, param: p}
}

func numProp(nt codec.NumberType, iv int64, uv uint64, fv float64) *codec.Property {
	data, _ := codec.EncodeNumericValue(nt, iv, uv, fv)
	return &codec.Property{PID: codec.PIDValue, VType: uint8(nt), PLen: uint16(4 + len(data)), Data: data}
}

// TestApplySetNumber_ClampSigned exercises the signed clamp branch:
// values below min snap up, above max snap down, in-range pass through.
func TestApplySetNumber_ClampSigned(t *testing.T) {
	p := rwParam(canonical.ParamInteger, "s16", int64(0))
	p.Minimum = int64(-5)
	p.Maximum = int64(5)
	e := numEntry(t, p)
	srv := &server{tree: emptyTree()}

	cases := []struct {
		in   int64
		want int64
	}{
		{-100, -5}, // clamp to min
		{100, 5},   // clamp to max
		{3, 3},     // in range
	}
	for _, tc := range cases {
		post, stat, err := srv.applySet(e, numProp(codec.NumTypeS16, tc.in, 0, 0))
		if err != nil || stat != 0 {
			t.Fatalf("in=%d stat=%d err=%v", tc.in, stat, err)
		}
		iv, _, _, _ := codec.DecodeNumericValue(codec.NumTypeS16, post.Data)
		if iv != tc.want {
			t.Errorf("in=%d post=%d want %d", tc.in, iv, tc.want)
		}
		if p.Value != tc.want {
			t.Errorf("in=%d stored=%v want %d", tc.in, p.Value, tc.want)
		}
	}
}

// TestApplySetNumber_ClampUnsigned exercises the unsigned clamp branch.
func TestApplySetNumber_ClampUnsigned(t *testing.T) {
	p := rwParam(canonical.ParamInteger, "u16", uint64(0))
	p.Minimum = int64(10)
	p.Maximum = int64(20)
	e := numEntry(t, p)
	srv := &server{tree: emptyTree()}

	post, stat, err := srv.applySet(e, numProp(codec.NumTypeU16, 0, 5, 0))
	if err != nil || stat != 0 {
		t.Fatalf("stat=%d err=%v", stat, err)
	}
	_, uv, _, _ := codec.DecodeNumericValue(codec.NumTypeU16, post.Data)
	if uv != 10 {
		t.Errorf("clamp-low uv=%d want 10", uv)
	}

	post, _, _ = srv.applySet(e, numProp(codec.NumTypeU16, 0, 99, 0))
	_, uv, _, _ = codec.DecodeNumericValue(codec.NumTypeU16, post.Data)
	if uv != 20 {
		t.Errorf("clamp-high uv=%d want 20", uv)
	}
}

// TestApplySetNumber_ClampFloat exercises the float clamp branch.
func TestApplySetNumber_ClampFloat(t *testing.T) {
	p := rwParam(canonical.ParamReal, "float", float64(0))
	p.Minimum = float64(-1.5)
	p.Maximum = float64(1.5)
	e := numEntry(t, p)
	srv := &server{tree: emptyTree()}

	post, stat, err := srv.applySet(e, numProp(codec.NumTypeFloat, 0, 0, -10))
	if err != nil || stat != 0 {
		t.Fatalf("stat=%d err=%v", stat, err)
	}
	_, _, fv, _ := codec.DecodeNumericValue(codec.NumTypeFloat, post.Data)
	if math.Abs(fv-(-1.5)) > 1e-6 {
		t.Errorf("clamp-low fv=%v want -1.5", fv)
	}
	if e.param.Value.(float64) != -1.5 {
		t.Errorf("stored=%v want -1.5", e.param.Value)
	}
}

// TestApplySetNumber_NoConstraintsPassThrough hits the ok=false branch of
// each constraint helper (nil Minimum/Maximum) — the value is stored
// verbatim.
func TestApplySetNumber_NoConstraintsPassThrough(t *testing.T) {
	p := rwParam(canonical.ParamInteger, "s32", int64(0)) // no Min/Max
	e := numEntry(t, p)
	srv := &server{tree: emptyTree()}
	post, stat, err := srv.applySet(e, numProp(codec.NumTypeS32, 12345, 0, 0))
	if err != nil || stat != 0 {
		t.Fatalf("stat=%d err=%v", stat, err)
	}
	iv, _, _, _ := codec.DecodeNumericValue(codec.NumTypeS32, post.Data)
	if iv != 12345 {
		t.Errorf("post=%d want 12345", iv)
	}
}

// TestApplySetNumber_BadBytes hits the DecodeNumericValue error path.
func TestApplySetNumber_BadBytes(t *testing.T) {
	p := rwParam(canonical.ParamInteger, "s32", int64(0))
	e := numEntry(t, p)
	srv := &server{tree: emptyTree()}
	in := &codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypeS32), PLen: 5, Data: []byte{1}}
	_, stat, err := srv.applySet(e, in)
	if err == nil || stat != codec.ErrInvalidValue {
		t.Fatalf("stat=%d err=%v want invalid_value", stat, err)
	}
}

// TestApplySetIPv4 covers the ipv4 write path + dotted-quad storage and
// the short-buffer reject.
func TestApplySetIPv4(t *testing.T) {
	p := rwParam(canonical.ParamString, "ipv4", "0.0.0.0")
	e := numEntry(t, p)
	srv := &server{tree: emptyTree()}

	in := &codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypeIPv4), PLen: 8,
		Data: []byte{192, 168, 1, 42}}
	post, stat, err := srv.applySet(e, in)
	if err != nil || stat != 0 {
		t.Fatalf("stat=%d err=%v", stat, err)
	}
	if e.param.Value != "192.168.1.42" {
		t.Errorf("stored=%v want 192.168.1.42", e.param.Value)
	}
	if len(post.Data) != 4 || post.Data[0] != 192 {
		t.Errorf("post.Data=%v", post.Data)
	}

	// short buffer → invalid_value
	short := &codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypeIPv4), PLen: 6, Data: []byte{1, 2}}
	_, stat, err = srv.applySet(e, short)
	if err == nil || stat != codec.ErrInvalidValue {
		t.Fatalf("short ipv4 stat=%d err=%v want invalid_value", stat, err)
	}
}

// TestApplySetString covers the string write path: NUL strip, maxLen
// truncation, and storage.
func TestApplySetString(t *testing.T) {
	p := rwParam(canonical.ParamString, "maxLen=4", "old")
	e := numEntry(t, p)
	srv := &server{tree: emptyTree()}

	// "ABCDEFG\0" → strip NUL → "ABCDEFG" → truncate to 4 → "ABCD".
	in := &codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypeString),
		Data: append([]byte("ABCDEFG"), 0)}
	post, stat, err := srv.applySet(e, in)
	if err != nil || stat != 0 {
		t.Fatalf("stat=%d err=%v", stat, err)
	}
	if e.param.Value != "ABCD" {
		t.Errorf("stored=%q want ABCD", e.param.Value)
	}
	if post.PID != codec.PIDValue {
		t.Errorf("post pid=%d", post.PID)
	}
}

// TestApplySet_NoWriteAccess pins stat=4 when the entry lacks the write
// bit.
func TestApplySet_NoWriteAccess(t *testing.T) {
	p := rwParam(canonical.ParamInteger, "s32", int64(0))
	e := numEntry(t, p)
	e.access = 0x01 // read only
	srv := &server{tree: emptyTree()}
	_, stat, err := srv.applySet(e, numProp(codec.NumTypeS32, 1, 0, 0))
	if err == nil || stat != codec.ErrNoAccess {
		t.Fatalf("stat=%d err=%v want no_access", stat, err)
	}
}

// TestApplySet_WrongPID pins stat=3 when the incoming property is not
// pid=8 (value).
func TestApplySet_WrongPID(t *testing.T) {
	p := rwParam(canonical.ParamInteger, "s32", int64(0))
	e := numEntry(t, p)
	srv := &server{tree: emptyTree()}
	in := numProp(codec.NumTypeS32, 1, 0, 0)
	in.PID = codec.PIDEventDelay // pid 4, not 8
	_, stat, err := srv.applySet(e, in)
	if err == nil || stat != codec.ErrInvalidPID {
		t.Fatalf("stat=%d err=%v want invalid_pid", stat, err)
	}
}

// TestApplySet_UnsupportedObjType pins the dispatch default: a node is
// not writable.
func TestApplySet_UnsupportedObjType(t *testing.T) {
	e := &entry{objID: 1, access: 0x03, objType: codec.ObjTypeNode}
	srv := &server{tree: emptyTree()}
	in := numProp(codec.NumTypeS32, 1, 0, 0)
	_, stat, err := srv.applySet(e, in)
	if err == nil || stat != codec.ErrInvalidPID {
		t.Fatalf("stat=%d err=%v want invalid_pid", stat, err)
	}
}

// TestApplySetEnum_ShortBuffer covers the <4 byte reject in applySetEnum.
func TestApplySetEnum_ShortBuffer(t *testing.T) {
	enum := "Off,On"
	p := &canonical.Parameter{
		Header:      canonical.Header{Number: 1, Access: canonical.AccessReadWrite},
		Type:        canonical.ParamEnum,
		Value:       int64(0),
		Enumeration: &enum,
	}
	e := &entry{objID: 1, access: 0x03, objType: codec.ObjTypeEnum,
		numType: codec.NumTypeU32, param: p}
	srv := &server{tree: emptyTree()}
	in := &codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypeU32), Data: []byte{0, 1}}
	_, stat, err := srv.applySet(e, in)
	if err == nil || stat != codec.ErrInvalidValue {
		t.Fatalf("stat=%d err=%v want invalid_value", stat, err)
	}
}

// TestConstraintHelpers covers intConstraint / uintConstraint /
// floatConstraint nil, type-mismatch, and NaN branches directly.
func TestConstraintHelpers(t *testing.T) {
	if _, ok := intConstraint(nil); ok {
		t.Error("intConstraint(nil) ok")
	}
	if v, ok := intConstraint(int64(7)); !ok || v != 7 {
		t.Errorf("intConstraint(7)=%d,%v", v, ok)
	}
	if _, ok := intConstraint("not-an-int"); ok {
		t.Error("intConstraint(bad string) ok")
	}
	if _, ok := uintConstraint(nil); ok {
		t.Error("uintConstraint(nil) ok")
	}
	if v, ok := uintConstraint(int64(3)); !ok || v != 3 {
		t.Errorf("uintConstraint(3)=%d,%v", v, ok)
	}
	if _, ok := uintConstraint(int64(-1)); ok {
		t.Error("uintConstraint(negative) ok")
	}
	if _, ok := floatConstraint(nil); ok {
		t.Error("floatConstraint(nil) ok")
	}
	if v, ok := floatConstraint(float64(2.5)); !ok || v != 2.5 {
		t.Errorf("floatConstraint(2.5)=%v,%v", v, ok)
	}
	if _, ok := floatConstraint(math.NaN()); ok {
		t.Error("floatConstraint(NaN) ok")
	}
}

// TestEnumIdxValid_NoEnumData covers the false-return when a parameter
// has neither EnumMap nor Enumeration string.
func TestEnumIdxValid_NoEnumData(t *testing.T) {
	p := &canonical.Parameter{Type: canonical.ParamEnum}
	if enumIdxValid(p, 0) {
		t.Error("enumIdxValid with no enum data should be false")
	}
}

// ensure binary import stays used even if assertions shift.
var _ = binary.BigEndian
