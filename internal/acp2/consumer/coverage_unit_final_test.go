package acp2

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
)

// coverage_unit_final_test.go drives the pure (no-I/O) decode/encode/
// resolve helpers across walker.go, plugin.go, value_validate.go,
// session.go, session_health.go and cache.go that the loopback suites
// don't reach. Every test calls the production function directly with
// hand-built codec values — no behaviour is mocked.

func unitLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ----------------------------------------------------------------------
// walker.go — Lookup nil, decodeValue / decodeConstraint / numberTypeToKind

// TestWalkedTree_LookupNil covers the nil-receiver guard.
func TestWalkedTree_LookupNil(t *testing.T) {
	var tr *WalkedTree
	if got := tr.Lookup("anything"); got != -1 {
		t.Errorf("nil tree Lookup = %d, want -1", got)
	}
}

// TestNumberTypeToKind_AllArms covers the preset/ipv4/string/default arms
// not exercised by the numeric walk fixtures.
func TestNumberTypeToKind_AllArms(t *testing.T) {
	cases := []struct {
		nt   codec.NumberType
		want consumer.ValueKind
	}{
		{codec.NumTypePreset, consumer.KindEnum},
		{codec.NumTypeIPv4, consumer.KindIPAddr},
		{codec.NumTypeString, consumer.KindString},
		{codec.NumberType(99), consumer.KindRaw}, // default
	}
	for _, tc := range cases {
		if got := numberTypeToKind(tc.nt); got != tc.want {
			t.Errorf("numberTypeToKind(%d)=%v want %v", tc.nt, got, tc.want)
		}
	}
}

// TestDecodeValue_NumericVtypeFallback covers decodeValue's nt==0 → numType
// fallback, the decode-error → raw arm, and the default-objType raw arm.
func TestDecodeValue_NumericVtypeFallback(t *testing.T) {
	w := &Walker{logger: unitLogger()}

	// vtype 0 on the property → falls back to the passed numType (S32).
	var obj consumer.Object
	p := &codec.Property{PID: codec.PIDValue, VType: 0, Data: []byte{0xFF, 0xFF, 0xFF, 0xFB}} // -5
	w.decodeValue(p, codec.ObjTypeNumber, codec.NumTypeS32, &obj, nil)
	if obj.Value.Kind != consumer.KindInt || obj.Value.Int != -5 {
		t.Errorf("vtype-fallback decode = %+v, want int -5", obj.Value)
	}

	// Short data for an S64 → DecodeNumericValue errors → raw fallback.
	var obj2 consumer.Object
	p2 := &codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypeS64), Data: []byte{0x01}}
	w.decodeValue(p2, codec.ObjTypeNumber, codec.NumTypeS64, &obj2, nil)
	if obj2.Value.Kind != consumer.KindRaw {
		t.Errorf("short-data decode = %+v, want raw", obj2.Value)
	}

	// Default objType (Node) with data → raw.
	var obj3 consumer.Object
	p3 := &codec.Property{PID: codec.PIDValue, Data: []byte{1, 2, 3, 4}}
	w.decodeValue(p3, codec.ObjTypeNode, 0, &obj3, nil)
	if obj3.Value.Kind != consumer.KindRaw {
		t.Errorf("node decodeValue = %+v, want raw", obj3.Value)
	}
}

// TestDecodeConstraint_Branches covers decodeConstraint's enum-default arm,
// the non-number early return, the decode-error return, and the uint/float
// constraint kinds + the step/default targets.
func TestDecodeConstraint_Branches(t *testing.T) {
	w := &Walker{logger: unitLogger()}

	// Enum default → obj.Def set as uint64 index.
	var enumObj consumer.Object
	dp := &codec.Property{PID: codec.PIDDefaultValue, Data: []byte{0, 0, 0, 2}}
	w.decodeConstraint(dp, codec.ObjTypeEnum, codec.NumTypePreset, &enumObj, "default")
	if got, _ := enumObj.Def.(uint64); got != 2 {
		t.Errorf("enum default = %v, want 2", enumObj.Def)
	}

	// Non-number, non-enum-default → early return (no-op).
	var strObj consumer.Object
	w.decodeConstraint(dp, codec.ObjTypeString, codec.NumTypeString, &strObj, "min")
	if strObj.Min != nil {
		t.Errorf("string min should be nil, got %v", strObj.Min)
	}

	// Number with short data → DecodeNumericValue error → return.
	var numObj consumer.Object
	bad := &codec.Property{PID: codec.PIDMinValue, VType: uint8(codec.NumTypeS64), Data: []byte{1}}
	w.decodeConstraint(bad, codec.ObjTypeNumber, codec.NumTypeS64, &numObj, "min")
	if numObj.Min != nil {
		t.Errorf("min after decode error should be nil, got %v", numObj.Min)
	}

	// Uint min, float step.
	var uObj consumer.Object
	up := &codec.Property{PID: codec.PIDMinValue, VType: uint8(codec.NumTypeU32), Data: []byte{0, 0, 0, 9}}
	w.decodeConstraint(up, codec.ObjTypeNumber, codec.NumTypeU32, &uObj, "min")
	if got, _ := uObj.Min.(uint64); got != 9 {
		t.Errorf("uint min = %v, want 9", uObj.Min)
	}
	var fObj consumer.Object
	fp := &codec.Property{PID: codec.PIDStepSize, VType: uint8(codec.NumTypeFloat), Data: f32be(0.25)}
	w.decodeConstraint(fp, codec.ObjTypeNumber, codec.NumTypeFloat, &fObj, "step")
	if got, _ := fObj.Step.(float64); got < 0.24 || got > 0.26 {
		t.Errorf("float step = %v, want ~0.25", fObj.Step)
	}

	// Number whose numberTypeToKind is KindRaw (e.g. nt after fallback maps
	// to default) → val stays nil via the default arm.
	var rawObj consumer.Object
	rp := &codec.Property{PID: codec.PIDMinValue, VType: uint8(codec.NumTypeIPv4), Data: []byte{1, 2, 3, 4}}
	w.decodeConstraint(rp, codec.ObjTypeNumber, codec.NumTypeIPv4, &rawObj, "min")
	if rawObj.Min != nil {
		t.Errorf("ipv4-as-number constraint min should be nil, got %v", rawObj.Min)
	}
}

// TestParseObjectProperties_NoLabel covers the obj.Label=="" → "obj_N" path
// in parseObjectProperties.
func TestParseObjectProperties_NoLabel(t *testing.T) {
	w := &Walker{logger: unitLogger()}
	props := []codec.Property{
		{PID: codec.PIDObjectType, VType: uint8(codec.ObjTypeNode), PLen: 4},
		// no PIDLabel
	}
	obj, _, _, _, _ := w.parseObjectProperties(props, 1, 77, nil)
	if len(obj.Path) == 0 || obj.Path[len(obj.Path)-1] != "obj_77" {
		t.Errorf("unlabeled obj path = %v, want trailing obj_77", obj.Path)
	}
}

// ----------------------------------------------------------------------
// plugin.go — metaToUint8, decodeStringlyOptionsMap, objTypeFromVType,
// decodePropertyValue, encodeSetProperty, resolveRequest, buildAnnounceClosure

func TestMetaToUint8_AllArms(t *testing.T) {
	cases := []struct {
		in   any
		want uint8
	}{
		{uint8(5), 5},
		{int(6), 6},
		{int64(7), 7},
		{float64(8), 8},
		{"nope", 0}, // default
	}
	for _, tc := range cases {
		if got := metaToUint8(tc.in); got != tc.want {
			t.Errorf("metaToUint8(%v=%T)=%d want %d", tc.in, tc.in, got, tc.want)
		}
	}
}

func TestDecodeStringlyOptionsMap_AllShapes(t *testing.T) {
	// Already in the in-memory shape.
	if m := decodeStringlyOptionsMap(map[uint32]string{1: "On"}); m[1] != "On" {
		t.Errorf("uint32 map passthrough failed: %v", m)
	}
	// map[string]string (stringified keys).
	if m := decodeStringlyOptionsMap(map[string]string{"2": "Auto"}); m[2] != "Auto" {
		t.Errorf("string map decode failed: %v", m)
	}
	// map[string]any (JSON-decoded).
	if m := decodeStringlyOptionsMap(map[string]any{"3": "Full", "4": 99}); m[3] != "Full" {
		t.Errorf("any map decode failed: %v", m)
	}
	// Unknown shape → nil.
	if m := decodeStringlyOptionsMap(12345); m != nil {
		t.Errorf("unknown shape should decode nil, got %v", m)
	}
}

func TestObjTypeFromVType_AllArms(t *testing.T) {
	cases := []struct {
		nt   codec.NumberType
		want codec.ACP2ObjType
	}{
		{codec.NumTypePreset, codec.ObjTypeEnum},
		{codec.NumTypeIPv4, codec.ObjTypeIPv4},
		{codec.NumTypeString, codec.ObjTypeString},
		{codec.NumTypeS32, codec.ObjTypeNumber}, // default
	}
	for _, tc := range cases {
		if got := objTypeFromVType(tc.nt); got != tc.want {
			t.Errorf("objTypeFromVType(%d)=%d want %d", tc.nt, got, tc.want)
		}
	}
}

// TestDecodePropertyValue_Branches covers the numeric vtype fallback +
// decode-error raw arm, the enum-with-tree-label resolution, the ipv4 and
// string arms, and the final raw fall-through.
func TestDecodePropertyValue_Branches(t *testing.T) {
	// numeric vtype 0 → numType fallback.
	p := &codec.Property{PID: codec.PIDValue, VType: 0, Data: []byte{0, 0, 0, 5}}
	v, _ := decodePropertyValue(p, codec.ObjTypeNumber, codec.NumTypeU32, nil, 1)
	if v.Kind != consumer.KindUint || v.Uint != 5 {
		t.Errorf("numeric fallback = %+v, want uint 5", v)
	}

	// numeric decode error → raw.
	pbad := &codec.Property{PID: codec.PIDValue, VType: uint8(codec.NumTypeS64), Data: []byte{1}}
	vb, _ := decodePropertyValue(pbad, codec.ObjTypeNumber, codec.NumTypeS64, nil, 1)
	if vb.Kind != consumer.KindRaw {
		t.Errorf("numeric decode error = %+v, want raw", vb)
	}

	// enum resolves label from tree options map.
	tree := &WalkedTree{
		Objects:     []consumer.Object{{ID: 9}},
		OptionsMaps: []map[uint32]string{{7: "Main"}},
	}
	pe := &codec.Property{PID: codec.PIDValue, Data: []byte{0, 0, 0, 7}}
	ve, _ := decodePropertyValue(pe, codec.ObjTypeEnum, codec.NumTypePreset, tree, 9)
	if ve.Kind != consumer.KindEnum || ve.Str != "Main" {
		t.Errorf("enum-with-tree = %+v, want enum Main", ve)
	}

	// ipv4 arm.
	pip := &codec.Property{PID: codec.PIDValue, Data: []byte{10, 0, 0, 1}}
	vip, _ := decodePropertyValue(pip, codec.ObjTypeIPv4, codec.NumTypeIPv4, nil, 1)
	if vip.Kind != consumer.KindIPAddr || vip.IPAddr != [4]byte{10, 0, 0, 1} {
		t.Errorf("ipv4 = %+v, want 10.0.0.1", vip)
	}

	// string arm.
	pstr := &codec.Property{PID: codec.PIDValue, Data: []byte("hi\x00")}
	vs, _ := decodePropertyValue(pstr, codec.ObjTypeString, codec.NumTypeString, nil, 1)
	if vs.Kind != consumer.KindString || vs.Str != "hi" {
		t.Errorf("string = %+v, want hi", vs)
	}

	// unknown objType → raw fall-through.
	praw := &codec.Property{PID: codec.PIDValue, Data: []byte{9}}
	vr, _ := decodePropertyValue(praw, codec.ACP2ObjType(99), 0, nil, 1)
	if vr.Kind != consumer.KindRaw {
		t.Errorf("unknown objType = %+v, want raw", vr)
	}
}

// TestEncodeSetProperty_Branches covers the raw-escape-hatch, numeric,
// enum-via-Enum-field, ipv4-via-string, string, default-raw, and the
// default-error arm of encodeSetProperty.
func TestEncodeSetProperty_Branches(t *testing.T) {
	// raw escape hatch.
	if _, err := encodeSetProperty(codec.ObjTypeNumber, codec.NumTypeU32,
		nil, consumer.Value{Raw: []byte{1, 2, 3, 4}}, nil); err != nil {
		t.Errorf("raw escape hatch errored: %v", err)
	}
	// numeric.
	if _, err := encodeSetProperty(codec.ObjTypeNumber, codec.NumTypeS32,
		nil, consumer.Value{Kind: consumer.KindInt, Int: -3}, nil); err != nil {
		t.Errorf("numeric encode errored: %v", err)
	}
	// enum via Enum field (no reverse map).
	if _, err := encodeSetProperty(codec.ObjTypeEnum, codec.NumTypePreset,
		nil, consumer.Value{Kind: consumer.KindEnum, Enum: 1}, nil); err != nil {
		t.Errorf("enum encode errored: %v", err)
	}
	// ipv4 from string.
	prop, err := encodeSetProperty(codec.ObjTypeIPv4, codec.NumTypeIPv4,
		nil, consumer.Value{Kind: consumer.KindString, Str: "1.2.3.4"}, nil)
	if err != nil || len(prop.Data) < 4 || prop.Data[0] != 1 || prop.Data[3] != 4 {
		t.Errorf("ipv4 encode = %+v err=%v, want 1.2.3.4", prop.Data, err)
	}
	// string.
	if _, err := encodeSetProperty(codec.ObjTypeString, codec.NumTypeString,
		nil, consumer.Value{Kind: consumer.KindString, Str: "x"}, nil); err != nil {
		t.Errorf("string encode errored: %v", err)
	}
	// default arm with raw bytes.
	if _, err := encodeSetProperty(codec.ACP2ObjType(99), codec.NumTypeU32,
		nil, consumer.Value{Raw: []byte{1}, Int: 1}, nil); err != nil {
		t.Errorf("default-raw encode errored: %v", err)
	}
	// default arm with no raw → error.
	if _, err := encodeSetProperty(codec.ACP2ObjType(99), codec.NumTypeU32,
		nil, consumer.Value{Kind: consumer.KindInt, Int: 1}, nil); err == nil {
		t.Error("default arm with no raw should error")
	}
}

// TestResolveRequest_Branches covers label-no-tree, label-not-found,
// label-found, id-found-in-tree, and id-no-tree arms.
func TestResolveRequest_Branches(t *testing.T) {
	p := &Plugin{logger: unitLogger()}

	// label, no tree → ErrUnknownLabel.
	if _, _, _, _, err := p.resolveRequest(consumer.ValueRequest{Label: "X"}, nil); err == nil {
		t.Error("label with no tree should error")
	}

	tree := &WalkedTree{
		Objects:  []consumer.Object{{ID: 3, Label: "Vol"}},
		ObjTypes: []codec.ACP2ObjType{codec.ObjTypeNumber},
		NumTypes: []codec.NumberType{codec.NumTypeU32},
		Labels:   map[string]int{"Vol": 0},
	}
	// label not found.
	if _, _, _, _, err := p.resolveRequest(consumer.ValueRequest{Label: "Nope"}, tree); err == nil {
		t.Error("missing label should error")
	}
	// label found.
	id, ot, _, obj, err := p.resolveRequest(consumer.ValueRequest{Label: "Vol"}, tree)
	if err != nil || id != 3 || ot != codec.ObjTypeNumber || obj == nil {
		t.Errorf("label found = (%d,%d,%v,%v)", id, ot, obj, err)
	}
	// id found in tree.
	id2, _, _, obj2, err := p.resolveRequest(consumer.ValueRequest{ID: 3}, tree)
	if err != nil || id2 != 3 || obj2 == nil {
		t.Errorf("id found = (%d,%v,%v)", id2, obj2, err)
	}
	// id with no tree → unknown type, no error.
	id3, ot3, _, obj3, err := p.resolveRequest(consumer.ValueRequest{ID: 42}, nil)
	if err != nil || id3 != 42 || ot3 != 0 || obj3 != nil {
		t.Errorf("id no tree = (%d,%d,%v,%v)", id3, ot3, obj3, err)
	}
}

// TestBuildAnnounceClosure_Filtering drives the announce closure directly to
// cover the slot filter, id filter, tree label/path resolution, label
// filter, value-decode-from-tree, and the vtype fallback decode arm.
func TestBuildAnnounceClosure_Filtering(t *testing.T) {
	p := &Plugin{logger: unitLogger(), trees: newWalkedTreeCache(8, 0)}

	// Seed a tree so the closure resolves label/path + decodes via tree.
	tree := &WalkedTree{
		Slot:     1,
		Objects:  []consumer.Object{{ID: 5, Label: "Gain", Unit: "dB", Group: "BOARD", Path: []string{"ROOT", "BOARD", "Gain"}}},
		ObjTypes: []codec.ACP2ObjType{codec.ObjTypeNumber},
		NumTypes: []codec.NumberType{codec.NumTypeU32},
		OptionsMaps: []map[uint32]string{nil},
		Labels:   map[string]int{"Gain": 0},
	}
	p.trees.Put(1, tree)

	var got []consumer.Event
	fn := func(ev consumer.Event) { got = append(got, ev) }
	cl := p.buildAnnounceClosure(consumer.ValueRequest{Slot: 1, ID: 5, Label: "Gain"}, fn)

	valProp := codec.MakeValueProperty(codec.PIDValue, codec.NumTypeU32, []byte{0, 0, 0, 42})
	msg := &codec.ACP2Message{Type: codec.ACP2TypeAnnounce, PID: codec.PIDValue, ObjID: 5,
		Properties: []codec.Property{valProp}}

	// Wrong slot → filtered out.
	cl(2, msg)
	// Wrong id → filtered out.
	cl(1, &codec.ACP2Message{Type: codec.ACP2TypeAnnounce, PID: codec.PIDValue, ObjID: 999,
		Properties: []codec.Property{valProp}})
	// Match.
	cl(1, msg)
	if len(got) != 1 {
		t.Fatalf("got %d events, want exactly 1 (slot+id filters drop the rest)", len(got))
	}
	if got[0].Label != "Gain" || got[0].Path != "ROOT.BOARD.Gain" || got[0].Value.Uint != 42 {
		t.Errorf("event = %+v, want Gain/path/42", got[0])
	}

	// Label-mismatch filter: a sub wanting label "Other" on the same obj.
	var got2 []consumer.Event
	cl2 := p.buildAnnounceClosure(consumer.ValueRequest{Slot: -1, ID: -1, Label: "Other"},
		func(ev consumer.Event) { got2 = append(got2, ev) })
	cl2(1, msg)
	if len(got2) != 0 {
		t.Errorf("label-mismatch should filter, got %d", len(got2))
	}

	// vtype fallback: obj not in tree → decode from prop.VType.
	var got3 []consumer.Event
	cl3 := p.buildAnnounceClosure(consumer.ValueRequest{Slot: -1, ID: -1},
		func(ev consumer.Event) { got3 = append(got3, ev) })
	ipProp := codec.MakeValueProperty(codec.PIDValue, codec.NumTypeIPv4, []byte{8, 8, 8, 8})
	cl3(1, &codec.ACP2Message{Type: codec.ACP2TypeAnnounce, PID: codec.PIDValue, ObjID: 8888,
		Properties: []codec.Property{ipProp}})
	if len(got3) != 1 || got3[0].Value.Kind != consumer.KindIPAddr {
		t.Errorf("vtype-fallback event = %+v, want ipv4", got3)
	}
}

// ----------------------------------------------------------------------
// value_validate.go — validateValueAgainstType numeric + enum arms

// TestValidateValueAgainstType_NumericAndEnum covers the numeric
// unparseable + numeric-string reject arms and the enum str-reverse-map +
// known-wire-idx accept + not-in-options reject arms.
func TestValidateValueAgainstType_NumericAndEnum(t *testing.T) {
	// numeric unparseable (KindUnknown).
	if err := validateValueAgainstType(codec.ObjTypeNumber, codec.NumTypeU32, nil,
		consumer.Value{Kind: consumer.KindUnknown}, nil); err == nil {
		t.Error("numeric KindUnknown should reject")
	}
	// numeric got string.
	if err := validateValueAgainstType(codec.ObjTypeNumber, codec.NumTypeU32, nil,
		consumer.Value{Kind: consumer.KindString, Str: "abc"}, nil); err == nil {
		t.Error("numeric string should reject")
	}
	// numeric int accepted.
	if err := validateValueAgainstType(codec.ObjTypeNumber, codec.NumTypeU32, nil,
		consumer.Value{Kind: consumer.KindInt, Int: 1}, nil); err != nil {
		t.Errorf("numeric int rejected: %v", err)
	}

	rev := map[string]uint32{"On": 1, "Off": 0}
	// enum via Str reverse map.
	if err := validateValueAgainstType(codec.ObjTypeEnum, codec.NumTypePreset, nil,
		consumer.Value{Kind: consumer.KindString, Str: "On"}, rev); err != nil {
		t.Errorf("enum str-reverse rejected: %v", err)
	}
	// enum via known wire idx.
	if err := validateValueAgainstType(codec.ObjTypeEnum, codec.NumTypePreset, nil,
		consumer.Value{Kind: consumer.KindEnum, Enum: 1}, rev); err != nil {
		t.Errorf("enum known-idx rejected: %v", err)
	}
	// enum not in options.
	if err := validateValueAgainstType(codec.ObjTypeEnum, codec.NumTypePreset, nil,
		consumer.Value{Kind: consumer.KindEnum, Enum: 9}, rev); err == nil {
		t.Error("enum out-of-options should reject")
	}
}

// ----------------------------------------------------------------------
// session.go — direct-callable helpers

// TestSession_ReleaseMTIDZero covers releaseMTID's mtid==0 early return.
func TestSession_ReleaseMTIDZero(t *testing.T) {
	s := NewSession(unitLogger())
	s.releaseMTID(0) // must not panic / touch the pool
}

// TestSession_CloseLockedNilConn covers closeLocked's conn==nil early return
// via Disconnect on a never-connected session.
func TestSession_CloseLockedNilConn(t *testing.T) {
	s := NewSession(unitLogger())
	if err := s.Disconnect(); err != nil {
		t.Errorf("Disconnect on fresh session = %v, want nil", err)
	}
}

// TestSession_CloseLockedTimeoutArm covers closeLocked's time.After(closeWait)
// arm: a session with a live conn but NO reader goroutine (so s.done never
// closes) must still return — bounded by closeWait — instead of blocking
// forever. Uses a tiny closeWait so the test is fast and a net.Pipe conn so
// Close() is a real call.
func TestSession_CloseLockedTimeoutArm(t *testing.T) {
	s := NewSession(unitLogger())
	clientConn, serverConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	s.conn = clientConn
	s.done = make(chan struct{}) // never closed: no readLoop running
	s.closeWait = 5 * time.Millisecond

	start := time.Now()
	if err := s.Disconnect(); err != nil {
		t.Errorf("Disconnect = %v, want nil", err)
	}
	elapsed := time.Since(start)
	if elapsed >= 2*time.Second {
		t.Fatalf("closeLocked blocked %v — timeout arm not taken (used the 2s default?)", elapsed)
	}
	if elapsed < 5*time.Millisecond {
		t.Fatalf("closeLocked returned in %v — before closeWait elapsed, timeout arm not exercised", elapsed)
	}
	if s.conn != nil {
		t.Error("closeLocked should have nilled s.conn")
	}
}

// TestSession_CloseLockedDoneArm covers closeLocked's <-s.done arm: when the
// reader goroutine has already exited (done closed), close returns promptly
// without waiting on the closeWait timer.
func TestSession_CloseLockedDoneArm(t *testing.T) {
	s := NewSession(unitLogger())
	clientConn, serverConn := net.Pipe()
	defer func() { _ = serverConn.Close() }()
	s.conn = clientConn
	s.done = make(chan struct{})
	close(s.done) // reader already exited
	s.closeWait = 2 * time.Second

	start := time.Now()
	if err := s.Disconnect(); err != nil {
		t.Errorf("Disconnect = %v, want nil", err)
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("closeLocked blocked %v — done arm not taken", elapsed)
	}
}

// TestSession_MarkSlotProbedNegative covers the slot<0 guard.
func TestSession_MarkSlotProbedNegative(t *testing.T) {
	s := NewSession(unitLogger())
	s.MarkSlotProbed(-1, nil) // no-op, must not panic
	if len(s.slotStatus) != 0 {
		t.Errorf("negative slot probe grew tables to %d", len(s.slotStatus))
	}
}

// TestIsClosedErr covers the nil + non-OpError + matching-OpError arms.
func TestIsClosedErr(t *testing.T) {
	if isClosedErr(nil) {
		t.Error("nil err is not a closed err")
	}
	if isClosedErr(context.Canceled) {
		t.Error("non-OpError is not a closed err")
	}
	opErr := &net.OpError{Op: "read", Err: errString("use of closed network connection")}
	if !isClosedErr(opErr) {
		t.Error("matching OpError should be a closed err")
	}
	// A non-matching OpError inner message → not closed.
	if isClosedErr(&net.OpError{Op: "read", Err: errString("connection reset")}) {
		t.Error("non-matching OpError should not be a closed err")
	}
	// A WRAPPED matching OpError (as codec.ReadAN2Frame wraps it with %w) is
	// still detected via errors.As — this is the real-world readLoop shape.
	if !isClosedErr(fmt.Errorf("an2 read header: %w", opErr)) {
		t.Error("wrapped closed OpError should be detected via errors.As")
	}
	// net.ErrClosed (possibly wrapped) is detected via errors.Is.
	if !isClosedErr(fmt.Errorf("an2 read payload: %w", net.ErrClosed)) {
		t.Error("wrapped net.ErrClosed should be detected via errors.Is")
	}
}

// TestRouteReply_NoWaiter covers routeReply's no-waiter (orphan) arm.
func TestRouteReply_NoWaiter(t *testing.T) {
	s := NewSession(unitLogger())
	// No waiter registered for mtid 7 → orphan path (note + debug log).
	s.routeReply(7, &codec.ACP2Message{Type: codec.ACP2TypeReply, MTID: 7})
}

// TestRouteReply_ChannelFull covers routeReply's full-channel default arm.
func TestRouteReply_ChannelFull(t *testing.T) {
	s := NewSession(unitLogger())
	ch := make(chan *codec.ACP2Message, 1)
	ch <- &codec.ACP2Message{} // pre-fill so the next send hits default
	s.waitMu.Lock()
	s.waiters[3] = ch
	s.waitMu.Unlock()
	s.routeReply(3, &codec.ACP2Message{Type: codec.ACP2TypeReply, MTID: 3})
}

// TestHandleAN2Internal_EventAndDefault covers the AN2 slot-event arm
// (updates slotStatus) and the unhandled-type default arm.
func TestHandleAN2Internal_EventAndDefault(t *testing.T) {
	s := NewSession(unitLogger())
	s.slotStatus = make([]consumer.SlotStatus, 3)
	// Slot event updates status.
	s.handleAN2Internal(&codec.AN2Frame{
		Proto: codec.AN2ProtoInternal, Slot: 1, Type: codec.AN2TypeEvent,
		Payload: []byte{byte(consumer.SlotPresent)}})
	if s.slotStatus[1] != consumer.SlotPresent {
		t.Errorf("slot 1 status = %v, want present after event", s.slotStatus[1])
	}
	// Unhandled type (data) → default arm, no panic.
	s.handleAN2Internal(&codec.AN2Frame{
		Proto: codec.AN2ProtoInternal, Slot: 0, Type: codec.AN2TypeData})
}

// TestHandleACP2Frame_Guards covers the non-data, short-payload, decode-error,
// and announce-fanout arms of handleACP2Frame.
func TestHandleACP2Frame_Guards(t *testing.T) {
	s := NewSession(unitLogger())

	// Non-data frame → debug + return.
	s.handleACP2Frame(&codec.AN2Frame{Proto: codec.AN2ProtoACP2, Type: codec.AN2TypeReply})

	// Short payload (< ACP2HeaderSize) → warn + return.
	s.handleACP2Frame(&codec.AN2Frame{Proto: codec.AN2ProtoACP2, Type: codec.AN2TypeData,
		Payload: []byte{0x00}})

	// Announce fan-out to a registered sub.
	var got int
	s.SubscribeAnnounces(func(slot uint8, msg *codec.ACP2Message) { got++ })
	annPayload := []byte{byte(codec.ACP2TypeAnnounce), 0, 0, codec.PIDValue,
		0, 0, 0, 5, 0, 0, 0, 0}
	s.handleACP2Frame(&codec.AN2Frame{Proto: codec.AN2ProtoACP2, Type: codec.AN2TypeData,
		Slot: 1, Payload: annPayload})
	if got != 1 {
		t.Errorf("announce fanout fired %d times, want 1", got)
	}
}

// ----------------------------------------------------------------------
// session_health.go — probeReachable ctx-deadline arms

func TestProbeReachable_TightDeadline(t *testing.T) {
	// Deadline tighter than 500ms but still positive → shrinks d.Timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if probeReachable(ctx, "127.0.0.1", 1) {
		t.Error("probe to closed port should be false")
	}
}

func TestProbeReachable_ExpiredDeadline(t *testing.T) {
	// Already-expired deadline → d.Timeout <= 0 → immediate false.
	ctx, cancel := context.WithTimeout(context.Background(), -time.Second)
	defer cancel()
	if probeReachable(ctx, "127.0.0.1", 80) {
		t.Error("probe with expired deadline should be false")
	}
}

// ---- helpers ----

// errString is a trivial error whose Error() returns the literal string,
// used to seed a synthetic *net.OpError inner error for isClosedErr.
type errString string

func (e errString) Error() string { return string(e) }
