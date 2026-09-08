package rollcall

import (
	"dhs/internal/snell-rollcall/codec"
)

// A router reaches the wire as three kinds of node, not one.
//
// That is the vendor's arrangement, measured on a Centra rather than inferred.
// Its plant appears as:
//
//	0000-11-00  type 636  Router Matrix  "Matrix 1"   services include Ports
//	0000-11-01  type 637  Router Level   "Level 1"
//	0000-11-02  type 637  Router Level   "Level 2"
//	0000-12-00  type 636  Router Matrix  "Matrix 2"   (with its own levels)
//	0000-80-00  type 731  TIELINES
//	0000-81-00  type 734  XY Panel                     the Full Control tables
//
// Each kind answers a different question and they do not overlap:
//
//   - The matrix says which levels exist. Its menu is three lines and its
//     template is two lines of text telling the operator to open a level.
//   - The level is the routing interface. Its menu carries the source and
//     destination lists, the take controls, and one command per destination
//     holding what is routed to it.
//   - The XY Panel node serves the Full Control command set — the tables at
//     100 and up that describe the whole plant to a client that would rather
//     read a structure than walk a menu.
//
// The three use overlapping command numbers for unrelated things. Command 100
// is the interface version on the XY Panel node and the selected destination
// on a level. Nothing on the wire disambiguates them but the type of the node
// they were asked of, which is why serving them from one node — as we did
// first — cannot be made to work.

// newRouterMatrixPort builds the node that names a matrix.
//
// It controls nothing. Its whole job is to exist, so that a client walking the
// plant finds a matrix at all, and to carry the levels beneath it.
func newRouterMatrixPort(number uint8, name string, mx *routerMatrix) *port {
	p := &port{
		number: number,
		id: codec.ID{
			// Ports, because the vendor's matrix advertises it: its levels are
			// its own ports, and that is how a client is meant to find them.
			Services: codec.SvcMenus | codec.SvcControl | codec.SvcFile |
				codec.SvcPorts | codec.SvcLongStr,
			TypeID:  codec.TypeIDRouterMatrix,
			Version: codec.Version{Major: 1, Minor: 0, Alpha: ' ', CmdSet: 1},
			Name:    codec.TruncateFixed(name, codec.MaxTextSize),
		},
		byCmd:  make(map[uint32]int),
		byPath: make(map[string]int),
		values: make(map[uint32]codec.Value),
		matrix: mx,
	}

	// Three lines, which is what a Centra matrix publishes: the root, the way
	// back out, and a notice. A client that walks it gets the same shape it
	// would get from the vendor, including the fact that there is nothing here.
	p.lines = []line{
		{Index: 0, Style: codec.StyleList, Step: 2, Text: "Menu", path: "menu"},
		{Index: 1, Style: codec.StylePartial | codec.StyleHidden, Text: "RETURN", path: "menu.return"},
		{Index: 2, Style: codec.StyleTiled | codec.StyleCacheable, Text: "Warning", path: "menu.warning"},
	}
	return p
}

// newRouterLevelPort builds the node one level is served on.
//
// This is where routing happens. A vendor Control Panel draws its XY screen
// from this node and nothing else, and our own consumer takes a crosspoint on
// it the same way.
func newRouterLevelPort(number uint8, name string, lv *routerLevel) *port {
	p := &port{
		number: number,
		id: codec.ID{
			Services: codec.SvcMenus | codec.SvcControl | codec.SvcFile | codec.SvcLongStr,
			TypeID:   codec.TypeIDRouterLevel,
			Version:  codec.Version{Major: 1, Minor: 0, Alpha: ' ', CmdSet: 1},
			Name:     codec.TruncateFixed(name, codec.MaxTextSize),
		},
		byCmd:  make(map[uint32]int),
		byPath: make(map[string]int),
		values: make(map[uint32]codec.Value),
		level:  lv,
	}
	buildLevelMenu(p, lv)
	return p
}

// newXYPanelPort builds the node that serves the Full Control command set.
//
// It carries no menu. Everything it has to say is in the tables, and a client
// walks those by reading commands rather than lines — which is what our own
// router verb does, and what found nothing when these tables were served from
// the matrix node instead.
func newXYPanelPort(number uint8, name string, r *routerModel) *port {
	return &port{
		number: number,
		id: codec.ID{
			Services: codec.SvcMenus | codec.SvcControl | codec.SvcFile | codec.SvcLongStr,
			TypeID:   codec.TypeIDXYPanel,
			Version:  codec.Version{Major: 1, Minor: 0, Alpha: ' ', CmdSet: 1},
			Name:     codec.TruncateFixed(name, codec.MaxTextSize),
		},
		byCmd:  make(map[uint32]int),
		byPath: make(map[string]int),
		values: r.values(),
		router: r,
	}
}
