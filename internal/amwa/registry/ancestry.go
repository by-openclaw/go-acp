package registry

import (
	"dhs/internal/amwa/codec/is04"
)

// ParentsIndex returns a snapshot of id -> parents[] for one resource
// kind. Only Sources and Flows carry `parents` in IS-04; every other
// kind returns nil, which callers treat as "ancestry undefined here".
//
// A snapshot rather than live access on purpose: ancestry traversal
// happens BEFORE Store.ListPaged takes the read lock, and reaching
// into the maps from the paging predicate would re-enter the lock.
func (s *Store) ParentsIndex(t is04.ResourceType) map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch t {
	case is04.ResourceSource:
		out := make(map[string][]string, len(s.sources))
		for id, src := range s.sources {
			out[id] = append([]string(nil), src.Parents...)
		}
		return out
	case is04.ResourceFlow:
		out := make(map[string][]string, len(s.flows))
		for id, f := range s.flows {
			out[id] = append([]string(nil), f.Parents...)
		}
		return out
	}
	return nil
}

// Ancestry directions, IS-04 §6.1.5 / the AMWA registries wiki:
// `children` selects the DESCENDANTS of ancestry_id (resources whose
// parents chain reaches it); `parents` selects its ANCESTORS.
const (
	ancestryChildren = "children"
	ancestryParents  = "parents"
)

// ancestrySet computes the set of resource ids the ancestry query
// selects. generations caps the traversal depth; 0 means unlimited.
//
// The root itself is never part of its own ancestry — a source is
// neither its own child nor its own parent — and an unknown root
// yields the empty set, which is precisely what AMWA test_25 checks
// with a random UUID: HTTP 200 and no records, not an error.
//
// Cycles cannot happen in spec-valid data (a resource cannot be its
// own ancestor), but a registry stores what peers sent it, so the
// visited set guards traversal against malformed parents chains.
func ancestrySet(index map[string][]string, rootID, ancestryType string, generations int) map[string]bool {
	out := map[string]bool{}
	if index == nil {
		return out
	}

	switch ancestryType {
	case ancestryParents:
		// Walk UP from the root along its own parents chain.
		frontier := append([]string(nil), index[rootID]...)
		visited := map[string]bool{rootID: true}
		for depth := 1; len(frontier) > 0 && (generations == 0 || depth <= generations); depth++ {
			var next []string
			for _, id := range frontier {
				if visited[id] {
					continue
				}
				visited[id] = true
				// Only ids that exist in the registry are results; a
				// dangling parent reference selects nothing but its own
				// ancestors are still unreachable, so skip entirely.
				if _, exists := index[id]; exists {
					out[id] = true
					next = append(next, index[id]...)
				}
			}
			frontier = next
		}

	case ancestryChildren:
		// Walk DOWN: invert the parents edges once, then BFS.
		children := make(map[string][]string, len(index))
		for id, parents := range index {
			for _, p := range parents {
				children[p] = append(children[p], id)
			}
		}
		frontier := append([]string(nil), children[rootID]...)
		visited := map[string]bool{rootID: true}
		for depth := 1; len(frontier) > 0 && (generations == 0 || depth <= generations); depth++ {
			var next []string
			for _, id := range frontier {
				if visited[id] {
					continue
				}
				visited[id] = true
				out[id] = true
				next = append(next, children[id]...)
			}
			frontier = next
		}
	}
	return out
}
