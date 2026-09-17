package rollcall

import (
	"context"
	"strings"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/dtp"
	"dhs/internal/snell-rollcall/codec/router"
)

// A salvo is a set of routes made together. Its contents are never on the
// wire: a client fires it by number and is told how many routes it made, so
// what a client can know before firing is the name and nothing else.

func salvoRouter(t *testing.T) *served {
	t.Helper()
	return newServed(t, &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "frame",
			Children: []canonical.Element{plantWithLevels(
				[]string{"Video", "Audio"},
				[]string{"CAM 1", "CAM 2", "VTR 1"},
				[]string{"MON 1", "MON 2"},
			)},
		},
	}})
}

func TestAPlantOffersOneSalvoPerSource(t *testing.T) {
	// What is in a salvo is this provider's own choice — a canonical tree has
	// no field for one — so this pins the choice rather than a protocol rule.
	s := salvoRouter(t)
	r := s.p.model.routerModel()
	if r == nil {
		t.Fatal("no router is served")
	}

	if len(r.salvos) != 3 {
		t.Fatalf("%d salvos, want one per source", len(r.salvos))
	}
	if r.salvos[0].name != "All CAM 1" {
		t.Errorf("salvo 1 is named %q", r.salvos[0].name)
	}
	// Every destination on every level: two levels of two destinations.
	if len(r.salvos[0].routes) != 4 {
		t.Errorf("salvo 1 makes %d routes, want one per destination per level",
			len(r.salvos[0].routes))
	}

	if got := r.values()[uint32(router.CmdNumSalvos)].Val; got != 3 {
		t.Errorf("the root block says %d salvos", got)
	}
}

func TestSalvoNamesAreServedAsAFile(t *testing.T) {
	// Everything countable on this interface is named in a file rather than in
	// commands: a plant with a thousand salvos would otherwise need a thousand
	// commands to say what they are called.
	s := salvoRouter(t)
	xy := s.p.model.tablePort()

	for _, tc := range []struct {
		command router.Command
		path    string
		width   int
		// A name too long for the field is truncated, which is what the
		// controller itself does when it writes the file: the eight-character
		// set is a different, shorter set of names rather than a promise that
		// every name fits in eight. The cut is at the byte, not the word, so a
		// name cut mid-word keeps the space it was cut at — padding is zeros
		// and only zeros are trimmed.
		first string
	}{
		{router.CmdSalvoNames8File, salvoNames8File, router.NameWidth8, "All CAM "},
		{router.CmdSalvoNames32File, salvoNames32File, router.NameWidth32, "All CAM 1"},
	} {
		v, ok := xy.value(uint32(tc.command))
		if !ok {
			t.Fatalf("command %d publishes no names file", tc.command)
		}
		items, err := dtp.Decode(v.Data)
		if err != nil {
			t.Fatalf("command %d: %v", tc.command, err)
		}
		if len(items) < 2 || items[0].Str != tc.path {
			t.Errorf("command %d names %q, want %q", tc.command, items[0].Str, tc.path)
		}

		// And the file itself is there to be fetched, at the path the command
		// gave: a name a client believes and then cannot open is worse than
		// none.
		body, ok := s.p.fileAt(xy.number, cleanPath(tc.path))
		if !ok {
			t.Fatalf("%s is named but not served", tc.path)
		}
		names, err := router.DecodeNamesFile(body, tc.width, 3, 0)
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if names.Srcs[0] != tc.first {
			t.Errorf("%s names salvo 1 %q, want %q", tc.path, names.Srcs[0], tc.first)
		}

		// The checksum is what lets a client keep a file it already has.
		if items[1].Uint == 0 {
			t.Errorf("command %d published no checksum", tc.command)
		}
	}
}

func TestFiringASalvoMakesItsRoutes(t *testing.T) {
	s := salvoRouter(t)
	xy := s.p.model.tablePort()
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	body, err := router.FireSalvo{Salvo: 2}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	stored, err := s.p.applyWrite(sess, xy, uint32(router.CmdFireSalvo), 0,
		codec.ModeData, 0, "", body)
	if err != nil {
		t.Fatalf("fire: %v", err)
	}

	fired, err := router.DecodeSalvoFired(stored.Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fired.Salvo != 2 {
		t.Errorf("the reply names salvo %d", fired.Salvo)
	}
	if fired.Routes != 4 {
		t.Errorf("the salvo made %d routes, want 4", fired.Routes)
	}

	// Source 2 is now on every destination of every level.
	m := &xy.router.matrices[0]
	for i := range m.levels {
		for d := range m.levels[i].dests {
			if got := m.levels[i].dests[d].routed.Source; got != 2 {
				t.Errorf("level %d destination %d carries source %d, want 2", i+1, d+1, got)
			}
		}
	}
}

func TestASalvoSkipsWhatItMayNotRoute(t *testing.T) {
	// A salvo is a set of independent routes, and an operator who protected
	// one destination did not mean to disable the button. The protected one is
	// left alone and the count says how many were actually made.
	s := salvoRouter(t)
	xy := s.p.model.tablePort()
	m := &xy.router.matrices[0]
	m.levels[0].dests[0].protect = router.ProtectState{Protected: true}

	made, moved := xy.router.fireSalvo(1)
	if made != 3 {
		t.Errorf("the salvo made %d routes, want the three it was allowed", made)
	}
	if len(moved) != 3 {
		t.Errorf("%d crosspoints reported as moved", len(moved))
	}
	if got := m.levels[0].dests[0].routed.Source; got != 0 {
		t.Errorf("the protected destination was routed to source %d", got)
	}
}

func TestFiringASalvoThatIsNotThere(t *testing.T) {
	// Zero routes, and no way to say why: the specification says "number of
	// routes made or 0 on error" and does not distinguish an empty salvo from
	// one that does not exist.
	s := salvoRouter(t)
	r := s.p.model.routerModel()

	for _, n := range []uint32{0, 99} {
		if made, moved := r.fireSalvo(n); made != 0 || moved != nil {
			t.Errorf("salvo %d made %d routes", n, made)
		}
	}
}

func TestFiringWithARequestThatIsNotOne(t *testing.T) {
	s := salvoRouter(t)
	xy := s.p.model.tablePort()
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	stored, err := s.p.applyWrite(sess, xy, uint32(router.CmdFireSalvo), 0,
		codec.ModeData, 0, "", []byte{0xFF, 0xFF})
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	fired, err := router.DecodeSalvoFired(stored.Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fired.Routes != 0 {
		t.Errorf("a malformed fire made %d routes", fired.Routes)
	}
}

func TestFiringIsPublishedOnBothViews(t *testing.T) {
	// A salvo's routes are the same facts as routes made one at a time, so a
	// panel watching a level has to see them.
	s := salvoRouter(t)
	xy := s.p.model.tablePort()
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	body, _ := router.FireSalvo{Salvo: 3}.AppendTo(nil)
	if _, err := s.p.applyWrite(sess, xy, uint32(router.CmdFireSalvo), 0,
		codec.ModeData, 0, "", body); err != nil {
		t.Fatalf("fire: %v", err)
	}

	lv := &xy.router.matrices[0].levels[0]
	lvPort := s.p.model.levelPort(lv)
	if lvPort == nil {
		t.Fatal("the level is not served")
	}
	if v, _ := lvPort.value(uint32(router.LvlRoute(1))); v.Val != 3 {
		t.Errorf("the level reports source %d on destination 1, want 3", v.Val)
	}
}

func TestAPlantWithNothingToMakeSalvosFrom(t *testing.T) {
	if got := buildSalvos(nil); got != nil {
		t.Errorf("%d salvos from no matrices", len(got))
	}
	if got := buildSalvos([]routerMatrix{{name: "m"}}); got != nil {
		t.Errorf("%d salvos from a matrix with no levels", len(got))
	}
}

func TestASalvoNamingRoutesThatAreNotThere(t *testing.T) {
	// The routes are built from the plant, so they always fit it. A route that
	// did not would otherwise be written over whatever came next.
	s := salvoRouter(t)
	r := s.p.model.routerModel()
	r.salvos = []routerSalvo{{
		name: "impossible",
		routes: []salvoRoute{
			{matrix: 9, level: 1, dest: 1, source: 1},
			{matrix: 1, level: 9, dest: 1, source: 1},
			{matrix: 1, level: 1, dest: 99, source: 1},
			{matrix: 1, level: 1, dest: 1, source: 99},
		},
	}}

	if made, _ := r.fireSalvo(1); made != 0 {
		t.Errorf("a salvo of impossible routes made %d of them", made)
	}
}

func TestTheSalvosArePublishedAsAListAndAButton(t *testing.T) {
	// Nothing in the routing interface makes salvos visible to a panel: the XY
	// grid understands names, counts, routing, protect and reference, and a
	// salvo is none of those. A menu is drawn by every client there is, so
	// they are published as one — a list to choose from and a button to act,
	// which is the vendor's own pattern.
	s := salvoRouter(t)
	xy := s.p.model.tablePort()
	lines := xy.menu(true)

	var group, fire *line
	for i := range lines {
		switch {
		case lines[i].Text == "Salvos":
			group = &lines[i]
		case lines[i].Command == uint32(router.CmdFireSalvo):
			fire = &lines[i]
		}
	}
	if group == nil || group.Param != "#SEL:" {
		t.Fatal("the salvos are not published as a list a panel can choose from")
	}
	if int(group.Step) != len(xy.router.salvos) {
		t.Errorf("the list spans %d lines for %d salvos", group.Step, len(xy.router.salvos))
	}
	if fire == nil {
		t.Fatal("nothing carries the fire command")
	}
	if fire.Style.Kind() != codec.StyleButton || fire.Text != "Fire" {
		t.Errorf("the fire control is %q, a %v", fire.Text, fire.Style.Kind())
	}

	// Each entry selects rather than acts: a list that acted on selection
	// would fire a salvo every time an operator scrolled past one.
	for n := uint32(1); n <= group.Step; n++ {
		l := lines[group.Index+n]
		if l.Command != cmdXYSalvoSelect {
			t.Errorf("salvo %d is on command %d, want the selection", n, l.Command)
		}
		if l.MinRange != int32(n) {
			t.Errorf("salvo %d selects %d", n, l.MinRange)
		}
	}
}

func TestFireActsOnWhatTheListSelected(t *testing.T) {
	// A list selects and a button acts, which is the vendor's own pattern: its
	// routing page has listboxes and a Take button beside them. A list that
	// acted on selection would fire a salvo every time an operator scrolled
	// past one.
	s := salvoRouter(t)
	xy := s.p.model.tablePort()
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	// Choose the third, then press Fire — which sends one, not three.
	if _, err := s.p.applyWrite(sess, xy, cmdXYSalvoSelect, 0,
		codec.ModeValue, 3, "", nil); err != nil {
		t.Fatalf("select: %v", err)
	}
	stored, err := s.p.applyWrite(sess, xy, uint32(router.CmdFireSalvo), 0,
		codec.ModeValue, 1, "", nil)
	if err != nil {
		t.Fatalf("fire: %v", err)
	}

	fired, err := router.DecodeSalvoFired(stored.Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fired.Salvo != 3 {
		t.Errorf("pressing Fire ran salvo %d, want the selected 3", fired.Salvo)
	}
	if fired.Routes != 4 {
		t.Errorf("it made %d routes", fired.Routes)
	}

	// And a client that reads the tables still names the salvo itself.
	body, _ := router.FireSalvo{Salvo: 1}.AppendTo(nil)
	stored, err = s.p.applyWrite(sess, xy, uint32(router.CmdFireSalvo), 0,
		codec.ModeData, 0, "", body)
	if err != nil {
		t.Fatalf("fire by number: %v", err)
	}
	fired, _ = router.DecodeSalvoFired(stored.Data)
	if fired.Salvo != 1 {
		t.Errorf("a request naming salvo 1 ran %d", fired.Salvo)
	}
}

func TestAPressThatNamesNoSalvo(t *testing.T) {
	// Parameters that are not a fire name nothing, whatever is selected.
	s := salvoRouter(t)
	xy := s.p.model.tablePort()

	if _, ok := salvoAsked(xy, codec.Value{Mode: codec.ModeData, Data: []byte{0xFF, 0xFF}}); ok {
		t.Error("parameters that are not a fire were taken for one")
	}

	// With nothing selected and no number, there is nothing to fire.
	bare := newServed(t, routerTree(0, 0)).p.model.tablePort()
	if _, ok := salvoAsked(bare, codec.Value{Mode: codec.ModeValue}); ok {
		t.Error("a press with nothing selected was taken for a salvo")
	}
}

func TestATablesNodeWithNoSalvosOffersNoList(t *testing.T) {
	// A plant with no salvos publishes no list: an empty one is a control that
	// does nothing.
	s := newServed(t, routerTree(0, 0))
	xy := s.p.model.tablePort()
	if xy == nil {
		t.Fatal("no tables node")
	}
	for _, l := range xy.menu(true) {
		if l.Text == "Salvos" {
			t.Error("a plant with no salvos published a salvo list")
		}
	}
}

func TestFiringThroughTheTablesWhenNoListIsPublished(t *testing.T) {
	// A plant with no salvos publishes no list, so the command is not in the
	// menu — and a client that read the tables may still send it, because the
	// tables say how many salvos there are and zero is an answer.
	s := newServed(t, routerTree(0, 0))
	xy := s.p.model.tablePort()
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	body, err := router.FireSalvo{Salvo: 1}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	stored, err := s.p.applyWrite(sess, xy, uint32(router.CmdFireSalvo), 0,
		codec.ModeData, 0, "", body)
	if err != nil {
		t.Fatalf("fire: %v", err)
	}
	fired, err := router.DecodeSalvoFired(stored.Data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fired.Routes != 0 {
		t.Errorf("a plant with no salvos made %d routes", fired.Routes)
	}
}

func TestFiringSaysWhatItDidInWords(t *testing.T) {
	// The routes a salvo makes are on other nodes, so an operator looking at
	// this page has no other way to tell a salvo that fired from one that was
	// refused. Pressing one used to say nothing at all.
	s := salvoRouter(t)
	xy := s.p.model.tablePort()
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	if v, ok := xy.value(cmdXYLastSalvo); !ok || v.Text != "none fired yet" {
		t.Errorf("before anything is fired the readout says %q", v.Text)
	}

	if _, err := s.p.applyWrite(sess, xy, cmdXYSalvoSelect, 0,
		codec.ModeValue, 3, "", nil); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := s.p.applyWrite(sess, xy, uint32(router.CmdFireSalvo), 0,
		codec.ModeValue, 1, "", nil); err != nil {
		t.Fatalf("press: %v", err)
	}

	v, ok := xy.value(cmdXYLastSalvo)
	if !ok {
		t.Fatal("the node says nothing about the salvo it just fired")
	}
	if !strings.Contains(v.Text, "VTR 1") || !strings.Contains(v.Text, "4 route") {
		t.Errorf("the readout says %q, want the salvo's name and what it did", v.Text)
	}

	// A salvo that made nothing says so rather than staying as it was. It is
	// chosen and then fired, because the button says only "fire what is
	// selected".
	if _, err := s.p.applyWrite(sess, xy, cmdXYSalvoSelect, 0,
		codec.ModeValue, 99, "", nil); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := s.p.applyWrite(sess, xy, uint32(router.CmdFireSalvo), 0,
		codec.ModeValue, 1, "", nil); err != nil {
		t.Fatalf("press: %v", err)
	}
	v, _ = xy.value(cmdXYLastSalvo)
	if !strings.Contains(v.Text, "no routes made") {
		t.Errorf("a salvo that is not there says %q", v.Text)
	}
	if !strings.Contains(v.Text, "unknown") {
		t.Errorf("a salvo that is not there is named %q", v.Text)
	}
}

func TestACategorySaysWhatItSelects(t *testing.T) {
	// A panel's XY grid has no category key, so nothing can make it filter by
	// them. What a category is, though, is a set of groups that each match a
	// name at a character index — so choosing one and being told what it picks
	// out is the whole of what a category does, and that it can do here.
	s := newServed(t, &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "frame",
			Children: []canonical.Element{namedMatrix(
				[]string{"CAM 1", "CAM 2", "VTR 1"}, []string{"MON 1", "REC 1"},
			)},
		},
	}})
	xy := s.p.model.tablePort()
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	lines := xy.menu(true)
	var groups, selects *line
	for i := range lines {
		switch lines[i].Text {
		case "Type":
			groups = &lines[i]
		case "Selects":
			selects = &lines[i]
		}
	}
	if groups == nil || groups.Param != "#SEL:" {
		t.Fatal("the groups are not published as a list a panel can choose from")
	}
	if selects == nil {
		t.Fatal("nothing says what a group selects")
	}

	// The groups of this plant, in order: CAM, MON, REC, VTR.
	if int(groups.Step) != 4 {
		t.Fatalf("the list spans %d lines, want one per group", groups.Step)
	}

	// Choosing the first says what it picks out, and it picks out both
	// cameras and nothing else.
	if _, err := s.p.applyWrite(sess, xy, uint32(cmdXYGroupSelect), 0,
		codec.ModeValue, 1, "", nil); err != nil {
		t.Fatalf("choose: %v", err)
	}
	v, _ := xy.value(uint32(cmdXYGroupMatch))
	if !strings.Contains(v.Text, "CAM 1") || !strings.Contains(v.Text, "CAM 2") {
		t.Errorf("the camera group selects %q", v.Text)
	}
	if strings.Contains(v.Text, "VTR") || strings.Contains(v.Text, "MON") {
		t.Errorf("the camera group also selected %q", v.Text)
	}

	// And a group that is not there selects nothing rather than everything.
	if _, err := s.p.applyWrite(sess, xy, uint32(cmdXYGroupSelect), 0,
		codec.ModeValue, 99, "", nil); err != nil {
		t.Fatalf("choose: %v", err)
	}
	if v, _ := xy.value(uint32(cmdXYGroupMatch)); v.Text != "nothing" {
		t.Errorf("a group that is not there selects %q", v.Text)
	}
}

func TestAGroupMatchesAtItsOwnCharacter(t *testing.T) {
	// The search string is looked for at a fixed index rather than anywhere in
	// the name, which is what the start field means.
	g := routerGroup{name: "CAM", search: "CAM", start: 0}
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"CAM 1", true},
		{"STUDIO CAM 1", false},
		{"CA", false},
		{"", false},
	} {
		if got := g.matches(tc.name); got != tc.want {
			t.Errorf("%q matched %v, want %v", tc.name, got, tc.want)
		}
	}

	// A start past the name matches nothing rather than reading past it.
	far := routerGroup{name: "X", search: "X", start: 20}
	if far.matches("CAM 1") {
		t.Error("a group searching past the end of a name matched it")
	}
}

func TestAGroupSelectForACategoryThatIsNotThere(t *testing.T) {
	s := newServed(t, routerTree(4, 4))
	xy := s.p.model.tablePort()

	s.p.showWhatAGroupSelects(context.Background(), nil, xy, codec.Value{
		Command: uint32(cmdXYGroupSelect + 99), Mode: codec.ModeValue, Val: 1,
	})
}

func TestANodeThatIsNotARouterHasNoCategories(t *testing.T) {
	// The page builder asks every node it draws, and a card is not a router.
	s := newServed(t, testTree())
	if got := categoriesOf(s.p.model.port(firstCardPort)); got != nil {
		t.Errorf("a card published %d categories", len(got))
	}
}

func TestAGroupThatSelectsNothing(t *testing.T) {
	// A group whose search matches no name in the plant says so, rather than
	// showing an empty line an operator would read as a fault.
	s := newServed(t, &canonical.Export{Root: &canonical.Node{
		Header: canonical.Header{
			Number: 1, Identifier: "frame",
			Children: []canonical.Element{namedMatrix(
				[]string{"CAM 1", "VTR 1"}, []string{"MON 1", "REC 1"},
			)},
		},
	}})
	r := s.p.model.routerModel()
	c := &r.categories[0]
	c.groups = append(c.groups, routerGroup{name: "XYZ", search: "XYZ"})

	if got := c.selects(r, len(c.groups)); got != "nothing" {
		t.Errorf("a group matching no name selects %q", got)
	}

	// And a plant names a source once however many levels carry it.
	if got := c.selects(r, 1); strings.Count(got, "CAM 1") != 1 {
		t.Errorf("the camera group lists CAM 1 more than once: %q", got)
	}
}

func TestFiringByNumberWhenNothingIsSelected(t *testing.T) {
	// A client that is not a panel may write the number without parameters.
	// With no selection to fall back on, that number is the request.
	s := newServed(t, routerTree(0, 0))
	xy := s.p.model.tablePort()

	got, ok := salvoAsked(xy, codec.Value{Mode: codec.ModeValue, Val: 2})
	if !ok || got != 2 {
		t.Errorf("a numeric fire read as %d, %v", got, ok)
	}
}

func TestTheClientThatCausedAChangeIsToldAboutIt(t *testing.T) {
	// A client is excluded from a push only for the command it wrote, because
	// the reply already carried that one. A different command that changed as
	// a consequence has to reach it too — it has no other way to learn of it.
	//
	// Got wrong, this is invisible from the provider's side and obvious from
	// the panel's: the one session not told what a salvo did was the session
	// that pressed Fire, which sat there showing "none fired yet" while the
	// node held the answer.
	s := salvoRouter(t)
	xy := s.p.model.tablePort()
	sess := s.open(xy.number, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	if _, err := sess.Do(context.Background(), codec.MsgBkChnReady,
		[]byte{codec.BackChannelFutureOnly}); err != nil {
		t.Fatalf("back channel: %v", err)
	}

	watching := s.p.slotSubscribers(xy.number, codec.SvcControl)
	if len(watching) != 1 {
		t.Fatalf("%d sessions watching the node", len(watching))
	}
	writer := watching[0].s

	// The rule, stated as the two lists differ by exactly the writer.
	if got := s.p.slotSubscribers(xy.number, codec.SvcControl); len(got) != 1 {
		t.Fatalf("a push to everyone reaches %d sessions", len(got))
	}

	// What publishText and publishRouterValue send goes to everyone, and the
	// writer is only left out of a push for the command it wrote.
	before := len(xy.values)
	s.p.publishText(context.Background(), writer, xy, cmdXYLastSalvo, "1 All CAM 1: 4 route(s) made")
	if len(xy.values) != before {
		t.Error("publishing a side effect added a command rather than changing one")
	}
	v, ok := xy.value(cmdXYLastSalvo)
	if !ok || !strings.Contains(v.Text, "route(s) made") {
		t.Errorf("the node holds %q", v.Text)
	}
}
