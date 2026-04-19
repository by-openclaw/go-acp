// Package diff computes the semantic difference between two canonical
// tree.json exports. Used by `acp diff` (issue #49) to:
//
//   - surface breaking schema changes before a firmware rollout
//   - auto-populate the per-product CHANGELOG.md inside the product
//     fixture layout (docs/fixtures-products.md §CHANGELOG)
//   - feed the firmware-upgrade restore workflow (VISION.md §16)
//
// The diff matches elements by OID across the two trees — the stable
// canonical identifier — so renames of identifier/path/label don't
// cascade into false-positive Removed/Added pairs.
package diff

import (
	"encoding/json"
	"fmt"
	"sort"

	"acp/internal/export/canonical"
)

// Category labels — one of these per Entry. Values match the
// Keep-a-Changelog section headings used by docs/fixtures-products.md.
const (
	CategoryBreaking = "Breaking"
	CategoryChanged  = "Changed"
	CategoryAdded    = "Added"
	CategoryRemoved  = "Removed"
)

// Entry is one single line of the diff report.
type Entry struct {
	Category string // Breaking / Changed / Added / Removed
	OID      string // canonical OID of the subject element (stable identifier)
	Path     string // dotted element path (human-readable location)
	Message  string // one-line description suitable for a changelog bullet
}

// Report aggregates every Entry produced by Diff. Entries are pre-sorted
// by category (Breaking → Changed → Added → Removed) then by path so
// output is deterministic.
type Report struct {
	Entries []Entry
}

// Counts returns {category → number of entries}.
func (r *Report) Counts() map[string]int {
	c := make(map[string]int, 4)
	for _, e := range r.Entries {
		c[e.Category]++
	}
	return c
}

// Diff walks both canonical Exports and produces the structured report.
// Either side may be nil — that's equivalent to an empty tree, useful
// for "initial capture" scenarios (everything in `after` comes out as
// Added).
func Diff(before, after *canonical.Export) *Report {
	bMap := flattenExport(before)
	aMap := flattenExport(after)

	report := &Report{}

	for oid, b := range bMap {
		if a, ok := aMap[oid]; ok {
			report.Entries = append(report.Entries, compareElements(b, a)...)
			continue
		}
		report.Entries = append(report.Entries, Entry{
			Category: CategoryRemoved,
			OID:      oid,
			Path:     b.Common().Path,
			Message:  fmt.Sprintf("%s %q removed", b.Kind(), b.Common().Path),
		})
	}

	for oid, a := range aMap {
		if _, ok := bMap[oid]; !ok {
			report.Entries = append(report.Entries, Entry{
				Category: CategoryAdded,
				OID:      oid,
				Path:     a.Common().Path,
				Message:  fmt.Sprintf("%s %q added", a.Kind(), a.Common().Path),
			})
		}
	}

	sort.SliceStable(report.Entries, func(i, j int) bool {
		ci, cj := categoryOrder(report.Entries[i].Category), categoryOrder(report.Entries[j].Category)
		if ci != cj {
			return ci < cj
		}
		return report.Entries[i].Path < report.Entries[j].Path
	})

	return report
}

// flattenExport produces OID → Element for the whole tree. OIDs are
// required for every element that participates in diff — unkeyed
// elements (rare, usually legacy) are skipped.
func flattenExport(x *canonical.Export) map[string]canonical.Element {
	out := make(map[string]canonical.Element)
	if x == nil {
		return out
	}
	flattenInto(x.Root, out)
	return out
}

func flattenInto(el canonical.Element, out map[string]canonical.Element) {
	if el == nil {
		return
	}
	h := el.Common()
	if h.OID != "" {
		out[h.OID] = el
	}
	for _, c := range h.Children {
		flattenInto(c, out)
	}
}

// compareElements produces per-field Entries for two elements that
// matched by OID. Only compares fields that every Element kind
// carries (access, description) plus Parameter-specific fields when
// both sides are Parameter.
func compareElements(before, after canonical.Element) []Entry {
	var out []Entry
	path := before.Common().Path
	oid := before.Common().OID

	// Access change
	bAccess := before.Common().Access
	aAccess := after.Common().Access
	if bAccess != aAccess {
		cat := CategoryChanged
		if accessNarrows(bAccess, aAccess) {
			cat = CategoryBreaking
		}
		out = append(out, Entry{
			Category: cat,
			OID:      oid,
			Path:     path,
			Message: fmt.Sprintf("%s %q access changed %s → %s",
				before.Kind(), path, bAccess, aAccess),
		})
	}

	// Description change (cosmetic-ish — always Changed, never Breaking)
	if !stringPtrEqual(before.Common().Description, after.Common().Description) {
		out = append(out, Entry{
			Category: CategoryChanged,
			OID:      oid,
			Path:     path,
			Message:  fmt.Sprintf("%s %q description updated", before.Kind(), path),
		})
	}

	// Parameter-specific comparison
	if bp, ok := before.(*canonical.Parameter); ok {
		if ap, ok := after.(*canonical.Parameter); ok {
			out = append(out, compareParameters(bp, ap, path, oid)...)
		}
	}

	return out
}

// compareParameters surfaces the field changes that matter for replay:
// type, min/max, unit, default, enumMap.
func compareParameters(b, a *canonical.Parameter, path, oid string) []Entry {
	var out []Entry

	if b.Type != a.Type {
		out = append(out, Entry{
			Category: CategoryBreaking,
			OID:      oid, Path: path,
			Message: fmt.Sprintf("Parameter %q type changed %s → %s", path, b.Type, a.Type),
		})
	}

	if !jsonEqual(b.Minimum, a.Minimum) {
		cat := CategoryChanged
		if rangeNarrows(b.Minimum, a.Minimum, true) {
			cat = CategoryBreaking
		}
		out = append(out, Entry{
			Category: cat,
			OID:      oid, Path: path,
			Message: fmt.Sprintf("Parameter %q min %v → %v", path, b.Minimum, a.Minimum),
		})
	}

	if !jsonEqual(b.Maximum, a.Maximum) {
		cat := CategoryChanged
		if rangeNarrows(b.Maximum, a.Maximum, false) {
			cat = CategoryBreaking
		}
		out = append(out, Entry{
			Category: cat,
			OID:      oid, Path: path,
			Message: fmt.Sprintf("Parameter %q max %v → %v", path, b.Maximum, a.Maximum),
		})
	}

	if !stringPtrEqual(b.Unit, a.Unit) {
		out = append(out, Entry{
			Category: CategoryBreaking, // unit-aware consumers break on unit drift
			OID:      oid, Path: path,
			Message: fmt.Sprintf("Parameter %q unit changed %s → %s",
				path, derefOrNone(b.Unit), derefOrNone(a.Unit)),
		})
	}

	if !jsonEqual(b.Default, a.Default) {
		out = append(out, Entry{
			Category: CategoryChanged,
			OID:      oid, Path: path,
			Message: fmt.Sprintf("Parameter %q default %v → %v", path, b.Default, a.Default),
		})
	}

	// EnumMap diff — by key (the label shown to users)
	bEnum := enumKeyed(b.EnumMap)
	aEnum := enumKeyed(a.EnumMap)
	for k, bv := range bEnum {
		if _, ok := aEnum[k]; !ok {
			out = append(out, Entry{
				Category: CategoryBreaking,
				OID:      oid, Path: path,
				Message: fmt.Sprintf("Parameter %q enum removed value %q (index %d)",
					path, k, bv),
			})
		}
	}
	for k, av := range aEnum {
		if _, ok := bEnum[k]; !ok {
			out = append(out, Entry{
				Category: CategoryAdded,
				OID:      oid, Path: path,
				Message: fmt.Sprintf("Parameter %q enum added value %q (index %d)",
					path, k, av),
			})
		}
	}

	return out
}

// Classifiers ----------------------------------------------------------

func categoryOrder(c string) int {
	switch c {
	case CategoryBreaking:
		return 0
	case CategoryChanged:
		return 1
	case CategoryAdded:
		return 2
	case CategoryRemoved:
		return 3
	}
	return 99
}

// accessNarrows reports whether the writer capability shrunk.
// readWrite or write → read or none is breaking for consumers that
// were writing. Widening (read → readWrite) is Changed, not Breaking.
func accessNarrows(before, after string) bool {
	if before == canonical.AccessReadWrite || before == canonical.AccessWrite {
		if after == canonical.AccessRead || after == canonical.AccessNone {
			return true
		}
	}
	if before == canonical.AccessRead && after == canonical.AccessNone {
		return true
	}
	return false
}

// rangeNarrows reports whether a numeric bound change cuts out values
// that used to be valid. For min: new_min > old_min narrows. For max:
// new_max < old_max narrows.
func rangeNarrows(before, after any, isMin bool) bool {
	bf, bOK := asFloat(before)
	af, aOK := asFloat(after)
	if !bOK || !aOK {
		return false
	}
	if isMin {
		return af > bf
	}
	return af < bf
}

// Small helpers --------------------------------------------------------

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func jsonEqual(a, b any) bool {
	aBytes, _ := json.Marshal(a)
	bBytes, _ := json.Marshal(b)
	return string(aBytes) == string(bBytes)
}

func stringPtrEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func derefOrNone(s *string) string {
	if s == nil {
		return "(none)"
	}
	return *s
}

func enumKeyed(entries []canonical.EnumEntry) map[string]int64 {
	out := make(map[string]int64, len(entries))
	for _, e := range entries {
		out[e.Key] = e.Value
	}
	return out
}
