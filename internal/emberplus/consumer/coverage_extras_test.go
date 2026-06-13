package emberplus

import (
	"context"
	"errors"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/datastore"
	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/export/canonical"
)

// TestSplitAndPersistDM drives the DM-split persistence over a tree that
// has a matrix-bearing slot, plus the rootOID / subtreeIsSlotWorthy /
// splitIdentityForSplit helpers.
func TestSplitAndPersistDM(t *testing.T) {
	p := newTreePlugin()
	// Tree shape that has a root-child Node ("slot1") containing a matrix,
	// matching the TinyEmber slot-detection contract.
	p.handleElements([]glow.Element{
		{Node: &glow.Node{Path: []int32{1}, Number: 1, Identifier: "root", IsOnline: true}},
		{Node: &glow.Node{Path: []int32{1, 1}, Number: 1, Identifier: "slot1", IsOnline: true}},
		{Matrix: &glow.Matrix{
			Path: []int32{1, 1, 1}, Number: 1, Identifier: "mat",
			MatrixType: glow.MatrixTypeNToN, TargetCount: 2, SourceCount: 2,
		}},
		{Parameter: &glow.Parameter{
			Path: []int32{1, 1, 2}, Number: 2, Identifier: "gain",
			Type: glow.ParamTypeInteger, Value: int64(1), Access: glow.AccessReadWrite, IsOnline: true,
		}},
	})
	store := datastore.NewTreeStore(t.TempDir())
	refs, err := p.SplitAndPersistDM(context.Background(), store, "TinyEmberRouter@1.6.2")
	if err != nil {
		t.Fatalf("SplitAndPersistDM: %v", err)
	}
	if len(refs) == 0 {
		t.Error("expected at least one slot DM persisted")
	}

	// Helpers.
	if rootOID(nil) != "" {
		t.Error("rootOID(nil) should be empty")
	}
	// Empty node is NOT slot-worthy; a node with a Parameter child is.
	if subtreeIsSlotWorthy(&canonical.Node{Header: canonical.Header{Children: canonical.EmptyChildren()}}) {
		t.Error("empty node should not be slot-worthy")
	}
	worthy := &canonical.Node{Header: canonical.Header{Children: []canonical.Element{
		&canonical.Parameter{Header: canonical.Header{Children: canonical.EmptyChildren()}, Type: canonical.ParamInteger},
	}}}
	if !subtreeIsSlotWorthy(worthy) {
		t.Error("node with a parameter child should be slot-worthy")
	}
	model, rev := splitIdentityForSplit("foo@1.2.3")
	if model != "foo" || rev != "1.2.3" {
		t.Errorf("splitIdentityForSplit = %q/%q", model, rev)
	}
	if m, r := splitIdentityForSplit("noat"); m != "noat" || r != "" {
		t.Errorf("splitIdentityForSplit no-@ = %q/%q", m, r)
	}
}

// TestDecodeStreamBytes covers every stream format width + the
// short-buffer / unknown-format guards, plus streamFormatSize.
func TestDecodeStreamBytes(t *testing.T) {
	cases := []struct {
		format int64
		buf    []byte
	}{
		{glow.StreamFmtUnsignedInt8, []byte{0x05}},
		{glow.StreamFmtSignedInt8, []byte{0xFF}},
		{glow.StreamFmtUnsignedInt16BigEndian, []byte{0x01, 0x02}},
		{glow.StreamFmtUnsignedInt16LittleEndian, []byte{0x02, 0x01}},
		{glow.StreamFmtSignedInt16BigEndian, []byte{0xFF, 0xFE}},
		{glow.StreamFmtSignedInt16LittleEndian, []byte{0xFE, 0xFF}},
		{glow.StreamFmtUnsignedInt32BigEndian, []byte{0, 0, 0, 1}},
		{glow.StreamFmtUnsignedInt32LittleEndian, []byte{1, 0, 0, 0}},
		{glow.StreamFmtSignedInt32BigEndian, []byte{0xFF, 0xFF, 0xFF, 0xFF}},
		{glow.StreamFmtSignedInt32LittleEndian, []byte{0xFF, 0xFF, 0xFF, 0xFF}},
		{glow.StreamFmtFloat32BigEndian, []byte{0x3f, 0x80, 0x00, 0x00}},
		{glow.StreamFmtFloat32LittleEndian, []byte{0x00, 0x00, 0x80, 0x3f}},
		{glow.StreamFmtFloat64BigEndian, []byte{0x3f, 0xf0, 0, 0, 0, 0, 0, 0}},
		{glow.StreamFmtFloat64LittleEndian, []byte{0, 0, 0, 0, 0, 0, 0xf0, 0x3f}},
	}
	for _, c := range cases {
		if decodeStreamBytes(c.format, c.buf) == nil {
			t.Errorf("decodeStreamBytes(%d) returned nil", c.format)
		}
	}
	// Short buffer + unknown format.
	if decodeStreamBytes(glow.StreamFmtUnsignedInt32BigEndian, []byte{0x01}) != nil {
		t.Error("short buffer should decode nil")
	}
	if decodeStreamBytes(999, []byte{0x01}) != nil {
		t.Error("unknown format should decode nil")
	}
	if streamFormatSize(999) != 0 {
		t.Error("unknown format size should be 0")
	}
	// 64-bit unsigned sizes (size lookup only).
	if streamFormatSize(glow.StreamFmtUnsignedInt64BigEndian) != 8 {
		t.Error("uint64 size should be 8")
	}
}

// TestAssignToValue covers each kind arm of assignToValue.
func TestAssignToValue(t *testing.T) {
	var v consumer.Value
	assignToValue(&v, consumer.KindInt, int64(7))
	if v.Int != 7 || v.Float != 7 {
		t.Errorf("int assign: %+v", v)
	}
	assignToValue(&v, consumer.KindFloat, float64(1.5))
	if v.Float != 1.5 {
		t.Errorf("float assign: %+v", v)
	}
	assignToValue(&v, consumer.KindBool, true)
	if !v.Bool {
		t.Errorf("bool assign: %+v", v)
	}
}

// TestValueToGlow covers each ValueKind arm + the string/int fallthrough.
func TestValueToGlow(t *testing.T) {
	if valueToGlow(consumer.Value{Kind: consumer.KindInt, Int: 3}).(int64) != 3 {
		t.Error("int")
	}
	if valueToGlow(consumer.Value{Kind: consumer.KindUint, Uint: 4}).(int64) != 4 {
		t.Error("uint")
	}
	if valueToGlow(consumer.Value{Kind: consumer.KindFloat, Float: 1.5}).(float64) != 1.5 {
		t.Error("float")
	}
	if valueToGlow(consumer.Value{Kind: consumer.KindString, Str: "s"}).(string) != "s" {
		t.Error("string")
	}
	if valueToGlow(consumer.Value{Kind: consumer.KindBool, Bool: true}).(bool) != true {
		t.Error("bool")
	}
	if valueToGlow(consumer.Value{Kind: consumer.KindEnum, Enum: 2}).(int64) != 2 {
		t.Error("enum")
	}
	// Fallthrough: unknown kind with a Str set returns the string.
	if valueToGlow(consumer.Value{Str: "x"}).(string) != "x" {
		t.Error("fallthrough str")
	}
	// Fallthrough: unknown kind, no str → Int.
	if valueToGlow(consumer.Value{Int: 9}).(int64) != 9 {
		t.Error("fallthrough int")
	}
}

// TestFreshnessLabel covers each freshness state + the empty default.
func TestFreshnessLabel(t *testing.T) {
	if freshnessLabel(FreshnessLive) != "live" ||
		freshnessLabel(FreshnessUpdated) != "updated" ||
		freshnessLabel(FreshnessStale) != "stale" ||
		freshnessLabel(Freshness(99)) != "" {
		t.Error("freshnessLabel arms")
	}
}

// TestLayeredError_Error covers the Error() string formatting with and
// without an inner cause.
func TestLayeredError_Error(t *testing.T) {
	noCause := WrapProto("boom", nil)
	if noCause.Error() == "" {
		t.Error("error string empty (no cause)")
	}
	withCause := WrapProto("boom", errors.New("inner"))
	if withCause.Error() == "" {
		t.Error("error string empty (with cause)")
	}
	// Unwrap exposes the cause.
	if errors.Unwrap(withCause) == nil {
		t.Error("Unwrap should expose inner cause")
	}
}

// TestCanonicalize_EmptyTree covers the emptyRoot fallback when the tree
// has no entries.
func TestCanonicalize_EmptyTree(t *testing.T) {
	p := newTreePlugin()
	exp, err := p.Canonicalize(context.Background(), CanonicalOptions{})
	if err != nil {
		t.Fatalf("Canonicalize empty: %v", err)
	}
	if exp.Root == nil {
		t.Error("empty tree should still yield a synthetic root")
	}
}

// TestCanonicalAccess covers every access mapping + default.
func TestCanonicalAccess(t *testing.T) {
	got := map[string]bool{
		canonicalAccess(0): true, canonicalAccess(1): true,
		canonicalAccess(2): true, canonicalAccess(3): true,
		canonicalAccess(99): true,
	}
	if len(got) < 3 {
		t.Errorf("canonicalAccess arms collapsed: %v", got)
	}
	// optString / optInt64 nil + non-nil.
	if optString("") != nil || optString("x") == nil {
		t.Error("optString arms")
	}
	if optInt64(0) != nil || optInt64(5) == nil {
		t.Error("optInt64 arms")
	}
}

// TestSessionInvocationRegistry covers NextInvocationID / Register /
// Unregister / deliverInvocationResult on a non-connected Session.
func TestSessionInvocationRegistry(t *testing.T) {
	s := NewSession(nil)
	id1 := s.NextInvocationID()
	id2 := s.NextInvocationID()
	if id2 != id1+1 {
		t.Errorf("NextInvocationID not monotonic: %d then %d", id1, id2)
	}
	ch := make(chan *glow.InvocationResult, 1)
	s.RegisterInvocation(id1, ch)
	s.deliverInvocationResult(&glow.InvocationResult{InvocationID: id1, Success: true})
	select {
	case r := <-ch:
		if r.InvocationID != id1 {
			t.Errorf("delivered wrong id: %d", r.InvocationID)
		}
	default:
		t.Error("result not delivered")
	}
	// Deliver to an unregistered id → no-op (no panic).
	s.deliverInvocationResult(&glow.InvocationResult{InvocationID: 999})
	s.UnregisterInvocation(id1)
	s.deliverInvocationResult(&glow.InvocationResult{InvocationID: id1})
}
