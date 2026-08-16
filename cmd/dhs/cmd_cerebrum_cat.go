package main

import (
	"fmt"
	"strings"
	"time"

	"dhs/internal/cerebrum-nb/codec"
	cerebrum "dhs/internal/cerebrum-nb/consumer"
)

// ----------------------------------------------------------------------
// Category CSV — build/maintain the navigation panel (categories,
// sub-categories, resources) from file, ensure-style (ADR-0007).
//
// Shape (minimal by owner decision — row order IS slot order):
//
//	category,type,value
//	SRC-STUDIO-A,TEXT,Cameras
//	SRC-STUDIO-A,SOURCE,10001
//	SRC-ALL,CATEGORY,SRC-STUDIO-A
//
// SRC and DST live in SEPARATE files (probel file-set pattern:
// <prefix>-cat-src.csv / <prefix>-cat-dst.csv): a DEST row in the src
// file (or SOURCE/SRCE in the dst file) fails validation before any
// wire I/O.
// ----------------------------------------------------------------------

// cerebrumCatItem is one desired item slot (§3.3 ITEM_TYPE + value).
type cerebrumCatItem struct {
	Type  string
	Value string
}

// cerebrumCatDef is one category's complete desired grid, in slot order.
type cerebrumCatDef struct {
	Name  string
	Items []cerebrumCatItem
}

// cerebrumCatItemTypes is the §3.3 ITEM_TYPE enum accepted in the CSV.
var cerebrumCatItemTypes = map[string]bool{
	"BLANK": true, "SRCE": true, "SOURCE": true, "DEST": true,
	"CATEGORY": true, "SALVO": true, "INHERIT": true,
	"TEXT": true, "FILE": true, "CUSTOM": true,
}

// parseCerebrumCatCSV decodes a category CSV. kind is "src" or "dst" and
// enforces the SRC/DST file separation. Row order within a category is
// the slot order (1-based). Blank lines and '#' comments are skipped.
func parseCerebrumCatCSV(data []byte, kind, srcName string) ([]cerebrumCatDef, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	start := 0
	for start < len(lines) && (strings.TrimSpace(lines[start]) == "" || strings.HasPrefix(strings.TrimSpace(lines[start]), "#")) {
		start++
	}
	if start >= len(lines) {
		return nil, fmt.Errorf("%s: empty (no header)", srcName)
	}
	header := strings.Split(strings.ToLower(strings.TrimSpace(lines[start])), ",")
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(h)] = i
	}
	for _, col := range []string{"category", "type", "value"} {
		if _, ok := idx[col]; !ok {
			return nil, fmt.Errorf("%s: missing column %q (need category,type,value)", srcName, col)
		}
	}

	var defs []cerebrumCatDef
	byName := map[string]int{}
	for n, line := range lines[start+1:] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < len(header) {
			return nil, fmt.Errorf("%s line %d: %d columns < header %d", srcName, start+n+2, len(f), len(header))
		}
		cat := strings.TrimSpace(f[idx["category"]])
		typ := strings.ToUpper(strings.TrimSpace(f[idx["type"]]))
		val := strings.TrimSpace(f[idx["value"]])
		if cat == "" || typ == "" {
			return nil, fmt.Errorf("%s line %d: category and type are required", srcName, start+n+2)
		}
		if !cerebrumCatItemTypes[typ] {
			return nil, fmt.Errorf("%s line %d: unknown type %q (§3.3: BLANK|SRCE|SOURCE|DEST|CATEGORY|SALVO|INHERIT|TEXT|FILE|CUSTOM)", srcName, start+n+2, typ)
		}
		if val == "" && typ != "BLANK" {
			return nil, fmt.Errorf("%s line %d: value is required for type %s", srcName, start+n+2, typ)
		}
		switch kind {
		case "src":
			if typ == "DEST" {
				return nil, fmt.Errorf("%s line %d: DEST item in the SRC category file — keep SRC and DST files separate (genuinely mixed categories belong in the -cat-mixed.csv file)", srcName, start+n+2)
			}
		case "dst":
			if typ == "SOURCE" || typ == "SRCE" {
				return nil, fmt.Errorf("%s line %d: %s item in the DST category file — keep SRC and DST files separate (genuinely mixed categories belong in the -cat-mixed.csv file)", srcName, start+n+2, typ)
			}
		case "mixed":
			// both kinds legal — the file for categories whose subtree
			// carries sources AND dests (e.g. a master category).
		}
		i, seen := byName[cat]
		if !seen {
			defs = append(defs, cerebrumCatDef{Name: cat})
			i = len(defs) - 1
			byName[cat] = i
		}
		defs[i].Items = append(defs[i].Items, cerebrumCatItem{Type: typ, Value: val})
	}
	if len(defs) == 0 {
		return nil, fmt.Errorf("%s: no category rows", srcName)
	}
	return defs, nil
}

// formatCerebrumCatCSV renders defs back to the exact shape
// parseCerebrumCatCSV reads — the export/import round-trip contract.
func formatCerebrumCatCSV(defs []cerebrumCatDef) string {
	var b strings.Builder
	b.WriteString("category,type,value\n")
	for _, d := range defs {
		for _, it := range d.Items {
			b.WriteString(d.Name)
			b.WriteByte(',')
			b.WriteString(it.Type)
			b.WriteByte(',')
			b.WriteString(it.Value)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// cerebrumCatChange is one converging category write.
type cerebrumCatChange struct {
	Cat   string
	Op    string // CREATE | MODIFY_ITEM | (BLANK clear via MODIFY_ITEM)
	Index int    // 1-based slot for MODIFY_ITEM
	Type  string
	Value string
	From  string
}

// diffCerebrumCategory computes the per-slot converge for one category
// (ADR-0007): desired row i owns slot i (1-based); a live slot beyond the
// desired grid is cleared by writing ITEM_TYPE=BLANK (never DELETE_ITEM —
// the spec does not define whether deletion shifts later indices, so
// ensure must not assume). live == nil means the category does not exist:
// a CREATE precedes the slot writes. Run-twice = 0.
func diffCerebrumCategory(cat string, live *codec.CategoryDetailsInfo, desired []cerebrumCatItem) []cerebrumCatChange {
	var out []cerebrumCatChange
	liveByIdx := map[int]codec.CategoryItem{}
	maxLive := 0
	if live == nil {
		out = append(out, cerebrumCatChange{Cat: cat, Op: "CREATE"})
	} else {
		for _, it := range live.Items {
			liveByIdx[it.Index] = it
			if it.Index > maxLive {
				maxLive = it.Index
			}
		}
	}
	for i, want := range desired {
		slot := i + 1
		cur, ok := liveByIdx[slot]
		if ok && strings.EqualFold(cur.Type, want.Type) && cur.Value == want.Value {
			continue
		}
		from := ""
		if ok {
			from = fmt.Sprintf("%s %s", cur.Type, cur.Value)
		}
		out = append(out, cerebrumCatChange{
			Cat: cat, Op: "MODIFY_ITEM", Index: slot,
			Type: want.Type, Value: want.Value, From: from,
		})
	}
	for slot := len(desired) + 1; slot <= maxLive; slot++ {
		cur, ok := liveByIdx[slot]
		if !ok || strings.EqualFold(cur.Type, "BLANK") {
			continue
		}
		out = append(out, cerebrumCatChange{
			Cat: cat, Op: "MODIFY_ITEM", Index: slot,
			Type: "BLANK", Value: "",
			From: fmt.Sprintf("%s %s", cur.Type, cur.Value),
		})
	}
	return out
}

// fetchCerebrumCategories walks §5.2 once: CATEGORY_LIST then
// CATEGORY_DETAILS per category. Shared by tree, export and import.
func fetchCerebrumCategories(sess *cerebrum.Session, timeout time.Duration) ([]string, map[string]*codec.CategoryDetailsInfo, error) {
	list, err := obtainSingleCategoryChange(sess, timeout,
		&codec.CategoryChange{Type: "CATEGORY_LIST"}, "CATEGORY_LIST")
	if err != nil {
		return nil, nil, err
	}
	if list == nil || list.Category == nil {
		return nil, nil, fmt.Errorf("cerebrum-nb: no CATEGORY_LIST reply within timeout")
	}
	names := list.Category.Categories
	details := map[string]*codec.CategoryDetailsInfo{}
	for _, cat := range names {
		det, derr := obtainSingleCategoryChange(sess, timeout,
			&codec.CategoryChange{Type: "CATEGORY_DETAILS", Category: cat}, "CATEGORY_DETAILS")
		if derr != nil {
			return nil, nil, derr
		}
		if det != nil && det.Category != nil {
			details[cat] = det.Category.Details
		} else {
			details[cat] = nil
		}
	}
	return names, details, nil
}

// classifyCerebrumCategories splits the live category set into src / dst
// for the two-file export: a category counts as src when its subtree
// contains any SOURCE/SRCE item, dst when it contains any DEST item
// (recursing through CATEGORY references). Categories whose subtree has
// both are reported in BOTH files (and counted for the caller's warning);
// categories with neither (pure TEXT / empty) land where they are
// referenced, or nowhere when unreferenced.
func classifyCerebrumCategories(names []string, details map[string]*codec.CategoryDetailsInfo) (src, dst map[string]bool, both []string) {
	src = map[string]bool{}
	dst = map[string]bool{}
	var walk func(cat string, visited map[string]bool) (hasSrc, hasDst bool)
	walk = func(cat string, visited map[string]bool) (bool, bool) {
		if visited[cat] {
			return false, false
		}
		visited[cat] = true
		d := details[cat]
		if d == nil {
			return false, false
		}
		hasSrc, hasDst := false, false
		for _, it := range d.Items {
			switch it.Type {
			case "SOURCE", "SRCE":
				hasSrc = true
			case "DEST", "DESTINATION":
				hasDst = true
			case "CATEGORY":
				s, dd := walk(it.Value, visited)
				hasSrc = hasSrc || s
				hasDst = hasDst || dd
			}
		}
		return hasSrc, hasDst
	}
	for _, cat := range names {
		hasSrc, hasDst := walk(cat, map[string]bool{})
		if hasSrc {
			src[cat] = true
		}
		if hasDst {
			dst[cat] = true
		}
		if hasSrc && hasDst {
			both = append(both, cat)
		}
	}
	return src, dst, both
}

// cerebrumCatDefsFromLive renders the live grids of the given categories
// (in list order) as CSV defs — the export side of the round-trip.
func cerebrumCatDefsFromLive(names []string, pick map[string]bool, details map[string]*codec.CategoryDetailsInfo) []cerebrumCatDef {
	var defs []cerebrumCatDef
	for _, cat := range names {
		if !pick[cat] {
			continue
		}
		d := details[cat]
		if d == nil {
			continue
		}
		def := cerebrumCatDef{Name: cat}
		for _, it := range d.Items {
			def.Items = append(def.Items, cerebrumCatItem{Type: it.Type, Value: it.Value})
		}
		defs = append(defs, def)
	}
	return defs
}
