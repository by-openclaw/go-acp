package spec

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SelectHighestMutual picks the highest APIVer mutually supported
// between this Registry and a peer's advertised list. It implements
// the IS-04 §3 selection rule: filter by intersection, prefer highest
// minor — never silently downgrade outside the intersection.
//
// peerVersions is the comma-split set from the peer's `api_ver` TXT
// (or the equivalent field in IS-09's `is-04` block). Whitespace and
// case are tolerated; ordering does not matter.
//
// Returns ErrNoCommonVersion when the intersection is empty — the
// caller fires a compliance event and refuses the peer.
func SelectHighestMutual[T Versioned](r *Registry[T], peerVersions []string) (T, error) {
	var zero T
	if r == nil {
		return zero, fmt.Errorf("spec.SelectHighestMutual: nil Registry")
	}

	peer := normalisePeerVersions(peerVersions)
	mine := r.SupportedVersions()

	// Sort our supported versions descending so the first match wins.
	descending := make([]string, len(mine))
	copy(descending, mine)
	sort.Slice(descending, func(i, j int) bool {
		return compareAPIVer(descending[i], descending[j]) > 0
	})

	for _, v := range descending {
		if peer[v] {
			c, _ := r.Get(v)
			return c, nil
		}
	}
	return zero, ErrNoCommonVersion{
		SpecID:         r.SpecID(),
		Mine:           mine,
		PeerAdvertised: peerVersions,
	}
}

// normalisePeerVersions trims whitespace + lower-cases each entry,
// drops empties, and returns a set keyed by canonical APIVer.
func normalisePeerVersions(in []string) map[string]bool {
	out := map[string]bool{}
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			out[v] = true
		}
	}
	return out
}

// compareAPIVer returns -1 / 0 / +1 for semver-style major.minor APIVer
// strings ("v1.2" < "v1.3" < "v2.0"). Strings without a leading "v"
// or with non-numeric components compare lexicographically as a
// fallback so we never panic on malformed input.
func compareAPIVer(a, b string) int {
	am, an, ok1 := splitAPIVer(a)
	bm, bn, ok2 := splitAPIVer(b)
	if !ok1 || !ok2 {
		return strings.Compare(a, b)
	}
	if am != bm {
		if am < bm {
			return -1
		}
		return 1
	}
	if an != bn {
		if an < bn {
			return -1
		}
		return 1
	}
	return 0
}

func splitAPIVer(v string) (major, minor int, ok bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return maj, min, true
}

// ErrNoCommonVersion fires when the peer's advertised version set
// shares no entries with our registered codecs. Plugin code formats
// it into a compliance event and refuses the peer.
type ErrNoCommonVersion struct {
	SpecID         string
	Mine           []string
	PeerAdvertised []string
}

func (e ErrNoCommonVersion) Error() string {
	return fmt.Sprintf(
		"%s: no mutually-supported version (we offer %v, peer advertised %v)",
		e.SpecID, e.Mine, e.PeerAdvertised,
	)
}
