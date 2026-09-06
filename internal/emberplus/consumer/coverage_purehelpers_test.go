package emberplus

import (
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/consumer/compliance"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/export/canonical"
)

// TestAsNumber covers every accepted numeric kind plus rejection.
func TestAsNumber(t *testing.T) {
	for _, v := range []any{int64(3), int32(3), int(3), float64(3), float32(3)} {
		if f, ok := asNumber(v); !ok || f != 3 {
			t.Errorf("asNumber(%T) = (%v,%v)", v, f, ok)
		}
	}
	if _, ok := asNumber(nil); ok {
		t.Error("asNumber(nil) should be false")
	}
	if _, ok := asNumber("x"); ok {
		t.Error("asNumber(string) should be false")
	}
}

// TestAsFloatWriteFloat covers each Kind branch of asFloat + writeFloat,
// including the uint clamp-to-zero path.
func TestAsFloatWriteFloat(t *testing.T) {
	cases := []struct {
		kind consumer.ValueKind
		set  func(*consumer.Value)
		want float64
	}{
		{consumer.KindInt, func(v *consumer.Value) { v.Int = 5 }, 5},
		{consumer.KindUint, func(v *consumer.Value) { v.Uint = 9 }, 9},
		{consumer.KindFloat, func(v *consumer.Value) { v.Float = 2.5 }, 2.5},
		{consumer.KindString, func(v *consumer.Value) {}, 0},
	}
	for _, c := range cases {
		v := &consumer.Value{Kind: c.kind}
		c.set(v)
		if got := asFloat(v); got != c.want {
			t.Errorf("asFloat(kind=%v) = %v, want %v", c.kind, got, c.want)
		}
	}

	vi := &consumer.Value{Kind: consumer.KindInt}
	writeFloat(vi, 4.6)
	if vi.Int != 5 {
		t.Errorf("writeFloat int = %d, want 5", vi.Int)
	}
	vu := &consumer.Value{Kind: consumer.KindUint}
	writeFloat(vu, -3) // clamp to 0
	if vu.Uint != 0 {
		t.Errorf("writeFloat uint clamp = %d, want 0", vu.Uint)
	}
	vf := &consumer.Value{Kind: consumer.KindFloat}
	writeFloat(vf, 1.25)
	if vf.Float != 1.25 {
		t.Errorf("writeFloat float = %v", vf.Float)
	}
	vs := &consumer.Value{Kind: consumer.KindString}
	writeFloat(vs, 9) // no-op branch
}

// TestParamKindFrom covers each Glow ParameterType branch + unknown.
func TestParamKindFrom(t *testing.T) {
	cases := map[int64]consumer.ValueKind{
		glow.ParamTypeInteger: consumer.KindInt,
		glow.ParamTypeReal:    consumer.KindFloat,
		glow.ParamTypeString:  consumer.KindString,
		glow.ParamTypeBoolean: consumer.KindBool,
		glow.ParamTypeEnum:    consumer.KindEnum,
		glow.ParamTypeOctets:  consumer.KindRaw,
		glow.ParamTypeNull:    consumer.KindUnknown,
	}
	for ty, want := range cases {
		if got := paramKindFrom(&glow.Parameter{Type: ty}); got != want {
			t.Errorf("paramKindFrom(%d) = %v, want %v", ty, got, want)
		}
	}
}

// TestPopulateValue covers the int64 (with each obj.Kind sub-branch),
// float64, string, bool, []byte, and nil paths.
func TestPopulateValue(t *testing.T) {
	// nil value → no-op.
	o := &consumer.Object{}
	populateValue(o, &glow.Parameter{Value: nil})

	// int64 with each target kind.
	for _, k := range []consumer.ValueKind{
		consumer.KindUnknown, consumer.KindInt, consumer.KindUint,
		consumer.KindEnum, consumer.KindFloat, consumer.KindBool,
	} {
		obj := &consumer.Object{Kind: k, Value: consumer.Value{Kind: k}}
		populateValue(obj, &glow.Parameter{Value: int64(7)})
	}
	// float64 onto Unknown.
	of := &consumer.Object{Kind: consumer.KindUnknown}
	populateValue(of, &glow.Parameter{Value: float64(1.5)})
	if of.Value.Float != 1.5 {
		t.Errorf("float populate = %v", of.Value.Float)
	}
	// string onto Unknown.
	os := &consumer.Object{Kind: consumer.KindUnknown}
	populateValue(os, &glow.Parameter{Value: "hi"})
	if os.Value.Str != "hi" {
		t.Errorf("string populate = %q", os.Value.Str)
	}
	// bool onto Unknown.
	ob := &consumer.Object{Kind: consumer.KindUnknown}
	populateValue(ob, &glow.Parameter{Value: true})
	if !ob.Value.Bool {
		t.Error("bool populate")
	}
	// []byte.
	oraw := &consumer.Object{Kind: consumer.KindRaw}
	populateValue(oraw, &glow.Parameter{Value: []byte{1, 2}})
	if len(oraw.Value.Raw) != 2 {
		t.Errorf("raw populate = %v", oraw.Value.Raw)
	}
}

// TestAssignToValue_AllArms covers every value-type branch including the
// string and []byte arms the existing TestAssignToValue omits.
func TestAssignToValue_AllArms(t *testing.T) {
	v := &consumer.Value{}
	assignToValue(v, consumer.KindInt, int64(9))
	if v.Int != 9 || v.Float != 9 {
		t.Errorf("int = %+v", v)
	}
	assignToValue(v, consumer.KindFloat, float64(2.7))
	if v.Float != 2.7 || v.Int != 2 {
		t.Errorf("float = %+v", v)
	}
	assignToValue(v, consumer.KindString, "z")
	if v.Str != "z" {
		t.Errorf("string = %+v", v)
	}
	assignToValue(v, consumer.KindBool, true)
	if !v.Bool {
		t.Errorf("bool = %+v", v)
	}
	assignToValue(v, consumer.KindRaw, []byte{4, 5})
	if len(v.Raw) != 2 {
		t.Errorf("raw = %+v", v)
	}
}

// TestAppendUniqueCloneInt32 covers appendUnique (hit + miss) and
// cloneInt32Slice (empty + populated).
func TestAppendUniqueCloneInt32(t *testing.T) {
	s := appendUnique(nil, "a")
	s = appendUnique(s, "a") // dup, no-op
	s = appendUnique(s, "b")
	if len(s) != 2 {
		t.Errorf("appendUnique = %v, want [a b]", s)
	}
	if cloneInt32Slice(nil) != nil {
		t.Error("clone(nil) should be nil")
	}
	c := cloneInt32Slice([]int32{1, 2})
	if len(c) != 2 || c[0] != 1 {
		t.Errorf("clone = %v", c)
	}
}

// TestPathForElement covers the no-numeric-path branch (parent + name),
// including the empty-identifier fallback to the number.
func TestPathForElement(t *testing.T) {
	p := &Plugin{numPath: map[string]string{}}
	got := p.pathForElement(nil, "leaf", 9, []string{"root"})
	if len(got) != 2 || got[0] != "root" || got[1] != "leaf" {
		t.Errorf("named = %v", got)
	}
	got = p.pathForElement(nil, "", 9, []string{"root"})
	if got[1] != "9" {
		t.Errorf("number fallback = %v", got)
	}
}

// TestEntryIdentifier covers each glow-pointer branch plus the path /
// label fallbacks.
func TestEntryIdentifier(t *testing.T) {
	if got := entryIdentifier(&treeEntry{glowParam: &glow.Parameter{Identifier: "p"}}); got != "p" {
		t.Errorf("param = %q", got)
	}
	if got := entryIdentifier(&treeEntry{glowNode: &glow.Node{Identifier: "n"}}); got != "n" {
		t.Errorf("node = %q", got)
	}
	if got := entryIdentifier(&treeEntry{glowMatrix: &glow.Matrix{Identifier: "m"}}); got != "m" {
		t.Errorf("matrix = %q", got)
	}
	if got := entryIdentifier(&treeEntry{glowFunc: &glow.Function{Identifier: "f"}}); got != "f" {
		t.Errorf("func = %q", got)
	}
	if got := entryIdentifier(&treeEntry{obj: consumer.Object{Path: []string{"a", "b"}}}); got != "b" {
		t.Errorf("path = %q", got)
	}
	if got := entryIdentifier(&treeEntry{obj: consumer.Object{Label: "L"}}); got != "L" {
		t.Errorf("label = %q", got)
	}
}

// TestIdentityStringValue covers nil, KindString, glowParam fallback,
// and the empty result.
func TestIdentityStringValue(t *testing.T) {
	if got := identityStringValue(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
	e := &treeEntry{obj: consumer.Object{Value: consumer.Value{Kind: consumer.KindString, Str: "v"}}}
	if got := identityStringValue(e); got != "v" {
		t.Errorf("string = %q", got)
	}
	e2 := &treeEntry{glowParam: &glow.Parameter{Value: int64(12)}}
	if got := identityStringValue(e2); got != "12" {
		t.Errorf("glowParam fallback = %q", got)
	}
	e3 := &treeEntry{glowParam: &glow.Parameter{}}
	if got := identityStringValue(e3); got != "" {
		t.Errorf("empty = %q", got)
	}
}

// TestRootOIDSubtreeWorthy covers rootOID (nil + populated) and
// subtreeIsSlotWorthy (leaf-bearing, nested, and empty).
func TestRootOIDSubtreeWorthy(t *testing.T) {
	if got := rootOID(nil); got != "" {
		t.Errorf("rootOID(nil) = %q", got)
	}
	n := &canonical.Node{Header: canonical.Header{OID: "1.2"}}
	if got := rootOID(n); got != "1.2" {
		t.Errorf("rootOID = %q", got)
	}

	if subtreeIsSlotWorthy(nil) {
		t.Error("nil node not slot-worthy")
	}
	leafy := &canonical.Node{Header: canonical.Header{OID: "1"}}
	leafy.Children = []canonical.Element{&canonical.Parameter{}}
	if !subtreeIsSlotWorthy(leafy) {
		t.Error("node with a Parameter is slot-worthy")
	}
	nested := &canonical.Node{Header: canonical.Header{OID: "1"}}
	nested.Children = []canonical.Element{leafy}
	if !subtreeIsSlotWorthy(nested) {
		t.Error("nested leaf is slot-worthy")
	}
	empty := &canonical.Node{Header: canonical.Header{OID: "1"}}
	empty.Children = []canonical.Element{&canonical.Node{Header: canonical.Header{OID: "1.1"}}}
	if subtreeIsSlotWorthy(empty) {
		t.Error("empty container is not slot-worthy")
	}
}

// TestNormaliseLabelsField covers both wire shapes + nil fallthrough.
func TestNormaliseLabelsField(t *testing.T) {
	live := []map[string]any{{"basePath": "1.2", "description": "P"}}
	if got := normaliseLabelsField(live); len(got) != 1 || got[0].description != "P" {
		t.Errorf("live = %+v", got)
	}
	jsonShape := []any{
		map[string]any{"basePath": "3.4", "description": "L"},
		"skip",
	}
	if got := normaliseLabelsField(jsonShape); len(got) != 1 || got[0].basePath != "3.4" {
		t.Errorf("json = %+v", got)
	}
	if got := normaliseLabelsField(42); got != nil {
		t.Errorf("unknown = %v", got)
	}
}

// TestExtractLabelValues covers the prefix filter, the nested-OID skip,
// the non-string skip, and the empty-string skip.
func TestExtractLabelValues(t *testing.T) {
	objs := []consumer.Object{
		{OID: "1.2.1", Value: consumer.Value{Kind: consumer.KindString, Str: "OUT 1"}},
		{OID: "1.2.2", Value: consumer.Value{Kind: consumer.KindString, Str: "OUT 2"}},
		{OID: "1.2.3.1", Value: consumer.Value{Kind: consumer.KindString, Str: "deep"}}, // nested → skip
		{OID: "1.2.4", Value: consumer.Value{Kind: consumer.KindInt, Int: 5}},           // non-string → skip
		{OID: "1.2.5", Value: consumer.Value{Kind: consumer.KindString, Str: ""}},       // empty → skip
		{OID: "9.9.9", Value: consumer.Value{Kind: consumer.KindString, Str: "other"}},  // wrong prefix → skip
	}
	got := extractLabelValues(objs, "1.2")
	if len(got) != 2 || got["1"] != "OUT 1" || got["2"] != "OUT 2" {
		t.Errorf("extractLabelValues = %v", got)
	}
}

// TestSplitFormatUnit covers empty, format-only, and format+unit (after
// the ° marker).
func TestSplitFormatUnit(t *testing.T) {
	f, u := splitFormatUnit("")
	if f != nil || u != nil {
		t.Error("empty must return nil,nil")
	}
	f, u = splitFormatUnit("%d")
	if f == nil || *f != "%d" || u != nil {
		t.Errorf("format-only = %v,%v", f, u)
	}
	f, u = splitFormatUnit("%d°dB")
	if f == nil || *f != "%d" || u == nil || *u != "dB" {
		t.Errorf("format+unit = %v,%v", f, u)
	}
}

// TestEnumMapToCanonical covers the native-map path (with sorting +
// masked item), the legacy-enumeration-derived path, and the empty
// (nil) path.
func TestEnumMapToCanonical(t *testing.T) {
	p := &Plugin{profile: &compliance.Profile{}}

	if got := p.enumMapToCanonical(nil, ""); got != nil {
		t.Errorf("empty = %v, want nil", got)
	}

	// Native map: out-of-order keys + a masked (~) label.
	native := map[int64]string{2: "two", 0: "~zero", 1: "one"}
	got := p.enumMapToCanonical(native, "")
	if len(got) != 3 || got[0].Value != 0 || got[0].Key != "zero" || !got[0].Masked {
		t.Errorf("native = %+v", got)
	}
	if got[2].Value != 2 {
		t.Errorf("native not sorted: %+v", got)
	}

	// Legacy enumeration only (LF-joined), one masked item.
	legacy := p.enumMapToCanonical(nil, "a\n~b\nc")
	if len(legacy) != 3 || legacy[1].Key != "b" || !legacy[1].Masked {
		t.Errorf("legacy = %+v", legacy)
	}
	if got := p.profile.Snapshot()[EnumMapDerived]; got != 1 {
		t.Errorf("enum_map_derived = %d, want 1", got)
	}

	// Both present + count mismatch → EnumDoubleSource.
	p2 := &Plugin{profile: &compliance.Profile{}}
	p2.enumMapToCanonical(map[int64]string{0: "x"}, "x\ny")
	if got := p2.profile.Snapshot()[EnumDoubleSource]; got != 1 {
		t.Errorf("enum_double_source = %d, want 1", got)
	}
}

// TestStripMaskPrefix covers both branches.
func TestStripMaskPrefix(t *testing.T) {
	if got := stripMaskPrefix("~x"); got != "x" {
		t.Errorf("masked = %q", got)
	}
	if got := stripMaskPrefix("y"); got != "y" {
		t.Errorf("plain = %q", got)
	}
}

// TestSessionAccessors covers SetRecorder, noteCompliance, and rxAge
// (zero + non-zero) which the loopback path never exercises directly.
func TestSessionAccessors(t *testing.T) {
	s := NewSession(discardLogger())
	prof := &compliance.Profile{}
	s.SetProfile(prof)

	// rxAge zero before any rx.
	if s.rxAge() != 0 {
		t.Error("rxAge should be 0 before first rx")
	}
	s.touchRX()
	// After touchRX, lastRX is non-zero so rxAge takes the time.Since
	// branch (a coarse monotonic clock may report exactly 0ns, so we
	// only require non-negative).
	if s.rxAge() < 0 {
		t.Error("rxAge should be >= 0 after touchRX")
	}

	// noteCompliance routes into the profile.
	s.noteCompliance(NonQualifiedElement)
	if got := prof.Snapshot()[NonQualifiedElement]; got != 1 {
		t.Errorf("noteCompliance count = %d, want 1", got)
	}

	// SetRecorder accepts nil (clear) without error/panic.
	s.SetRecorder(nil)
}
