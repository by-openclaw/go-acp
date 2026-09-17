package rollcall

import (
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec/router"
)

// A plant of a thousand sources is not navigable as a list. Categories are how
// a panel narrows it down, and a group matches a name rather than owning a
// set: nothing is tagged and nothing is assigned, so a plant whose names say
// what they are is navigable without being told anything about itself.

// namedMatrix is a plant with the kind of names a real one has.
func namedMatrix(sources, dests []string) *canonical.Matrix {
	m := testMatrix(int64(len(dests)), int64(len(sources)))
	m.SourceLabels = map[string]map[string]string{"Primary": {}}
	m.TargetLabels = map[string]map[string]string{"Primary": {}}
	for i, n := range sources {
		m.SourceLabels["Primary"][itoa(i)] = n
	}
	for i, n := range dests {
		m.TargetLabels["Primary"][itoa(i)] = n
	}
	return m
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestAPlantIsNamedByItsTree(t *testing.T) {
	// The names come from the tree when it carries them, because a plant's own
	// names are the only evidence of how it is organised.
	r := buildRouter("router", []*canonical.Matrix{
		namedMatrix([]string{"CAM 1", "VTR 1"}, []string{"MON 1", "REC 1"}),
	})
	lv := &r.matrices[0].levels[0]

	if got := lv.sources; got[0] != "CAM 1" || got[1] != "VTR 1" {
		t.Errorf("sources = %v, want the tree's names", got)
	}
	if lv.dests[0].name != "MON 1" || lv.dests[1].name != "REC 1" {
		t.Errorf("destinations = %q %q", lv.dests[0].name, lv.dests[1].name)
	}
}

func TestAPlantWithNoNamesIsNamedByItsPositions(t *testing.T) {
	// Something has to name a source, and its position is the only thing left.
	r := buildRouter("router", []*canonical.Matrix{testMatrix(2, 2)})
	lv := &r.matrices[0].levels[0]
	if lv.sources[0] != "SRC 1" || lv.dests[1].name != "DST 2" {
		t.Errorf("sources %v dests %q", lv.sources, lv.dests[1].name)
	}
	// A placeholder says where a source sits and nothing about what it is, so
	// there is nothing to organise: grouping by "SRC" separates sources from
	// destinations, which a panel already does.
	if len(r.categories) != 0 {
		t.Errorf("%d categories on a plant with one kind of name", len(r.categories))
	}
}

func TestCategoriesComeFromTheNames(t *testing.T) {
	r := buildRouter("router", []*canonical.Matrix{
		namedMatrix(
			[]string{"CAM 1", "CAM 2", "VTR 1", "GFX 1"},
			[]string{"MON 1", "MON 2", "REC 1", "AUX 1"},
		),
	})

	if len(r.categories) != 1 {
		t.Fatalf("%d categories, want the one that sorts a plant by kind", len(r.categories))
	}
	c := r.categories[0]
	if c.name != "Type" {
		t.Errorf("category is called %q", c.name)
	}
	// A source is one kind of thing, so choosing a group rules out the others.
	if !c.exclusive {
		t.Error("the kind category is not exclusive")
	}

	// Sources and destinations are grouped together: a plant where CAM names a
	// source and MON a destination is one plant with two kinds of name in it.
	want := []string{"AUX", "CAM", "GFX", "MON", "REC", "VTR"}
	if len(c.groups) != len(want) {
		t.Fatalf("%d groups, want %d", len(c.groups), len(want))
	}
	for i, w := range want {
		g := c.groups[i]
		if g.name != w || g.search != w || g.start != 0 {
			t.Errorf("group %d = %q searching %q at %d, want %q at 0",
				i, g.name, g.search, g.start, w)
		}
	}
}

func TestTheCategoryTablesArePublished(t *testing.T) {
	// A client walks the root block to find the categories, then each
	// category's own block to find its groups. Every command in both has to
	// answer, because a refusal reads as a broken interface rather than an
	// absent feature.
	r := buildRouter("router", []*canonical.Matrix{
		namedMatrix([]string{"CAM 1", "VTR 1"}, []string{"MON 1", "REC 1"}),
	})
	vals := r.values()

	if got := vals[uint32(router.CmdNumCategories)].Val; got != int32(len(r.categories)) {
		t.Errorf("the root block says %d categories, the model has %d", got, len(r.categories))
	}
	if vals[uint32(router.CmdCategoryBase)].Val == 0 || vals[uint32(router.CmdCategoryStep)].Val == 0 {
		t.Error("the category table was published with no base or step")
	}

	base, ok := r.catTable.Command(1)
	if !ok {
		t.Fatal("category 1 has no table entry")
	}
	if got := vals[uint32(base+router.OffCategoryName)].Text; got != "Type" {
		t.Errorf("category 1 is named %q", got)
	}
	if got := vals[uint32(base+router.OffCategoryExclusive)].Val; got != 1 {
		t.Errorf("category 1 exclusive = %d", got)
	}
	if got := vals[uint32(base+router.OffCategorySortIndex)].Val; got != 1 {
		t.Errorf("category 1 sort index = %d", got)
	}

	groups := vals[uint32(base+router.OffNumGroups)].Val
	if groups != int32(len(r.categories[0].groups)) {
		t.Errorf("category 1 says %d groups, it has %d", groups, len(r.categories[0].groups))
	}

	gb, ok := r.categories[0].table.Command(1)
	if !ok {
		t.Fatal("group 1 has no table entry")
	}
	if got := vals[uint32(gb+router.OffGroupName)].Text; got == "" {
		t.Error("group 1 has no name")
	}
	if got := vals[uint32(gb+router.OffGroupSearchString)].Text; got == "" {
		t.Error("group 1 has nothing to search for, so it selects nothing")
	}
	if _, ok := vals[uint32(gb+router.OffGroupSearchStart)]; !ok {
		t.Error("group 1 does not say where to search")
	}
}

func TestCategoryTablesOverlapNothing(t *testing.T) {
	// Every table is allocated from one running counter, so nothing can
	// overlap whatever the sizes are. Categories were added between the
	// matrices and their levels, which is exactly where an overlap would have
	// gone unnoticed.
	r := buildRouter("router", []*canonical.Matrix{
		namedMatrix([]string{"CAM 1", "VTR 1", "GFX 1"}, []string{"MON 1", "REC 1"}),
	})

	seen := make(map[uint32]bool)
	for cmd := range r.values() {
		if seen[cmd] {
			t.Fatalf("command %d is published twice", cmd)
		}
		seen[cmd] = true
	}
}

func TestTheLeadingWordOfAName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"CAM 1", "CAM"},
		{"CAM12", "CAM"},
		{"VTR-2", "VTR"},
		{"AUD_L", "AUD"},
		{"Studio", "Studio"},
		{"1", ""},
		{"", ""},
	} {
		if got := leadingWord(tc.in); got != tc.want {
			t.Errorf("leadingWord(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestALabelSetThatDoesNotFitThePlant(t *testing.T) {
	// A tree may carry labels for numbers that are not there, or none at all.
	// Neither is a reason to serve nothing.
	m := testMatrix(2, 2)
	m.SourceLabels = map[string]map[string]string{
		"Primary": {"0": "CAM 1", "9": "off the end", "x": "not a number", "1": ""},
	}
	got, named := matrixLabels(m.SourceLabels, 2, "SRC")
	if !named {
		t.Error("a tree that labelled a source did not count as naming the plant")
	}
	if got[0] != "CAM 1" {
		t.Errorf("labelled source = %q", got[0])
	}
	if got[1] != "SRC 2" {
		t.Errorf("a source whose label is empty is named %q, want its position", got[1])
	}
}

func TestAPlantWhoseNamesAreAllOneKind(t *testing.T) {
	// Named, but all of a piece. There is still nothing to narrow down, and a
	// category offering one group that matches everything is a click that does
	// nothing.
	r := buildRouter("router", []*canonical.Matrix{
		namedMatrix([]string{"CAM 1", "CAM 2"}, []string{"CAM 3", "CAM 4"}),
	})
	if len(r.categories) != 0 {
		t.Errorf("%d categories on a plant of one kind of thing", len(r.categories))
	}
}

func TestACategoryWhoseGroupTableIsEmpty(t *testing.T) {
	// The tables are allocated from the groups, so the two agree by
	// construction. A category publishing more groups than its table describes
	// would otherwise write them over whatever command came next.
	c := routerCategory{
		name:   "Type",
		groups: []routerGroup{{name: "CAM", search: "CAM"}},
		table:  router.Table{Base: 500, Step: router.GroupTableSize, Count: 0},
	}

	out := make(map[router.Command]any)
	c.values(200,
		func(cmd router.Command, v any) { out[cmd] = v },
		func(cmd router.Command, v any) { out[cmd] = v },
	)

	// The category itself is published; the group it cannot address is not.
	if _, ok := out[200+router.OffCategoryName]; !ok {
		t.Error("the category was not published")
	}
	if _, ok := out[500+router.OffGroupName]; ok {
		t.Error("a group outside the table was published")
	}
}

func TestAFlagThatIsNotSet(t *testing.T) {
	// A category that is not exclusive lets an operator combine its groups.
	// Ours are exclusive, so this is the only place the other value is made.
	c := routerCategory{name: "Area", table: router.Table{Base: 1, Step: 3}}

	out := make(map[router.Command]any)
	c.values(300,
		func(cmd router.Command, v any) { out[cmd] = v },
		func(cmd router.Command, v any) { out[cmd] = v },
	)
	if got := out[300+router.OffCategoryExclusive]; got != int32(0) {
		t.Errorf("a category that is not exclusive published %v", got)
	}
}

func TestACategoryOutsideTheRouterTable(t *testing.T) {
	// values walks the categories against the table that describes them, and
	// stops at whichever ends first.
	r := buildRouter("router", []*canonical.Matrix{
		namedMatrix([]string{"CAM 1", "VTR 1"}, []string{"MON 1", "REC 1"}),
	})
	r.catTable.Count = 0

	vals := r.values()
	base, _ := router.Table{Base: r.catTable.Base, Step: router.CategoryTableSize, Count: 1}.Command(1)
	if _, ok := vals[uint32(base+router.OffCategoryName)]; ok {
		t.Error("a category outside the table was published")
	}
}
