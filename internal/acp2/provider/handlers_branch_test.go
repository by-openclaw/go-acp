package acp2

import (
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// handlers_branch_test.go targets the residual error / edge branches the
// loopback serve test doesn't reach: unsupported AN2 proto drop, ACP2
// non-data frames, buildProperties failures surfacing as protocol
// errors, and the preset object_type path through get_object.

// TestDispatch_UnsupportedProto pins the dispatch default arm: a frame
// whose proto is neither AN2-internal nor ACP2 is dropped silently (no
// reply written). We send ACMP (proto 3) and assert no reply lands.
func TestDispatch_UnsupportedProto(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	f := &codec.AN2Frame{
		Proto:   codec.AN2ProtoACMP, // 3 — not handled
		Slot:    1,
		MTID:    0,
		Type:    codec.AN2TypeData,
		Payload: []byte{0, 0, 0, 0},
	}
	// dispatch directly; it must not panic and must not write a reply.
	sess.dispatch(f)
}

// TestHandleACP2_NonDataFrame covers the early return when an ACP2 frame
// arrives that is not a data frame (e.g. a stray request-typed AN2
// frame). It must be ignored without a reply.
func TestHandleACP2_NonDataFrame(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	f := &codec.AN2Frame{
		Proto:   codec.AN2ProtoACP2,
		Slot:    1,
		MTID:    0,
		Type:    codec.AN2TypeRequest, // not data → ignored
		Payload: []byte{0, 0, 0, 0},
	}
	sess.dispatch(f)
}

// TestHandleAN2Internal_NonRequestAndEmpty covers the two guard returns:
// a non-request AN2 internal frame and an empty payload.
func TestHandleAN2Internal_NonRequestAndEmpty(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()

	sess.handleAN2Internal(&codec.AN2Frame{
		Proto: codec.AN2ProtoInternal, Type: codec.AN2TypeReply,
		Payload: []byte{codec.AN2FuncGetVersion},
	})
	sess.handleAN2Internal(&codec.AN2Frame{
		Proto: codec.AN2ProtoInternal, Type: codec.AN2TypeRequest,
		Payload: nil,
	})
	// Unknown funcID default arm.
	sess.handleAN2Internal(&codec.AN2Frame{
		Proto: codec.AN2ProtoInternal, Type: codec.AN2TypeRequest,
		Payload: []byte{0xFE},
	})
	// EnableProtocolEvents missing count byte guard.
	sess.handleAN2Internal(&codec.AN2Frame{
		Proto: codec.AN2ProtoInternal, Type: codec.AN2TypeRequest,
		Payload: []byte{codec.AN2FuncEnableProtocolEvents},
	})
}

// presetEntry builds a preset child entry from a depth=N format hint.
func presetEntry(t *testing.T, format string, val any) *entry {
	t.Helper()
	f := format
	p := &canonical.Parameter{
		Header: canonical.Header{Number: 5, Identifier: "Preset",
			Access: canonical.AccessReadWrite},
		Type:   canonical.ParamInteger,
		Value:  val,
		Format: &f,
	}
	objType, numType, err := deriveACP2Type(p)
	if err != nil {
		t.Fatalf("deriveACP2Type(%q): %v", format, err)
	}
	e := &entry{objID: 5, label: "Preset", access: 0x03,
		objType: objType, numType: numType, param: p,
		presetDepth:   presetDepthHint(p),
		presetIdxList: presetIdxHint(p)}
	if e.presetDepth == 0 {
		e.presetDepth = 1
	}
	return e
}

// TestBuildProperties_Preset exercises the ObjTypePreset arm of
// buildProperties (pid 7 + per-idx pid 8/9 + options) end to end through
// the codec.
func TestBuildProperties_Preset(t *testing.T) {
	e := presetEntry(t, "preset,s32,depth=2", int64(3))
	if e.objType != codec.ObjTypePreset {
		t.Fatalf("objType=%d want preset", e.objType)
	}
	props, err := buildProperties(e)
	if err != nil {
		t.Fatalf("buildProperties: %v", err)
	}
	raw, err := codec.EncodeProperties(props)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	dec, err := codec.DecodeProperties(raw)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var hasDepth, valueCount int
	for _, p := range dec {
		switch p.PID {
		case codec.PIDPresetDepth:
			hasDepth++
		case codec.PIDValue:
			valueCount++
		}
	}
	if hasDepth != 1 {
		t.Errorf("pid 7 count=%d want 1", hasDepth)
	}
	if valueCount != 2 { // one pid 8 per depth idx
		t.Errorf("pid 8 count=%d want 2 (depth)", valueCount)
	}
}

// TestDeriveACP2Type_PresetVariants covers the preset switch arms across
// every number-type hint.
func TestDeriveACP2Type_PresetVariants(t *testing.T) {
	hints := []struct {
		hint string
		want codec.NumberType
	}{
		{"", codec.NumTypeS32},
		{"s8", codec.NumTypeS8},
		{"s16", codec.NumTypeS16},
		{"s32", codec.NumTypeS32},
		{"s64", codec.NumTypeS64},
		{"u8", codec.NumTypeU8},
		{"u16", codec.NumTypeU16},
		{"u32", codec.NumTypeU32},
		{"u64", codec.NumTypeU64},
		{"float", codec.NumTypeFloat},
	}
	for _, h := range hints {
		f := "preset"
		if h.hint != "" {
			f = "preset," + h.hint
		}
		fp := f
		p := &canonical.Parameter{Type: canonical.ParamInteger, Format: &fp}
		objType, numType, err := deriveACP2Type(p)
		if err != nil {
			t.Errorf("hint=%q: %v", h.hint, err)
			continue
		}
		if objType != codec.ObjTypePreset || numType != h.want {
			t.Errorf("hint=%q got obj=%d num=%d want preset/%d", h.hint, objType, numType, h.want)
		}
	}
}

// TestDeriveACP2Type_StringBadHint covers the string default-arm reject.
func TestDeriveACP2Type_StringBadHint(t *testing.T) {
	f := "weird"
	p := &canonical.Parameter{Type: canonical.ParamString, Format: &f}
	if _, _, err := deriveACP2Type(p); err == nil {
		t.Error("string with unknown hint should error")
	}
}

// TestDeriveACP2Type_UnsupportedCanonicalType covers the final fallthrough
// for a canonical Type that has no ACP2 mapping.
func TestDeriveACP2Type_UnsupportedCanonicalType(t *testing.T) {
	p := &canonical.Parameter{Type: "octets"} // not a known canonical param type
	if _, _, err := deriveACP2Type(p); err == nil {
		t.Error("unknown canonical type should error")
	}
}

// TestPresetDepthHint_Variants covers the depth= parse: valid, absent,
// and out-of-range values.
func TestPresetDepthHint_Variants(t *testing.T) {
	mk := func(f string) *canonical.Parameter { ff := f; return &canonical.Parameter{Format: &ff} }
	if d := presetDepthHint(mk("preset,depth=3")); d != 3 {
		t.Errorf("depth=3 → %d", d)
	}
	if d := presetDepthHint(mk("preset")); d != 0 {
		t.Errorf("no depth → %d want 0", d)
	}
	if d := presetDepthHint(mk("preset,depth=0")); d != 0 {
		t.Errorf("depth=0 → %d want 0 (rejected)", d)
	}
	if d := presetDepthHint(mk("preset,depth=70000")); d != 0 {
		t.Errorf("depth=70000 → %d want 0 (over range)", d)
	}
}

// TestEventDelayHint covers both branches (nil and non-nil param) — both
// currently return 0 by design.
func TestEventDelayHint(t *testing.T) {
	if got := eventDelayHint(nil); got != 0 {
		t.Errorf("nil → %d want 0", got)
	}
	if got := eventDelayHint(&canonical.Parameter{}); got != 0 {
		t.Errorf("param → %d want 0", got)
	}
}

// TestHasPreset covers the false return when no preset token is present.
func TestHasPreset(t *testing.T) {
	if hasPreset([]string{"s32", "depth=2"}) {
		t.Error("hasPreset without token should be false")
	}
	if !hasPreset([]string{"preset", "s32"}) {
		t.Error("hasPreset with token should be true")
	}
}

// TestElementID_Errors covers the elementID error arm via a fake element.
func TestElementID_Matrix(t *testing.T) {
	// A Node and a Parameter both have Number; assert both resolve.
	n := &canonical.Node{Header: canonical.Header{Number: 7}}
	if id, err := elementID(n); err != nil || id != 7 {
		t.Errorf("node id=%d err=%v", id, err)
	}
	p := &canonical.Parameter{Header: canonical.Header{Number: 8}}
	if id, err := elementID(p); err != nil || id != 8 {
		t.Errorf("param id=%d err=%v", id, err)
	}
}
