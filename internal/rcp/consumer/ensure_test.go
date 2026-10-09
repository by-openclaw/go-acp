package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dhs/internal/consumer/compliance"
	"dhs/internal/rcp/codec"
)

// routeMaster is an in-memory RCP server that behaves as Cerebrum 2.5.3
// was measured to on 2026-10-09:
//
//   - an id is a position: deleting an IO moves every IO after it down;
//   - a virtual is a pair — creating, renaming or deleting the source
//     does the same to a destination;
//   - a single IO answers under the collection's plural name;
//   - writes answer 202 (routes: 200) with nothing but the reqid;
//   - a federation create is accepted and creates nothing;
//   - a mnemonic row is written under its table key.
type routeMaster struct {
	mu     sync.Mutex
	ios    map[codec.Collection][]*codec.IO
	routes map[string]map[int]string // destination mnemonic → level → source mnemonic
	writes []string
}

func newRouteMaster() *routeMaster {
	return &routeMaster{ios: map[codec.Collection][]*codec.IO{}, routes: map[string]map[int]string{}}
}

func (rm *routeMaster) add(col codec.Collection, mnemonic string, virtual bool) *codec.IO {
	io := &codec.IO{Mnemonic: codec.Mnemonic{Original: mnemonic}, Virtual: virtual, Levels: map[string]codec.IOLevel{}}
	rm.ios[col] = append(rm.ios[col], io)
	return io
}

func (rm *routeMaster) idOf(col codec.Collection, mnemonic string) int64 {
	for i, io := range rm.ios[col] {
		if io.Mnemonic.Original == mnemonic {
			return int64(i + 1)
		}
	}
	return 0
}

func (rm *routeMaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	reqid := r.Header.Get("reqid")
	answer := func(status int, extra string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"reqid":`+reqid+extra+`}`)
	}
	fail := func(status int, msg string) {
		answer(status, `,"error":{"code":1,"message":"`+msg+`"}`)
	}
	body, _ := io.ReadAll(r.Body)
	path := strings.TrimPrefix(r.URL.Path, "/v2")
	if r.Method != http.MethodGet && path != "/login" {
		rm.writes = append(rm.writes, r.Method+" "+path+" "+string(body))
	}

	switch {
	case path == "/login":
		answer(200, `,"login":{"token":"t","websocketPort":9081}`)
		return
	case strings.HasPrefix(path, "/routemaster/"):
		rm.serveIO(r.Method, strings.Split(strings.TrimPrefix(path, "/routemaster/"), "/"), body, answer, fail)
		return
	case path == "/devices/Cerebrum/0/routers/routes":
		rm.serveRoutes(r, body, answer, fail)
		return
	case strings.HasPrefix(path, "/devices/Cerebrum/0/routers/source-mnemonics"):
		rm.serveMnemonics(r.Method, path, body, answer, fail)
		return
	}
	fail(404, "no such path")
}

func (rm *routeMaster) serveIO(method string, parts []string, body []byte, answer func(int, string), fail func(int, string)) {
	col, err := codec.ParseCollection(parts[0])
	if err != nil {
		fail(404, "Unknown RouteMaster collection")
		return
	}
	list := rm.ios[col]
	if len(parts) == 1 {
		switch method {
		case http.MethodGet:
			ids := make([]string, len(list))
			for i := range list {
				ids[i] = strconv.Itoa(i + 1)
			}
			raw, _ := json.Marshal(ids)
			answer(200, `,"ids":`+string(raw))
		case http.MethodPost:
			var u codec.Update
			if err := json.Unmarshal(body, &u); err != nil || u.TieLineGroup != nil {
				fail(400, "Unknown field in the create request body")
				return
			}
			if !col.IsFederation() {
				name := ""
				if u.Mnemonic != nil {
					name = *u.Mnemonic
				}
				virtual := u.Virtual != nil && *u.Virtual
				rm.add(col, name, virtual)
				if virtual {
					other := codec.Destinations
					if col == codec.Destinations {
						other = codec.Sources
					}
					rm.add(other, name, true)
				}
			}
			answer(202, "")
		}
		return
	}
	n, _ := strconv.Atoi(parts[1])
	if n < 1 || n > len(list) {
		fail(404, "RouteMaster IO not found")
		return
	}
	cur := list[n-1]
	switch method {
	case http.MethodGet:
		out := *cur
		out.ID = int64(n)
		raw, _ := json.Marshal(out)
		answer(200, `,"`+string(col)+`":`+string(raw))
	case http.MethodPatch:
		var u codec.Update
		if err := json.Unmarshal(body, &u); err != nil || u.TieLineGroup != nil {
			fail(400, "Unknown field in the update request body")
			return
		}
		if u.Mnemonic != nil {
			if cur.Virtual {
				for _, other := range []codec.Collection{codec.Sources, codec.Destinations} {
					for _, o := range rm.ios[other] {
						if o.Virtual && o.Mnemonic.Original == cur.Mnemonic.Original && o != cur {
							o.Mnemonic.Original = *u.Mnemonic
						}
					}
				}
			}
			cur.Mnemonic.Original = *u.Mnemonic
		}
		if u.TieLineInhibit != nil {
			cur.TieLineInhibit = *u.TieLineInhibit
		}
		// "virtual" on an update is accepted and ignored, as measured.
		for name, l := range u.Levels {
			id, _ := codec.KeyID(name)
			have := cur.Levels[name]
			have.ID = int(id)
			if l.Clear != nil && *l.Clear {
				have = codec.IOLevel{ID: int(id), TagsInherited: true, Device: &codec.Device{Name: "Snell SW-P-08"}}
			}
			if l.Tags != nil {
				have.Tags, have.TagsInherited = *l.Tags, false
			}
			if l.TagsInherit != nil && *l.TagsInherit {
				have.Tags, have.TagsInherited = nil, true
			}
			if d := l.Device; d != nil {
				if have.Device == nil {
					have.Device = &codec.Device{Name: "Snell SW-P-08"}
				}
				if d.IO != nil {
					have.Device.IO = *d.IO
				}
				if d.Inhibit != nil {
					have.Device.Inhibit = *d.Inhibit
				}
			}
			cur.Levels[name] = have
		}
		answer(202, "")
	case http.MethodDelete:
		rm.ios[col] = append(list[:n-1:n-1], list[n:]...)
		if cur.Virtual {
			other := codec.Destinations
			if col == codec.Destinations {
				other = codec.Sources
			}
			for i, o := range rm.ios[other] {
				if o.Virtual && o.Mnemonic.Original == cur.Mnemonic.Original {
					rm.ios[other] = append(rm.ios[other][:i:i], rm.ios[other][i+1:]...)
					break
				}
			}
		}
		answer(202, "")
	}
}

func (rm *routeMaster) serveRoutes(r *http.Request, body []byte, answer func(int, string), fail func(int, string)) {
	dsts := rm.ios[codec.Destinations]
	if r.Method == http.MethodGet {
		table := codec.Routes{}
		for i, d := range dsts {
			id := int64(i + 1)
			if want := r.URL.Query().Get("DestinationId"); want != "" && want != strconv.FormatInt(id, 10) {
				continue
			}
			rd := codec.RouteDest{Name: d.Mnemonic.Original, Levels: map[string]codec.RouteLevel{}}
			for level := 1; level <= 2; level++ {
				src := rm.idOf(codec.Sources, rm.routes[d.Mnemonic.Original][level])
				if level == 2 && src == 0 {
					src = 4294967292 // a reserved id, not a source
				}
				rd.Levels[codec.LevelKey(level)] = codec.RouteLevel{Source: codec.RouteSource{ID: src}}
			}
			table[codec.DestKey(id)] = rd
		}
		raw, _ := json.Marshal(table)
		answer(200, `,"routes":`+string(raw))
		return
	}
	var take map[string]struct {
		Source *codec.RouteSource          `json:"source"`
		Levels map[string]codec.RouteLevel `json:"levels"`
	}
	if err := json.Unmarshal(body, &take); err != nil {
		fail(400, "Error parsing request")
		return
	}
	for key, t := range take {
		id, _ := codec.KeyID(key)
		if id < 1 || int(id) > len(dsts) {
			fail(404, "no such destination")
			return
		}
		name := dsts[id-1].Mnemonic.Original
		if rm.routes[name] == nil {
			rm.routes[name] = map[int]string{}
		}
		srcName := func(sid int64) string {
			if sid < 1 || int(sid) > len(rm.ios[codec.Sources]) {
				return ""
			}
			return rm.ios[codec.Sources][sid-1].Mnemonic.Original
		}
		if t.Source != nil {
			rm.routes[name][1] = srcName(t.Source.ID) // the destination exists on level 1 only
		}
		for lk, l := range t.Levels {
			lid, _ := codec.KeyID(lk)
			rm.routes[name][int(lid)] = srcName(l.Source.ID)
		}
	}
	answer(200, "")
}

func (rm *routeMaster) serveMnemonics(method, path string, body []byte, answer func(int, string), fail func(int, string)) {
	srcs := rm.ios[codec.Sources]
	if method == http.MethodGet {
		out := map[string]codec.MnemonicEntry{}
		for i, s := range srcs {
			out["src_"+strconv.Itoa(i+1)] = codec.MnemonicEntry{Original: s.Mnemonic.Original}
		}
		raw, _ := json.Marshal(out)
		answer(200, `,"sourceMnemonics":`+string(raw))
		return
	}
	id := path[strings.LastIndexByte(path, '/')+1:]
	var rows map[string]codec.MnemonicUpdate
	n, _ := strconv.Atoi(id)
	row, keyed := rows["src_"+id]
	if json.Unmarshal(body, &rows) == nil {
		row, keyed = rows["src_"+id]
	}
	if !keyed || row.Original == nil || n < 1 || n > len(srcs) {
		fail(400, "Error parsing request")
		return
	}
	srcs[n-1].Mnemonic.Original = *row.Original
	answer(200, "")
}

func session(t *testing.T, rm *routeMaster) *Client {
	t.Helper()
	srv := httptest.NewServer(rm)
	t.Cleanup(srv.Close)
	c := New(Options{Host: strings.TrimPrefix(srv.URL, "http://"), Profile: &compliance.Profile{}})
	if err := c.Login(context.Background(), "u", "p"); err != nil {
		t.Fatal(err)
	}
	return c
}

func fullPlan() Plan {
	yes, io11 := true, 11
	camera := []string{"Camera", "HD"}
	return Plan{
		Sources: []PlanIO{
			{Mnemonic: "DHS-SRC-0001", Levels: map[string]codec.LevelUpdate{"level_1": {Tags: &camera, Device: &codec.DeviceUpdate{IO: &io11}}}},
			{Mnemonic: "DHS-SRC-0002", TieLineInhibit: &yes},
		},
		Destinations:           []PlanIO{{Mnemonic: "DHS-DST-0001"}},
		Virtuals:               []PlanIO{{Mnemonic: "DHS-VIRT-0001"}},
		FederationSources:      []PlanIO{{Mnemonic: "DHS-FED-0001"}},
		FederationDestinations: []PlanIO{{Mnemonic: "DHS-FED-D001"}},
		Routes: []PlanRoute{
			{Destination: "DHS-DST-0001", Source: "DHS-SRC-0001"},
			{Destination: "DHS-VIRT-0001", Source: "DHS-SRC-0002", Level: 2},
		},
	}
}

var fast = EnsureOptions{FederationOptional: true, Settle: 300 * time.Millisecond}

func actions(r Report) string {
	var b strings.Builder
	for _, c := range r.Changes {
		fmt.Fprintf(&b, "%s %s; ", c.Action, c.Target)
	}
	return b.String()
}

func TestEnsureMakesThePlanAndTheSecondRunChangesNothing(t *testing.T) {
	rm := newRouteMaster()
	rm.add(codec.Sources, "Src 1", false) // what was there before
	rm.add(codec.Destinations, "Dest 1", false)
	c := session(t, rm)
	ctx := context.Background()

	first, err := c.Ensure(ctx, fullPlan(), fast)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	want := "create sources DHS-SRC-0001; create sources DHS-SRC-0002; create destinations DHS-DST-0001; create virtuals DHS-VIRT-0001; " +
		"take route DHS-DST-0001 <- DHS-SRC-0001; take route DHS-VIRT-0001 <- DHS-SRC-0002 level 2; "
	if got := actions(first); got != want {
		t.Errorf("first run:\n got %s\nwant %s", got, want)
	}
	if len(first.Pending) != 2 {
		t.Errorf("pending = %v, want the two federation entries", first.Pending)
	}

	// The server holds what the plan states.
	src := rm.ios[codec.Sources]
	if len(src) != 4 || src[1].Levels["level_1"].Device.IO != 11 || !src[2].TieLineInhibit || !src[3].Virtual {
		t.Errorf("sources = %+v", src)
	}
	if rm.routes["DHS-DST-0001"][1] != "DHS-SRC-0001" || rm.routes["DHS-VIRT-0001"][2] != "DHS-SRC-0002" {
		t.Errorf("routes = %v", rm.routes)
	}
	if len(rm.ios[codec.Destinations]) != 3 { // Dest 1, DHS-DST-0001, the virtual's other half
		t.Errorf("destinations = %d, want 3 — one virtual create makes one destination", len(rm.ios[codec.Destinations]))
	}

	writes := len(rm.writes)
	second, err := c.Ensure(ctx, fullPlan(), fast)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(second.Changes) != 0 {
		t.Errorf("second run changed: %s", actions(second))
	}
	// The federation creates are tried again — that is how a federation
	// that has since appeared is noticed — and nothing else is written.
	for _, w := range rm.writes[writes:] {
		if !strings.HasPrefix(w, "POST /routemaster/federation-") {
			t.Errorf("second run wrote: %s", w)
		}
	}
}

func TestEnsureCheckWritesNothing(t *testing.T) {
	rm := newRouteMaster()
	c := session(t, rm)
	opts := fast
	opts.Check = true
	r, err := c.Ensure(context.Background(), fullPlan(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Changes) != 8 { // 4 local creates, 2 federation creates, 2 takes
		t.Errorf("would change %d: %s", len(r.Changes), actions(r))
	}
	if len(rm.writes) != 0 {
		t.Errorf("a check wrote: %v", rm.writes)
	}
}

func TestEnsureUpdatesOnlyWhatDiffers(t *testing.T) {
	rm := newRouteMaster()
	c := session(t, rm)
	ctx := context.Background()
	if _, err := c.Ensure(ctx, fullPlan(), fast); err != nil {
		t.Fatal(err)
	}
	// Somebody changes one thing behind our back.
	rm.mu.Lock()
	rm.ios[codec.Sources][1].TieLineInhibit = false
	rm.writes = nil
	rm.mu.Unlock()

	r, err := c.Ensure(ctx, fullPlan(), fast)
	if err != nil {
		t.Fatal(err)
	}
	if got := actions(r); got != "update sources DHS-SRC-0002; " {
		t.Errorf("changes = %s", got)
	}
	var patches []string
	for _, w := range rm.writes {
		if strings.HasPrefix(w, "PATCH /routemaster/") {
			patches = append(patches, w)
		}
	}
	if len(patches) != 1 || patches[0] != `PATCH /routemaster/sources/2 {"tieLineInhibit":true}` {
		t.Errorf("patches = %v, want the one field on the one IO", patches)
	}
}

func TestEnsureAbsentRemovesThePlanThroughTheRenumbering(t *testing.T) {
	rm := newRouteMaster()
	rm.add(codec.Sources, "Src 1", false)
	rm.add(codec.Destinations, "Dest 1", false)
	c := session(t, rm)
	ctx := context.Background()
	if _, err := c.Ensure(ctx, fullPlan(), fast); err != nil {
		t.Fatal(err)
	}
	rm.add(codec.Sources, "Somebody else's", false) // after ours: it will be renumbered

	opts := fast
	opts.Absent = true
	r, err := c.Ensure(ctx, fullPlan(), opts)
	if err != nil {
		t.Fatalf("absent: %v", err)
	}
	if got, want := actions(r), "delete sources DHS-SRC-0001; delete sources DHS-SRC-0002; delete destinations DHS-DST-0001; delete virtuals DHS-VIRT-0001; "; got != want {
		t.Errorf("absent:\n got %s\nwant %s", got, want)
	}
	var left []string
	for _, col := range []codec.Collection{codec.Sources, codec.Destinations} {
		for _, io := range rm.ios[col] {
			left = append(left, io.Mnemonic.Original)
		}
	}
	if strings.Join(left, ",") != "Src 1,Somebody else's,Dest 1" {
		t.Errorf("left on the server: %v — only what the plan named may go", left)
	}

	again, err := c.Ensure(ctx, fullPlan(), opts)
	if err != nil || len(again.Changes) != 0 {
		t.Errorf("second absent run: %s, %v", actions(again), err)
	}
}

func TestEnsureFailsOnAFederationThatIsNotThereUnlessToldItMayBeMissing(t *testing.T) {
	rm := newRouteMaster()
	c := session(t, rm)
	strict := EnsureOptions{Settle: 200 * time.Millisecond}
	_, err := c.Ensure(context.Background(), Plan{FederationSources: []PlanIO{{Mnemonic: "F1"}}}, strict)
	if err == nil || !strings.Contains(err.Error(), "created nothing") {
		t.Errorf("err = %v, want the create that created nothing", err)
	}
}

func TestEnsureFailsWhenAnAcceptedUpdateIsNotApplied(t *testing.T) {
	rm := newRouteMaster()
	rm.add(codec.Destinations, "D1", false)
	c := session(t, rm)
	yes := true
	// The fake, like the server, keeps no "disconnect": the write is
	// accepted and never shows.
	plan := Plan{Destinations: []PlanIO{{Mnemonic: "D1", Levels: map[string]codec.LevelUpdate{"level_1": {Device: &codec.DeviceUpdate{Disconnect: &yes}}}}}}
	_, err := c.Ensure(context.Background(), plan, EnsureOptions{Settle: 200 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "did not apply: level_1") {
		t.Errorf("err = %v, want accepted-and-not-applied on level_1", err)
	}
}

func TestAPlanThatCannotBeAppliedIsRefusedBeforeAnythingIsSent(t *testing.T) {
	rm := newRouteMaster()
	c := session(t, rm)
	yes := true
	for name, plan := range map[string]Plan{
		"no mnemonic":         {Sources: []PlanIO{{}}},
		"named twice":         {Sources: []PlanIO{{Mnemonic: "A"}, {Mnemonic: "A"}}},
		"virtual with levels": {Virtuals: []PlanIO{{Mnemonic: "V", TieLineInhibit: &yes}}},
		"route without ends":  {Routes: []PlanRoute{{Destination: "D"}}},
	} {
		if _, err := c.Ensure(context.Background(), plan, fast); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(rm.writes) != 0 {
		t.Errorf("writes = %v", rm.writes)
	}
}

func TestRoutesTakeAndMnemonics(t *testing.T) {
	rm := newRouteMaster()
	rm.add(codec.Sources, "S1", false)
	rm.add(codec.Sources, "S2", false)
	rm.add(codec.Destinations, "D1", false)
	c := session(t, rm)
	ctx := context.Background()

	if err := c.Take(ctx, RouteMaster, 1, 2, 0); err != nil {
		t.Fatal(err)
	}
	if got := rm.writes[len(rm.writes)-1]; got != `PATCH /devices/Cerebrum/0/routers/routes {"dest_1":{"source":{"id":2}}}` {
		t.Errorf("take all levels = %s", got)
	}
	if err := c.Take(ctx, RouteMaster, 1, 1, 2); err != nil {
		t.Fatal(err)
	}
	if got := rm.writes[len(rm.writes)-1]; got != `PATCH /devices/Cerebrum/0/routers/routes {"dest_1":{"levels":{"destLevel_2":{"source":{"id":1}}}}}` {
		t.Errorf("take one level = %s", got)
	}
	table, err := c.Routes(ctx, RouteMaster, 1)
	if err != nil {
		t.Fatal(err)
	}
	d := table[codec.DestKey(1)]
	if d.Name != "D1" || d.Levels[codec.LevelKey(1)].Source.ID != 2 || d.Levels[codec.LevelKey(2)].Source.ID != 1 {
		t.Errorf("routes = %+v", d)
	}
	if err := c.Take(ctx, RouteMaster, 0, 1, 0); err == nil {
		t.Error("a take without a destination was sent")
	}

	name := "CAM 2"
	if err := c.SetMnemonic(ctx, RouteMaster, codec.SourceMnemonics, 2, codec.MnemonicUpdate{Original: &name}); err != nil {
		t.Fatal(err)
	}
	// Keyed by the table key: the bare row of the document is refused.
	if got := rm.writes[len(rm.writes)-1]; got != `PATCH /devices/Cerebrum/0/routers/source-mnemonics/2 {"src_2":{"originalMnemonic":"CAM 2"}}` {
		t.Errorf("set mnemonic = %s", got)
	}
	rows, err := c.Mnemonics(ctx, RouteMaster, codec.SourceMnemonics)
	if err != nil || rows[2].Original != "CAM 2" || rows[1].Original != "S1" {
		t.Errorf("mnemonics = %v, %v", rows, err)
	}
	if err := c.SetMnemonic(ctx, RouteMaster, codec.SourceMnemonics, 2, codec.MnemonicUpdate{}); err == nil {
		t.Error("an empty mnemonic update was sent")
	}
}

func TestRoutedToIgnoresLevelsThatHoldNoSource(t *testing.T) {
	known := map[int64]bool{3: true, 7: true}
	dest := func(ids ...int64) codec.RouteDest {
		d := codec.RouteDest{Levels: map[string]codec.RouteLevel{}}
		for i, id := range ids {
			d.Levels[codec.LevelKey(i+1)] = codec.RouteLevel{Source: codec.RouteSource{ID: id}}
		}
		return d
	}
	for name, tc := range map[string]struct {
		d     codec.RouteDest
		level int
		want  bool
	}{
		"routed on its one level":         {dest(7, 0, 0, 0), 0, true},
		"a reserved id is not a source":   {dest(7, 7, 7, 4294967292), 0, true},
		"another source on one level":     {dest(7, 3, 0, 0), 0, false},
		"routed nowhere":                  {dest(0, 0, 0, 0), 0, false},
		"the asked level":                 {dest(3, 7, 0, 0), 2, true},
		"the asked level, another source": {dest(7, 3, 0, 0), 2, false},
	} {
		if got := routedTo(tc.d, 7, tc.level, known); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}
