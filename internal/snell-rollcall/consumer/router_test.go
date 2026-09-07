package rollcall

import (
	"context"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/router"
)

// The shape the vendor Centra has: two matrices, the first with two levels and
// the second with three, ten of everything. Small enough to read in a test and
// laid out by the same arithmetic a real controller uses.
func testRouterShape() []fakeMatrix {
	return []fakeMatrix{
		{
			name:      "R1 Matrix 1",
			srcAssocs: 10, dstAssocs: 10,
			levels: []fakeLevel{
				{name: "R1M1 Level 1", srcs: 10, dsts: 10},
				{name: "R1M1 Level 2", srcs: 10, dsts: 10},
			},
		},
		{
			name:      "R1 Matrix 2",
			srcAssocs: 10, dstAssocs: 10,
			levels: []fakeLevel{
				{name: "R1M2 Level 1", srcs: 10, dsts: 10},
				{name: "R1M2 Level 2", srcs: 10, dsts: 10},
				{name: "R1M2 Level 3", srcs: 10, dsts: 10},
			},
		},
	}
}

// routerHarness is a device whose port 2 is a router and whose other ports are
// ordinary cards, which is the arrangement a controller presents.
func routerHarness(t *testing.T, tweak func(*fakeRouter)) (*harness, *RouterInterface) {
	t.Helper()

	var fake *fakeRouter
	h := newHarness(t, func(d *device) {
		d.ports = 3
		d.setMenu(1, testMenu())
		fake = newFakeRouter(2, testRouterShape())
		if tweak != nil {
			tweak(fake)
		}
		d.routers[2] = fake
		// The controller's bulk files are served by its own file service,
		// which is how a client fetches names without a command per name.
		for name, body := range fake.files {
			d.files[name] = body
		}
	})

	r, err := h.plugin.RouterAt(context.Background(), 2)
	if err != nil {
		t.Fatalf("RouterAt: %v", err)
	}
	return h, r
}

func TestRouterModelIsReadByArithmetic(t *testing.T) {
	_, r := routerHarness(t, nil)

	if r.Version != router.VersionRouteErrors {
		t.Errorf("interface version = %d", r.Version)
	}
	if r.Name != "Fake Router" {
		t.Errorf("router name = %q", r.Name)
	}
	if len(r.Matrices) != 2 {
		t.Fatalf("%d matrices, want 2", len(r.Matrices))
	}

	m1 := r.Matrices[0]
	if m1.Name != "R1 Matrix 1" || m1.Controller != 1 {
		t.Errorf("matrix 1 = %q controller %d", m1.Name, m1.Controller)
	}
	if len(m1.Levels) != 2 || len(r.Matrices[1].Levels) != 3 {
		t.Errorf("levels = %d and %d, want 2 and 3",
			len(m1.Levels), len(r.Matrices[1].Levels))
	}
	if m1.SrcAssocs.Count != 10 || m1.DstAssocs.Count != 10 {
		t.Errorf("associations = %d/%d", m1.SrcAssocs.Count, m1.DstAssocs.Count)
	}

	lv := m1.Levels[0]
	if lv.Name != "R1M1 Level 1" || lv.Srcs.Count != 10 || lv.Dsts.Count != 10 {
		t.Errorf("level 1 = %q %d/%d", lv.Name, lv.Srcs.Count, lv.Dsts.Count)
	}
	// The steps are the table sizes the controller published, and reading them
	// wrongly is what makes a client address the next entity's fields.
	if lv.Srcs.Step != router.SrcTableSize || lv.Dsts.Step != router.DstTableSize {
		t.Errorf("steps = %d/%d, want %d/%d",
			lv.Srcs.Step, lv.Dsts.Step, router.SrcTableSize, router.DstTableSize)
	}

	// Every bulk file is named with a checksum, which is what makes caching
	// possible at all.
	if lv.Names8.Empty() || lv.Names.Empty() || lv.MCData.Empty() {
		t.Errorf("level 1 published no names files: %+v", lv)
	}
	if m1.AssocMappings.Name != `RC_Files\AssocMap_1.dat` {
		t.Errorf("mappings file = %q", m1.AssocMappings.Name)
	}
	if r.Salvos != 4 || r.Devices != 8 {
		t.Errorf("salvos = %d, devices = %d", r.Salvos, r.Devices)
	}
}

func TestFindRouterProbesEveryNode(t *testing.T) {
	h, _ := routerHarness(t, nil)

	// Nothing in a device list says which node is the panel driver: on the
	// vendor Centra the matrices are their own nodes and neither serves the
	// interface. Probing is safe because a node without one refuses.
	r, err := h.plugin.FindRouter(context.Background())
	if err != nil {
		t.Fatalf("FindRouter: %v", err)
	}
	if r.Slot != 2 {
		t.Errorf("found the router on slot %d, want 2", r.Slot)
	}

	// The address names the node and nothing else. A session index would make
	// two runs against an unchanged router differ — the same panel reported
	// itself as :010 on one run and :026 on the next — so the printed address
	// carries none.
	if r.Addr.Index != codec.IndexUnknown {
		t.Errorf("addr = %s; a router address carries no session index", r.Addr)
	}
	if r.Addr.Unit == 0 && r.Addr.Port == 0 {
		t.Errorf("addr = %s names no node", r.Addr)
	}
}

func TestFindRouterOnADeviceWithNone(t *testing.T) {
	h := newHarness(t, func(d *device) { d.ports = 2 })

	if _, err := h.plugin.FindRouter(context.Background()); err == nil {
		t.Error("a frame of ordinary cards should have no routing interface")
	}
}

func TestARouterNeedsTheLongStringGeneration(t *testing.T) {
	// The command set exists only on the 32-bit generation: a matrix of any
	// useful size needs more command numbers than the 16-bit field holds.
	h := newHarness(t, func(d *device) {
		d.services = codec.SvcMenus | codec.SvcControl | codec.SvcDisplay
		d.ports = 2
	})

	_, err := h.plugin.RouterAt(context.Background(), 1)
	if err == nil {
		t.Fatal("a 16-bit session should have no routing interface")
	}
	if _, err := h.plugin.FindRouter(context.Background()); err == nil {
		t.Error("FindRouter should have found nothing")
	}
}

func TestReadingACrosspoint(t *testing.T) {
	h, r := routerHarness(t, nil)

	xpt, err := h.plugin.Route(context.Background(), r, 1, 1, 3)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if xpt.Dest.Matrix != 1 || xpt.Dest.Level != 1 || xpt.Dest.Source != 3 {
		t.Errorf("destination = %s", xpt.Dest)
	}
	if xpt.Source.Matrix != 1 || xpt.Source.Level != 1 || xpt.Source.Source != 1 {
		t.Errorf("routed source = %s, want m1/l1/s1", xpt.Source)
	}
}

func TestSettingACrosspoint(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	src := router.SourcePin{Matrix: 1, Level: 1, Source: 5}
	reply, err := h.plugin.SetRoute(ctx, r, 1, 1, 2, src, 0)
	if err != nil {
		t.Fatalf("SetRoute: %v", err)
	}

	// The reply carries the value from *before* the change, with the result
	// appended. A client that reads the new crosspoint out of it shows a stale
	// one until something else refreshes.
	if reply.Source.Source != 1 {
		t.Errorf("the reply carried %s; it should carry what was routed before", reply.Source)
	}
	if !reply.HasResult || !reply.Result.OK() {
		t.Errorf("result = %s present=%v", reply.Result, reply.HasResult)
	}

	// The new value is there when read again.
	after, err := h.plugin.Route(ctx, r, 1, 1, 2)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if after.Source != src {
		t.Errorf("after the set the destination has %s, want %s", after.Source, src)
	}
}

func TestATallyFollowsTielines(t *testing.T) {
	// Setting a source can read back as a different one: where a route crosses
	// a tieline the controller reports the far end, because the pin is the
	// final upstream source rather than what was asked for.
	far := router.SourcePin{Matrix: 2, Level: 1, Source: 1}
	h, r := routerHarness(t, func(f *fakeRouter) {
		near := router.SourcePin{Matrix: 1, Level: 1, Source: 7}
		f.tieline[near.Pack()] = far.Pack()
	})
	ctx := context.Background()

	asked := router.SourcePin{Matrix: 1, Level: 1, Source: 7}
	if _, err := h.plugin.SetRoute(ctx, r, 1, 1, 1, asked, 0); err != nil {
		t.Fatalf("SetRoute: %v", err)
	}

	after, err := h.plugin.Route(ctx, r, 1, 1, 1)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if after.Source != far {
		t.Errorf("tally = %s, want the upstream %s", after.Source, far)
	}
}

func TestWatchingRoutes(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	changes := make(chan Crosspoint, 8)
	if err := h.plugin.WatchRoutes(ctx, r, func(x Crosspoint) {
		select {
		case changes <- x:
		default:
		}
	}); err != nil {
		t.Fatalf("WatchRoutes: %v", err)
	}

	src := router.SourcePin{Matrix: 1, Level: 2, Source: 4}
	if _, err := h.plugin.SetRoute(ctx, r, 1, 2, 6, src, 0); err != nil {
		t.Fatalf("SetRoute: %v", err)
	}

	select {
	case got := <-changes:
		// The push carries a command number, and turning it back into a
		// destination is the arithmetic in reverse.
		if got.Dest.Matrix != 1 || got.Dest.Level != 2 || got.Dest.Source != 6 {
			t.Errorf("tally named %s, want matrix 1 level 2 destination 6", got.Dest)
		}
		if got.Source != src {
			t.Errorf("tally carried %s, want %s", got.Source, src)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no tally arrived")
	}
}

func TestWatchingRoutesNeedsACallback(t *testing.T) {
	h, r := routerHarness(t, nil)

	if err := h.plugin.WatchRoutes(context.Background(), r, nil); err == nil {
		t.Error("watching without a callback should fail")
	}
}

func TestAPushThatIsNotACrosspointIsIgnored(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	changes := make(chan Crosspoint, 8)
	if err := h.plugin.WatchRoutes(ctx, r, func(x Crosspoint) {
		select {
		case changes <- x:
		default:
		}
	}); err != nil {
		t.Fatalf("WatchRoutes: %v", err)
	}

	// A router pushes on the same channel as everything else. A display line,
	// a name, a value the model does not place: none of them is a crosspoint
	// and none of them is a fault.
	h.device.push(2, codec.MsgDispData, make([]byte, codec.DispSize))
	h.device.push(2, codec.MsgRetValue, []byte{0x01})
	name, err := codec.Value{Command: 121, Mode: codec.ModeString, Text: "x"}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	h.device.push(2, codec.MsgRetValue, name)

	// Then a real one, which must still arrive: the barrier proves the three
	// before it were taken and dropped rather than merely slow.
	src := router.SourcePin{Matrix: 2, Level: 3, Source: 2}
	if _, err := h.plugin.SetRoute(ctx, r, 2, 3, 4, src, 0); err != nil {
		t.Fatalf("SetRoute: %v", err)
	}

	select {
	case got := <-changes:
		if got.Dest.Matrix != 2 || got.Dest.Level != 3 || got.Dest.Source != 4 {
			t.Errorf("the first tally was %s, want matrix 2 level 3 destination 4", got.Dest)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no tally arrived")
	}
}

func TestReadingEveryCrosspointOfALevel(t *testing.T) {
	h, r := routerHarness(t, nil)

	xpts, err := h.plugin.Routes(context.Background(), r, 2, 2)
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}
	if len(xpts) != 10 {
		t.Fatalf("%d crosspoints, want 10", len(xpts))
	}
	for i, x := range xpts {
		if x.Dest.Source != uint16(i+1) {
			t.Errorf("crosspoint %d names destination %d", i, x.Dest.Source)
		}
	}
}

func TestProtects(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	before, err := h.plugin.Protect(ctx, r, 1, 1, 1)
	if err != nil {
		t.Fatalf("Protect: %v", err)
	}
	if before.On {
		t.Error("a destination starts unprotected")
	}

	got, err := h.plugin.SetProtect(ctx, r, 1, 1, 1, true, 42, false)
	if err != nil {
		t.Fatalf("SetProtect: %v", err)
	}
	if !got.On || got.ID != 42 {
		t.Errorf("protect = %+v, want on with id 42", got)
	}

	// A protect with no id could never be released, and one past the
	// configured range names a panel that cannot exist.
	if _, err := h.plugin.SetProtect(ctx, r, 1, 1, 1, true, 0, false); err == nil {
		t.Error("a protect with no panel id should be refused")
	}
	if _, err := h.plugin.SetProtect(ctx, r, 1, 1, 1, true, router.MaxProtectID+1, false); err == nil {
		t.Error("a protect id past the configured range should be refused")
	}
}

func TestFiringASalvo(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	if err := h.plugin.FireSalvo(ctx, r, 2); err != nil {
		t.Fatalf("FireSalvo: %v", err)
	}
	if err := h.plugin.FireSalvo(ctx, r, 0); err == nil {
		t.Error("salvo zero is not one of them")
	}
	if err := h.plugin.FireSalvo(ctx, r, r.Salvos+1); err == nil {
		t.Error("a salvo past the end should be refused")
	}

	// Salvos arrived at a known interface version, and a controller older than
	// that does not have the command at all.
	old := *r
	old.Version = router.VersionSalvos - 1
	if err := h.plugin.FireSalvo(ctx, &old, 1); err == nil {
		t.Error("an older interface has no salvos")
	}
}

func TestAddressingWhatTheRouterDoesNotHave(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	for _, tc := range []struct {
		name                  string
		matrix, level, entity uint32

		// levelExists says whether the matrix and level themselves are real,
		// which decides whether reading the whole level should work.
		levelExists bool
	}{
		{name: "matrix zero", matrix: 0, level: 1, entity: 1},
		{name: "a matrix past the end", matrix: 9, level: 1, entity: 1},
		{name: "level zero", matrix: 1, level: 0, entity: 1},
		{name: "a level past the end", matrix: 1, level: 9, entity: 1},
		{name: "a destination past the end", matrix: 1, level: 1, entity: 99,
			levelExists: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.plugin.Route(ctx, r, tc.matrix, tc.level, tc.entity); err == nil {
				t.Error("reading it should fail")
			}
			if _, err := h.plugin.SetRoute(ctx, r, tc.matrix, tc.level, tc.entity,
				router.SourcePin{Matrix: 1, Level: 1, Source: 1}, 0); err == nil {
				t.Error("routing to it should fail")
			}
			if _, err := h.plugin.Protect(ctx, r, tc.matrix, tc.level, tc.entity); err == nil {
				t.Error("reading its protect should fail")
			}
			if _, err := h.plugin.SetProtect(ctx, r, tc.matrix, tc.level, tc.entity,
				true, 1, false); err == nil {
				t.Error("protecting it should fail")
			}
			_, err := h.plugin.Routes(ctx, r, tc.matrix, tc.level)
			if tc.levelExists && err != nil {
				t.Errorf("reading a level that exists failed: %v", err)
			}
			if !tc.levelExists && err == nil {
				t.Error("reading a level that does not exist should fail")
			}
		})
	}
}

func TestARouteThatTheControllerRefuses(t *testing.T) {
	h, r := routerHarness(t, nil)

	// A command the controller will not answer at all.
	cmd, err := r.destCommand(1, 1, 1, router.OffDestRoutedSrc)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}
	h.device.mu.Lock()
	h.device.routers[2].refuse[cmd] = true
	h.device.mu.Unlock()

	ctx := context.Background()
	if _, err := h.plugin.Route(ctx, r, 1, 1, 1); err == nil {
		t.Error("a refused read should reach the caller")
	}
	if _, err := h.plugin.SetRoute(ctx, r, 1, 1, 1,
		router.SourcePin{Matrix: 1, Level: 1, Source: 2}, 0); err == nil {
		t.Error("a refused write should reach the caller")
	}
}

func TestARouteWithAProtectID(t *testing.T) {
	h, r := routerHarness(t, nil)

	// The id rides along with the pin in a two-entry array. It is a temporary
	// override for local routing and rarely wanted, but a client that needs it
	// has no other way to ask.
	src := router.SourcePin{Matrix: 1, Level: 1, Source: 3}
	if _, err := h.plugin.SetRoute(context.Background(), r, 1, 1, 4, src, 17); err != nil {
		t.Fatalf("SetRoute with a protect id: %v", err)
	}

	after, err := h.plugin.Route(context.Background(), r, 1, 1, 4)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if after.Source != src {
		t.Errorf("routed %s, want %s", after.Source, src)
	}
}
