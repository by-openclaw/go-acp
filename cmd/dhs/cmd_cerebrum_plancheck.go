package main

import (
	"fmt"
	"strings"

	"dhs/internal/consumer"
)

// cerebrumPlan is what an import asks for, by row kind, as the IDs it names.
type cerebrumPlan struct {
	routes    []routeSpec
	locks     []cerebrumLockRow // destination locks
	srceLocks []cerebrumLockRow // source locks: Dest carries the source ID
	src, dst  []cerebrumMneRow
	lvl       []cerebrumMneRow // a level the plan names here may be new: LEVEL_MNE creates it
}

// cerebrumPlanUnknown lists every row of the plan that names a destination,
// source or level the server does not list. An ID cannot be clamped like a
// value: such a plan is refused whole, before anything is sent. A catalogue
// the server did not return (a refused obtain) is not judged.
func cerebrumPlanUnknown(st *cerebrumState, p cerebrumPlan) []string {
	ids := func(rows []cerebrumMneRow) map[string]bool {
		if len(rows) == 0 {
			return nil
		}
		m := map[string]bool{}
		for _, r := range rows {
			m[r.ID] = true
		}
		return m
	}
	src, dst, lvl := ids(st.Src), ids(st.Dst), ids(st.Lvl)
	if lvl != nil {
		for _, r := range p.lvl {
			lvl[r.ID] = true
		}
	}
	var bad []string
	miss := func(set map[string]bool, id string) bool {
		id = strings.TrimSpace(id)
		return set != nil && id != "" && !set[id]
	}
	for _, r := range p.routes {
		if miss(dst, r.Dest) {
			bad = append(bad, fmt.Sprintf("route %s:%s:%s: destination %s does not exist", r.Dest, r.Srce, r.Level, r.Dest))
		}
		if miss(src, r.Srce) {
			bad = append(bad, fmt.Sprintf("route %s:%s:%s: source %s does not exist", r.Dest, r.Srce, r.Level, r.Srce))
		}
		if miss(lvl, r.Level) {
			bad = append(bad, fmt.Sprintf("route %s:%s:%s: level %s does not exist", r.Dest, r.Srce, r.Level, r.Level))
		}
		if miss(lvl, r.SrceLevel) {
			bad = append(bad, fmt.Sprintf("route %s:%s:%s: source level %s does not exist", r.Dest, r.Srce, r.Level, r.SrceLevel))
		}
	}
	lockRows := func(kind string, set map[string]bool, rows []cerebrumLockRow) {
		for _, r := range rows {
			if miss(set, r.Dest) {
				bad = append(bad, fmt.Sprintf("%s lock %s: %s %s does not exist", kind, r.Dest, kind, r.Dest))
			}
			for _, l := range r.Levels {
				if miss(lvl, l) {
					bad = append(bad, fmt.Sprintf("%s lock %s: level %s does not exist", kind, r.Dest, l))
				}
			}
		}
	}
	lockRows("destination", dst, p.locks)
	lockRows("source", src, p.srceLocks)
	for _, r := range p.src {
		if miss(src, r.ID) {
			bad = append(bad, fmt.Sprintf("source label %s: source %s does not exist", r.ID, r.ID))
		}
	}
	for _, r := range p.dst {
		if miss(dst, r.ID) {
			bad = append(bad, fmt.Sprintf("destination label %s: destination %s does not exist", r.ID, r.ID))
		}
	}
	return bad
}

// cerebrumPlanError is the refusal of a plan naming IDs the server lacks
// (exit 2), every row listed.
func cerebrumPlanError(bad []string) error {
	return &consumer.ValidationError{Field: "import",
		Reason: fmt.Sprintf("%d row(s) name an IO or level the server does not have; nothing sent:\n  %s", len(bad), strings.Join(bad, "\n  "))}
}
