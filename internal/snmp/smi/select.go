package smi

import (
	"fmt"
	"sort"
	"strings"
)

// Select keeps one module per name.
//
// Vendor sets ship the same module more than once — the Snell set carries
// SNELL-WILCOX-PRODUCT-REG three times in three revisions — and a compiler
// that loaded every copy would define each OID up to three times with no
// way to say which is true. The rules, in order:
//
//  1. a pin wins: pins maps a module name to a substring of the one file
//     that must be used, for the cases no date can decide;
//  2. otherwise the newest LAST-UPDATED wins;
//  3. otherwise — equal or absent dates — the lexically first path wins,
//     so the choice is at least the same on every machine.
//
// Every copy that loses is reported, with the one that won.
func Select(mods []*Module, pins map[string]string) ([]*Module, []Finding) {
	byName := map[string][]*Module{}
	var names []string
	for _, m := range mods {
		if _, seen := byName[m.Name]; !seen {
			names = append(names, m.Name)
		}
		byName[m.Name] = append(byName[m.Name], m)
	}
	sort.Strings(names)

	var out []*Module
	var findings []Finding
	for _, name := range names {
		copies := byName[name]
		if len(copies) == 1 {
			out = append(out, copies[0])
			continue
		}
		winner, why := choose(copies, pins[name])
		out = append(out, winner)
		for _, c := range copies {
			if c != winner {
				findings = append(findings, Finding{c.File, 0, "duplicate-module",
					fmt.Sprintf("module %s is also defined in %s, which was kept (%s)",
						name, winner.File, why)})
			}
		}
	}
	return out, findings
}

func choose(copies []*Module, pin string) (*Module, string) {
	if pin != "" {
		for _, c := range copies {
			if strings.Contains(c.File, pin) {
				return c, "pinned"
			}
		}
	}
	sorted := append([]*Module(nil), copies...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := revision(sorted[i].LastUpdated), revision(sorted[j].LastUpdated)
		if a != b {
			return a > b
		}
		return sorted[i].File < sorted[j].File
	})
	w := sorted[0]
	if revision(w.LastUpdated) == revision(sorted[1].LastUpdated) {
		return w, "same revision, first by path"
	}
	return w, "newest LAST-UPDATED " + w.LastUpdated
}

// revision makes a LAST-UPDATED comparable as a string.
//
// RFC 2578 §2 allows "YYMMDDHHMMZ" only for 1900-1999 and requires four
// digits after that. Vendors ignore it — the Snell set writes 2013 as
// "1305161501Z" — so a two-digit year below 70 is read as 20YY. That is a
// deviation from the RFC, made because the alternative dates every Snell
// module to the 1900s and picks the oldest copy of each.
func revision(lu string) string {
	s := strings.TrimSuffix(strings.TrimSpace(lu), "Z")
	if len(s) == 10 {
		if s[0] < '7' {
			return "20" + s
		}
		return "19" + s
	}
	return s
}
