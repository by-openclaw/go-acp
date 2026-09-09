package rollcall

import (
	"context"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
)

func testMatrix(targets, sources int64) *canonical.Matrix {
	return &canonical.Matrix{
		Header: canonical.Header{
			Number: 3, Identifier: "router", Path: "frame.router", Access: canonical.AccessRead,
		},
		Type: "oneToN", Mode: "linear",
		TargetCount: targets, SourceCount: sources,
	}
}

func routerTree(targets, sources int64) *canonical.Export {
	return &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "frame", Path: "frame",
			Children: []canonical.Element{testMatrix(targets, sources)},
		},
	}}
}

// A router serves no menu: everything a client needs is in the command space,
// and the tables that describe it are what it walks. The layout is the one
// codec/router encodes, which was confirmed against the vendor Centra offset by
// offset, so this side has to agree with it exactly.

func TestTheRootBlockAnswersEveryCommandItDefines(t *testing.T) {
	// A client walks the block to find out what a router has. A refusal there
	// reads as "this is not a routing interface" rather than "there are none
	// of those": our own consumer gives up on the whole node when command 106
	// will not answer.
	vals := buildRouter("router", []*canonical.Matrix{testMatrix(8, 8)}).values()

	for c := router.CmdInterfaceVersion; c <= router.CmdGetAHPNode; c++ {
		if _, ok := vals[uint32(c)]; !ok {
			t.Errorf("command %d is not answered", c)
		}
	}
	if v := vals[uint32(router.CmdInterfaceVersion)]; v.Val != router.VersionRouteErrors {
		t.Errorf("interface version = %d, want %d", v.Val, router.VersionRouteErrors)
	}
	if v := vals[uint32(router.CmdNumMatrices)]; v.Val != 1 {
		t.Errorf("matrices = %d", v.Val)
	}
}

func TestNoTableOverlapsAnother(t *testing.T) {
	// Bases are allocated in sequence rather than to a fixed scheme, because
	// two tables sharing a command would have one entity reading another's
	// fields — which is the failure Table.Field guards against on the read
	// side and this guards against on the write side.
	r := buildRouter("router", []*canonical.Matrix{testMatrix(8, 8)})

	type span struct {
		name   string
		lo, hi router.Command
	}
	var spans []span
	add := func(name string, tb router.Table) {
		if tb.Count == 0 {
			return
		}
		spans = append(spans, span{name, tb.Base, tb.Base + router.Command(tb.Count*tb.Step)})
	}
	add("matrices", r.table)
	for i := range r.matrices {
		m := &r.matrices[i]
		add("levels", m.table)
		for j := range m.levels {
			add("sources", m.levels[j].srcTable)
			add("dests", m.levels[j].dstTable)
		}
	}

	for i := range spans {
		if spans[i].lo <= router.CmdGetAHPNode {
			t.Errorf("%s starts at %d, inside the root block", spans[i].name, spans[i].lo)
		}
		for j := i + 1; j < len(spans); j++ {
			if spans[i].lo < spans[j].hi && spans[j].lo < spans[i].hi {
				t.Errorf("%s [%d,%d) overlaps %s [%d,%d)",
					spans[i].name, spans[i].lo, spans[i].hi,
					spans[j].name, spans[j].lo, spans[j].hi)
			}
		}
	}
}

func TestTheTablesDescribeTheMatrix(t *testing.T) {
	r := buildRouter("router", []*canonical.Matrix{testMatrix(8, 6)})
	vals := r.values()

	base, ok := r.table.Command(1)
	if !ok {
		t.Fatal("no matrix 1")
	}
	if got := vals[uint32(base+router.OffNumLevels)].Val; got != 1 {
		t.Errorf("levels = %d", got)
	}

	lb, _ := r.matrices[0].table.Command(1)
	if got := vals[uint32(lb+router.OffNumSrcs)].Val; got != 6 {
		t.Errorf("sources = %d, want the matrix's source count", got)
	}
	if got := vals[uint32(lb+router.OffNumDsts)].Val; got != 8 {
		t.Errorf("destinations = %d, want the matrix's target count", got)
	}

	// Every destination reads back, and nothing is routed yet.
	dt := r.matrices[0].levels[0].dstTable
	for n := uint32(1); n <= dt.Count; n++ {
		db, _ := dt.Command(n)
		v, ok := vals[uint32(db+router.OffDestRoutedSrc)]
		if !ok {
			t.Fatalf("destination %d has no routed source", n)
		}
		pin, ok := decodeRoutedSource(v)
		if !ok {
			t.Fatalf("destination %d carries a routed source that is not one", n)
		}
		if !pin.IsUnrouted() {
			t.Errorf("destination %d starts routed", n)
		}
	}
}

func TestARouterIsServedAsThreeKindsOfNode(t *testing.T) {
	// A Centra publishes a matrix, its levels, and the tables as separate
	// nodes with separate types, and the three use the same command numbers
	// for unrelated things. Serving them from one node cannot be made to work,
	// so this pins the arrangement rather than the fact that it exists.
	s := newServed(t, routerTree(8, 8))

	mx := s.p.model.port(firstCardPort)
	if mx == nil {
		t.Fatal("the matrix was not served as a node")
	}
	if mx.id.TypeID != codec.TypeIDRouterMatrix {
		t.Errorf("matrix type id = %d, want the router matrix id", mx.id.TypeID)
	}
	// Not Ports. The vendor's matrix advertises it because its levels really
	// are its ports; ours are siblings, so there is nothing behind a port
	// enquiry and claiming the service invites a question we cannot answer.
	if mx.id.Services&codec.SvcPorts != 0 {
		t.Error("a matrix claims Ports it cannot serve")
	}
	// Three lines, as the vendor's matrix publishes: the root, the way back
	// out, and a notice. It controls nothing.
	if got := len(mx.menu(true)); got != 3 {
		t.Errorf("matrix served %d menu lines, want the vendor's three", got)
	}

	lv := s.p.model.port(firstCardPort + 1)
	if lv == nil {
		t.Fatal("the level was not served as a node of its own")
	}
	if lv.id.TypeID != codec.TypeIDRouterLevel {
		t.Errorf("level type id = %d, want the router level id", lv.id.TypeID)
	}
	if len(lv.menu(true)) == 0 {
		t.Error("the level is the routing interface and its menu carries it")
	}

	xy := s.p.model.port(firstCardPort + 2)
	if xy == nil {
		t.Fatal("the tables were not served as a node of their own")
	}
	if xy.id.TypeID != codec.TypeIDXYPanel {
		t.Errorf("tables type id = %d, want the XY panel id", xy.id.TypeID)
	}
	// It is not menuless. The Centra publishes a Status line on command 99 and
	// the node's template binds its only control to it; without the line the
	// panel draws an empty box.
	xyLines := xy.menu(true)
	if len(xyLines) != 3 {
		t.Fatalf("the tables node served %d menu lines, want the vendor's three", len(xyLines))
	}
	if xyLines[2].Command != cmdXYStatus || xyLines[2].Text != "Status" {
		t.Errorf("its third line is %q on command %d, want Status on %d",
			xyLines[2].Text, xyLines[2].Command, cmdXYStatus)
	}
	if xyLines[2].Param != "%s" {
		t.Errorf("the status line carries %q, want the string format", xyLines[2].Param)
	}
	if v, ok := xy.value(cmdXYStatus); !ok || v.Text == "" {
		t.Error("the status command has to say something: it is the only thing the page draws")
	}
}

func TestReadingTheRouterOverASession(t *testing.T) {
	// What our own consumer does first: ask command 100 and believe the answer
	// only if it is a number in a plausible range. It is asked of the node
	// that serves the tables, which is not the matrix.
	s := newServed(t, routerTree(8, 8))
	sess := s.open(firstCardPort+2, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	reply, err := sess.Do(context.Background(), codec.MsgGetValue,
		codec.GetValue{Command: uint32(router.CmdInterfaceVersion)}.AppendTo(nil))
	if err != nil {
		t.Fatalf("interface version: %v", err)
	}
	v, err := codec.DecodeValue(reply.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Val != router.VersionRouteErrors {
		t.Errorf("interface version = %d", v.Val)
	}
}
