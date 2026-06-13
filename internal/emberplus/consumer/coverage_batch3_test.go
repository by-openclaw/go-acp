package emberplus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
)

// TestSignalPendingSet_Branches covers signalPendingSet's nil-guard,
// coerced-mismatch, and already-timed-out (drop) branches.
func TestSignalPendingSet_Branches(t *testing.T) {
	p := freshPlugin()

	// nil entry / nil glowParam → early return.
	p.signalPendingSet(nil)
	p.signalPendingSet(&treeEntry{})

	// Coerced mismatch: expected 5, provider echoed 9.
	key := "1.1"
	ps := &pendingSet{expected: int64(5), kind: consumer.KindInt, done: make(chan pendingResult, 1)}
	p.pendingSets.register(key, ps)
	entry := &treeEntry{
		numericPath: []int32{1, 1},
		glowParam:   &glow.Parameter{Value: int64(9)},
		obj:         consumer.Object{OID: "1.1", Value: consumer.Value{Kind: consumer.KindInt, Int: 9}},
	}
	p.signalPendingSet(entry)
	res := <-ps.done
	if res.err == nil {
		t.Error("mismatch should report a coerced error")
	}

	// Drop branch: pending channel already full (caller timed out).
	ps2 := &pendingSet{expected: int64(1), kind: consumer.KindInt, done: make(chan pendingResult, 1)}
	ps2.done <- pendingResult{} // pre-fill so the send default-drops
	p.pendingSets.register(key, ps2)
	p.signalPendingSet(entry) // must not block or panic
}

// TestDisplayValueAndUnit_Nil covers the nil-entry guard.
func TestDisplayValueAndUnit_Nil(t *testing.T) {
	v, u := displayValueAndUnit(nil)
	if v.Kind != consumer.KindUnknown || u != "" {
		t.Errorf("nil entry = (%+v, %q)", v, u)
	}
}

// TestGlowSnapshot_Branches covers the cancelled-ctx return, the
// no-concrete-glow-type continue, and the multi-template sort.
func TestGlowSnapshot_Branches(t *testing.T) {
	p := freshPlugin()

	// Cancelled ctx → error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.GlowSnapshot(ctx); err == nil {
		t.Error("cancelled ctx should error")
	}

	// One real entry, one entry with no glow pointer (continue), and two
	// templates so the sort comparator runs.
	p.numIndex["1"] = &treeEntry{numericPath: []int32{1}, glowNode: &glow.Node{Identifier: "root"}, obj: consumer.Object{OID: "1"}}
	p.numIndex["2"] = &treeEntry{numericPath: []int32{2}, obj: consumer.Object{OID: "2"}} // no glow → skipped
	p.templates["10.2"] = &glow.Template{Path: []int32{10, 2}}
	p.templates["10.1"] = &glow.Template{Path: []int32{10, 1}}

	dump, err := p.GlowSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GlowSnapshot: %v", err)
	}
	if len(dump.Entries) != 1 {
		t.Errorf("entries = %d, want 1 (the no-glow entry skipped)", len(dump.Entries))
	}
	if len(dump.Templates) != 2 {
		t.Errorf("templates = %d, want 2", len(dump.Templates))
	}
}

// TestUnknownCTXAudit_RecordEmptyTags covers record's empty-tags early
// return.
func TestUnknownCTXAudit_RecordEmptyTags(t *testing.T) {
	a := newUnknownCTXAudit()
	a.record("node", []int32{1}, nil) // empty tags → no-op
	if len(a.snapshot()) != 0 {
		t.Error("empty tags should record nothing")
	}
}

// TestRenderUnknownCTXMarkdown_Branches covers the host-without-port
// branch, an unknown kind (title fallthrough), and an empty-samples row.
func TestRenderUnknownCTXMarkdown_Branches(t *testing.T) {
	rows := []unknownCTXRow{
		{Key: unknownCTXKey{Kind: "weird", Tag: 7}, Count: 1, SampleOIDs: nil}, // unknown kind + no samples
	}
	md := renderUnknownCTXMarkdown(unknownCTXHeader{Host: "h"}, rows) // host, no port
	if !strings.Contains(md, "**Endpoint**: h\n") {
		t.Errorf("host-without-port endpoint missing:\n%s", md)
	}
	if !strings.Contains(md, "## weird") {
		t.Errorf("unknown-kind title fallthrough missing:\n%s", md)
	}
	if !strings.Contains(md, "_(no OID captured)_") {
		t.Errorf("empty-samples placeholder missing:\n%s", md)
	}
}

// TestWriteUnknownCTXReport_MkdirError covers the mkdir error path: a
// path whose parent is an existing FILE (so MkdirAll fails).
func TestWriteUnknownCTXReport_MkdirError(t *testing.T) {
	dir := t.TempDir()
	fileAsDir := filepath.Join(dir, "afile")
	if err := os.WriteFile(fileAsDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// parent path is a file → MkdirAll under it must fail.
	path := filepath.Join(fileAsDir, "sub", "out.md")
	rows := []unknownCTXRow{{Key: unknownCTXKey{Kind: "node", Tag: 1}, Count: 1}}
	if err := writeUnknownCTXReport(path, unknownCTXHeader{}, rows); err == nil {
		t.Error("writeUnknownCTXReport should fail when the parent is a file")
	}
}
