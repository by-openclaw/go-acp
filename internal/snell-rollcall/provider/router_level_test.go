package rollcall

import (
	"fmt"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
)

// A level's menu is the routing interface, and its shape was walked off a
// vendor Centra rather than designed. What these tests pin is that shape: the
// selector convention a listbox depends on, the container spans a walk
// depends on, and the command block a panel subscribes to for tally.

// levelPort returns the level node of a served router tree.
func levelPort(t *testing.T, targets, sources int64) *port {
	t.Helper()
	s := newServed(t, routerTree(targets, sources))
	prt := s.p.model.port(firstCardPort + 1)
	if prt == nil || prt.level == nil {
		t.Fatal("no level node was served")
	}
	return prt
}

func TestALevelPublishesItsSourcesAsASelector(t *testing.T) {
	// A panel draws a listbox from a container whose parameter is "#SEL:" and
	// whose children are buttons that all carry one command, each holding the
	// value it sends in its minimum-range field. Nothing in the specification
	// says so; it is how the Centra publishes both of its lists, and a listbox
	// bound to a command with no such lines under it draws empty.
	prt := levelPort(t, 8, 6)
	lines := prt.menu(true)

	var sources, dests *line
	for i := range lines {
		switch lines[i].Text {
		case "Source List":
			sources = &lines[i]
		case "Dest List":
			dests = &lines[i]
		}
	}
	if sources == nil || dests == nil {
		t.Fatal("the level published no source or destination list")
	}
	for _, l := range []*line{sources, dests} {
		if l.Param != "#SEL:" {
			t.Errorf("%q carries %q, want the selector marker", l.Text, l.Param)
		}
		if l.Style.Kind() != codec.StyleList {
			t.Errorf("%q is %v, want a list", l.Text, l.Style.Kind())
		}
	}
	if sources.Step != 6 {
		t.Errorf("the source list spans %d lines, want one per source", sources.Step)
	}
	if dests.Step != 8 {
		t.Errorf("the dest list spans %d lines, want one per destination", dests.Step)
	}

	// Each entry carries its own number, and they share the command.
	for n := uint32(1); n <= sources.Step; n++ {
		l := lines[sources.Index+n]
		if l.Command != uint32(router.LvlSrcSelect) {
			t.Errorf("source %d is on command %d, want %d", n, l.Command, router.LvlSrcSelect)
		}
		if l.MinRange != int32(n) {
			t.Errorf("source %d sends %d", n, l.MinRange)
		}
		if l.Style.Kind() != codec.StyleButton {
			t.Errorf("source %d is %v, want a button", n, l.Style.Kind())
		}
	}
}

func TestALevelCarriesOneCommandPerDestination(t *testing.T) {
	// This is what a panel subscribes to for tally: the whole crosspoint state
	// of the level, one command each, rather than a selection it has to drive
	// to read anything.
	prt := levelPort(t, 8, 6)

	for d := 1; d <= 8; d++ {
		v, ok := prt.value(uint32(router.LvlRoute(d)))
		if !ok {
			t.Fatalf("destination %d has no routed source", d)
		}
		// A destination nothing has been routed to still holds a number in
		// range: there is no value in 1..n that means "no source".
		if v.Val < 1 || v.Val > 6 {
			t.Errorf("destination %d holds %d, outside 1..6", d, v.Val)
		}
		p, ok := prt.value(uint32(router.LvlProtect(d)))
		if !ok {
			t.Fatalf("destination %d has no protect", d)
		}
		if p.Val != router.ProtectOff {
			t.Errorf("destination %d starts protected", d)
		}
	}

	// And the counts say how many there are without walking the lists.
	if v, _ := prt.value(uint32(router.LvlSourceCount)); v.Val != 6 {
		t.Errorf("source count = %d", v.Val)
	}
	if v, _ := prt.value(uint32(router.LvlDestCount)); v.Val != 8 {
		t.Errorf("dest count = %d", v.Val)
	}
}

func TestALevelAnswersEveryCommandItsMenuNames(t *testing.T) {
	// A panel asks for all of them before it draws anything, and one refusal
	// is enough for it to treat the node as broken rather than as empty.
	prt := levelPort(t, 8, 6)

	for _, l := range prt.menu(true) {
		if l.Command == 0 {
			continue
		}
		if _, ok := prt.value(l.Command); !ok {
			t.Errorf("command %d (%q) has no value", l.Command, l.Text)
		}
	}

	// Including the twenty monitor readouts, which say what they are rather
	// than staying blank — which is what the Centra answers on every one.
	for m := 1; m <= router.LvlMonitors; m++ {
		for _, base := range []router.Command{
			router.LvlMonKind, router.LvlMonIndex, router.LvlMonName,
			router.LvlMonSrcAddr, router.LvlMonDstAddr,
		} {
			v, ok := prt.value(uint32(router.LvlMonitor(base, m)))
			if !ok || v.Text != "Unknown" {
				t.Errorf("monitor %d readout %d = %q", m, base, v.Text)
			}
		}
	}
}

func TestALevelWithNothingOnIt(t *testing.T) {
	// A matrix configured with no sources and no destinations is still a
	// level, and the name commands still answer: a panel that asks for a name
	// before there is anything to name gets an empty string, not a refusal.
	prt := levelPort(t, 0, 0)

	v, ok := prt.value(uint32(router.LvlSrcName))
	if !ok || v.Text != "" {
		t.Errorf("source name = %q, %v", v.Text, ok)
	}
	d, ok := prt.value(uint32(router.LvlDstName))
	if !ok || d.Text != "" {
		t.Errorf("dest name = %q, %v", d.Text, ok)
	}
	if c, _ := prt.value(uint32(router.LvlSourceCount)); c.Val != 0 {
		t.Errorf("source count = %d on an empty level", c.Val)
	}
}

func TestARoutedDestinationKeepsTheSourceItHas(t *testing.T) {
	// The seed is not always one: a level built from a matrix that already has
	// crosspoints publishes them, because the value is the tally.
	r := buildRouter("router", []*canonical.Matrix{testMatrix(4, 4)})
	lv := &r.matrices[0].levels[0]
	lv.dests[2].routed = router.SourcePin{Source: 3}

	p := newRouterLevelPort(9, lv.name, lv)
	if v, _ := p.value(uint32(router.LvlRoute(3))); v.Val != 3 {
		t.Errorf("a routed destination reads back %d, want the source it has", v.Val)
	}
}

func TestTheContainerSpansCoverTheWholeSubtree(t *testing.T) {
	// A container's step is the size of everything under it, not the count of
	// its immediate children. A walk that trusts it and finds otherwise nests
	// the page wrongly, which is a bug nobody sees until a panel draws it.
	prt := levelPort(t, 8, 6)
	lines := prt.menu(true)

	for i, l := range lines {
		if !l.Style.Container() || l.Step == 0 {
			continue
		}
		if end := uint32(i) + l.Step; end >= uint32(len(lines)) {
			t.Errorf("%q spans past the end of the menu", l.Text)
		}
	}
	// The root spans everything but itself.
	if lines[0].Step != uint32(len(lines))-1 {
		t.Errorf("the root spans %d of %d lines", lines[0].Step, len(lines))
	}
}

func TestAPlantTooBigForOnePortList(t *testing.T) {
	// Port numbers run out before a large plant does: past 0xE0 they are the
	// ones a gateway hands its own clients, and a matrix there would be
	// addressed as one. What is served is truncated rather than wrapped.
	var children []canonical.Element
	for i := range 300 {
		children = append(children, testMatrix(2, 2))
		_ = i
	}
	m := buildModel(&canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "frame", Path: "frame", Children: children,
		},
	}}, "big")

	for n := range m.ports {
		if n >= firstClientPort {
			t.Fatalf("a node was served at port %d, which is a client's", n)
		}
	}
}

func TestManyCardsAlsoStopAtTheClientPorts(t *testing.T) {
	var children []canonical.Element
	for i := range 300 {
		children = append(children, &canonical.Node{Header: canonical.Header{
			Number: i + 1, Identifier: fmt.Sprintf("card%d", i+1),
		}})
	}
	m := buildModel(&canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "frame", Path: "frame", Children: children,
		},
	}}, "big")

	for n := range m.ports {
		if n >= firstClientPort {
			t.Fatalf("a card was served at port %d, which is a client's", n)
		}
	}
}
