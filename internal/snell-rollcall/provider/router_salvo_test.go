package rollcall

import (
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
