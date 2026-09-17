package rollcall

import (
	"fmt"

	"dhs/internal/snell-rollcall/codec"
)

// The tielines get a node of their own, as they do on a Centra.
//
// Its plant carries one at 0000-80-00, typed 731 TIELINES, beside the matrices
// and the node that serves the tables. What that node offers, walked off it:
// an upstream selection of matrix, level and source; a downstream selection of
// matrix, level and destination; a Make Route button and a status line; and a
// list of the cables with a Clear button and what each is used by.
//
// So a tieline is never routed by a client. This is the manual view of a pool
// the controller manages by itself — a place to see which cables exist, what
// is holding each, and to put one back when something has gone wrong.

// Commands the tieline node serves.
//
// The numbers are the Centra's, read off its menu: the actions live at 300 and
// upwards. They sit in a node of their own, so they collide with nothing — 300
// is a salvo count on the node that serves the tables, and neither node sees
// the other's.
const (
	cmdTLMakeRoute = 300
	cmdTLStatus    = 302
	cmdTLSelect    = 303
	cmdTLClear     = 304
	cmdTLUsedBy    = 306
)

// What of the vendor's node is not here, and why.
//
// A Centra's tieline node also carries an upstream selection of matrix, level
// and source on 100 to 103 and a downstream one on 200 to 203, so that an
// operator can nominate both ends and press Make Route. That is a second way
// to make a cross-matrix route, and this provider already has one that a panel
// and a client both reach: routing by association, which takes a cable for
// each level it needs. A second path to the same crosspoint is a second place
// for the pool and the plant to disagree.
//
// So the selection commands are not served. Make Route says where routes are
// made instead of pretending to be a third route-maker, and what is here is
// the part nothing else offers: seeing which cables exist, what is holding
// each, and putting one back.

// newTielinePort builds the node the cables are managed from.
func newTielinePort(number uint8, r *routerModel) *port {
	p := &port{
		number: number,
		id: codec.ID{
			Services: codec.SvcMenus | codec.SvcControl | codec.SvcFile | codec.SvcLongStr,
			TypeID:   codec.TypeIDTielines,
			Version:  codec.Version{Major: 1, Minor: 0, Alpha: ' ', CmdSet: 1},
			Name:     codec.TruncateFixed("TIELINES", codec.MaxTextSize),
		},
		byCmd:  make(map[uint32]int),
		byPath: make(map[string]int),
		values: make(map[uint32]codec.Value),
		router: r,
	}
	buildTielineMenu(p, r)
	return p
}

// buildTielineMenu lays the node out in the shape a Centra publishes.
func buildTielineMenu(p *port, r *routerModel) {
	b := &levelMenuBuilder{p: p}

	cache := codec.StyleCacheable
	btn := codec.StyleButton | cache
	disp := codec.StyleDisplay | cache

	root := b.add("Menu", "menu", codec.StyleList, 0, 0, 0, "")
	b.add("RETURN", "menu.return", codec.StylePartial|codec.StyleHidden, 0, 0, 0, "")

	// The cables themselves, which is the thing an operator came here to see.
	// A tieline that cannot be seen cannot be diagnosed: the whole complaint
	// about a plant that will not route across matrices is "which cable is
	// stuck", and nothing else answers it.
	b.group("Tielines", "tielines", "#SEL:", func() {
		for i := range r.tielines {
			b.add(r.tielines[i].name, fmt.Sprintf("tielines.%d", i+1),
				btn, cmdTLSelect, int32(i+1), 0, "")
		}
	})
	b.step(b.add("Used By", "tielines.usedby", disp, cmdTLUsedBy, -32767, 23767, "%s"), 0)
	b.add("Clear", "tielines.clear", btn, cmdTLClear, 1, 0, "")

	b.group("Actions", "actions", "", func() {
		b.step(b.add("Status", "actions.status", disp, cmdTLStatus, -32767, 23767, "%s"), 0)
		b.add("Make Route", "actions.makeroute", btn, cmdTLMakeRoute, 1, 0, "")
	})

	p.lines[root].Step = uint32(len(p.lines) - root - 1)

	str := func(cmd uint32, text string) {
		p.values[cmd] = codec.Value{Command: cmd, Mode: codec.ModeString, Text: text}
	}
	num := func(cmd uint32, v int32) {
		p.values[cmd] = codec.Value{Command: cmd, Mode: codec.ModeValue, Val: v}
	}

	num(cmdTLSelect, 1)
	str(cmdTLUsedBy, r.tielineUsedBy(1))
	num(cmdTLClear, 0)
	num(cmdTLMakeRoute, 0)
	str(cmdTLStatus, fmt.Sprintf("%d cable(s), %d in use", len(r.tielines), heldTielines(r)))
}

// tielineUsedBy says what is holding one cable, counting from one.
func (r *routerModel) tielineUsedBy(n uint32) string {
	t, ok := r.tielineAt(n)
	if !ok {
		return "no such tieline"
	}
	if t.free() {
		return fmt.Sprintf("free — m%d/l%d dest %d to m%d/l%d source %d",
			t.upMatrix, t.upLevel, t.upDest, t.downMatrix, t.downLevel, t.downSource)
	}
	return fmt.Sprintf("m%d/l%d destination %d — carrying source %d from m%d",
		t.downMatrix, t.downLevel, t.heldFor,
		r.sourceOnCable(t), t.upMatrix)
}

// sourceOnCable is what the upstream end of a cable is carrying.
func (r *routerModel) sourceOnCable(t *routerTieline) uint16 {
	up, ok := r.levelOf(uint32(t.upMatrix), uint32(t.upLevel))
	if !ok || t.upDest < 1 || t.upDest > len(up.dests) {
		return 0
	}
	return up.dests[t.upDest-1].routed.Source
}

// tielineAt returns one cable, counting from one.
func (r *routerModel) tielineAt(n uint32) (*routerTieline, bool) {
	if n < 1 || int(n) > len(r.tielines) {
		return nil, false
	}
	return &r.tielines[n-1], true
}

// heldTielines is how many cables are in use.
func heldTielines(r *routerModel) int {
	n := 0
	for i := range r.tielines {
		if !r.tielines[i].free() {
			n++
		}
	}
	return n
}

// clearTieline puts one cable back.
//
// It frees the cable and says so; the destination it was feeding keeps
// whatever the crosspoints say, because clearing a tieline is an admission
// that the pool and the plant have drifted apart rather than an instruction to
// unroute anything. An operator clearing a stuck cable wants it available, not
// a destination going black.
func (r *routerModel) clearTieline(n uint32) string {
	t, ok := r.tielineAt(n)
	if !ok {
		return "no such tieline"
	}
	if t.free() {
		return fmt.Sprintf("%s was already free", t.name)
	}
	t.heldFor = 0
	return fmt.Sprintf("%s freed", t.name)
}
