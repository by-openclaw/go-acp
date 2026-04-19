package export

import (
	"strings"

	"acp/internal/protocol"
)

// ImportFilter narrows an import operation to a specific subset of
// objects. IDs and Paths are mutually exclusive addressing schemes —
// callers choose one per invocation and pass only that slice. Empty
// filter means "apply to everything" (today's default).
//
// Within the chosen slice, entries combine additively: passing two
// IDs or two Paths targets both objects.
//
// Deliberately no Labels field: labels collide across sub-trees
// thousands of times in Ember+ (per-channel "gain") and in ACP2
// ("Present" under each PSU). Only ID and dotted Path are
// unambiguous. A qualified label IS a dotted path, so --label would
// just duplicate --path.
//
// Comparisons are exact and case-sensitive. Paths are matched after
// the snapshot's Path slice is joined with "." so the user can write
// --path "BOARD.Gain A" to target a single hierarchical element.
//
// Enforcement of mutual exclusion lives at the CLI / API boundary;
// Matches() tolerates both being set for defensive behaviour in tests.
type ImportFilter struct {
	IDs   []int
	Paths []string
}

// Empty reports whether the filter lets everything through.
func (f *ImportFilter) Empty() bool {
	if f == nil {
		return true
	}
	return len(f.IDs) == 0 && len(f.Paths) == 0
}

// Matches reports whether the object satisfies at least one of the
// filter's criteria. Called per object by ApplyFilter.
func (f *ImportFilter) Matches(o protocol.Object) bool {
	if f.Empty() {
		return true
	}
	for _, id := range f.IDs {
		if id == o.ID {
			return true
		}
	}
	if len(f.Paths) > 0 {
		joined := strings.Join(o.Path, ".")
		for _, p := range f.Paths {
			if p == joined {
				return true
			}
		}
	}
	return false
}

// ApplyFilter mutates the snapshot in place, removing every object
// that does not match the filter, and returns the count of objects
// removed. Empty filter leaves the snapshot untouched and returns 0.
//
// Per-slot ordering is preserved for the objects that survive.
func ApplyFilter(s *Snapshot, f *ImportFilter) int {
	if f.Empty() {
		return 0
	}
	removed := 0
	for i := range s.Slots {
		kept := s.Slots[i].Objects[:0]
		for _, o := range s.Slots[i].Objects {
			if f.Matches(o) {
				kept = append(kept, o)
			} else {
				removed++
			}
		}
		s.Slots[i].Objects = kept
	}
	return removed
}
