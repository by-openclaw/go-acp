package acp2

import (
	"context"
	"dhs/internal/plugin"
	"math"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/export/canonical"
)

// coverage_final_test.go closes the last reachable provider branches the
// other suites miss: the handleACP2 decode-fail / non-request guards, the
// get/set invalid-obj-id arms reached through the get_property/set_property
// handlers (the loopback error-stat suite only drives get_object for the
// invalid-obj case), the applySetNumber float max-clamp + ipv4-as-number
// "not writable" fall-through, the floatConstraint NaN guard, the
// deriveACP2Type integer/string/preset type-hint arms, newTree's
// multi-slot maxSlot bump + Function-child elementID error, flatten's
// preset depth defaulting, and the Serve ctx-cancel listener-close arm.
//
// Every assertion uses the production codec and real canonical types; no
// behaviour is mocked.

// ----------------------------------------------------------------------
// handlers.go — handleACP2 decode-fail + non-request guards

// TestHandleACP2_DecodeError feeds handleACP2 an AN2 data frame whose
// payload is shorter than the 4-byte ACP2 header, so DecodeACP2Message
// errors and the warn-and-drop arm runs.
func TestHandleACP2_DecodeError(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()
	drain(peer)
	sess.handleACP2(&codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Slot: 1, Type: codec.AN2TypeData,
		Payload: []byte{0x00, 0x01}, // < ACP2HeaderSize (4) → decode error
	})
}

// TestHandleACP2_NonRequest feeds handleACP2 a well-formed ACP2 message
// whose Type is reply (not request), so the "ignore non-request" arm runs.
func TestHandleACP2_NonRequest(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()
	drain(peer)
	// 4-byte header: type=reply, mtid=1, func=0, pid=0.
	sess.handleACP2(&codec.AN2Frame{
		Proto: codec.AN2ProtoACP2, Slot: 1, Type: codec.AN2TypeData,
		Payload: []byte{byte(codec.ACP2TypeReply), 1, 0, 0},
	})
}

// TestHandleGetProperty_InvalidObj drives the get_property invalid-obj-id
// arm directly (the loopback error suite only covers it via get_object).
func TestHandleGetProperty_InvalidObj(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()
	drain(peer)
	sess.handleGetProperty(1, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 1, Func: codec.ACP2FuncGetProperty,
		ObjID: 4242, PID: codec.PIDValue})
}

// TestHandleSetProperty_InvalidObj drives the set_property invalid-obj-id
// arm directly.
func TestHandleSetProperty_InvalidObj(t *testing.T) {
	sess, peer := newTestSession(t)
	defer func() { _ = sess.conn.Close() }()
	defer func() { _ = peer.Close() }()
	drain(peer)
	sess.handleSetProperty(1, &codec.ACP2Message{
		Type: codec.ACP2TypeRequest, MTID: 1, Func: codec.ACP2FuncSetProperty,
		ObjID: 4242, PID: codec.PIDValue,
		Properties: []codec.Property{{PID: codec.PIDValue, VType: uint8(codec.NumTypeS32), PLen: 8, Data: make([]byte, 4)}}})
}

// ----------------------------------------------------------------------
// set.go — applySetNumber float max-clamp + ipv4-as-number + floatConstraint NaN

// TestApplySetNumber_FloatMaxClamp sets a float above its declared max so
// the max-clamp arm (fv > maxV → fv = maxV) runs.
func TestApplySetNumber_FloatMaxClamp(t *testing.T) {
	srv := newServer(plugin.Deps{Logger: quietLogger()}, buildServeExport())
	e := &entry{objID: 50, slot: 1, access: 0x03,
		objType: codec.ObjTypeNumber, numType: codec.NumTypeFloat,
		param: &canonical.Parameter{Value: float64(0),
			Minimum: float64(-1), Maximum: float64(1)}}
	// incoming value 9.0 → clamped down to max=1.0.
	data, err := codec.EncodeNumericValue(codec.NumTypeFloat, 0, 0, 9.0)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	in := &codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypeFloat), PLen: 8, Data: data}
	post, stat, err := srv.applySet(e, in)
	if err != nil || stat != 0 {
		t.Fatalf("applySet float max clamp: stat=%d err=%v", stat, err)
	}
	_, _, fv, derr := codec.DecodeNumericValue(codec.NumTypeFloat, post.Data)
	if derr != nil || fv != 1.0 {
		t.Fatalf("clamped float=%v err=%v want 1.0", fv, derr)
	}
}

// TestApplySetNumber_NotWritableNumType drives the applySetNumber final
// "number_type not writable" fall-through: an entry routed to applySetNumber
// (objType=Number) whose numType is IPv4 — DecodeNumericValue accepts IPv4
// but none of the signed/unsigned/float switch arms match it.
func TestApplySetNumber_NotWritableNumType(t *testing.T) {
	srv := newServer(plugin.Deps{Logger: quietLogger()}, buildServeExport())
	e := &entry{objID: 51, slot: 1, access: 0x03,
		objType: codec.ObjTypeNumber, numType: codec.NumTypeIPv4,
		param: &canonical.Parameter{Value: int64(0)}}
	in := &codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypeIPv4), PLen: 8, Data: make([]byte, 4)}
	_, stat, err := srv.applySet(e, in)
	if err == nil || stat != codec.ErrInvalidValue {
		t.Fatalf("not-writable numType: stat=%d err=%v want invalid_value+err", stat, err)
	}
}

// TestFloatConstraint_Branches covers floatConstraint's three guard arms:
// the asFloat64-error arm (un-coercible value), the NaN guard, and the
// happy ok=true return.
func TestFloatConstraint_Branches(t *testing.T) {
	// asFloat64 error arm: a non-nil, non-coercible value.
	if _, ok := floatConstraint([]byte{1}); ok {
		t.Error("un-coercible constraint should report ok=false")
	}
	// NaN guard.
	if _, ok := floatConstraint(math.NaN()); ok {
		t.Error("NaN constraint should report ok=false")
	}
	// A real float reports ok=true (positive control).
	if v, ok := floatConstraint(float64(3.5)); !ok || v != 3.5 {
		t.Errorf("floatConstraint(3.5)=%v,%v want 3.5,true", v, ok)
	}
}

// ----------------------------------------------------------------------
// tree.go — deriveACP2Type integer/string/preset hint arms

// TestDeriveACP2Type_TypeHints exercises every reachable deriveACP2Type
// arm not already covered by the serve fixtures: integer s8/s16/s64/u*/float,
// the integer/string unknown-hint rejects, and every preset numeric hint
// plus the preset unknown-hint reject.
func TestDeriveACP2Type_TypeHints(t *testing.T) {
	str := func(s string) *string { return &s }
	ck := func(name string, p *canonical.Parameter, wantObj codec.ACP2ObjType, wantNum codec.NumberType) {
		obj, num, err := deriveACP2Type(p)
		if err != nil {
			t.Fatalf("%s: unexpected err %v", name, err)
		}
		if obj != wantObj || num != wantNum {
			t.Fatalf("%s: obj=%d num=%d want %d/%d", name, obj, num, wantObj, wantNum)
		}
	}
	ckErr := func(name string, p *canonical.Parameter) {
		if _, _, err := deriveACP2Type(p); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}

	// integer numeric hints
	ck("int s8", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("s8")},
		codec.ObjTypeNumber, codec.NumTypeS8)
	ck("int s64", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("s64")},
		codec.ObjTypeNumber, codec.NumTypeS64)
	ck("int float", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("float")},
		codec.ObjTypeNumber, codec.NumTypeFloat)
	// integer with a known-but-wrong-for-integer hint (ipv4) → reject arm.
	ckErr("int ipv4", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("ipv4")})
	// string with a numeric hint → string reject arm.
	ckErr("string s32", &canonical.Parameter{Type: canonical.ParamString, Format: str("s32")})

	// preset numeric hints (every arm of the preset switch)
	ck("preset s8", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("preset,s8")},
		codec.ObjTypePreset, codec.NumTypeS8)
	ck("preset s16", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("preset,s16")},
		codec.ObjTypePreset, codec.NumTypeS16)
	ck("preset s64", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("preset,s64")},
		codec.ObjTypePreset, codec.NumTypeS64)
	ck("preset u8", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("preset,u8")},
		codec.ObjTypePreset, codec.NumTypeU8)
	ck("preset u16", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("preset,u16")},
		codec.ObjTypePreset, codec.NumTypeU16)
	ck("preset u32", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("preset,u32")},
		codec.ObjTypePreset, codec.NumTypeU32)
	ck("preset u64", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("preset,u64")},
		codec.ObjTypePreset, codec.NumTypeU64)
	ck("preset float", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("preset,float")},
		codec.ObjTypePreset, codec.NumTypeFloat)
	// preset with a known-but-non-numeric hint (ipv4) → preset reject arm.
	ckErr("preset ipv4", &canonical.Parameter{Type: canonical.ParamInteger, Format: str("preset,ipv4")})
}

// ----------------------------------------------------------------------
// tree.go — newTree multi-slot maxSlot, Function-child elementID error,
// flatten preset-depth default

// TestNewTree_MultiSlotMaxBump builds an export with a slot Node whose
// Number > 1 so the `slot > maxSlot` bump arm runs.
func TestNewTree_MultiSlotMaxBump(t *testing.T) {
	okFmt := "s32"
	leaf := &canonical.Parameter{
		Header: canonical.Header{Number: 10, Identifier: "L"},
		Type:   canonical.ParamInteger, Value: int64(0), Format: &okFmt,
	}
	root := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "ROOT",
		Children: []canonical.Element{leaf}}}
	// slot Node with Number=5 (> default maxSlot 1).
	slot5 := &canonical.Node{Header: canonical.Header{Number: 5, Identifier: "slot-5",
		Children: []canonical.Element{root}}}
	dev := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "device",
		Children: []canonical.Element{slot5}}}
	tr, err := newTree(&canonical.Export{Root: dev})
	if err != nil {
		t.Fatalf("newTree: %v", err)
	}
	if tr.slotN != 5 {
		t.Errorf("slotN=%d want 5 (max-slot bump)", tr.slotN)
	}
}

// TestNewTree_NodeChildFunctionError covers flatten's elementID error arm:
// a Node whose child is a Function (no Number-bearing kind handled by
// elementID) makes flatten return the elementID error, surfaced by newTree.
func TestNewTree_NodeChildFunctionError(t *testing.T) {
	fn := &canonical.Function{Header: canonical.Header{Number: 3, Identifier: "F"}}
	root := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "ROOT",
		Children: []canonical.Element{fn}}}
	slot := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "slot-1",
		Children: []canonical.Element{root}}}
	dev := &canonical.Node{Header: canonical.Header{Number: 1, Identifier: "device",
		Children: []canonical.Element{slot}}}
	if _, err := newTree(&canonical.Export{Root: dev}); err == nil {
		t.Error("Function child should surface an elementID error")
	}
}

// TestFlatten_PresetDepthDefault covers the preset depth defaulting arm: a
// preset Parameter with no depth=N hint defaults presetDepth to 1.
func TestFlatten_PresetDepthDefault(t *testing.T) {
	pf := "preset,s32" // preset, no depth=N
	preset := &canonical.Parameter{
		Header: canonical.Header{Number: 12, Identifier: "P"},
		Type:   canonical.ParamInteger, Value: int64(0), Format: &pf,
	}
	idx := map[uint32]*entry{}
	if err := flatten(1, 0, preset, idx); err != nil {
		t.Fatalf("flatten preset: %v", err)
	}
	e, ok := idx[12]
	if !ok {
		t.Fatal("preset leaf 12 not indexed")
	}
	if e.objType != codec.ObjTypePreset {
		t.Fatalf("objType=%d want preset", e.objType)
	}
	if e.presetDepth != 1 {
		t.Errorf("presetDepth=%d want 1 (defaulted)", e.presetDepth)
	}
}

// ----------------------------------------------------------------------
// server.go — Serve ctx-cancel listener-close arm

// TestServe_CtxCancelClosesListener cancels the Serve context (without
// calling Stop first) so the ctx.Done goroutine's `if !s.closed` arm runs:
// it sets closed=true and closes the listener, which unblocks Accept and
// returns Serve cleanly.
func TestServe_CtxCancelClosesListener(t *testing.T) {
	srv := newServer(plugin.Deps{Logger: quietLogger()}, buildServeExport())
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx, "127.0.0.1:0") }()

	// Wait until Serve has bound + published its listener.
	if !waitFor(t, 2*time.Second, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.listener != nil
	}) {
		cancel()
		t.Fatal("Serve never bound a listener")
	}

	cancel() // triggers the ctx.Done goroutine's close path

	// Serve must return nil (clean shutdown via net.ErrClosed).
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve after ctx cancel returned %v want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after ctx cancel")
	}

	// The ctx.Done goroutine must have flipped closed=true itself.
	if !waitFor(t, time.Second, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.closed
	}) {
		t.Error("ctx-cancel goroutine never set closed=true")
	}
}
