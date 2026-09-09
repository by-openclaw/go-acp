package rollcall

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"dhs/internal/snell-rollcall/codec/router"
)

// Categories are how a panel narrows a plant down to what an operator wants.
//
// A plant of a thousand sources is not navigable as a list, so the interface
// carries categories, each holding groups, and a group selects by matching a
// name: it has a search string and the character index to look for it at. That
// is the whole mechanism — nothing is tagged, nothing is assigned. A plant
// whose names carry their kind in them is navigable without being told
// anything about itself.
//
// Which means the categories can be derived rather than configured, and are.
// The names a plant uses are the only evidence of how it is organised, so the
// groups here are the distinct leading words of those names: a plant of "CAM
// 1", "CAM 2", "VTR 1" offers Cameras and VTRs without anybody writing that
// down. A plant whose names share one word offers one group, honestly, and a
// panel shows it a list — which is what a list of one kind of thing is.

// routerCategory is one way of narrowing a plant down.
type routerCategory struct {
	name string

	// exclusive says whether choosing a group here excludes the others. Ours
	// are: a source is one kind of thing, and offering "CAM or VTR" as a
	// combination would offer an empty selection for every pair.
	exclusive bool

	// sortIndex is the order a panel walks categories in, which the
	// specification calls a "guided flow of category selections".
	sortIndex int32

	groups []routerGroup
	table  router.Table
}

// routerGroup selects the names that match it.
type routerGroup struct {
	name string

	// search and start are the match: the string to look for and the character
	// index to look for it at. Zero means the name begins with it.
	search string
	start  int32
}

// buildCategories derives the categories of a plant from the names in it.
//
// Sources and destinations are looked at together. A panel filters both with
// one set of categories, and a plant where "CAM" names a source and "MON" a
// destination is one plant with two kinds of name in it, not two plants.
func buildCategories(sources []string, dests []routerDest) []routerCategory {
	names := make([]string, 0, len(sources)+len(dests))
	names = append(names, sources...)
	for i := range dests {
		names = append(names, dests[i].name)
	}

	words := leadingWords(names)
	if len(words) < 2 {
		// One kind of name is not a way of narrowing anything down. A
		// category offering a single group that matches everything is worse
		// than no category: it is a click that does nothing.
		return nil
	}

	groups := make([]routerGroup, 0, len(words))
	for _, w := range words {
		groups = append(groups, routerGroup{name: w, search: w, start: 0})
	}

	return []routerCategory{{
		name:      "Type",
		exclusive: true,
		sortIndex: 1,
		groups:    groups,
	}}
}

// leadingWords returns the distinct leading words of a set of names, in the
// order a panel should offer them.
//
// The leading word is everything before the first digit or separator, which is
// how plant names are written: a name is a kind and then which one of it.
func leadingWords(names []string) []string {
	seen := make(map[string]bool, len(names))
	var out []string
	for _, n := range names {
		w := leadingWord(n)
		if w == "" || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// leadingWord is the kind half of a plant name.
func leadingWord(name string) string {
	end := len(name)
	for i, r := range name {
		if unicode.IsDigit(r) || r == ' ' || r == '_' || r == '-' {
			end = i
			break
		}
	}
	return strings.TrimSpace(name[:end])
}

// values publishes one category and the groups under it.
func (c *routerCategory) values(base router.Command, num, str func(router.Command, any)) {
	str(base+router.OffCategoryName, c.name)
	num(base+router.OffCategoryExclusive, boolValue(c.exclusive))
	num(base+router.OffCategorySortIndex, c.sortIndex)
	num(base+router.OffNumGroups, int32(c.table.Count))
	num(base+router.OffGroupBase, int32(c.table.Base))
	num(base+router.OffGroupStep, int32(c.table.Step))

	for i := range c.groups {
		g := &c.groups[i]
		gb, ok := c.table.Command(uint32(i) + 1)
		if !ok {
			continue
		}
		str(gb+router.OffGroupName, g.name)
		str(gb+router.OffGroupSearchString, g.search)
		num(gb+router.OffGroupSearchStart, g.start)
	}
}

// boolValue is how a flag travels on this interface.
func boolValue(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// matrixLabels returns one name per source or destination.
//
// A canonical matrix carries its labels per level, keyed by the level's own
// description and then by number as a decimal string. The first level is used:
// a name is a name, and a plant that labels its levels differently is naming
// the same physical thing twice.
//
// Numbering is from zero in the canonical form and from one on this interface,
// which is where the offset comes from. A number with no label is named after
// its position, because something has to name it.
// It also reports whether the tree named anything at all, which is what
// decides if the plant can be organised: a placeholder says where a source
// sits and nothing about what it is, and grouping by "SRC" separates sources
// from destinations, which a panel already does.
func matrixLabels(labels map[string]map[string]string, count int, prefix string) ([]string, bool) {
	return applyLabels(firstLevel(labels), count, prefix)
}

// applyLabels names each entity from one level's label set, falling back to
// its position.
func applyLabels(level map[string]string, count int, prefix string) ([]string, bool) {
	out := make([]string, count)
	for i := range out {
		out[i] = fmt.Sprintf("%s %d", prefix, i+1)
	}

	if level == nil {
		return out, false
	}
	named := false
	for k, v := range level {
		n, err := strconv.Atoi(k)
		if err != nil || n < 0 || n >= count || v == "" {
			continue
		}
		out[n] = v
		named = true
	}
	return out, named
}

// firstLevel picks a label set deterministically, so two runs of one tree
// produce one plant.
func firstLevel(labels map[string]map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return labels[keys[0]]
}

// selects lists what one of a category's groups picks out of the plant.
//
// A group is a search string and the character index to look for it at, and
// nothing else: it owns no set and nothing is tagged with it. So the only way
// to say what it selects is to run the match, which is what a panel would do
// if it could — and what this says instead, because the panel's grid has no
// category key to run it with.
func (c *routerCategory) selects(r *routerModel, group int) string {
	if group < 1 || group > len(c.groups) {
		return "nothing"
	}
	g := &c.groups[group-1]

	var names []string
	for i := range r.matrices {
		for j := range r.matrices[i].levels {
			lv := &r.matrices[i].levels[j]
			for _, n := range lv.sources {
				names = appendMatch(names, n, g)
			}
			for k := range lv.dests {
				names = appendMatch(names, lv.dests[k].name, g)
			}
		}
	}
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, ", ")
}

// appendMatch adds a name to the list when the group matches it, and never
// twice: a plant of four levels names the same source four times.
func appendMatch(out []string, name string, g *routerGroup) []string {
	if !g.matches(name) {
		return out
	}
	for _, seen := range out {
		if seen == name {
			return out
		}
	}
	return append(out, name)
}

// matches reports whether a group picks out a name.
//
// The search string is looked for at a fixed character index rather than
// anywhere in the name, which is what the start field means: a group searching
// "CAM" at 0 selects "CAM 1" and not "STUDIO CAM 1".
func (g *routerGroup) matches(name string) bool {
	start := int(g.start)
	if start < 0 || start+len(g.search) > len(name) {
		return false
	}
	return name[start:start+len(g.search)] == g.search
}
