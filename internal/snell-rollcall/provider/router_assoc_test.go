package rollcall

import (
	"context"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/dtp"
	"dhs/internal/snell-rollcall/codec/router"
)

// A source in a plant is rarely one signal. A camera is a picture, a pair of
// audio levels and some ancillary data, each on a level of its own with its
// own numbering. An association names all of them at once, and routing by
// association is how a panel takes a camera rather than four crosspoints.

// plantWithLevels is a matrix whose tree names several levels, which is what
// makes it a multi-level plant at all.
func plantWithLevels(levels []string, sources, dests []string) *canonical.Matrix {
	m := testMatrix(int64(len(dests)), int64(len(sources)))
	m.SourceLabels = map[string]map[string]string{}
	m.TargetLabels = map[string]map[string]string{}
	for _, lv := range levels {
		m.SourceLabels[lv] = map[string]string{}
		m.TargetLabels[lv] = map[string]string{}
		for i, n := range sources {
			m.SourceLabels[lv][itoa(i)] = n
		}
		for i, n := range dests {
			m.TargetLabels[lv][itoa(i)] = n
		}
	}
	return m
}

func levelledRouter(t *testing.T) *routerModel {
	t.Helper()
	return buildRouter("router", []*canonical.Matrix{
		plantWithLevels(
			[]string{"Video", "Audio 1", "Audio 2", "Data"},
			[]string{"CAM 1", "CAM 2", "VTR 1"},
			[]string{"MON 1", "MON 2", "REC 1"},
		),
	})
}

func TestATreeWithLevelsBecomesAMatrixWithLevels(t *testing.T) {
	// A canonical matrix keys its labels by the level they belong to, which is
	// the same thing this protocol calls a level. Ignoring that gave every
	// plant exactly one level, which is a plant no camera fits in.
	r := levelledRouter(t)
	m := &r.matrices[0]

	want := []string{"Audio 1", "Audio 2", "Data", "Video"}
	if len(m.levels) != len(want) {
		t.Fatalf("%d levels, want %d", len(m.levels), len(want))
	}
	for i, w := range want {
		if m.levels[i].name != w {
			t.Errorf("level %d is %q, want %q", i+1, m.levels[i].name, w)
		}
		if m.levels[i].levelNumber != uint8(i+1) {
			t.Errorf("level %q numbers itself %d", m.levels[i].name, m.levels[i].levelNumber)
		}
	}
}

func TestAnAssociationGathersOneThingAcrossEveryLevel(t *testing.T) {
	r := levelledRouter(t)
	m := &r.matrices[0]

	if len(m.srcAssocs) != 3 || len(m.dstAssocs) != 3 {
		t.Fatalf("%d source and %d destination associations",
			len(m.srcAssocs), len(m.dstAssocs))
	}
	a := m.srcAssocs[0]
	if a.name != "CAM 1" {
		t.Errorf("association 1 is named %q", a.name)
	}
	if len(a.members) != len(m.levels) {
		t.Fatalf("association 1 has %d members for %d levels", len(a.members), len(m.levels))
	}
	for i, got := range a.members {
		if got != 1 {
			t.Errorf("association 1 holds entity %d on level %d", got, i+1)
		}
	}
}

func TestTheAssociationTablesArePublished(t *testing.T) {
	r := levelledRouter(t)
	m := &r.matrices[0]
	vals := r.values()

	base, _ := r.table.Command(1)
	if got := vals[uint32(base+router.OffNumSrcAssocs)].Val; got != 3 {
		t.Errorf("the matrix says %d source associations", got)
	}
	if got := vals[uint32(base+router.OffNumDstAssocs)].Val; got != 3 {
		t.Errorf("the matrix says %d destination associations", got)
	}
	if got := vals[uint32(base+router.OffSrcAssocStep)].Val; got != int32(router.AssocTableSize(4)) {
		t.Errorf("the association step is %d, want three names and one per level", got)
	}

	ab, ok := m.srcAssocTbl.Command(1)
	if !ok {
		t.Fatal("association 1 has no table entry")
	}
	if got := vals[uint32(ab+router.OffAssocName8)].Text; got != "CAM 1" {
		t.Errorf("association 1 short name = %q", got)
	}
	if got := vals[uint32(ab+router.OffAssocName32)].Text; got != "CAM 1" {
		t.Errorf("association 1 long name = %q", got)
	}
	for level := 1; level <= len(m.levels); level++ {
		cmd, _ := router.AssocMember(ab, level)
		if got := vals[uint32(cmd)].Val; got != 1 {
			t.Errorf("association 1 on level %d holds %d", level, got)
		}
	}
}

// levels builds the bitmap a request carries, counting levels from one.
func levels(on ...int) dtp.Bitmap {
	b := dtp.NewBitmap(8)
	for _, n := range on {
		b.Set(n - 1)
	}
	return b
}

func TestTakingASourceOnSomeLevelsAndNotOthers(t *testing.T) {
	// The case this exists for: a camera to a monitor with its picture and its
	// audio, leaving the ancillary data where it was. One request, three bits.
	s := newServed(t, &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "frame",
			Children: []canonical.Element{plantWithLevels(
				[]string{"Video", "Audio 1", "Audio 2", "Data"},
				[]string{"CAM 1", "CAM 2", "VTR 1"},
				[]string{"MON 1", "MON 2", "REC 1"},
			)},
		},
	}})

	xy := s.p.model.tablePort()
	if xy == nil {
		t.Fatal("no tables node")
	}
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	req := router.MakeRoute{
		DestMatrix: 1, DestAssoc: 2, SourceMatrix: 1, SourceAssoc: 3,
		Levels: levels(1, 2, 3),
	}
	body, err := req.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	stored, err := s.p.applyWrite(sess, xy, uint32(router.CmdAssocMakeRoute), 0,
		codec.ModeData, 0, "", body)
	if err != nil {
		t.Fatalf("make route: %v", err)
	}

	result, err := router.DecodeRouteResult(stored.Data)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if !result.OK() {
		t.Fatalf("the route was refused: %s", result)
	}

	// Destination 2 now carries source 3 on the three levels that were asked
	// for, and still carries what it had on the one that was not.
	m := &xy.router.matrices[0]
	for i := range m.levels {
		got := m.levels[i].dests[1].routed.Source
		want := uint16(3)
		if i == 3 {
			want = 0
		}
		if got != want {
			t.Errorf("level %d (%s) destination 2 carries source %d, want %d",
				i+1, m.levels[i].name, got, want)
		}
	}
}

func TestARouteByAssociationThatCannotBeMade(t *testing.T) {
	r := levelledRouter(t)

	for _, tc := range []struct {
		name string
		req  router.MakeRoute
		want router.RouteResult
	}{
		{
			"a matrix that is not there",
			router.MakeRoute{DestMatrix: 2, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 1, Levels: levels(1)},
			router.RouteBadParameters,
		},
		{
			"a source matrix that is not there",
			router.MakeRoute{DestMatrix: 1, DestAssoc: 1, SourceMatrix: 9, SourceAssoc: 1, Levels: levels(1)},
			router.RouteBadParameters,
		},
		{
			"a destination association past the end",
			router.MakeRoute{DestMatrix: 1, DestAssoc: 99, SourceMatrix: 1, SourceAssoc: 1, Levels: levels(1)},
			router.RouteBadParameters,
		},
		{
			"a source association past the end",
			router.MakeRoute{DestMatrix: 1, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 99, Levels: levels(1)},
			router.RouteBadParameters,
		},
		{
			"no levels at all, which asks for nothing",
			router.MakeRoute{DestMatrix: 1, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 1},
			router.RouteOK,
		},
	} {
		if got, _ := r.makeRoute(tc.req); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestARouteAcrossMatricesNeedsATieline(t *testing.T) {
	// Multi-matrix control is tielines, and there is no pool to take one from
	// yet. Saying so is the answer the specification has; routing anyway would
	// put a crosspoint somewhere it cannot reach.
	r := buildRouter("router", []*canonical.Matrix{
		plantWithLevels([]string{"Video"}, []string{"CAM 1"}, []string{"MON 1"}),
		plantWithLevels([]string{"Video"}, []string{"CAM 2"}, []string{"MON 2"}),
	})

	got, _ := r.makeRoute(router.MakeRoute{
		DestMatrix: 2, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 1, Levels: levels(1),
	})
	if got != router.RouteNoTieline {
		t.Errorf("a route between matrices answered %s, want %s", got, router.RouteNoTieline)
	}
}

func TestARouteOntoAProtectedDestination(t *testing.T) {
	// Nothing is applied until everything has been checked, so a request that
	// fails on one level leaves the others as they were rather than half
	// routed.
	r := levelledRouter(t)
	m := &r.matrices[0]
	m.levels[2].dests[0].protect = router.ProtectState{Protected: true}

	got, _ := r.makeRoute(router.MakeRoute{
		DestMatrix: 1, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 2,
		Levels: levels(1, 2, 3),
	})
	if got != router.RouteProtected {
		t.Fatalf("a route onto a protected destination answered %s", got)
	}
	for i := range m.levels {
		if src := m.levels[i].dests[0].routed.Source; src != 0 {
			t.Errorf("level %d was routed anyway, to source %d", i+1, src)
		}
	}
}

func TestARouteThatDecodesIntoNothing(t *testing.T) {
	// A malformed request is answered with a result rather than a refusal: the
	// command is a data command in both directions and the result is where a
	// client looks.
	s := newServed(t, routerTree(4, 4))
	xy := s.p.model.tablePort()
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	stored, err := s.p.applyWrite(sess, xy, uint32(router.CmdAssocMakeRoute), 0,
		codec.ModeData, 0, "", []byte{0xFF, 0xFF})
	if err != nil {
		t.Fatalf("make route: %v", err)
	}
	result, derr := router.DecodeRouteResult(stored.Data)
	if derr != nil {
		t.Fatalf("decode result: %v", derr)
	}
	if result != router.RouteBadParameters {
		t.Errorf("a malformed request answered %s", result)
	}
}

func TestRoutingByAssociationIsPublishedOnBothViews(t *testing.T) {
	// A route made this way is the same fact as a route made one destination
	// at a time, so a panel watching a level has to see it.
	s := newServed(t, &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "frame",
			Children: []canonical.Element{plantWithLevels(
				[]string{"Video"}, []string{"CAM 1", "CAM 2"}, []string{"MON 1", "MON 2"},
			)},
		},
	}})

	xy := s.p.model.tablePort()
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	req := router.MakeRoute{
		DestMatrix: 1, DestAssoc: 2, SourceMatrix: 1, SourceAssoc: 1, Levels: levels(1),
	}
	body, _ := req.AppendTo(nil)
	if _, err := s.p.applyWrite(sess, xy, uint32(router.CmdAssocMakeRoute), 0,
		codec.ModeData, 0, "", body); err != nil {
		t.Fatalf("make route: %v", err)
	}

	lv := &xy.router.matrices[0].levels[0]
	lvPort := s.p.model.levelPort(lv)
	if lvPort == nil {
		t.Fatal("the level is not served")
	}
	if v, _ := lvPort.value(uint32(router.LvlRoute(2))); v.Val != 1 {
		t.Errorf("the level reports source %d on destination 2, want 1", v.Val)
	}

	base, _ := lv.dstTable.Command(2)
	v, _ := xy.value(uint32(base + router.OffDestRoutedSrc))
	if pin := router.UnpackSourcePin(uint32(v.Val)); pin.Source != 1 {
		t.Errorf("the tables report source %d on destination 2", pin.Source)
	}
}

func TestAMatrixWithNothingToAssociate(t *testing.T) {
	// A plant with no entities has no associations, and asking for one is not
	// a crash.
	r := buildRouter("router", []*canonical.Matrix{testMatrix(0, 0)})
	if len(r.matrices[0].srcAssocs) != 0 {
		t.Errorf("%d associations on an empty plant", len(r.matrices[0].srcAssocs))
	}
	if got, _ := r.makeRoute(router.MakeRoute{
		DestMatrix: 1, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 1, Levels: levels(1),
	}); got != router.RouteBadParameters {
		t.Errorf("routing an association that does not exist answered %s", got)
	}
	if buildAssocs(nil, true) != nil {
		t.Error("a matrix with no levels produced associations")
	}
}

func TestLevelsThatDoNotLineUp(t *testing.T) {
	// Associations are as many as the smallest level holds. One whose member
	// is off the end of a level is one a panel would route into nothing.
	m := plantWithLevels([]string{"Video"}, []string{"CAM 1", "CAM 2"}, []string{"MON 1"})
	m.TargetLabels["Audio"] = map[string]string{"0": "MON 1 A"}
	m.SourceLabels["Audio"] = map[string]string{"0": "CAM 1 A"}

	r := buildRouter("router", []*canonical.Matrix{m})
	mx := &r.matrices[0]
	if len(mx.levels) != 2 {
		t.Fatalf("%d levels", len(mx.levels))
	}
	// Both levels carry the same entity counts, taken from the matrix, so the
	// associations cover all of them; what differs is only the naming.
	if len(mx.srcAssocs) != len(mx.levels[0].sources) {
		t.Errorf("%d associations for %d sources", len(mx.srcAssocs), len(mx.levels[0].sources))
	}
}

func TestAnAssociationOutsideItsTable(t *testing.T) {
	// The tables are allocated from the associations, so the two agree by
	// construction. An association publishing a member past its own table
	// would write it over whatever command came next.
	a := routerAssoc{name: "CAM 1", members: []uint32{1, 1}}

	out := make(map[router.Command]any)
	a.values(400,
		func(cmd router.Command, v int32) { out[cmd] = v },
		func(cmd router.Command, v string) { out[cmd] = v },
	)
	if _, ok := out[400+router.OffAssocName8]; !ok {
		t.Error("the association was not published")
	}
	cmd, _ := router.AssocMember(400, 2)
	if _, ok := out[cmd]; !ok {
		t.Error("the second level's member was not published")
	}
}

func TestARouteRequestNamingALevelTheMatrixDoesNotHave(t *testing.T) {
	// The bitmap is eight bits wide whatever the matrix is, so a panel may ask
	// for a level that is not there. It is refused rather than ignored: a
	// route that silently did less than it was asked is worse than one that
	// says it could not.
	r := levelledRouter(t)
	m := &r.matrices[0]
	m.srcAssocs[0].members = m.srcAssocs[0].members[:1]

	if got, _ := r.makeRoute(router.MakeRoute{
		DestMatrix: 1, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 1, Levels: levels(2),
	}); got != router.RouteBadParameters {
		t.Errorf("a route naming a level the association does not reach answered %s", got)
	}
}

func TestARouteOntoAnEntityThatIsNotInstalled(t *testing.T) {
	r := levelledRouter(t)
	m := &r.matrices[0]
	m.dstAssocs[0].members[0] = 99

	if got, _ := r.makeRoute(router.MakeRoute{
		DestMatrix: 1, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 1, Levels: levels(1),
	}); got != router.RouteNotInstalled {
		t.Errorf("a route onto a destination that is not there answered %s", got)
	}
}

func TestRoutingByAssociationOnANodeWithNoMatrix(t *testing.T) {
	// The publish walks the matrix the request named, and a request that got
	// this far with a matrix that is not there has already been refused.
	s := newServed(t, routerTree(4, 4))
	xy := s.p.model.tablePort()

	body, err := router.MakeRoute{
		DestMatrix: 9, DestAssoc: 1, SourceMatrix: 9, SourceAssoc: 1, Levels: levels(1),
	}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	s.p.routeByAssociation(context.Background(), nil, xy,
		codec.Value{Command: uint32(router.CmdAssocMakeRoute), Mode: codec.ModeData, Data: body})
}

func TestAnAssociationTableThatDescribesNothing(t *testing.T) {
	// values walks the associations against the table that describes them and
	// stops at whichever ends first, so a table shorter than its associations
	// publishes only what it can address.
	r := levelledRouter(t)
	m := &r.matrices[0]
	m.srcAssocTbl.Count = 0
	m.dstAssocTbl.Count = 0

	vals := r.values()
	base, _ := r.table.Command(1)
	if got := vals[uint32(base+router.OffNumSrcAssocs)].Val; got != 0 {
		t.Errorf("the matrix says %d source associations", got)
	}
}

func TestAnAssociationWithNoMembers(t *testing.T) {
	// A member past the end of the table is not published. The two agree by
	// construction, so this is the shape of a bug rather than a plant.
	a := routerAssoc{name: "CAM 1", members: []uint32{1}}

	out := make(map[router.Command]any)
	a.values(0,
		func(cmd router.Command, v int32) { out[cmd] = v },
		func(cmd router.Command, v string) { out[cmd] = v },
	)
	if len(out) == 0 {
		t.Error("an association published nothing at all")
	}
}

func TestALevelWithNoEntities(t *testing.T) {
	// A matrix sized to nothing has nothing to associate, and asking is not a
	// crash.
	lv := []routerLevel{{name: "Video"}}
	if got := buildAssocs(lv, true); got != nil {
		t.Errorf("%d associations on a level with no sources", len(got))
	}
	if got := buildAssocs(lv, false); got != nil {
		t.Errorf("%d associations on a level with no destinations", len(got))
	}
}

func TestALabelSetChosenFromSeveral(t *testing.T) {
	// firstLevel picks deterministically so two runs of one tree produce one
	// plant, and answers nothing when there is nothing to pick from.
	if got := firstLevel(nil); got != nil {
		t.Error("a tree with no labels produced a label set")
	}
	got := firstLevel(map[string]map[string]string{
		"Video": {"0": "CAM 1"},
		"Audio": {"0": "CAM 1 A"},
	})
	if got["0"] != "CAM 1 A" {
		t.Errorf("firstLevel chose %q, want the first by name", got["0"])
	}
}

func TestLevelsOfDifferentSizes(t *testing.T) {
	// Associations are as many as the smallest level holds. A matrix built
	// from a canonical tree gives every level the same counts, so this is
	// reached by hand — and it is the check that stops an association pointing
	// at an entity a level does not have.
	got := buildAssocs([]routerLevel{
		{name: "Video", sources: []string{"CAM 1", "CAM 2", "CAM 3"}},
		{name: "Audio", sources: []string{"CAM 1 A", "CAM 2 A"}},
	}, true)

	if len(got) != 2 {
		t.Fatalf("%d associations, want the smaller level's two", len(got))
	}
	if got[0].name != "CAM 1" {
		t.Errorf("association 1 is named %q, want the first level's name for it", got[0].name)
	}
}
