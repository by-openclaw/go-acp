package diff

import (
	"strings"
	"testing"

	"dhs/internal/export/canonical"
)

// oneEntry returns the single entry of a report or fails the test —
// most field-level cases below expect exactly one line.
func oneEntry(t *testing.T, r *Report) Entry {
	t.Helper()
	if len(r.Entries) != 1 {
		t.Fatalf("want exactly 1 entry, got %d: %+v", len(r.Entries), r.Entries)
	}
	return r.Entries[0]
}

// TestDiff_NilSides — a nil side is an empty tree: the "initial
// capture" case reports everything as Added; a lost tree reports
// everything as Removed. An Export with no Root (what ReadCanonicalJSON
// returns for `{}`) behaves the same as nil.
func TestDiff_NilSides(t *testing.T) {
	tree := makeExport(makeParam("1.1", "root.gain", canonical.ParamReal))
	cases := []struct {
		name          string
		before, after *canonical.Export
		wantCategory  string
		wantMessage   string
	}{
		{"nil before reports every element added", nil, tree, CategoryAdded, `parameter "root.gain" added`},
		{"nil after reports every element removed", tree, nil, CategoryRemoved, `parameter "root.gain" removed`},
		{"rootless export counts as empty", &canonical.Export{}, tree, CategoryAdded, `parameter "root.gain" added`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Diff(c.before, c.after)
			// root ("1") + gain ("1.1") — both are keyed, both reported.
			if r.Counts()[c.wantCategory] != 2 {
				t.Fatalf("%s count got %d, want 2: %+v", c.wantCategory, r.Counts()[c.wantCategory], r.Entries)
			}
			found := false
			for _, e := range r.Entries {
				if e.Message == c.wantMessage {
					found = true
				}
			}
			if !found {
				t.Errorf("missing message %q in %+v", c.wantMessage, r.Entries)
			}
		})
	}
}

// TestDiff_UnkeyedElementsAreInvisible — an element without an OID has
// no stable identity, so it can neither be Added nor Removed; its keyed
// children are still walked.
func TestDiff_UnkeyedElementsAreInvisible(t *testing.T) {
	unkeyed := &canonical.Node{Header: canonical.Header{
		Identifier: "legacy", Path: "root.legacy", OID: "",
		Children: []canonical.Element{makeParam("1.5.1", "root.legacy.gain", canonical.ParamReal)},
	}}
	r := Diff(nil, makeExport(unkeyed))
	for _, e := range r.Entries {
		if e.Path == "root.legacy" {
			t.Errorf("unkeyed node must not be reported, got %+v", e)
		}
	}
	if got := r.Counts()[CategoryAdded]; got != 2 { // root + nested keyed gain
		t.Errorf("Added got %d, want 2 (root + keyed child of the unkeyed node): %+v", got, r.Entries)
	}
}

// TestDiff_DescriptionIsCosmetic — description drift is never Breaking,
// regardless of which side is missing it.
func TestDiff_DescriptionIsCosmetic(t *testing.T) {
	desc := func(s *string) func(*canonical.Parameter) {
		return func(p *canonical.Parameter) { p.Description = s }
	}
	cases := []struct {
		name          string
		before, after *string
		wantChange    bool
	}{
		{"both absent is unchanged", nil, nil, false},
		{"description added", nil, stringPtr("Input gain"), true},
		{"description dropped", stringPtr("Input gain"), nil, true},
		{"same text is unchanged", stringPtr("Input gain"), stringPtr("Input gain"), false},
		{"text edited", stringPtr("Input gain"), stringPtr("Input trim"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Diff(
				makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, desc(c.before))),
				makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, desc(c.after))),
			)
			if !c.wantChange {
				if len(r.Entries) != 0 {
					t.Fatalf("want no entries, got %+v", r.Entries)
				}
				return
			}
			e := oneEntry(t, r)
			if e.Category != CategoryChanged || e.Message != `parameter "root.gain" description updated` {
				t.Errorf("got %+v", e)
			}
		})
	}
}

// TestDiff_HeaderOnlyForNonParameters — nodes match by OID and compare
// only header fields; a node that became a parameter under the same
// OID is still compared on its header, never on parameter fields.
func TestDiff_HeaderOnlyForNonParameters(t *testing.T) {
	node := func(access string) *canonical.Node {
		return &canonical.Node{Header: canonical.Header{Number: 2, Identifier: "io", Path: "root.io", OID: "1.2", Access: access}}
	}
	t.Run("node access narrowing is breaking", func(t *testing.T) {
		r := Diff(makeExport(node(canonical.AccessRead)), makeExport(node(canonical.AccessNone)))
		e := oneEntry(t, r)
		if e.Category != CategoryBreaking || e.Message != `node "root.io" access changed read → none` {
			t.Errorf("got %+v", e)
		}
	})
	t.Run("kind swap under one oid compares header only", func(t *testing.T) {
		p := makeParam("1.2", "root.io", canonical.ParamInteger, func(p *canonical.Parameter) {
			p.Access = canonical.AccessRead
			p.Minimum = int64(0)
		})
		r := Diff(makeExport(node(canonical.AccessRead)), makeExport(p))
		if len(r.Entries) != 0 {
			t.Errorf("node→parameter with equal header must be silent, got %+v", r.Entries)
		}
	})
}

// TestDiff_ParameterFields — one case per replay-relevant field, each
// naming the category the changelog must file it under.
func TestDiff_ParameterFields(t *testing.T) {
	cases := []struct {
		name          string
		before, after func(*canonical.Parameter)
		wantCategory  string
		wantMessage   string
	}{
		{
			"type change is breaking",
			func(p *canonical.Parameter) { p.Type = canonical.ParamInteger },
			func(p *canonical.Parameter) { p.Type = canonical.ParamReal },
			CategoryBreaking, `Parameter "root.gain" type changed integer → real`,
		},
		{
			"raised minimum is breaking",
			func(p *canonical.Parameter) { p.Minimum = float64(-20) },
			func(p *canonical.Parameter) { p.Minimum = float64(-10) },
			CategoryBreaking, `Parameter "root.gain" min -20 → -10`,
		},
		{
			"lowered minimum is changed",
			func(p *canonical.Parameter) { p.Minimum = int64(-10) },
			func(p *canonical.Parameter) { p.Minimum = int64(-20) },
			CategoryChanged, `Parameter "root.gain" min -10 → -20`,
		},
		{
			"minimum appearing is changed, not breaking",
			func(*canonical.Parameter) {},
			func(p *canonical.Parameter) { p.Minimum = int(0) },
			CategoryChanged, `Parameter "root.gain" min <nil> → 0`,
		},
		{
			"non-numeric bound change is changed",
			func(p *canonical.Parameter) { p.Maximum = "a" },
			func(p *canonical.Parameter) { p.Maximum = "b" },
			CategoryChanged, `Parameter "root.gain" max a → b`,
		},
		{
			"float32 bounds compare numerically",
			func(p *canonical.Parameter) { p.Maximum = float32(12) },
			func(p *canonical.Parameter) { p.Maximum = float32(6) },
			CategoryBreaking, `Parameter "root.gain" max 12 → 6`,
		},
		{
			"unit appearing is breaking",
			func(*canonical.Parameter) {},
			func(p *canonical.Parameter) { p.Unit = stringPtr("dB") },
			CategoryBreaking, `Parameter "root.gain" unit changed (none) → dB`,
		},
		{
			"default change is changed",
			func(p *canonical.Parameter) { p.Default = float64(0) },
			func(p *canonical.Parameter) { p.Default = float64(-6) },
			CategoryChanged, `Parameter "root.gain" default 0 → -6`,
		},
		{
			"enum value added is added",
			func(p *canonical.Parameter) { p.EnumMap = []canonical.EnumEntry{{Key: "Off", Value: 0}} },
			func(p *canonical.Parameter) {
				p.EnumMap = []canonical.EnumEntry{{Key: "Off", Value: 0}, {Key: "On", Value: 1}}
			},
			CategoryAdded, `Parameter "root.gain" enum added value "On" (index 1)`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Diff(
				makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, c.before)),
				makeExport(makeParam("1.1", "root.gain", canonical.ParamReal, c.after)),
			)
			e := oneEntry(t, r)
			if e.Category != c.wantCategory {
				t.Errorf("category got %s, want %s (%s)", e.Category, c.wantCategory, e.Message)
			}
			if e.Message != c.wantMessage {
				t.Errorf("message got %q, want %q", e.Message, c.wantMessage)
			}
			if e.OID != "1.1" || e.Path != "root.gain" {
				t.Errorf("entry must carry oid+path, got oid=%q path=%q", e.OID, e.Path)
			}
		})
	}
}

// TestAccessNarrows — the four Ember+ access levels (§4.1.1) and which
// transitions strip a capability a consumer may already depend on.
func TestAccessNarrows(t *testing.T) {
	cases := []struct {
		name          string
		before, after string
		want          bool
	}{
		{"readWrite to read strips write", canonical.AccessReadWrite, canonical.AccessRead, true},
		{"readWrite to none strips everything", canonical.AccessReadWrite, canonical.AccessNone, true},
		{"write to read strips write", canonical.AccessWrite, canonical.AccessRead, true},
		{"read to none strips read", canonical.AccessRead, canonical.AccessNone, true},
		{"read to readWrite widens", canonical.AccessRead, canonical.AccessReadWrite, false},
		{"none to read widens", canonical.AccessNone, canonical.AccessRead, false},
		{"readWrite to write keeps write", canonical.AccessReadWrite, canonical.AccessWrite, false},
		{"read to write swaps, not narrows", canonical.AccessRead, canonical.AccessWrite, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := accessNarrows(c.before, c.after); got != c.want {
				t.Errorf("accessNarrows(%s, %s) got %v, want %v", c.before, c.after, got, c.want)
			}
		})
	}
}

// TestRangeNarrows — every numeric Go type an exporter may place in
// Parameter.Minimum/Maximum must be compared as a number; anything
// else cannot be judged and is reported as not narrowing.
func TestRangeNarrows(t *testing.T) {
	cases := []struct {
		name          string
		before, after any
		isMin         bool
		want          bool
	}{
		{"float64 min raised", float64(0), float64(1), true, true},
		{"float64 min lowered", float64(1), float64(0), true, false},
		{"float32 max lowered", float32(10), float32(5), false, true},
		{"int max raised", int(5), int(10), false, false},
		{"int64 min raised", int64(-5), int64(0), true, true},
		{"mixed int and float compare numerically", int(3), float64(2.5), false, true},
		{"string bound cannot narrow", "3", "4", true, false},
		{"nil before cannot narrow", nil, float64(4), true, false},
		{"nil after cannot narrow", float64(4), nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := rangeNarrows(c.before, c.after, c.isMin); got != c.want {
				t.Errorf("rangeNarrows(%v, %v, isMin=%v) got %v, want %v", c.before, c.after, c.isMin, got, c.want)
			}
		})
	}
}

// TestCategoryOrder — the report is sorted risk-first (Breaking →
// Changed → Added → Removed); any category the diff does not know
// about sorts after all of them rather than being lost among them.
func TestCategoryOrder(t *testing.T) {
	want := []string{CategoryBreaking, CategoryChanged, CategoryAdded, CategoryRemoved, "Deprecated"}
	for i := 1; i < len(want); i++ {
		if categoryOrder(want[i-1]) >= categoryOrder(want[i]) {
			t.Errorf("%s must sort before %s", want[i-1], want[i])
		}
	}
}

// TestDiff_SortWithinCategoryByPath — inside one category entries are
// ordered by path so two runs on the same trees print identically.
func TestDiff_SortWithinCategoryByPath(t *testing.T) {
	after := makeExport(
		makeParam("1.3", "root.c", canonical.ParamReal),
		makeParam("1.1", "root.a", canonical.ParamReal),
		makeParam("1.2", "root.b", canonical.ParamReal),
	)
	r := Diff(makeExport(), after)
	var paths []string
	for _, e := range r.Entries {
		paths = append(paths, e.Path)
	}
	if got := strings.Join(paths, ","); got != "root.a,root.b,root.c" {
		t.Errorf("Added entries sorted by path: got %s", got)
	}
}
