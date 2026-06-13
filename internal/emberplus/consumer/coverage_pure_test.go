package emberplus

import (
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
)

// TestCatalogue covers Catalogue, CatalogueEntry.Address, LookupCatalogue
// (kind/cmd/oid/path + misses), isOIDPath, and isDottedPath.
func TestCatalogue(t *testing.T) {
	if len(Catalogue()) == 0 {
		t.Fatal("empty catalogue")
	}
	if got := (CatalogueEntry{Kind: KindElement, Name: "Node"}).Address(); got != "kind:Node" {
		t.Errorf("Address = %q", got)
	}
	// kind: lookup hit + miss.
	if _, ok := LookupCatalogue("kind:Parameter"); !ok {
		t.Error("kind:Parameter should resolve")
	}
	if _, ok := LookupCatalogue("kind:Nope"); ok {
		t.Error("kind:Nope should miss")
	}
	// cmd: lookup.
	if _, ok := LookupCatalogue("cmd:GetDirectory"); !ok {
		t.Error("cmd:GetDirectory should resolve")
	}
	// OID path (synthetic).
	if e, ok := LookupCatalogue("1.2.4.1"); !ok || e.Kind != KindOID {
		t.Errorf("oid path: e=%+v ok=%v", e, ok)
	}
	// Dotted label path (synthetic).
	if e, ok := LookupCatalogue("root.foo.bar"); !ok || e.Kind != KindPath {
		t.Errorf("dotted path: e=%+v ok=%v", e, ok)
	}
	// Bare single identifier (no dot, not OID) → miss.
	if _, ok := LookupCatalogue("loner"); ok {
		t.Error("bare identifier should miss")
	}
	// isOIDPath / isDottedPath edge cases.
	if isOIDPath("") || isOIDPath("a.b") || !isOIDPath("1.2") {
		t.Error("isOIDPath edges")
	}
	if isDottedPath("") || isDottedPath("nodot") || !isDottedPath("a.b") || isDottedPath("a/b") {
		t.Error("isDottedPath edges")
	}
}

// TestDiffParameters covers diffParameters across every field plus the
// nil-input guard, and valueEqual / formatAny / accessLabel / boolLabel /
// compactEnumeration helpers.
func TestDiffParameters(t *testing.T) {
	if diffParameters(nil, &glow.Parameter{}) != nil {
		t.Error("nil prev should return nil")
	}
	if diffParameters(&glow.Parameter{}, nil) != nil {
		t.Error("nil next should return nil")
	}

	prev := &glow.Parameter{
		Value: int64(1), Description: "a", Access: glow.AccessRead,
		Minimum: int64(0), Maximum: int64(10), Step: int64(1), Default: int64(0),
		Format: "%d", Factor: 1, Formula: "f1",
		Enumeration: "x\ny\nz\nw", StreamIdentifier: 1, IsOnline: true,
	}
	next := &glow.Parameter{
		Value: int64(2), Description: "b", Access: glow.AccessReadWrite,
		Minimum: int64(1), Maximum: int64(20), Step: int64(2), Default: int64(1),
		Format: "%f", Factor: 2, Formula: "f2",
		Enumeration: "p\nq\nr\ns\nt", StreamIdentifier: 2, IsOnline: false,
	}
	changes := diffParameters(prev, next)
	if len(changes) == 0 {
		t.Fatal("expected field changes")
	}
	// No-diff case: identical params produce no changes.
	if got := diffParameters(prev, prev); len(got) != 0 {
		t.Errorf("identical params should diff empty, got %v", got)
	}

	// valueEqual.
	if !valueEqual(nil, nil) || valueEqual(nil, 1) || valueEqual(1, nil) || !valueEqual(1, 1) || valueEqual(1, 2) {
		t.Error("valueEqual arms")
	}
	// formatAny: nil, string, bytes, default.
	if formatAny(nil) != "nil" || formatAny("s") != `"s"` ||
		formatAny([]byte{1, 2}) != "2 bytes" || formatAny(int64(7)) != "7" {
		t.Error("formatAny arms")
	}
	// accessLabel: read(0), R, W, RW, unknown.
	if accessLabel(0) != "R" || accessLabel(glow.AccessWrite) != "W" ||
		accessLabel(glow.AccessReadWrite) != "RW" || accessLabel(99) == "" {
		t.Error("accessLabel arms")
	}
	if boolLabel(true) != "y" || boolLabel(false) != "n" {
		t.Error("boolLabel arms")
	}
	// compactEnumeration: empty, short (<=3), long (>3).
	if compactEnumeration("") != "" {
		t.Error("compactEnumeration empty")
	}
	if compactEnumeration("a\nb") != "a\nb" {
		t.Error("compactEnumeration short passthrough")
	}
	if compactEnumeration("a\nb\nc\nd") == "" {
		t.Error("compactEnumeration long summary")
	}
}

// TestPendingSetRegistry covers register / unregister / take (hit + miss)
// and valuesMatch across nil, float-tolerance, and exact arms.
func TestPendingSetRegistry(t *testing.T) {
	r := newPendingSetRegistry()
	ps := &pendingSet{expected: int64(1), kind: consumer.KindInt, done: make(chan pendingResult, 1)}
	r.register("1.1", ps)
	if got := r.take("1.1"); got != ps {
		t.Error("take should return the registered pendingSet")
	}
	if got := r.take("1.1"); got != nil {
		t.Error("second take should miss")
	}
	r.register("1.2", ps)
	r.unregister("1.2")
	if got := r.take("1.2"); got != nil {
		t.Error("unregistered key should miss")
	}

	// valuesMatch.
	if !valuesMatch(nil, nil, consumer.KindInt) {
		t.Error("nil==nil should match")
	}
	if valuesMatch(nil, 1, consumer.KindInt) || valuesMatch(1, nil, consumer.KindInt) {
		t.Error("nil vs value should not match")
	}
	if !valuesMatch(float64(1.0), float64(1.0000001), consumer.KindFloat) {
		t.Error("float within tolerance should match")
	}
	if valuesMatch(float64(1.0), float64(2.0), consumer.KindFloat) {
		t.Error("float outside tolerance should not match")
	}
	if !valuesMatch(int64(5), int64(5), consumer.KindInt) {
		t.Error("exact int should match")
	}
	if valuesMatch("a", "b", consumer.KindString) {
		t.Error("different strings should not match")
	}
	// float kind but non-float values falls through to string compare.
	if !valuesMatch("x", "x", consumer.KindFloat) {
		t.Error("float-kind non-float values fall back to string equal")
	}
}

// TestNameMappers covers the pure Glow enum → name helpers across every
// case + the default fallbacks.
func TestNameMappers(t *testing.T) {
	paramTypes := map[int64]string{
		glow.ParamTypeNull: "null", glow.ParamTypeInteger: "integer",
		glow.ParamTypeReal: "real", glow.ParamTypeString: "string",
		glow.ParamTypeBoolean: "boolean", glow.ParamTypeTrigger: "trigger",
		glow.ParamTypeEnum: "enum", glow.ParamTypeOctets: "octets",
	}
	for v, want := range paramTypes {
		if got := paramTypeName(v); got != want {
			t.Errorf("paramTypeName(%d)=%q want %q", v, got, want)
		}
	}
	if paramTypeName(999) != "" {
		t.Error("paramTypeName default")
	}

	if matrixTypeName(glow.MatrixTypeOneToN) != "oneToN" ||
		matrixTypeName(glow.MatrixTypeOneToOne) != "oneToOne" ||
		matrixTypeName(glow.MatrixTypeNToN) != "nToN" ||
		matrixTypeName(99) != "oneToN" {
		t.Error("matrixTypeName arms")
	}
	if matrixAddrName(glow.MatrixAddrNonLinear) != "nonLinear" || matrixAddrName(0) != "linear" {
		t.Error("matrixAddrName arms")
	}
	if connOpName(glow.ConnOpConnect) != "connect" ||
		connOpName(glow.ConnOpDisconnect) != "disconnect" ||
		connOpName(99) != "absolute" {
		t.Error("connOpName arms")
	}
	if connDispName(glow.ConnDispModified) != "modified" ||
		connDispName(glow.ConnDispPending) != "pending" ||
		connDispName(glow.ConnDispLocked) != "locked" ||
		connDispName(99) != "tally" {
		t.Error("connDispName arms")
	}

	// streamFormatName across every defined format + default.
	fmts := []int64{
		glow.StreamFmtUnsignedInt8, glow.StreamFmtUnsignedInt16BigEndian,
		glow.StreamFmtUnsignedInt16LittleEndian, glow.StreamFmtUnsignedInt32BigEndian,
		glow.StreamFmtUnsignedInt32LittleEndian, glow.StreamFmtUnsignedInt64BigEndian,
		glow.StreamFmtUnsignedInt64LittleEndian, glow.StreamFmtSignedInt8,
		glow.StreamFmtSignedInt16BigEndian, glow.StreamFmtSignedInt16LittleEndian,
		glow.StreamFmtSignedInt32BigEndian, glow.StreamFmtSignedInt32LittleEndian,
		glow.StreamFmtSignedInt64BigEndian, glow.StreamFmtSignedInt64LittleEndian,
		glow.StreamFmtFloat32BigEndian, glow.StreamFmtFloat32LittleEndian,
		glow.StreamFmtFloat64BigEndian, glow.StreamFmtFloat64LittleEndian,
	}
	for _, f := range fmts {
		if streamFormatName(f) == "" {
			t.Errorf("streamFormatName(%d) empty", f)
		}
	}
	if streamFormatName(999) != "" {
		t.Error("streamFormatName default")
	}

	// inferParamType across types + default.
	if inferParamType(int64(1)) != "integer" || inferParamType(1.0) != "real" ||
		inferParamType("s") != "string" || inferParamType(true) != "boolean" ||
		inferParamType([]byte{1}) != "octets" || inferParamType(struct{}{}) != "" {
		t.Error("inferParamType arms")
	}
}
