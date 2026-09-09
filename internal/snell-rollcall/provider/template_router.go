package rollcall

import (
	"bytes"
	"fmt"

	"dhs/internal/snell-rollcall/codec/router"
)

// Router nodes carry templates the vendor writes by hand rather than pages
// generated from a menu, because routing is not a menu: a matrix node says
// where to go and a level node draws an XY panel.
//
// Both layouts below were read out of the vendor's own template cache, which
// ships with the Control Panel under assets/Protocol/Menu Cache/templatecache
// as <typeID>_<cmdSet>.tpl. 636 is the matrix and 637 is the level. Every
// command set the vendor publishes for 636 carries the same two lines of text,
// and every one of the seven it publishes for 637 — 2, 4, 9, 11, 12, 13 and 16
// — carries the same three pages, control for control. That is the contract:
// not a shape we chose, one we measured.

// Control types used only by a router page. The first two are absent from
// TemplateDoc.html's table, which stops at CM_STATIC; they are taken from the
// vendor's files, where they appear in every 637 template.
const (
	ctlXYPanel  = -40  // the XY routing grid, drawn by the panel itself
	ctlPageList = -100 // CM_PAGELIST, the tab strip naming the pages
	ctlListbox  = -10  // CM_LISTBOX
	ctlPushBtn  = -17  // CM_PUSHBUTTON
	ctlRadioBtn = 48   // CM_BUTTON, one command with a value per button
)

// The command space an XY panel drives on a level node.
//
// This is the older "basic router control via RollCall" interface the Full
// RollCall Control of Routers document refers to as already "working and
// proven" before the command tables at 100 and up were designed. The numbers
// and the widget bound to each one are read off the vendor's 637 template; the
// meaning of each is read off the caption of the group it sits in.
const (
	// cmdXYStatus is what the node says about itself while a client is still
	// reading its tables. The vendor's template binds its only control to it.
	cmdXYStatus = 99

	// cmdXYLastSalvo carries what the last salvo did, and the category block
	// carries what each category's groups match. Neither is in the routing
	// interface: they are how this node makes salvos and categories visible to
	// a panel that has no other way to see them. They sit far above the
	// command space the tables are allocated from, which starts just after the
	// root block at 119 and grows with the plant.
	cmdXYLastSalvo    = 90000
	cmdXYSalvoSelect  = 90001
	cmdXYCategoryBase = 91000
	cmdXYGroupSelect  = 92000
	cmdXYGroupMatch   = 93000

	// maxCategoryGroups is the room each category has for its groups.
	maxCategoryGroups = 1000

	cmdXYDestSelect = 100 // the destination list, and which entry is selected
	cmdXYSrcSelect  = 110 // the source list, and which entry is selected
	cmdXYProtect    = 113 // protect state of the selected destination
	cmdXYTakeMode   = 120 // 0 = take immediately, 1 = wait for the button
	cmdXYTake       = 121 // apply the pending route
	cmdXYCancel     = 122 // drop it

	// Monitor outputs, four of them, five readouts each. The vendor lays them
	// out as a base per row and one command per monitor: 401..404 is the first
	// row, 411..414 the second, and so on to 441..444.
	cmdXYMonKind    = 400 // "Source or Dest"
	cmdXYMonIndex   = 410
	cmdXYMonName    = 420
	cmdXYMonSrcAddr = 430
	cmdXYMonDstAddr = 440

	xyMonitors = 4
)

// writeRouterMatrixPage renders the matrix node's page.
//
// A matrix has nothing to control: the routing is on the levels below it. The
// vendor still ships a page rather than nothing, because a node with no
// template at all makes a panel say the unit is not supported, and a panel
// that says that about a working matrix sends the operator looking for a fault
// that is not there. So it ships two lines of text saying where to go, and
// those are the two lines below, word for word.
func writeRouterMatrixPage(body *bytes.Buffer, prt *port) {
	fmt.Fprintf(body, "[Version]%sversion=%d%s", nl, templateFormatVersion, nl)
	fmt.Fprintf(body, "[%d:%d:%d:0]%s", prt.id.TypeID, prt.id.Version.CmdSet, templateAllLevels, nl)
	fmt.Fprintf(body, "Size=0,0,300,155%s", nl)
	fmt.Fprintf(body, "Ctl0=Please open the Matrix node and choose the desired Level within,-1,0,%d,15,30,200,13%s", ctlLabel, nl)
	fmt.Fprintf(body, "Ctl1=the matrix where you will find the XY router control screens,-1,0,%d,15,45,175,13%s", ctlLabel, nl)
	body.WriteString(nl)
}

// writeRouterLevelPages renders the three pages an XY panel is drawn from.
//
// Page 0 is the panel itself, page 1 the list-and-take form behind it, and
// page 2 the monitor readouts. The tab strip on page 0 names all three and
// carries the user level each is visible at; a panel connected below that
// level is shown the pages it is allowed and told nothing about the rest.
//
// Two things about the file rather than the pages, both learned from a panel
// refusing the first version of this:
//
// Every SaveSet line belongs to page zero. Anywhere else the panel stops with
// "SaveSet entries found in page other than zero" and draws no page at all,
// which is a whole template lost to a line in the wrong place.
//
// The [XYPanel] section at the end is not decoration. It is how the panel is
// told which commands mean what — the grid is a generic control and the
// section is its wiring — and it ends with the same numbers this provider
// serves, which is the closest thing to a specification for this interface
// that exists.
func writeRouterLevelPages(body *bytes.Buffer, prt *port) {
	fmt.Fprintf(body, "[Version]%sversion=%d%s", nl, templateFormatVersion, nl)

	page := func(n int) {
		fmt.Fprintf(body, "[%d:%d:%d:%d]%s", prt.id.TypeID, prt.id.Version.CmdSet, templateAllLevels, n, nl)
	}
	i := 0
	ctl := func(caption string, command int64, flags, typ, x, y, w, h int) {
		fmt.Fprintf(body, "Ctl%d=%s,%d,%d,%d,%d,%d,%d,%d%s",
			i, caption, command, flags, typ, x, y, w, h, nl)
		i++
	}

	readouts := []struct {
		label string
		base  int64
	}{
		{"Source or Dest", cmdXYMonKind},
		{"Index", cmdXYMonIndex},
		{"Name", cmdXYMonName},
		{"RC Source Addr", cmdXYMonSrcAddr},
		{"RC Dest Addr", cmdXYMonDstAddr},
	}

	// Page 0 — the XY grid. The tab strip is a control on this page and not a
	// property of the file, so it is written first and only here.
	page(0)
	fmt.Fprintf(body, "Size=0,0,350,347%s", nl)
	ctl("XYPanel Control=15:Routing=15:Monitor Outputs=15", -1, 0, ctlPageList, 2, 2, 94, 44)
	ctl("New XYPanel", -1, 0, ctlXYPanel, 8, 57, 309, 210)

	// The commands a panel keeps across a restart, and the block it watches
	// for tally: every destination's routed source and every destination's
	// protect, then the take mode and the monitor readouts.
	//
	// Only commands this provider actually answers are listed. The vendor's
	// own file carries a third per-destination block at 23001 that appears in
	// neither the menu it publishes nor the [XYPanel] wiring below, so there
	// is nothing here to serve it with; naming it would subscribe a panel to
	// commands that would then be refused.
	var saved []int64
	for d := 1; d <= len(prt.level.dests); d++ {
		saved = append(saved, int64(router.LvlRoute(d)))
	}
	for d := 1; d <= len(prt.level.dests); d++ {
		saved = append(saved, int64(router.LvlProtect(d)))
	}
	saved = append(saved, cmdXYTakeMode)
	for _, r := range readouts {
		for mon := 1; mon <= xyMonitors; mon++ {
			saved = append(saved, r.base+int64(mon))
		}
	}
	for start := 0; start < len(saved); start += 10 {
		end := min(start+10, len(saved))
		body.WriteString("SaveSet=")
		for k, c := range saved[start:end] {
			if k > 0 {
				body.WriteString(",")
			}
			fmt.Fprintf(body, "%d", c)
		}
		body.WriteString(nl)
	}

	// Page 1 — the same routing done as a form: pick a source, pick a
	// destination, take it.
	i = 0
	page(1)
	fmt.Fprintf(body, "Size=0,0,500,447%s", nl)
	ctl("Sources", -1, 0, ctlGroupBox, 6, 63, 86, 281)
	ctl("New Listbox", cmdXYSrcSelect, 0, ctlListbox, 10, 73, 77, 262)
	ctl("Destinations", -1, 0, ctlGroupBox, 102, 63, 137, 281)
	ctl("New Listbox", cmdXYDestSelect, 0, ctlListbox, 106, 73, 128, 262)
	ctl("Take", cmdXYTake, 1, ctlPushBtn, 258, 99, 48, 20)
	ctl("Cancel", cmdXYCancel, 1, ctlPushBtn, 258, 137, 47, 19)
	ctl("Dest Protect", -1, 0, ctlGroupBox, 258, 237, 80, 22)
	ctl("Protected", cmdXYProtect, 1, ctlCheckbox, 268, 247, 60, 10)
	ctl("Take Mode", -1, 0, ctlGroupBox, 258, 172, 80, 44)
	ctl("Immediate Take", cmdXYTakeMode, 0, ctlRadioBtn, 268, 182, 60, 10)
	ctl("Use Take Button", cmdXYTakeMode, 1, ctlRadioBtn, 268, 197, 60, 10)

	// Page 2 — four monitor outputs, five readouts each, in a two by two grid.
	i = 0
	page(2)
	fmt.Fprintf(body, "Size=0,0,300,237%s", nl)
	for mon := 1; mon <= xyMonitors; mon++ {
		// Monitors 1 and 3 sit in the left column, 2 and 4 in the right.
		x, y := 16, 63
		if mon%2 == 0 {
			x = 150
		}
		if mon > 2 {
			y = 147
		}
		ctl(fmt.Sprintf("Monitor %d", mon), -1, 0, ctlGroupBox, x, y, 117, 74)
		for r, row := range readouts {
			ctl("New Displaytext", row.base+int64(mon), 0, ctlValueText,
				x+60, y+10+r*10, 51, 10)
		}
		for r, row := range readouts {
			ctl(row.label, -1, 0, ctlLabel, x+10, y+10+r*10, 40, 10)
		}
	}

	writeXYPanelSection(body, prt)
	body.WriteString(nl)
}

// writeXYPanelSection wires the grid control to this level's commands.
//
// The keys are the vendor's, in the vendor's order, and every command in them
// is one this provider answers. It is worth reading as documentation: it is
// the only place the meaning of the level command set is written down at all.
func writeXYPanelSection(body *bytes.Buffer, prt *port) {
	kv := func(k string, v any) { fmt.Fprintf(body, "%s=%v%s", k, v, nl) }

	body.WriteString("[XYPanel]" + nl)
	kv("AllowNameEdit", false)
	kv("AllowOptions", true)
	kv("AllowPopout", true)
	kv("AllowProtectEdit", true)
	kv("AllowResize", true)
	kv("CMDDestCount", router.LvlDestCount)
	kv("CMDDestName", router.LvlDstName)
	kv("CMDDestNameIndex", router.LvlDstNameIndex)
	kv("CMDDestProtect", router.LvlDestProtect)
	kv("CMDDestRouteIndex", router.LvlDestSelect)
	kv("CMDProtectDestBase", router.LvlProtect(1))
	// Where each source comes from, which is what Show Source Reference draws.
	kv("CMDReferenceSourceBase", router.LvlRefSource(1))
	kv("CMDRoutingDestBase", router.LvlRoute(1))
	kv("CMDSourceCount", router.LvlSourceCount)
	kv("CMDSourceName", router.LvlSrcName)
	kv("CMDSourceNameIndex", router.LvlSrcNameIndex)
	kv("CMDSourceRoute", router.LvlSrcSelect)
	kv("DestButtonHeight", 50)
	kv("DestButtonWidth", 100)
	kv("DestCount", len(prt.level.dests))
	kv("DestPanelColumns", xyPanelColumns)
	kv("DestPanelRows", xyPanelRows)
	// The switch that draws the category filter at all. A plant with nothing
	// to narrow itself down by leaves it off, because a filter that finds
	// nothing is worse than no filter.
	kv("FilterFunction", prt.level.filter)
	// The whole interface counts from one — a source is 1..n and a protect is
	// off at 1 — and this is where the panel is told so.
	kv("OneBased", true)
	// The index a source sits at, which the panel has already and needs no
	// command for; its protect, which the level publishes; and its reference,
	// which it does now.
	kv("ShowPorts", true)
	kv("ShowProtect", true)
	kv("ShowReference", true)
	kv("SourceButtonHeight", 50)
	kv("SourceButtonWidth", 100)
	kv("SourceCount", len(prt.level.sources))
	kv("SourcePanelColumns", xyPanelColumns)
	kv("SourcePanelRows", xyPanelRows)
	kv("TakeFunction", true)
	kv("UndoFunction", true)
}

// The grid a panel opens with. It is a starting size rather than a limit: the
// control pages through a plant larger than one screen, which is how the
// vendor draws eleven hundred sources in an eight by eight grid.
const (
	xyPanelColumns = 8
	xyPanelRows    = 8
)

// writeXYPanelPage renders the node that serves the Full Control tables.
//
// One line, and the vendor's is the same: a display bound to command 99,
// captioned "Initialising...". A client reading a plant of tables has nothing
// to draw until it has read them, so the node publishes a sentence about what
// it is doing instead of a page it would have to redraw.
func writeXYPanelPage(body *bytes.Buffer, prt *port) {
	fmt.Fprintf(body, "[Version]%sversion=%d%s", nl, templateFormatVersion, nl)
	fmt.Fprintf(body, "[%d:%d:%d:0]%s", prt.id.TypeID, prt.id.Version.CmdSet, templateAllLevels, nl)
	// A page tall enough for what is on it. The vendor's own is a hundred high
	// and holds nothing but the status line, because its controller offers a
	// panel nothing else here.
	salvos := 0
	if prt.router != nil {
		salvos = len(prt.router.salvos)
	}
	rows := salvos + 4
	for _, c := range categoriesOf(prt) {
		rows += len(c.groups) + 4
	}
	height := 100 + rows*14
	fmt.Fprintf(body, "Size=0,0,420,%d%s", height, nl)

	n := 0
	ctl := func(caption string, command int64, flags, typ, x, y, w, h int) {
		fmt.Fprintf(body, "Ctl%d=%s,%d,%d,%d,%d,%d,%d,%d%s",
			n, caption, command, flags, typ, x, y, w, h, nl)
		n++
	}

	ctl("Initialising...", cmdXYStatus, 0, ctlValueText, 8, 10, 400, 20)
	y := 36

	// The salvos: a list to choose from and a button to act, which is the
	// vendor's own pattern — its routing page has listboxes and a Take button
	// beside them. A list that acted on selection would fire a salvo every
	// time an operator scrolled past one.
	if salvos > 0 {
		listHeight := salvos*14 + 10
		ctl("Salvos", -1, 0, ctlGroupBox, 8, y, 400, listHeight+52)
		ctl("New Listbox", cmdXYSalvoSelect, 0, ctlListbox, 14, y+14, 300, listHeight)
		ctl("Fire", int64(router.CmdFireSalvo), 1, ctlPushBtn, 324, y+14, 76, 24)
		ctl("Last Salvo", -1, 0, ctlLabel, 14, y+listHeight+22, 60, 10)
		ctl("New Displaytext", cmdXYLastSalvo, 0, ctlValueText, 80, y+listHeight+22, 322, 10)
		y += listHeight + 58
	}

	// The categories. A panel's grid has no category key, so nothing here can
	// make it filter by them; what it can do is let one be chosen and say what
	// it picks out, which is the whole of what a category does.
	for i, c := range categoriesOf(prt) {
		listHeight := len(c.groups)*14 + 10
		ctl(c.name, -1, 0, ctlGroupBox, 8, y, 400, listHeight+40)
		ctl("New Listbox", int64(cmdXYGroupSelect+i), 0, ctlListbox, 14, y+14, 150, listHeight)
		ctl("Selects", -1, 0, ctlLabel, 174, y+14, 50, 10)
		ctl("New Displaytext", int64(cmdXYGroupMatch+i), 0, ctlValueText,
			174, y+26, 228, listHeight-16)
		y += listHeight + 46
	}

	fmt.Fprintf(body, "SaveSet=%d,%d%s", cmdXYDestSelect, cmdXYStatus, nl)
	body.WriteString(nl)
}

// categoriesOf returns the categories a node publishes, if it publishes any.
func categoriesOf(prt *port) []routerCategory {
	if prt.router == nil {
		return nil
	}
	return prt.router.categories
}
