package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
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
// level, per the file's key column) and its primary mnemonic.
type cerebrumMneRow struct {
	ID       string
	Mnemonic string
}

// parseCerebrumMneCSV decodes a mnemonic CSV whose key column is keyCol
// ("srce", "dest" or "level"). Header is case-insensitive and
// order-independent; '#' comment lines are skipped; quoting per RFC 4180.
func parseCerebrumMneCSV(data []byte, keyCol, srcName string) ([]cerebrumMneRow, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.Comment = '#'
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
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
	var out []cerebrumMneRow
	for n, rec := range recs[1:] {
		if len(rec) <= key || len(rec) <= mne {
			return nil, fmt.Errorf("%s row %d: too few columns", srcName, n+2)
		}
		id := strings.TrimSpace(rec[key])
		m := rec[mne]
		if id == "" || m == "" {
			return nil, fmt.Errorf("%s row %d: %s and mnemonic are both required", srcName, n+2, keyCol)
		}
		out = append(out, cerebrumMneRow{ID: id, Mnemonic: m})
	}
	return out, nil
}

// dedupeCerebrumMnes drops exact duplicate (ID, mnemonic) rows — the OBTAIN
// snapshot may deliver one row per level for a router ID, all carrying the
// same global mnemonic — and orders the result numeric-aware by ID (then
// mnemonic) for stable diffs. Conflicting mnemonics for one ID are kept (both
// rows), so an inconsistency is visible rather than silently resolved.
func dedupeCerebrumMnes(rows []cerebrumMneRow) []cerebrumMneRow {
	seen := map[string]bool{}
	out := make([]cerebrumMneRow, 0, len(rows))
	for _, r := range rows {
		if r.ID == "" || r.Mnemonic == "" {
			continue
		}
		k := r.ID + "\x00" + r.Mnemonic
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if c := cmpCerebrumID(out[i].ID, out[j].ID); c != 0 {
			return c < 0
		}
		return out[i].Mnemonic < out[j].Mnemonic
	})
	return out
}

// formatCerebrumMneCSV renders rows as `keyCol,mnemonic` with RFC 4180
// quoting — the exact shape parseCerebrumMneCSV reads back.
func formatCerebrumMneCSV(keyCol string, rows []cerebrumMneRow) string {
	var b strings.Builder
	w := csv.NewWriter(&b)
	_ = w.Write([]string{keyCol, "mnemonic"})
	for _, r := range rows {
		_ = w.Write([]string{r.ID, r.Mnemonic})
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
