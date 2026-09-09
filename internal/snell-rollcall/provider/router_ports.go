package rollcall

import (
	"fmt"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
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
			// Not Ports, though the vendor's matrix advertises it.
			//
			// On a Centra a matrix is a unit of its own and its levels are
			// literally its ports — 0000-11-01 and 0000-11-02 under
			// 0000-11-00 — so the claim is honoured there. This provider
			// serves one unit, so our levels are siblings of the matrix
			// rather than children of it, and there is nothing behind a port
			// enquiry here. Advertising the service anyway invites a client
			// to ask a question we can only answer with a lie or with
			// nothing; it is claimed again when the levels are really ports.
			Services: codec.SvcMenus | codec.SvcControl | codec.SvcFile |
				codec.SvcLongStr,
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
// Almost everything it has to say is in the tables, which a client reads as
// commands rather than walking as lines — which is what our own router verb
// does, and what found nothing when these tables were served from the matrix
// node instead.
//
// But it is not menuless. The Centra publishes three lines here — the root,
// the way out, and a Status display on command 99 — and the node's template
// binds its only control to that command. Served without the menu the panel
// draws an empty box: a template names a command, and a command with no line
// behind it is not something a panel will render, whatever its value.
func newXYPanelPort(number uint8, name string, r *routerModel) *port {
	p := &port{
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

	p.lines = []line{
		{Index: 0, Style: codec.StyleList, Text: "Menu", path: "menu"},
		{Index: 1, Style: codec.StylePartial | codec.StyleHidden, Text: "RETURN", path: "menu.return"},
		{
			Index: 2, Style: codec.StyleDisplay | codec.StyleCacheable,
			Command: cmdXYStatus, MinRange: -32767, MaxRange: 23767,
			Text: "Status", Param: "%s", path: "menu.status",
		},
	}
	p.byCmd[cmdXYStatus] = 2
	p.byPath["menu.status"] = 2

	// The salvos, as a list a panel can press.
	//
	// Nothing in the routing interface makes salvos visible to a panel: the XY
	// grid understands names, counts, routing, protect and reference, and
	// salvos are none of those. But a menu is drawn by every client there is,
	// and a list whose parameter is "#SEL:" with a button per entry is how the
	// vendor publishes any set of choices — so that is how these are offered.
	if len(r.salvos) > 0 {
		// A list selects and a button acts, which is the vendor's own pattern:
		// its routing page has listboxes for sources and destinations and a
		// Take button beside them. A list that acted on selection would fire a
		// salvo every time an operator scrolled past one.
		group := len(p.lines)
		p.lines = append(p.lines, line{
			Index: uint32(group), Style: codec.StyleList | codec.StyleCacheable,
			Text: "Salvos", Param: "#SEL:", path: "menu.salvos",
		})
		for i := range r.salvos {
			idx := len(p.lines)
			path := fmt.Sprintf("menu.salvos.%d", i+1)
			p.lines = append(p.lines, line{
				Index: uint32(idx), Style: codec.StyleButton | codec.StyleCacheable,
				Command: cmdXYSalvoSelect, MinRange: int32(i + 1),
				Text: r.salvos[i].name, path: path,
			})
			p.byPath[path] = idx
		}
		p.lines[group].Step = uint32(len(p.lines) - group - 1)
		p.byCmd[cmdXYSalvoSelect] = group + 1
		p.values[cmdXYSalvoSelect] = codec.Value{
			Command: cmdXYSalvoSelect, Mode: codec.ModeValue, Val: 1,
		}

		fire := len(p.lines)
		p.lines = append(p.lines, line{
			Index: uint32(fire), Style: codec.StyleButton | codec.StyleCacheable,
			Command: uint32(router.CmdFireSalvo), MinRange: 1,
			Text: "Fire", path: "menu.salvos.fire",
		})
		p.byCmd[uint32(router.CmdFireSalvo)] = fire
		p.byPath["menu.salvos.fire"] = fire

		// What the last one did. Pressing Fire otherwise says nothing at all:
		// the routes it makes are on other nodes, and an operator looking at
		// this page has no way to tell a salvo that fired from one that was
		// refused.
		idx := len(p.lines)
		p.lines = append(p.lines, line{
			Index: uint32(idx), Style: codec.StyleDisplay | codec.StyleCacheable,
			Command: cmdXYLastSalvo, MinRange: -32767, MaxRange: 23767,
			Text: "Last Salvo", Param: "%s", path: "menu.salvos.last",
		})
		p.byCmd[cmdXYLastSalvo] = idx
		p.byPath["menu.salvos.last"] = idx
		p.values[cmdXYLastSalvo] = codec.Value{
			Command: cmdXYLastSalvo, Mode: codec.ModeString, Text: "none fired yet",
		}
	}

	// The categories, so they can be seen and tried.
	//
	// A panel's XY grid has no category key — the whole set it accepts names
	// counts, names, routing, protect and reference — so nothing here can make
	// it filter by them. What it can do is show what a category is: a set of
	// groups, each matching a name at a character index. Choosing one lists
	// what it selects, which is the whole of what a category does.
	for i := range r.categories {
		c := &r.categories[i]
		path := fmt.Sprintf("menu.category.%d", i+1)

		// Flat, beside the salvos, rather than nested under a container of
		// its own. The vendor's panel draws the salvo list and would not draw
		// this one until the two had the same shape, and one list is one list
		// whatever it is a list of.
		groups := len(p.lines)
		p.lines = append(p.lines, line{
			Index: uint32(groups), Style: codec.StyleList | codec.StyleCacheable,
			Text: c.name, Param: "#SEL:", path: path,
		})
		selectCmd := uint32(cmdXYGroupSelect + i)
		for j := range c.groups {
			idx := len(p.lines)
			gpath := fmt.Sprintf("%s.%d", path, j+1)
			p.lines = append(p.lines, line{
				Index: uint32(idx), Style: codec.StyleButton | codec.StyleCacheable,
				Command: selectCmd, MinRange: int32(j + 1),
				Text: c.groups[j].name, path: gpath,
			})
			p.byPath[gpath] = idx
		}
		p.lines[groups].Step = uint32(len(p.lines) - groups - 1)
		p.byCmd[selectCmd] = groups + 1
		p.values[selectCmd] = codec.Value{
			Command: selectCmd, Mode: codec.ModeValue, Val: 1,
		}

		matchCmd := uint32(cmdXYGroupMatch + i)
		idx := len(p.lines)
		p.lines = append(p.lines, line{
			Index: uint32(idx), Style: codec.StyleDisplay | codec.StyleCacheable,
			Command: matchCmd, MinRange: -32767, MaxRange: 23767,
			Text: "Selects", Param: "%s", path: path + ".selects",
		})
		p.byCmd[matchCmd] = idx
		p.byPath[path+".selects"] = idx
		p.values[matchCmd] = codec.Value{
			Command: matchCmd, Mode: codec.ModeString, Text: c.selects(r, 1),
		}
	}

	// The root spans everything under it.
	p.lines[0].Step = uint32(len(p.lines) - 1)
	return p
}
