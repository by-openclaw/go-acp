package rollcall

import (
	"context"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
)

// A router's state is published twice — once as a level's per-destination
// commands, once as a field in the tables — and the two have to agree. Left
// alone they drift the moment anybody routes anything, and nothing on the wire
// would say which was lying.

// routerNodes returns the level node and the tables node of a served router.
func routerNodes(t *testing.T, s *served) (*port, *port) {
	t.Helper()
	lv := s.p.model.port(firstCardPort + 1)
	xy := s.p.model.port(firstCardPort + 2)
	if lv == nil || lv.level == nil || xy == nil || xy.router == nil {
		t.Fatal("the router was not served as a level and a tables node")
	}
	return lv, xy
}

func TestRoutingOnALevelShowsInTheTables(t *testing.T) {
	s := newServed(t, routerTree(8, 8))
	lv, xy := routerNodes(t, s)
	sess := s.open(firstCardPort+1, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	// Route source 4 to destination 2, the way a panel does it.
	_, err := s.p.applyWrite(sess, lv, uint32(router.LvlRoute(2)), 0, codec.ModeValue, 4, "")
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	// The level says so.
	if v, _ := lv.value(uint32(router.LvlRoute(2))); v.Val != 4 {
		t.Errorf("the level reports source %d on destination 2", v.Val)
	}

	// And so do the tables, as a packed pin naming this matrix and level: a
	// route made on a level is a local one, and a local route is one whose
	// source names the same matrix and level as its destination.
	base, ok := lv.level.dstTable.Command(2)
	if !ok {
		t.Fatal("destination 2 has no table entry")
	}
	v, ok := xy.value(uint32(base + router.OffDestRoutedSrc))
	if !ok {
		t.Fatal("the tables carry no routed source for destination 2")
	}
	pin := router.UnpackSourcePin(uint32(v.Val))
	if pin.Source != 4 {
		t.Errorf("the tables report source %d", pin.Source)
	}
	if pin.Matrix != lv.level.matrixNumber || pin.Level != lv.level.levelNumber {
		t.Errorf("the pin names matrix %d level %d, want the level's own %d and %d",
			pin.Matrix, pin.Level, lv.level.matrixNumber, lv.level.levelNumber)
	}
	if pin.IsUnrouted() {
		t.Error("a destination that was just routed reads as unrouted")
	}
}

func TestRoutingThroughTheTablesShowsOnTheLevel(t *testing.T) {
	// The other direction: a client that reads a plant structurally and routes
	// through the tables has to move the panel's view too.
	s := newServed(t, routerTree(8, 8))
	lv, xy := routerNodes(t, s)
	sess := s.open(firstCardPort+2, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	base, _ := lv.level.dstTable.Command(3)
	pin := router.PackSourcePin(router.SourcePin{
		Matrix: lv.level.matrixNumber, Level: lv.level.levelNumber, Source: 7,
	})
	_, err := s.p.applyWrite(sess, xy, uint32(base+router.OffDestRoutedSrc), 0,
		codec.ModeValue, int32(pin), "")
	if err != nil {
		t.Fatalf("route through the tables: %v", err)
	}

	if v, _ := lv.value(uint32(router.LvlRoute(3))); v.Val != 7 {
		t.Errorf("the level reports source %d on destination 3, want 7", v.Val)
	}
}

func TestProtectingOnALevelShowsInTheTables(t *testing.T) {
	s := newServed(t, routerTree(8, 8))
	lv, xy := routerNodes(t, s)
	sess := s.open(firstCardPort+1, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	// A checkbox on this interface counts from one: on is two.
	_, err := s.p.applyWrite(sess, lv, uint32(router.LvlProtect(5)), 0,
		codec.ModeValue, router.ProtectOn, "")
	if err != nil {
		t.Fatalf("protect: %v", err)
	}

	base, _ := lv.level.dstTable.Command(5)
	v, ok := xy.value(uint32(base + router.OffDestProtect))
	if !ok {
		t.Fatal("the tables carry no protect for destination 5")
	}
	if !router.UnpackProtectState(uint32(v.Val)).Protected {
		t.Error("the tables report destination 5 unprotected after it was protected")
	}

	// And back off again, because a protect that cannot be cleared is a fault
	// an operator meets at the worst moment.
	if _, err := s.p.applyWrite(sess, lv, uint32(router.LvlProtect(5)), 0,
		codec.ModeValue, router.ProtectOff, ""); err != nil {
		t.Fatalf("unprotect: %v", err)
	}
	v, _ = xy.value(uint32(base + router.OffDestProtect))
	if router.UnpackProtectState(uint32(v.Val)).Protected {
		t.Error("destination 5 stayed protected")
	}
}

func TestProtectingThroughTheTablesShowsOnTheLevel(t *testing.T) {
	s := newServed(t, routerTree(8, 8))
	lv, xy := routerNodes(t, s)
	sess := s.open(firstCardPort+2, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	base, _ := lv.level.dstTable.Command(1)
	packed := router.PackProtectState(router.ProtectState{Protected: true})
	if _, err := s.p.applyWrite(sess, xy, uint32(base+router.OffDestProtect), 0,
		codec.ModeValue, int32(packed), ""); err != nil {
		t.Fatalf("protect through the tables: %v", err)
	}

	if v, _ := lv.value(uint32(router.LvlProtect(1))); v.Val != router.ProtectOn {
		t.Errorf("the level reports protect %d on destination 1, want %d",
			v.Val, router.ProtectOn)
	}
}

func TestWritesThatAreNotRoutingChangeNothingElsewhere(t *testing.T) {
	// The selection controls are a panel's own state, not the plant's. Writing
	// one must not touch the tables: two panels pointed at one level each keep
	// their own selection and share only what is routed.
	s := newServed(t, routerTree(8, 8))
	lv, xy := routerNodes(t, s)
	sess := s.open(firstCardPort+1, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	before := len(xy.values)
	for _, cmd := range []router.Command{
		router.LvlSrcSelect, router.LvlDestSelect, router.LvlTakeMode,
	} {
		if _, err := s.p.applyWrite(sess, lv, uint32(cmd), 0, codec.ModeValue, 1, ""); err != nil {
			t.Fatalf("write %d: %v", cmd, err)
		}
	}
	if len(xy.values) != before {
		t.Error("a selection write reached the tables")
	}
}

func TestSyncIgnoresANodeThatIsNotARouter(t *testing.T) {
	// A card write goes nowhere near this, and asking a card's port to mirror
	// a crosspoint would be a panic waiting for the first frame served with
	// both a card and a matrix in it.
	s := newServed(t, routerTree(8, 8))
	card := s.p.model.port(firstCardPort)
	if card == nil {
		t.Fatal("no first node")
	}
	s.p.syncRouterWrite(context.Background(), nil, card,
		codec.Value{Command: 10001, Mode: codec.ModeValue, Val: 1})
}

func TestSyncIgnoresCommandsOutsideTheRoutingBlocks(t *testing.T) {
	s := newServed(t, routerTree(8, 8))
	lv, xy := routerNodes(t, s)

	// A destination past the end of the level, and a table command that falls
	// in no destination's entry: both are refused rather than written to
	// whatever memory happens to be next.
	s.p.syncRouterWrite(context.Background(), nil, lv,
		codec.Value{Command: uint32(router.LvlRoute(999)), Mode: codec.ModeValue, Val: 1})
	s.p.syncRouterWrite(context.Background(), nil, xy,
		codec.Value{Command: uint32(router.CmdRouterName), Mode: codec.ModeValue, Val: 1})

	// A table command inside a destination's entry but not one of the two
	// fields that carry state.
	base, _ := lv.level.dstTable.Command(1)
	s.p.syncRouterWrite(context.Background(), nil, xy,
		codec.Value{Command: uint32(base + router.OffDestName8), Mode: codec.ModeString, Text: "x"})
}

func TestPublishingToANodeThatIsNotServed(t *testing.T) {
	// levelPort and tablePort answer nil for a router nobody serves, and the
	// publish has to survive it: a half-built model is what a test harness
	// hands us, and a nil dereference here would be a crash in the field.
	s := newServed(t, routerTree(8, 8))
	s.p.publishRouterValue(context.Background(), nil, nil, router.LvlRoute(1), 1)

	orphan := buildRouter("orphan", []*canonical.Matrix{testMatrix(2, 2)})
	if got := s.p.model.levelPort(&orphan.matrices[0].levels[0]); got != nil {
		t.Error("a level nobody serves was matched to a port")
	}
}

func TestTheTablesRefuseWhatIsNotState(t *testing.T) {
	// A name and a base say what the plant is, not what it is doing. A client
	// able to overwrite a table's own base could make the interface
	// undescribable, so only the routed source and the protect may be written.
	s := newServed(t, routerTree(4, 4))
	lv, xy := routerNodes(t, s)
	base, _ := lv.level.dstTable.Command(1)

	for _, tc := range []struct {
		name string
		cmd  router.Command
		mode codec.Mode
	}{
		{"a destination's name", base + router.OffDestName8, codec.ModeString},
		{"a table's own base", router.CmdMatrixBase, codec.ModeValue},
		{"a command in no table at all", 999999, codec.ModeValue},
	} {
		if _, err := xy.setValue(uint32(tc.cmd), tc.mode, 1, "x"); err == nil {
			t.Errorf("%s was accepted as a write", tc.name)
		}
	}

	// And a packed word is a number: writing a string to one is a client
	// confusing a name with a state.
	if _, err := xy.setValue(uint32(base+router.OffDestRoutedSrc), codec.ModeString, 0, "SRC 1"); err == nil {
		t.Error("a string write to the routed source was accepted")
	}
}

func TestAServedFrameWithNoRouterHasNoTablesNode(t *testing.T) {
	// tablePort answers nil when nothing serves tables, which is every frame
	// that is only cards. A mirror that assumed one would be a crash on the
	// first ordinary frame.
	s := newServed(t, testTree())
	if got := s.p.model.tablePort(); got != nil {
		t.Errorf("a frame of cards reported a tables node at port %d", got.number)
	}
}

func TestATableSmallerThanTheLevelItDescribes(t *testing.T) {
	// The tables are allocated from the level's own sizes, so the two agree by
	// construction. They would stop agreeing if a plant were ever resized
	// under a running provider, and a mirror that trusted the arithmetic would
	// then write a crosspoint into whatever command happened to be next.
	// Nothing is written instead, in both directions.
	s := newServed(t, routerTree(8, 8))
	lv, xy := routerNodes(t, s)
	base, _ := lv.level.dstTable.Command(4)

	lv.level.dstTable.Count = 0

	before := len(xy.values)
	s.p.syncRouterWrite(context.Background(), nil, lv,
		codec.Value{Command: uint32(router.LvlRoute(4)), Mode: codec.ModeValue, Val: 2})
	if len(xy.values) != before {
		t.Error("a crosspoint was written into the tables through a table that does not describe it")
	}

	s.p.syncRouterWrite(context.Background(), nil, xy,
		codec.Value{Command: uint32(base + router.OffDestRoutedSrc), Mode: codec.ModeValue, Val: 2})
	if v, _ := lv.value(uint32(router.LvlRoute(4))); v.Val == 2 {
		t.Error("a table write reached a destination the tables no longer describe")
	}
}
