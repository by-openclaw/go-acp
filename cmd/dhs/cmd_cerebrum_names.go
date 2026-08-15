package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
	"strings"

	"dhs/internal/cerebrum-nb/codec"
)

// Cerebrum src/dst mnemonic CSVs — the names leg of the probel-sw08p-style
// export/import trio (<prefix>-src.csv / <prefix>-dst.csv / <prefix>-xpoint.csv).
//
// Shape (proper CSV, quoted, so mnemonics may contain commas):
//
//	srce,levels,mnemonic      dest,levels,mnemonic
//	5121,1;2,"CAM 1"          5123,1,"MON 1"
//
// `levels` is the same ';'-separated multi-level list the xpoint CSV uses; a
// legacy single `level` column is accepted on read. One row expands to one
// SRCE_MNE / DEST_MNE action per level on import.

// cerebrumMneRow is one line of a mnemonic CSV: an ID named `Mnemonic` across
// one or more levels.
type cerebrumMneRow struct {
	ID       string
	Levels   []string
	Mnemonic string
}

// parseCerebrumMneCSV decodes a mnemonic CSV whose key column is keyCol
// ("srce" or "dest"). Header is case-insensitive and order-independent;
// '#' comment lines are skipped; quoting per RFC 4180 (encoding/csv).
func parseCerebrumMneCSV(data []byte, keyCol, srcName string) ([]cerebrumMneRow, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.Comment = '#'
	r.TrimLeadingSpace = true
	recs, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", srcName, err)
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("%s: empty (no header)", srcName)
	}
	idx := map[string]int{}
	for i, h := range recs[0] {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	key, ok := idx[keyCol]
	if !ok {
		return nil, fmt.Errorf("%s: missing column %q", srcName, keyCol)
	}
	mne, ok := idx["mnemonic"]
	if !ok {
		return nil, fmt.Errorf("%s: missing column \"mnemonic\"", srcName)
	}
	lvlCol, multi := idx["levels"]
	if !multi {
		lvlCol, ok = idx["level"]
		if !ok {
			return nil, fmt.Errorf("%s: missing level column (need \"levels\" or \"level\")", srcName)
		}
	}
	var out []cerebrumMneRow
	for n, rec := range recs[1:] {
		if len(rec) <= key || len(rec) <= mne || len(rec) <= lvlCol {
			return nil, fmt.Errorf("%s row %d: too few columns", srcName, n+2)
		}
		id := strings.TrimSpace(rec[key])
		levels := splitLevelCell(rec[lvlCol])
		m := rec[mne]
		if id == "" || len(levels) == 0 || m == "" {
			return nil, fmt.Errorf("%s row %d: %s, level(s) and mnemonic are all required", srcName, n+2, keyCol)
		}
		out = append(out, cerebrumMneRow{ID: id, Levels: levels, Mnemonic: m})
	}
	return out, nil
}

// collapseCerebrumMnes groups per-(id,level) snapshot rows into multi-level
// rows: levels sharing the same mnemonic for an ID coalesce; an ID whose
// mnemonic differs per level keeps one row per distinct mnemonic. Levels are
// deduped and numeric-sorted; rows are ordered (ID, mnemonic) for stable diffs.
func collapseCerebrumMnes(rows []cerebrumMneRow) []cerebrumMneRow {
	type acc struct {
		id, mne string
		levels  []string
		seen    map[string]bool
	}
	groups := map[string]*acc{}
	var order []string
	for _, r := range rows {
		if r.ID == "" || r.Mnemonic == "" {
			continue
		}
		k := r.ID + "\x00" + r.Mnemonic
		g := groups[k]
		if g == nil {
			g = &acc{id: r.ID, mne: r.Mnemonic, seen: map[string]bool{}}
			groups[k] = g
			order = append(order, k)
		}
		for _, lvl := range r.Levels {
			if lvl != "" && !g.seen[lvl] {
				g.seen[lvl] = true
				g.levels = append(g.levels, lvl)
			}
		}
	}
	out := make([]cerebrumMneRow, 0, len(order))
	for _, k := range order {
		g := groups[k]
		sortCerebrumIDs(g.levels)
		out = append(out, cerebrumMneRow{ID: g.id, Levels: g.levels, Mnemonic: g.mne})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if c := cmpCerebrumID(out[i].ID, out[j].ID); c != 0 {
			return c < 0
		}
		return out[i].Mnemonic < out[j].Mnemonic
	})
	return out
}

// formatCerebrumMneCSV renders rows as `keyCol,levels,mnemonic` with RFC 4180
// quoting — the exact shape parseCerebrumMneCSV reads back.
func formatCerebrumMneCSV(keyCol string, rows []cerebrumMneRow) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{keyCol, "levels", "mnemonic"})
	for _, r := range rows {
		_ = w.Write([]string{r.ID, strings.Join(r.Levels, ";"), r.Mnemonic})
	}
	w.Flush()
	return b.String()
}

// crossLevelRoute reports whether a routing-snapshot row is a cross-level
// route (source level differs from the row's dest level). The dest,srce,levels
// CSV cannot represent those yet, so export skips them LOUDLY, never silently.
func crossLevelRoute(destLevelID, srcLevelID string) bool {
	return srcLevelID != "" && destLevelID != "" && srcLevelID != destLevelID
}

// primaryMnemonic extracts the primary (slot 0) mnemonic from a *_MNE
// routing_change RX row: Cerebrum returns slot-keyed child elements flattened
// into Mnemonics[slot]; the TX-attr Mnemonic field is the fallback.
func primaryMnemonic(rc *codec.RoutingChange) string {
	if rc == nil {
		return ""
	}
	if m, ok := rc.Mnemonics[0]; ok && m != "" {
		return m
	}
	return rc.Mnemonic
}
