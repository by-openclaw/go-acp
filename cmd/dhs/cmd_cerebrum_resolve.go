package main

// Names on the command line. §1.8 of the Northbound document lets a route
// name its destination, source and level (DEST_NAME / SRCE_NAME /
// *_LEVEL_NAME) instead of numbering them. The CLI accepts either form on
// --dest / --srce / --level and resolves a name client-side against the
// mnemonic catalogue it reads anyway (one wildcard OBTAIN per kind), then
// sends the ID: an ID round-trips through the read-back unchanged, a
// duplicated or mistyped label is reported before anything is sent.

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// cerebrumIsID reports whether s is a wire ID: digits only, or the "*"
// wildcard the §5.1 reads use.
func cerebrumIsID(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if s == "*" {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// cerebrumSplitList splits a comma- or semicolon-separated flag value,
// trimming blanks and dropping empties: "Video, StrAudio1" → two items.
func cerebrumSplitList(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// cerebrumResolveID maps one destination, source or level given by ID or
// by primary mnemonic to its ID. Digits (and "*") pass through. A name
// must match exactly one row: exact spelling first, then one
// case-insensitive match. Several rows under the same label, or none, is
// an error naming the candidates so the operator can pick the ID.
func cerebrumResolveID(kind, given string, rows []cerebrumMneRow) (string, error) {
	g := strings.TrimSpace(given)
	if g == "" {
		return "", fmt.Errorf("--%s: empty", kind)
	}
	if cerebrumIsID(g) {
		return g, nil
	}
	var exact, loose []cerebrumMneRow
	for _, r := range dedupeCerebrumMnes(rows) {
		switch {
		case r.Mnemonic == g:
			exact = append(exact, r)
		case strings.EqualFold(strings.TrimSpace(r.Mnemonic), g):
			loose = append(loose, r)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0].ID, nil
	case len(exact) > 1:
		return "", fmt.Errorf("--%s %q: %d %ss carry that mnemonic (%s) — give the ID", kind, g, len(exact), kind, cerebrumIDList(exact))
	case len(loose) == 1:
		return loose[0].ID, nil
	case len(loose) > 1:
		return "", fmt.Errorf("--%s %q: %d %ss match it ignoring case (%s) — give the exact spelling or the ID", kind, g, len(loose), kind, cerebrumIDList(loose))
	}
	var near []cerebrumMneRow
	for _, r := range dedupeCerebrumMnes(rows) {
		if strings.Contains(strings.ToLower(r.Mnemonic), strings.ToLower(g)) {
			near = append(near, r)
		}
	}
	if len(near) > 0 {
		return "", fmt.Errorf("--%s %q: no %s has that mnemonic; containing it: %s", kind, g, kind, cerebrumIDList(near))
	}
	return "", fmt.Errorf("--%s %q: no %s has that mnemonic (%d read)", kind, g, kind, len(dedupeCerebrumMnes(rows)))
}

// cerebrumIDList renders rows as `"label" (id)` joined by ", ", sorted by
// ID for a stable message.
func cerebrumIDList(rows []cerebrumMneRow) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, fmt.Sprintf("%q (%s)", r.Mnemonic, r.ID))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// cerebrumResolveLevels resolves a level flag — one ID or name, or a
// comma-separated list of them — to IDs in the order given, duplicates
// dropped.
func cerebrumResolveLevels(given string, rows []cerebrumMneRow) ([]string, error) {
	items := cerebrumSplitList(given)
	if len(items) == 0 {
		return nil, fmt.Errorf("--level: empty")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, it := range items {
		id, err := cerebrumResolveID("level", it, rows)
		if err != nil {
			return nil, err
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// cerebrumRoutesNeedNames reports whether any route names a destination,
// source or level instead of numbering it.
func cerebrumRoutesNeedNames(routes []routeSpec) bool {
	for _, r := range routes {
		if !cerebrumIsID(r.Dest) || !cerebrumIsID(r.Srce) || !cerebrumIsID(r.Level) {
			return true
		}
	}
	return false
}

// cerebrumResolveRoutes resolves every named field of the routes against
// the catalogue in st (Dst, Src, Lvl) and reports each distinct resolution
// once on w as `[route] name "VIEW 3" = dest 7`.
func cerebrumResolveRoutes(routes []routeSpec, st *cerebrumState, w io.Writer) ([]routeSpec, error) {
	type key struct{ kind, name string }
	told := map[key]bool{}
	tell := func(kind, name, id string) {
		if name == id || told[key{kind, name}] {
			return
		}
		told[key{kind, name}] = true
		_, _ = fmt.Fprintf(w, "[route] name %q = %s %s\n", name, kind, id)
	}
	out := make([]routeSpec, 0, len(routes))
	for _, r := range routes {
		d, err := cerebrumResolveID("dest", r.Dest, st.Dst)
		if err != nil {
			return nil, err
		}
		tell("dest", strings.TrimSpace(r.Dest), d)
		s, err := cerebrumResolveID("srce", r.Srce, st.Src)
		if err != nil {
			return nil, err
		}
		tell("srce", strings.TrimSpace(r.Srce), s)
		l, err := cerebrumResolveID("level", r.Level, st.Lvl)
		if err != nil {
			return nil, err
		}
		tell("level", strings.TrimSpace(r.Level), l)
		out = append(out, routeSpec{Dest: d, Srce: s, Level: l})
	}
	return out, nil
}

// cerebrumFilterUsageLevels keeps the usage rows that touch one of the
// level IDs, with their Levels trimmed to that set (order kept).
func cerebrumFilterUsageLevels(rows []cerebrumUsageRow, ids []string) []cerebrumUsageRow {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := rows[:0]
	for _, r := range rows {
		var keep []string
		for _, l := range r.Levels {
			if want[l] {
				keep = append(keep, l)
			}
		}
		if len(keep) == 0 {
			continue
		}
		r.Levels = keep
		out = append(out, r)
	}
	return out
}
