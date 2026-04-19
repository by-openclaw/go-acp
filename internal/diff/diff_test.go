package diff

import (
	"bytes"
	"strings"
	"testing"

	"acp/internal/export/canonical"
)

// stringPtr + helpers for building synthetic elements.
func stringPtr(s string) *string { return &s }

// makeExport builds a minimal canonical.Export whose Root is a Node
// containing the given children. The root itself carries OID "1".
func makeExport(children ...canonical.Element) *canonical.Export {
	root := &canonical.Node{
		Header: canonical.Header{
			Number:     1,
			Identifier: "root",
			Path:       "root",
			OID:        "1",
			Access:     canonical.AccessRead,
			Children:   children,
		},
	}
	return &canonical.Export{Root: root}
}

// makeParam is a small factory that populates common Parameter
// fields so tests stay readable.
func makeParam(oid, path, typ string, opts ...func(*canonical.Parameter)) *canonical.Parameter {
	p := &canonical.Parameter{
		Header: canonical.Header{
			Number:     1,
			Identifier: "p",
			Path:       path,
			OID:        oid,
			Access:     canonical.AccessReadWrite,
		},
		Type: typ,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// TestDiff_NoChanges — identical trees return no entries.
func TestDiff_NoChanges(t *testing.T) {
	a := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal))
	b := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal))
	r := Diff(a, b)
	if len(r.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d: %+v", len(r.Entries), r.Entries)
	}
}

// TestDiff_AccessNarrowsIsBreaking — RW → R classifies as Breaking.
func TestDiff_AccessNarrowsIsBreaking(t *testing.T) {
	before := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Access = canonical.AccessReadWrite
	}))
	after := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Access = canonical.AccessRead
	}))

	r := Diff(before, after)
	if r.Counts()[CategoryBreaking] != 1 {
		t.Errorf("Breaking count got %d, want 1", r.Counts()[CategoryBreaking])
	}
	if !strings.Contains(r.Entries[0].Message, "access changed readWrite → read") {
		t.Errorf("message unexpected: %s", r.Entries[0].Message)
	}
}

// TestDiff_AccessWidensIsChanged — R → RW is Changed (new capability).
func TestDiff_AccessWidensIsChanged(t *testing.T) {
	before := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Access = canonical.AccessRead
	}))
	after := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Access = canonical.AccessReadWrite
	}))
	r := Diff(before, after)
	if r.Counts()[CategoryChanged] != 1 {
		t.Errorf("Changed count got %d, want 1", r.Counts()[CategoryChanged])
	}
	if r.Counts()[CategoryBreaking] != 0 {
		t.Errorf("Breaking count got %d, want 0", r.Counts()[CategoryBreaking])
	}
}

// TestDiff_MaxNarrowsIsBreaking — new max < old max excludes valid values.
func TestDiff_MaxNarrowsIsBreaking(t *testing.T) {
	before := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Maximum = float64(12)
	}))
	after := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Maximum = float64(6)
	}))
	r := Diff(before, after)
	if r.Counts()[CategoryBreaking] != 1 {
		t.Errorf("Breaking count got %d, want 1", r.Counts()[CategoryBreaking])
	}
}

// TestDiff_MaxWidensIsChanged — new max > old max admits more values.
func TestDiff_MaxWidensIsChanged(t *testing.T) {
	before := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Maximum = float64(12)
	}))
	after := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Maximum = float64(18)
	}))
	r := Diff(before, after)
	if r.Counts()[CategoryChanged] != 1 {
		t.Errorf("Changed count got %d, want 1", r.Counts()[CategoryChanged])
	}
	if r.Counts()[CategoryBreaking] != 0 {
		t.Errorf("Breaking count got %d, want 0", r.Counts()[CategoryBreaking])
	}
}

// TestDiff_EnumValueRemovedIsBreaking — losing an enum option breaks
// every consumer that held that value.
func TestDiff_EnumValueRemovedIsBreaking(t *testing.T) {
	before := makeExport(makeParam("1.1", "root.mode", canonical.ParamEnum, func(p *canonical.Parameter) {
		p.EnumMap = []canonical.EnumEntry{
			{Key: "Off", Value: 0},
			{Key: "On", Value: 1},
			{Key: "Experimental", Value: 2},
		}
	}))
	after := makeExport(makeParam("1.1", "root.mode", canonical.ParamEnum, func(p *canonical.Parameter) {
		p.EnumMap = []canonical.EnumEntry{
			{Key: "Off", Value: 0},
			{Key: "On", Value: 1},
		}
	}))
	r := Diff(before, after)
	if r.Counts()[CategoryBreaking] != 1 {
		t.Errorf("Breaking count got %d, want 1: %+v", r.Counts()[CategoryBreaking], r.Entries)
	}
	if !strings.Contains(r.Entries[0].Message, `enum removed value "Experimental"`) {
		t.Errorf("message unexpected: %s", r.Entries[0].Message)
	}
}

// TestDiff_ParameterAddedAndRemoved — new OID = Added, missing OID = Removed.
func TestDiff_ParameterAddedAndRemoved(t *testing.T) {
	before := makeExport(
		makeParam("1.1", "root.gain", canonical.ParamReal),
		makeParam("1.2", "root.mute", canonical.ParamBoolean),
	)
	after := makeExport(
		makeParam("1.1", "root.gain", canonical.ParamReal),
		makeParam("1.3", "root.new", canonical.ParamInteger),
	)
	r := Diff(before, after)
	if r.Counts()[CategoryAdded] != 1 {
		t.Errorf("Added got %d, want 1", r.Counts()[CategoryAdded])
	}
	if r.Counts()[CategoryRemoved] != 1 {
		t.Errorf("Removed got %d, want 1", r.Counts()[CategoryRemoved])
	}
}

// TestDiff_UnitChangedIsBreaking — unit-aware consumers break on
// silent unit drift (e.g. dB → dBm).
func TestDiff_UnitChangedIsBreaking(t *testing.T) {
	before := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Unit = stringPtr("dB")
	}))
	after := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Unit = stringPtr("dBm")
	}))
	r := Diff(before, after)
	if r.Counts()[CategoryBreaking] != 1 {
		t.Errorf("Breaking got %d, want 1", r.Counts()[CategoryBreaking])
	}
}

// TestDiff_EntriesSortedByCategory — Breaking first, Removed last.
func TestDiff_EntriesSortedByCategory(t *testing.T) {
	before := makeExport(
		makeParam("1.1", "a", canonical.ParamReal),
		makeParam("1.2", "b", canonical.ParamReal, func(p *canonical.Parameter) {
			p.Access = canonical.AccessReadWrite
		}),
	)
	after := makeExport(
		makeParam("1.2", "b", canonical.ParamReal, func(p *canonical.Parameter) {
			p.Access = canonical.AccessRead // breaking
		}),
		makeParam("1.3", "c", canonical.ParamReal), // added
	)
	r := Diff(before, after)

	var categoryOrderSeen []string
	for _, e := range r.Entries {
		if len(categoryOrderSeen) == 0 || categoryOrderSeen[len(categoryOrderSeen)-1] != e.Category {
			categoryOrderSeen = append(categoryOrderSeen, e.Category)
		}
	}
	// Must see Breaking before Added before Removed.
	expectedOrder := []string{CategoryBreaking, CategoryAdded, CategoryRemoved}
	if len(categoryOrderSeen) != len(expectedOrder) {
		t.Fatalf("category order got %v, want %v", categoryOrderSeen, expectedOrder)
	}
	for i, got := range categoryOrderSeen {
		if got != expectedOrder[i] {
			t.Errorf("category[%d] got %s, want %s", i, got, expectedOrder[i])
		}
	}
}

// TestDiff_WriteChangelog — produced markdown has the expected shape.
func TestDiff_WriteChangelog(t *testing.T) {
	before := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Access = canonical.AccessReadWrite
	}))
	after := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, func(p *canonical.Parameter) {
		p.Access = canonical.AccessRead
	}))
	r := Diff(before, after)

	var buf bytes.Buffer
	if err := r.WriteChangelog(&buf, "2.4", "2026-05-15"); err != nil {
		t.Fatalf("WriteChangelog: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "## [2.4] — 2026-05-15") {
		t.Errorf("missing version header in changelog:\n%s", out)
	}
	if !strings.Contains(out, "### Breaking") {
		t.Errorf("missing Breaking section:\n%s", out)
	}
	if !strings.Contains(out, "- parameter") {
		t.Errorf("missing bullet:\n%s", out)
	}
}

// TestDiff_WriteChangelog_NoChanges — zero-diff version still emits a
// valid section stating "No schema changes."
func TestDiff_WriteChangelog_NoChanges(t *testing.T) {
	a := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal))
	b := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal))
	r := Diff(a, b)

	var buf bytes.Buffer
	if err := r.WriteChangelog(&buf, "2.4", "2026-05-15"); err != nil {
		t.Fatalf("WriteChangelog: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "## [2.4] — 2026-05-15") {
		t.Errorf("missing version header:\n%s", out)
	}
	if !strings.Contains(out, "No schema changes.") {
		t.Errorf("missing 'No schema changes.' placeholder:\n%s", out)
	}
}

// TestDiff_WriteText_NoChanges — text mode says "no changes" on match.
func TestDiff_WriteText_NoChanges(t *testing.T) {
	a := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal))
	b := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal))
	r := Diff(a, b)

	var buf bytes.Buffer
	if err := r.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "no changes" {
		t.Errorf("got %q, want %q", strings.TrimSpace(buf.String()), "no changes")
	}
}
