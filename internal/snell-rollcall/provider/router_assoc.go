package rollcall

import (
	"sort"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec/dtp"
	"dhs/internal/snell-rollcall/codec/router"
)

// An association is one logical thing spread across the levels of a matrix.
//
// A source in a plant is rarely one signal. A camera is a picture, a pair of
// audio levels and some ancillary data, each on a level of its own with its
// own numbering. An association names all of them at once, and routing by
// association is how a panel takes a camera rather than taking four unrelated
// crosspoints and hoping they match.
//
// The specification is explicit about which mechanism solves which problem:
// multi-level control is routing by association, multi-matrix control is
// tielines. Neither substitutes for the other, and a plant needing both needs
// both.

// routerAssoc is one association: a name and its member on each level.
type routerAssoc struct {
	name string

	// members is the entity number on each level, counting levels from one and
	// entities from one. Zero means this association has nothing on that
	// level, which is ordinary: a graphics source with no audio is not a
	// fault.
	members []uint32
}

// buildAssocs gathers one association per entity across a matrix's levels.
//
// The members are the same number on every level, which is what a tree of
// label sets can describe and what a plant that numbers its levels alike
// actually does. A plant that pairs video 4 with audio 7 carries a mapping the
// canonical form has no field for; when it grows one, this is where it lands.
func buildAssocs(levels []routerLevel, sources bool) []routerAssoc {
	if len(levels) == 0 {
		return nil
	}

	// The count is the smallest level's, because an association whose member
	// is off the end of a level is one a panel would route into nothing.
	count := entityCount(levels[0], sources)
	for _, lv := range levels[1:] {
		if n := entityCount(lv, sources); n < count {
			count = n
		}
	}
	if count == 0 {
		return nil
	}

	out := make([]routerAssoc, 0, count)
	for i := range count {
		a := routerAssoc{
			name:    entityName(levels[0], sources, i),
			members: make([]uint32, len(levels)),
		}
		for j := range levels {
			a.members[j] = uint32(i + 1)
		}
		out = append(out, a)
	}
	return out
}

func entityCount(lv routerLevel, sources bool) int {
	if sources {
		return len(lv.sources)
	}
	return len(lv.dests)
}

func entityName(lv routerLevel, sources bool, i int) string {
	if sources {
		return lv.sources[i]
	}
	return lv.dests[i].name
}

// values publishes one association's table.
func (a *routerAssoc) values(base router.Command, num func(router.Command, int32), str func(router.Command, string)) {
	str(base+router.OffAssocName8, a.name)
	str(base+router.OffAssocName32, a.name)
	str(base+router.OffAssocAltName, a.name)

	// Levels count from one, so the member command is always addressable: the
	// only way AssocMember refuses is a level below one, which this cannot
	// produce.
	for level := 1; level <= len(a.members); level++ {
		cmd, _ := router.AssocMember(base, level)
		num(cmd, int32(a.members[level-1]))
	}
}

// levelNames returns the levels a canonical matrix describes.
//
// A canonical matrix keys its labels by the level they belong to, which is the
// same thing this protocol calls a level. The two label maps are merged
// because a plant may name its destinations on a level whose sources it did
// not, and a level with names on one side is still a level.
//
// A matrix that names none has one level called Level 1: a matrix with no
// levels has nowhere to put a crosspoint.
func levelNames(m *canonical.Matrix) []string {
	seen := make(map[string]bool)
	var out []string
	for _, set := range []map[string]map[string]string{m.SourceLabels, m.TargetLabels} {
		for k := range set {
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return []string{"Level 1"}
	}
	sort.Strings(out)
	return out
}

// levelLabels returns the names one level gives its entities.
func levelLabels(labels map[string]map[string]string, level string, count int, prefix string) ([]string, bool) {
	if set, ok := labels[level]; ok {
		return applyLabels(set, count, prefix)
	}
	return applyLabels(nil, count, prefix)
}

// makeRoute applies a route by association and says what happened.
//
// It is the only routing command that is not a write to a destination's own
// table entry, because it is not about one destination: it names a source
// association, a destination association, and which of the source's levels to
// carry across. A camera taken to a monitor without its audio is one request
// here with two bits set rather than two requests.
// routedChange is one crosspoint a route moved.
type routedChange struct {
	level *routerLevel
	dest  int
}

func (r *routerModel) makeRoute(req router.MakeRoute) (router.RouteResult, []routedChange) {
	dstMx, ok := r.matrixAt(req.DestMatrix)
	if !ok {
		return router.RouteBadParameters, nil
	}
	srcMx, ok := r.matrixAt(req.SourceMatrix)
	if !ok {
		return router.RouteBadParameters, nil
	}

	dst, ok := assocAt(dstMx.dstAssocs, req.DestAssoc)
	if !ok {
		return router.RouteBadParameters, nil
	}
	src, ok := assocAt(srcMx.srcAssocs, req.SourceAssoc)
	if !ok {
		return router.RouteBadParameters, nil
	}

	// A route across matrices needs a tieline, and there is no pool to take
	// one from yet. Saying so is the answer the specification has for it;
	// routing anyway would put a crosspoint somewhere it cannot reach.
	if req.DestMatrix != req.SourceMatrix {
		return router.RouteNoTieline, nil
	}

	// Nothing is applied until everything has been checked, so a request that
	// fails halfway leaves the plant as it was rather than half routed.
	type change struct {
		routedChange
		src uint32
	}
	var changes []change

	for level := 1; level <= len(dstMx.levels); level++ {
		if !levelSelected(req.Levels, level) {
			continue
		}
		if level > len(src.members) || level > len(dst.members) {
			return router.RouteBadParameters, nil
		}
		lv := &dstMx.levels[level-1]

		d := int(dst.members[level-1])
		s := src.members[level-1]
		if d < 1 || d > len(lv.dests) || s < 1 || int(s) > len(lv.sources) {
			return router.RouteNotInstalled, nil
		}
		if lv.dests[d-1].protect.Protected {
			return router.RouteProtected, nil
		}
		changes = append(changes, change{routedChange{level: lv, dest: d}, s})
	}

	moved := make([]routedChange, 0, len(changes))
	for _, c := range changes {
		c.level.dests[c.dest-1].routed = router.SourcePin{
			Matrix: c.level.matrixNumber,
			Level:  c.level.levelNumber,
			Source: uint16(c.src),
		}
		moved = append(moved, c.routedChange)
	}
	return router.RouteOK, moved
}

// levelSelected reports whether a request asked for a level, counting from one.
//
// A bitmap that says nothing about a level is not asking for it. An empty
// bitmap therefore selects nothing, which is a request to do nothing rather
// than a request to do everything — the difference matters when a panel has
// deselected every level and the operator presses take.
func levelSelected(b dtp.Bitmap, level int) bool {
	return b.Get(level - 1)
}

func (r *routerModel) matrixAt(n uint32) (*routerMatrix, bool) {
	if n < 1 || int(n) > len(r.matrices) {
		return nil, false
	}
	return &r.matrices[n-1], true
}

func assocAt(list []routerAssoc, n uint32) (*routerAssoc, bool) {
	if n < 1 || int(n) > len(list) {
		return nil, false
	}
	return &list[n-1], true
}
