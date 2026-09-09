package rollcall

import (
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec/router"
)

// A tieline is a cable between two matrices. A source on matrix 1 cannot reach
// a destination on matrix 2 by itself — they are different hardware with no
// crosspoint in common — so the controller routes the source onto a cable and
// the cable to the destination, and holds the pair until that destination is
// fed by something nearer.

// twoMatrices is a plant big enough to spare cables between its matrices.
func twoMatrices(t *testing.T) *routerModel {
	t.Helper()
	return buildRouter("router", []*canonical.Matrix{
		plantWithLevels([]string{"Video"},
			[]string{"CAM 1", "CAM 2", "CAM 3", "CAM 4", "CAM 5", "CAM 6"},
			[]string{"MON 1", "MON 2", "MON 3", "MON 4", "MON 5", "MON 6"}),
		plantWithLevels([]string{"Video"},
			[]string{"VTR 1", "VTR 2", "VTR 3", "VTR 4", "VTR 5", "VTR 6"},
			[]string{"REC 1", "REC 2", "REC 3", "REC 4", "REC 5", "REC 6"}),
	})
}

func TestAPlantOfOneMatrixHasNoTielines(t *testing.T) {
	// There is nowhere for a cable to go.
	r := buildRouter("router", []*canonical.Matrix{testMatrix(4, 4)})
	if len(r.tielines) != 0 {
		t.Errorf("%d tielines on a plant of one matrix", len(r.tielines))
	}
}

func TestCablesAreWiredBothWays(t *testing.T) {
	// A cable carries one way. Matrix 1 reaching matrix 2 is a different cable
	// from matrix 2 reaching matrix 1, and a plant with only the first cannot
	// answer the second.
	r := twoMatrices(t)

	var oneToTwo, twoToOne int
	for i := range r.tielines {
		t := &r.tielines[i]
		switch {
		case t.upMatrix == 1 && t.downMatrix == 2:
			oneToTwo++
		case t.upMatrix == 2 && t.downMatrix == 1:
			twoToOne++
		}
	}
	if oneToTwo != tielinesBetween || twoToOne != tielinesBetween {
		t.Errorf("%d cables one way and %d the other, want %d each",
			oneToTwo, twoToOne, tielinesBetween)
	}

	// They take the high numbers, because the low ones are the real plant.
	for i := range r.tielines {
		c := &r.tielines[i]
		if c.upDest <= 4 || c.downSource <= 4 {
			t.Errorf("cable %q took destination %d and source %d, which are plant",
				c.name, c.upDest, c.downSource)
		}
	}
}

func TestAMatrixTooSmallToSpareACable(t *testing.T) {
	// A two-by-two matrix with a tieline taken out of it is a one-by-two
	// matrix, which is not what anybody installed.
	r := buildRouter("router", []*canonical.Matrix{
		plantWithLevels([]string{"Video"}, []string{"CAM 1"}, []string{"MON 1"}),
		plantWithLevels([]string{"Video"}, []string{"VTR 1"}, []string{"REC 1"}),
	})
	if len(r.tielines) != 0 {
		t.Errorf("%d cables out of a plant with none to spare", len(r.tielines))
	}
}

func TestARouteAcrossMatricesTakesACable(t *testing.T) {
	r := twoMatrices(t)

	up, ok := r.levelOf(1, 1)
	if !ok {
		t.Fatal("matrix 1 has no level 1")
	}
	down, ok := r.levelOf(2, 1)
	if !ok {
		t.Fatal("matrix 2 has no level 1")
	}

	result, moved := r.routeAcross(up, down, 2, 1)
	if !result.OK() {
		t.Fatalf("the route was refused: %s", result)
	}
	if len(moved) != 2 {
		t.Fatalf("%d crosspoints moved, want the cable's end and the destination", len(moved))
	}

	// Two crosspoints: the real source onto the cable, and the cable to the
	// destination that asked for it.
	var cable *routerTieline
	for i := range r.tielines {
		if r.tielines[i].heldFor == 1 {
			cable = &r.tielines[i]
		}
	}
	if cable == nil {
		t.Fatal("no cable was taken")
	}
	if got := up.dests[cable.upDest-1].routed.Source; got != 2 {
		t.Errorf("the cable carries source %d, want the one that was asked for", got)
	}
	if got := down.dests[0].routed.Source; got != uint16(cable.downSource) {
		t.Errorf("destination 1 carries source %d, want the cable's %d",
			got, cable.downSource)
	}
}

func TestRoutingTheSameDestinationTwiceReusesItsCable(t *testing.T) {
	// Routing a destination again is a change of source, not a second signal.
	// Taking another cable each time is how a plant runs out of them while
	// looking idle.
	r := twoMatrices(t)
	up, _ := r.levelOf(1, 1)
	down, _ := r.levelOf(2, 1)

	if result, _ := r.routeAcross(up, down, 2, 1); !result.OK() {
		t.Fatalf("first route: %s", result)
	}
	held := heldCables(r)

	if result, _ := r.routeAcross(up, down, 3, 1); !result.OK() {
		t.Fatalf("second route: %s", result)
	}
	if got := heldCables(r); got != held {
		t.Errorf("%d cables held after routing one destination twice, want %d", got, held)
	}
}

func heldCables(r *routerModel) int {
	n := 0
	for i := range r.tielines {
		if !r.tielines[i].free() {
			n++
		}
	}
	return n
}

func TestRunningOutOfCables(t *testing.T) {
	// The answer the specification has for it, and the one this provider used
	// to give unconditionally because it had no pool to take one from.
	r := twoMatrices(t)
	up, _ := r.levelOf(1, 1)
	down, _ := r.levelOf(2, 1)

	for d := 1; d <= tielinesBetween; d++ {
		if result, _ := r.routeAcross(up, down, 1, d); !result.OK() {
			t.Fatalf("route to destination %d: %s", d, result)
		}
	}
	if result, _ := r.routeAcross(up, down, 1, tielinesBetween+1); result != router.RouteNoTieline {
		t.Errorf("a route with no cable left answered %s, want %s",
			result, router.RouteNoTieline)
	}
}

func TestACableComesBackWhenItsDestinationIsFedLocally(t *testing.T) {
	// A tieline is held by the destination it feeds, and that destination
	// stops needing it the moment something nearer feeds it.
	r := twoMatrices(t)
	up, _ := r.levelOf(1, 1)
	down, _ := r.levelOf(2, 1)

	if result, _ := r.routeAcross(up, down, 1, 1); !result.OK() {
		t.Fatal("the first route was refused")
	}
	if heldCables(r) != 1 {
		t.Fatalf("%d cables held after one route", heldCables(r))
	}

	r.releaseTielines(down, 1)
	if heldCables(r) != 0 {
		t.Errorf("%d cables still held after the destination was fed locally", heldCables(r))
	}
}

func TestRoutingAcrossOntoSomethingThatCannotTakeIt(t *testing.T) {
	r := twoMatrices(t)
	up, _ := r.levelOf(1, 1)
	down, _ := r.levelOf(2, 1)
	down.dests[0].protect = router.ProtectState{Protected: true}

	for _, tc := range []struct {
		name   string
		source uint16
		dest   int
		want   router.RouteResult
	}{
		{"a source that is not there", 99, 2, router.RouteNotInstalled},
		{"a destination that is not there", 1, 99, router.RouteNotInstalled},
		{"a protected destination", 1, 1, router.RouteProtected},
	} {
		if got, _ := r.routeAcross(up, down, tc.source, tc.dest); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
	// And none of them took a cable.
	if got := heldCables(r); got != 0 {
		t.Errorf("%d cables held after routes that were all refused", got)
	}
}

func TestALevelThatIsNotThere(t *testing.T) {
	r := twoMatrices(t)
	for _, tc := range []struct{ matrix, level uint32 }{
		{9, 1}, {1, 9}, {1, 0},
	} {
		if _, ok := r.levelOf(tc.matrix, tc.level); ok {
			t.Errorf("matrix %d level %d was found", tc.matrix, tc.level)
		}
	}
}

func TestAnAssociationRouteAcrossMatrices(t *testing.T) {
	// The whole point: a camera on one matrix taken to a monitor on another,
	// asked for exactly as a local route is.
	r := twoMatrices(t)

	result, moved := r.makeRoute(router.MakeRoute{
		DestMatrix: 2, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 3,
		Levels: levels(1),
	})
	if !result.OK() {
		t.Fatalf("the route was refused: %s", result)
	}
	if len(moved) != 2 {
		t.Errorf("%d crosspoints moved, want the cable and the destination", len(moved))
	}

	down, _ := r.levelOf(2, 1)
	if down.dests[0].routed.Source == 0 {
		t.Error("the destination was left with nothing routed to it")
	}
}

func TestAnAssociationRouteAcrossThatRunsOut(t *testing.T) {
	// Nothing is applied until every level has a cable, because a camera that
	// arrived with its picture and no sound is worse than one that did not
	// arrive. Here there is one level, so the whole route fails and every
	// cable it had taken comes back.
	r := twoMatrices(t)
	up, _ := r.levelOf(1, 1)
	down, _ := r.levelOf(2, 1)

	for d := 1; d <= tielinesBetween; d++ {
		if result, _ := r.routeAcross(up, down, 1, d); !result.OK() {
			t.Fatalf("setup route %d: %s", d, result)
		}
	}

	result, moved := r.makeRoute(router.MakeRoute{
		DestMatrix: 2, DestAssoc: 5, SourceMatrix: 1, SourceAssoc: 1,
		Levels: levels(1),
	})
	if result != router.RouteNoTieline {
		t.Errorf("a route with no cable left answered %s", result)
	}
	if moved != nil {
		t.Errorf("%d crosspoints moved by a route that was refused", len(moved))
	}
}

func TestAnAssociationRouteAcrossNamingALevelThatIsNotThere(t *testing.T) {
	r := twoMatrices(t)
	m := &r.matrices[0]
	m.srcAssocs[0].members = m.srcAssocs[0].members[:0]

	if got, _ := r.makeRoute(router.MakeRoute{
		DestMatrix: 2, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 1,
		Levels: levels(1),
	}); got != router.RouteBadParameters {
		t.Errorf("a route naming a level the association does not reach answered %s", got)
	}
}

// twoMatricesTwoLevels is a plant whose matrices each carry a picture and its
// audio, which is what makes a partly-crossed route possible.
func twoMatricesTwoLevels(t *testing.T) *routerModel {
	t.Helper()
	names := []string{"A 1", "A 2", "A 3", "A 4", "A 5", "A 6"}
	other := []string{"B 1", "B 2", "B 3", "B 4", "B 5", "B 6"}
	return buildRouter("router", []*canonical.Matrix{
		plantWithLevels([]string{"Video", "Audio"}, names, names),
		plantWithLevels([]string{"Video", "Audio"}, other, other),
	})
}

func TestACrossedRouteThatRunsOutPartWayGivesEverythingBack(t *testing.T) {
	// A camera that arrived with its picture and no sound is worse than one
	// that did not arrive, so nothing is kept unless every level was found a
	// cable. The cables taken for the levels that did work come back.
	r := twoMatricesTwoLevels(t)

	// Take away the cables for the second level, so a route across both finds
	// one for the picture and none for the sound.
	var kept []routerTieline
	for _, c := range r.tielines {
		if c.upLevel == 1 {
			kept = append(kept, c)
		}
	}
	r.tielines = kept

	result, moved := r.makeRoute(router.MakeRoute{
		DestMatrix: 2, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 1,
		Levels: levels(1, 2),
	})
	if result != router.RouteNoTieline {
		t.Fatalf("a route that could not cross every level answered %s", result)
	}
	if moved != nil {
		t.Errorf("%d crosspoints reported as moved by a refused route", len(moved))
	}
	if got := heldCables(r); got != 0 {
		t.Errorf("%d cables still held after the route was given up", got)
	}
}

func TestACrossedRouteNamingALevelTheSourceMatrixDoesNotHave(t *testing.T) {
	// The two matrices need not be the same shape, and a level the source side
	// does not have is a level the route cannot cross.
	r := twoMatricesTwoLevels(t)
	r.matrices[0].levels = r.matrices[0].levels[:1]

	if got, _ := r.makeRoute(router.MakeRoute{
		DestMatrix: 2, DestAssoc: 1, SourceMatrix: 1, SourceAssoc: 1,
		Levels: levels(2),
	}); got != router.RouteBadParameters {
		t.Errorf("a route across a level the source matrix lacks answered %s", got)
	}
}
