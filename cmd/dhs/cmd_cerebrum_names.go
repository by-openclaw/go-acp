package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"dhs/internal/cerebrum-nb/codec"
)

// Cerebrum src/dst/level mnemonic CSVs — the names legs of the
// probel-sw08p-style export/import set (<prefix>-src.csv / -dst.csv /
// -level.csv / -xpoint.csv).
//
// Per 0v16 §4.1.5/§4.1.6 a source/destination mnemonic on a ROUTER
// (Routemaster) target is global for that ID — the LEVEL attribute exists only
// for non-router devices ("ignored unless the DEVICE_TYPE is DEVICE",
// §5.1.5/§5.1.6). So the mnemonic CSVs carry NO level column:
//
//	srce,mnemonic        dest,mnemonic        level,mnemonic
//	5121,"CAM 1, main"   5123,"MON 1"         1,LVL-1
//
// Level names themselves are per-level via LEVEL_MNE (§4.1.4). RFC 4180
// quoting throughout (mnemonics may contain commas/quotes).

// cerebrumMneRow is one line of a mnemonic CSV: an ID (source, destination or
// level, per the file's key column), its primary mnemonic, and — for src/dst
// rows — the levels the resource EXISTS on (capability, from the §5.1.5/§5.1.6
// ASSOCIATION children; RM_LEVEL_ID per binding). Capability, not state: the
// xpoint CSV's levels are the routed ones; these are the routable ones — the
// input to the straight-route vs cross-level(shuffle) decision (§4.1.1).
// Renames stay per-ID (the levels column is inventory, never rename scope).
type cerebrumMneRow struct {
	ID       string
	Levels   []string
	Mnemonic string
	// Alts holds the alternate mnemonic sets (slot -> value, slot >= 1) — the
	// "levels of labels". RX carries an ALT_MNE_N child only for slots WITH a
	// value, so presence == used. Slot MEANING is plant config (NOC: 1=Panels,
	// 2=Ref_Short_Edit, 3=Ref_Long_Edit, 4=Engineer, 5=Ref_Long_Tech); the
	// wire knows only indexes.
	Alts map[int]string
}

// parseCerebrumMneCSV decodes a mnemonic CSV whose key column is keyCol
// ("srce", "dest" or "level"). Header is case-insensitive and
// order-independent; '#' comment lines are skipped; quoting per RFC 4180.
// The second return lists the alt_N slots PRESENT IN THE HEADER (sorted) —
// the managed columns; --allow-clear may only clear cells in managed columns.
// An empty mnemonic cell is legal (row managed for alts / clear-mode only).
func parseCerebrumMneCSV(data []byte, keyCol, srcName string) ([]cerebrumMneRow, []int, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.Comment = '#'
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", srcName, err)
	}
	if len(recs) == 0 {
		return nil, nil, fmt.Errorf("%s: empty (no header)", srcName)
	}
	idx := map[string]int{}
	for i, h := range recs[0] {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	key, ok := idx[keyCol]
	if !ok {
		return nil, nil, fmt.Errorf("%s: missing column %q", srcName, keyCol)
	}
	mne, ok := idx["mnemonic"]
	if !ok {
		return nil, nil, fmt.Errorf("%s: missing column \"mnemonic\"", srcName)
	}
	var headerSlots []int
	for h := range idx {
		if strings.HasPrefix(h, "alt_") {
			if slot, aerr := strconv.Atoi(h[4:]); aerr == nil && slot >= 1 {
				headerSlots = append(headerSlots, slot)
			}
		}
	}
	sort.Ints(headerSlots)
	// Optional capability column ("levels", ';'-list) on src/dst files. Import
	// ignores it (renames are per-ID); parse keeps it for round-trip fidelity.
	lvlCol, hasLevels := idx["levels"]
	var out []cerebrumMneRow
	for n, rec := range recs[1:] {
		if len(rec) <= key || len(rec) <= mne {
			return nil, nil, fmt.Errorf("%s row %d: too few columns", srcName, n+2)
		}
		id := strings.TrimSpace(rec[key])
		if id == "" {
			return nil, nil, fmt.Errorf("%s row %d: %s is required", srcName, n+2, keyCol)
		}
		row := cerebrumMneRow{ID: id, Mnemonic: rec[mne]}
		if hasLevels && len(rec) > lvlCol {
			if lv := splitLevelCell(rec[lvlCol]); len(lv) > 0 {
				row.Levels = lv
			}
		}
		for h, col := range idx {
			if !strings.HasPrefix(h, "alt_") || len(rec) <= col || rec[col] == "" {
				continue
			}
			slot, aerr := strconv.Atoi(h[4:])
			if aerr != nil || slot < 1 {
				continue
			}
			if row.Alts == nil {
				row.Alts = map[int]string{}
			}
			row.Alts[slot] = rec[col]
		}
		out = append(out, row)
	}
	return out, headerSlots, nil
}

// dedupeCerebrumMnes drops exact duplicate (ID, mnemonic) rows — the OBTAIN
// snapshot may deliver one row per level for a router ID, all carrying the
// same global mnemonic — and orders the result numeric-aware by ID (then
// mnemonic) for stable diffs. Conflicting mnemonics for one ID are kept (both
// rows), so an inconsistency is visible rather than silently resolved.
func dedupeCerebrumMnes(rows []cerebrumMneRow) []cerebrumMneRow {
	type acc struct {
		row  cerebrumMneRow
		seen map[string]bool
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
			g = &acc{row: cerebrumMneRow{ID: r.ID, Mnemonic: r.Mnemonic}, seen: map[string]bool{}}
			groups[k] = g
			order = append(order, k)
		}
		// Merge capability levels across per-level snapshot repeats.
		for _, lvl := range r.Levels {
			if lvl != "" && !g.seen[lvl] {
				g.seen[lvl] = true
				g.row.Levels = append(g.row.Levels, lvl)
			}
		}
		// Merge alternate mnemonics (first value per slot wins).
		for slot, v := range r.Alts {
			if v == "" {
				continue
			}
			if g.row.Alts == nil {
				g.row.Alts = map[int]string{}
			}
			if _, ok := g.row.Alts[slot]; !ok {
				g.row.Alts[slot] = v
			}
		}
	}
	out := make([]cerebrumMneRow, 0, len(order))
	for _, k := range order {
		sortCerebrumIDs(groups[k].row.Levels)
		out = append(out, groups[k].row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if c := cmpCerebrumID(out[i].ID, out[j].ID); c != 0 {
			return c < 0
		}
		return out[i].Mnemonic < out[j].Mnemonic
	})
	return out
}

// maxAltSlot returns the highest alternate-mnemonic slot present across rows
// (0 = none), so the CSV grows exactly the alt_1..alt_N columns the plant uses.
func maxAltSlot(rows []cerebrumMneRow) int {
	max := 0
	for _, r := range rows {
		for k := range r.Alts {
			if k > max {
				max = k
			}
		}
	}
	return max
}

// formatCerebrumMneCSV renders rows with RFC 4180 quoting — the exact shape
// parseCerebrumMneCSV reads back. src/dst files carry the capability column
// (`keyCol,levels,mnemonic[,alt_1..alt_N]`); the level file is
// `level,mnemonic[,alt_1..alt_N]` (levels column on levels is meaningless).
// Alt columns appear only when at least one row uses that slot; empty cell =
// slot unused on that resource.
func formatCerebrumMneCSV(keyCol string, rows []cerebrumMneRow) string {
	return formatCerebrumMneCSVN(keyCol, rows, maxAltSlot(rows))
}

// formatCerebrumMneCSVN is formatCerebrumMneCSV with an explicit alt-column
// count — the export uses the plant-wide maximum so src/dst/level files all
// carry the same alt_1..alt_N headers (an empty column is editable: fill a
// cell and import converges it).
func formatCerebrumMneCSVN(keyCol string, rows []cerebrumMneRow, nAlt int) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	hdr := []string{keyCol}
	if keyCol != "level" {
		hdr = append(hdr, "levels")
	}
	hdr = append(hdr, "mnemonic")
	for i := 1; i <= nAlt; i++ {
		hdr = append(hdr, fmt.Sprintf("alt_%d", i))
	}
	_ = w.Write(hdr)
	for _, r := range rows {
		rec := []string{r.ID}
		if keyCol != "level" {
			rec = append(rec, strings.Join(r.Levels, ";"))
		}
		rec = append(rec, r.Mnemonic)
		for i := 1; i <= nAlt; i++ {
			rec = append(rec, r.Alts[i])
		}
		_ = w.Write(rec)
	}
	w.Flush()
	return b.String()
}

// mneLevelsFromChange extracts the capability levels for the row's resource
// from a *_MNE routing_change RX: the ASSOCIATION children's RM_LEVEL_ID
// (filtered to this row's ID when the association names one), plus the row's
// own LEVEL_ID as fallback. Capability = levels the resource exists on.
func mneLevelsFromChange(rc *codec.RoutingChange) []string {
	if rc == nil {
		return nil
	}
	// Live NOC wire truth (frames-src.log 2026-08-15, cross-checked against
	// the Routemaster UI): the ASSOCIATION_n INDEX is the Routemaster level
	// the binding lives on (ASSOCIATION_1 = level 1/Video: its RM_IP_UID names
	// the Video sender). RM_LEVEL_ID is the level INSIDE the bound physical
	// device (0 on single-level SDI/NMOS devices) and the association SRCE_ID/
	// DEST_ID is the device port UID (e.g. 4026531837) — NEVER the row's
	// resource ID, so no ID filtering.
	var out []string
	for _, a := range rc.Associations {
		if a.Index > 0 {
			out = append(out, strconv.Itoa(a.Index))
		}
	}
	// Fallback: the row's own LEVEL_ID — but NEVER the literal "*", which is
	// just our wildcard filter echoed back ("attributes as per TX"). No
	// associations and no concrete level = no binding reported: leave the
	// capability empty rather than invent one.
	if len(out) == 0 && rc.LevelID != "" && rc.LevelID != "*" {
		out = append(out, rc.LevelID)
	}
	return out
}

// cerebrumNoRouteSentinel reports whether a ROUTE child's SOURCE_ID is one of
// the live NOC's undocumented "no route" markers rather than a real source
// (frames-xp.log 2026-08-15, 178,584 cells): "0" (12,248x, with
// SOURCE_LEVEL_ID=0), "4294967294" (0xFFFFFFFE, 164,093x = disconnected) and
// "4294967295" (0xFFFFFFFF, 474x). Only 1,769 cells carried real sources.
// Cells with a sentinel are unrouted: no CSV row, no warning.
func cerebrumNoRouteSentinel(sourceID string) bool {
	switch sourceID {
	case "", "0", "4294967294", "4294967295":
		return true
	}
	return false
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

// altMnemonics extracts the alternate sets (slot >= 1) from a *_MNE RX row —
// nil when the resource uses none.
func altMnemonics(rc *codec.RoutingChange) map[int]string {
	if rc == nil {
		return nil
	}
	var out map[int]string
	for slot, v := range rc.Mnemonics {
		if slot >= 1 && v != "" {
			if out == nil {
				out = map[int]string{}
			}
			out[slot] = v
		}
	}
	return out
}

// formatAlts renders a compact used-slots view for table output, e.g.
// "1=Black 4=ENG" — "-" when no alternate is set.
func formatAlts(alts map[int]string) string {
	if len(alts) == 0 {
		return "-"
	}
	slots := make([]int, 0, len(alts))
	for k := range alts {
		slots = append(slots, k)
	}
	sort.Ints(slots)
	parts := make([]string, 0, len(slots))
	for _, s := range slots {
		parts = append(parts, fmt.Sprintf("%d=%s", s, alts[s]))
	}
	return strings.Join(parts, " ")
}
