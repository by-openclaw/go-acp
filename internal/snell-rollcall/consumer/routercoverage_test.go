package rollcall

import (
	"context"
	"fmt"
	"testing"
	"time"

	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/codec/dtp"
	"dhs/internal/snell-rollcall/codec/router"
)

// Discovery reads a few dozen commands and every one of them can be refused.
// A controller that stops answering half way through is not hypothetical: it
// is what a unit under load, or one being reconfigured, looks like.

// tryRouter builds a harness and reads the router without failing the test.
func tryRouter(t *testing.T, tweak func(*fakeRouter)) (*harness, *RouterInterface, error) {
	t.Helper()

	h := newHarness(t, func(d *device) {
		d.ports = 3
		f := newFakeRouter(2, testRouterShape())
		if tweak != nil {
			tweak(f)
		}
		d.routers[2] = f
		for name, body := range f.files {
			d.files[name] = body
		}
	})
	r, err := h.plugin.RouterAt(context.Background(), 2)
	return h, r, err
}

// discoveryCommands are the commands a full read touches, worked out the same
// way the reader works them out.
func discoveryCommands() map[string]uint32 {
	const (
		matrixBase = 121
		matrixStep = router.MatrixTableSize
	)
	out := map[string]uint32{}

	// The root commands discovery reads. The others in the table are written
	// rather than read — make a route, fire a salvo, fetch a track template —
	// so refusing one changes nothing about reading the model.
	for _, cmd := range []router.Command{
		router.CmdInterfaceVersion, router.CmdRouterName,
		router.CmdNumMatrices, router.CmdMatrixBase, router.CmdMatrixStep,
		router.CmdNumCategories, router.CmdCategoryBase, router.CmdCategoryStep,
		router.CmdNumSalvos, router.CmdSalvoNames8File, router.CmdSalvoNames32File,
		router.CmdNumDevices, router.CmdDeviceNamesFile,
	} {
		out[fmt.Sprintf("root command %d", cmd)] = uint32(cmd)
	}
	for off := uint32(0); off < router.MatrixTableSize; off++ {
		out[fmt.Sprintf("matrix 1 field %d", off)] = matrixBase + off
	}

	// The level table's own base is what the matrix table pointed at, and the
	// fake lays the first one out immediately after the categories.
	levelBase := uint32(matrixBase + 2*matrixStep + 32)
	for off := uint32(0); off < router.LevelTableSize; off++ {
		out[fmt.Sprintf("level 1 field %d", off)] = levelBase + off
	}
	return out
}

func TestDiscoveryGivesUpWhenACommandIsRefused(t *testing.T) {
	for name, cmd := range discoveryCommands() {
		t.Run(name, func(t *testing.T) {
			_, _, err := tryRouter(t, func(f *fakeRouter) { f.refuse[cmd] = true })
			if err == nil {
				t.Errorf("command %d was refused and the read succeeded anyway", cmd)
			}
		})
	}
}

func TestDiscoveryRefusesACountThatCannotBeOne(t *testing.T) {
	// A count or a base cannot be negative. Following one would read whatever
	// happens to live at the wrapped-around command number.
	_, _, err := tryRouter(t, func(f *fakeRouter) {
		f.num(uint32(router.CmdNumMatrices), -1)
	})
	if err == nil {
		t.Error("a negative matrix count should be refused")
	}
}

func TestDiscoveryWithAFileNameItCannotDecode(t *testing.T) {
	_, _, err := tryRouter(t, func(f *fakeRouter) {
		// Data that is not Data Transfer Params at all.
		f.data(uint32(router.CmdSalvoNames8File), []byte{0xFF, 0xFF, 0xFF})
	})
	if err == nil {
		t.Error("a filename that will not decode should be reported")
	}
}

func TestDiscoverySkipsWhatTheVersionSaysIsAbsent(t *testing.T) {
	// Salvos and device names arrived at known interface versions. On an older
	// controller the commands do not exist, and asking would fire a compliance
	// event for something the version already said.
	_, r, err := tryRouter(t, func(f *fakeRouter) {
		f.num(uint32(router.CmdInterfaceVersion), router.VersionProtectID16)
		f.refuse[uint32(router.CmdNumSalvos)] = true
		f.refuse[uint32(router.CmdNumDevices)] = true
	})
	if err != nil {
		t.Fatalf("an older interface should still read: %v", err)
	}
	if r.Salvos != 0 || r.Devices != 0 {
		t.Errorf("salvos = %d devices = %d, want neither", r.Salvos, r.Devices)
	}
}

func TestARouterThatPublishesAStepSmallerThanItsTable(t *testing.T) {
	// A controller whose step is smaller than the table it documents has
	// entities overlapping in the command space. Reading past the step would
	// read the next entity's fields rather than this one's, so the reader
	// stops instead.
	h, r, err := tryRouter(t, func(f *fakeRouter) {
		// Level 1 of matrix 1: source step of one, so only the first field of
		// each source is addressable.
		levelBase := uint32(121 + 2*router.MatrixTableSize + 32)
		f.num(levelBase+router.OffSrcStep, 1)
	})
	if err != nil {
		t.Fatalf("RouterAt: %v", err)
	}

	lv, err := r.Level(1, 1)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	lv.Names = RouterFile{}
	lv.Names8 = RouterFile{}

	// The long names live at offset one, which is past the step, so nothing
	// can be read and the answer is empty rather than wrong.
	names, err := h.plugin.LevelNames(context.Background(), r, 1, 1, router.NameWidth32)
	if err != nil {
		t.Fatalf("LevelNames: %v", err)
	}
	if len(names.Srcs) != 0 {
		t.Errorf("%d source names came back from a table that cannot address them", len(names.Srcs))
	}

	// Destinations are addressable, so those still arrive.
	if len(names.Dsts) != 10 {
		t.Errorf("%d destination names, want 10", len(names.Dsts))
	}
}

func TestARouterWhoseDestinationStepIsTooSmall(t *testing.T) {
	h, r, err := tryRouter(t, func(f *fakeRouter) {
		levelBase := uint32(121 + 2*router.MatrixTableSize + 32)
		f.num(levelBase+router.OffDstStep, 1)
	})
	if err != nil {
		t.Fatalf("RouterAt: %v", err)
	}

	lv, err := r.Level(1, 1)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	lv.Names = RouterFile{}
	lv.Names8 = RouterFile{}

	names, err := h.plugin.LevelNames(context.Background(), r, 1, 1, router.NameWidth32)
	if err != nil {
		t.Fatalf("LevelNames: %v", err)
	}
	if len(names.Dsts) != 0 {
		t.Errorf("%d destination names came back", len(names.Dsts))
	}

	// And the routed source is past the step too, so a route cannot even be
	// addressed.
	if _, err := h.plugin.Route(context.Background(), r, 1, 1, 1); err == nil {
		t.Error("a destination whose fields are past its step should not be addressable")
	}
}

func TestRoutingWhenTheDeviceHasGoneAway(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	if err := h.plugin.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	src := router.SourcePin{Matrix: 1, Level: 1, Source: 2}
	if _, err := h.plugin.Route(ctx, r, 1, 1, 1); err == nil {
		t.Error("reading a route without a connection should fail")
	}
	if _, err := h.plugin.SetRoute(ctx, r, 1, 1, 1, src, 0); err == nil {
		t.Error("setting a route without a connection should fail")
	}
	if _, err := h.plugin.Protect(ctx, r, 1, 1, 1); err == nil {
		t.Error("reading a protect without a connection should fail")
	}
	if _, err := h.plugin.SetProtect(ctx, r, 1, 1, 1, true, 4, false); err == nil {
		t.Error("setting a protect without a connection should fail")
	}
	if err := h.plugin.FireSalvo(ctx, r, 1); err == nil {
		t.Error("firing a salvo without a connection should fail")
	}
	if err := h.plugin.WatchRoutes(ctx, r, func(Crosspoint) {}); err == nil {
		t.Error("watching without a connection should fail")
	}
	if _, err := h.plugin.LevelNames(ctx, r, 1, 1, router.NameWidth32); err == nil {
		t.Error("reading names without a connection should fail")
	}
	if _, err := h.plugin.Mappings(ctx, r, 1); err == nil {
		t.Error("reading mappings without a connection should fail")
	}
	if _, err := h.plugin.RouterAt(ctx, 2); err == nil {
		t.Error("reading the interface without a connection should fail")
	}
	if _, err := h.plugin.FindRouter(ctx); err == nil {
		t.Error("finding a router without a connection should fail")
	}
}

func TestARouteReplyThatWillNotDecode(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	// A controller answering a route with something that is not Data Transfer
	// Params at all.
	cmd, err := r.destCommand(1, 1, 1, router.OffDestRoutedSrc)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}
	h.device.mu.Lock()
	h.device.routers[2].data(cmd, []byte{0xFF, 0xFF})
	h.device.mu.Unlock()

	if _, err := h.plugin.Route(ctx, r, 1, 1, 1); err == nil {
		t.Error("a crosspoint that will not decode should be reported")
	}
}

func TestARouteTheControllerRefusesToMake(t *testing.T) {
	h, r := routerHarness(t, nil)

	// The result code is what says whether it worked, and a client that
	// ignores it believes a route it did not get.
	cmd, err := r.destCommand(1, 1, 1, router.OffDestRoutedSrc)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}
	h.device.mu.Lock()
	h.device.routers[2].results = map[uint32]router.RouteResult{cmd: router.RouteInhibited}
	h.device.mu.Unlock()

	src := router.SourcePin{Matrix: 1, Level: 1, Source: 2}
	got, err := h.plugin.SetRoute(context.Background(), r, 1, 1, 1, src, 0)
	if err == nil {
		t.Fatal("an inhibited route should be reported as one")
	}
	if got.Result != router.RouteInhibited {
		t.Errorf("result = %s, want inhibited", got.Result)
	}
}

func TestASalvoTheControllerRefuses(t *testing.T) {
	h, r := routerHarness(t, func(f *fakeRouter) {
		f.refuse[uint32(router.CmdFireSalvo)] = true
	})

	if err := h.plugin.FireSalvo(context.Background(), r, 1); err == nil {
		t.Error("a refused salvo should reach the caller")
	}
}

func TestAProtectTheControllerRefuses(t *testing.T) {
	h, r := routerHarness(t, nil)

	cmd, err := r.destCommand(1, 1, 2, router.OffDestProtect)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}
	h.device.mu.Lock()
	h.device.routers[2].refuse[cmd] = true
	h.device.mu.Unlock()

	ctx := context.Background()
	if _, err := h.plugin.Protect(ctx, r, 1, 1, 2); err == nil {
		t.Error("a refused protect read should reach the caller")
	}
	if _, err := h.plugin.SetProtect(ctx, r, 1, 1, 2, true, 9, false); err == nil {
		t.Error("a refused protect write should reach the caller")
	}
}

func TestReadingALevelThatStopsPartWay(t *testing.T) {
	h, r := routerHarness(t, nil)

	cmd, err := r.destCommand(1, 1, 4, router.OffDestRoutedSrc)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}
	h.device.mu.Lock()
	h.device.routers[2].refuse[cmd] = true
	h.device.mu.Unlock()

	// What was read before the failure comes back with it: three destinations
	// are better than none to a caller drawing a panel.
	got, err := h.plugin.Routes(context.Background(), r, 1, 1)
	if err == nil {
		t.Fatal("a level that stops part way should report it")
	}
	if len(got) != 3 {
		t.Errorf("%d crosspoints came back with the error, want the 3 that were read", len(got))
	}
}

func TestWatchingWhenTheBackChannelIsRefused(t *testing.T) {
	h, r := routerHarness(t, func(f *fakeRouter) {})

	h.device.mu.Lock()
	h.device.refuse[codec.MsgBkChnReady] = true
	h.device.mu.Unlock()

	if err := h.plugin.WatchRoutes(context.Background(), r, func(Crosspoint) {}); err == nil {
		t.Error("a refused back channel should reach the caller")
	}
}

func TestWatchingStopsWhenTheLinkDoes(t *testing.T) {
	h, r := routerHarness(t, nil)

	done := make(chan struct{})
	if err := h.plugin.WatchRoutes(context.Background(), r, func(Crosspoint) {
		close(done)
	}); err != nil {
		t.Fatalf("WatchRoutes: %v", err)
	}

	// The device goes away. The watcher must end rather than spin on a dead
	// session, and nothing may be delivered afterwards.
	h.device.close()

	select {
	case <-done:
		t.Error("a crosspoint was delivered after the device went away")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestCrosspointDecoding(t *testing.T) {
	dest := router.SourcePin{Matrix: 1, Level: 1, Source: 1}

	// A destination with nothing routed to it and nothing to report.
	got, err := decodeCrosspoint(codec.Value{}, dest)
	if err != nil {
		t.Fatalf("an empty value: %v", err)
	}
	if got.Source.Source != 0 || got.HasResult {
		t.Errorf("an empty value decoded to %+v", got)
	}

	// A controller may answer with the array form it accepts on a write.
	pin := router.SourcePin{Matrix: 2, Level: 1, Source: 6}
	data, err := dtp.Encode(dtp.Params{dtp.Uints(pin.Pack()), dtp.Uint(uint32(router.RouteOK))}, false)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err = decodeCrosspoint(codec.Value{Mode: codec.ModeData, Data: data}, dest)
	if err != nil {
		t.Fatalf("the array form: %v", err)
	}
	if got.Source != pin || !got.HasResult {
		t.Errorf("the array form decoded to %+v, want %s", got, pin)
	}

	// An array that is there but empty says nothing about the source.
	data, err = dtp.Encode(dtp.Params{dtp.Uints()}, false)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err = decodeCrosspoint(codec.Value{Mode: codec.ModeData, Data: data}, dest)
	if err != nil {
		t.Fatalf("an empty array: %v", err)
	}
	if got.Source.Source != 0 {
		t.Errorf("an empty array decoded to %s", got.Source)
	}
}

func TestSourceCommandsOutsideTheirRange(t *testing.T) {
	_, r := routerHarness(t, nil)

	if _, err := r.srcCommand(1, 1, 99, router.OffSrcName8); err == nil {
		t.Error("a source past the end should not be addressable")
	}
	if _, err := r.srcCommand(9, 1, 1, router.OffSrcName8); err == nil {
		t.Error("a source on a matrix past the end should not be addressable")
	}
}

func TestRepliesThatWillNotDecode(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	routed, err := r.destCommand(1, 1, 5, router.OffDestRoutedSrc)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}
	prot, err := r.destCommand(1, 1, 5, router.OffDestProtect)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}

	h.device.mu.Lock()
	h.device.routers[2].garble[routed] = true
	h.device.routers[2].garble[prot] = true
	h.device.mu.Unlock()

	src := router.SourcePin{Matrix: 1, Level: 1, Source: 2}
	if _, err := h.plugin.Route(ctx, r, 1, 1, 5); err == nil {
		t.Error("a garbled read should be an error, not a zero crosspoint")
	}
	if _, err := h.plugin.SetRoute(ctx, r, 1, 1, 5, src, 0); err == nil {
		t.Error("a garbled set reply should be an error")
	}
	if _, err := h.plugin.Protect(ctx, r, 1, 1, 5); err == nil {
		t.Error("a garbled protect read should be an error")
	}
	if _, err := h.plugin.SetProtect(ctx, r, 1, 1, 5, true, 3, false); err == nil {
		t.Error("a garbled protect reply should be an error")
	}
}

func TestASetReplyThatIsNotACrosspoint(t *testing.T) {
	h, r := routerHarness(t, nil)

	// The value decodes but its data is not Data Transfer Params, so there is
	// no crosspoint in it.
	cmd, err := r.destCommand(1, 2, 3, router.OffDestRoutedSrc)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}
	h.device.mu.Lock()
	h.device.routers[2].results = map[uint32]router.RouteResult{}
	h.device.routers[2].badReply = map[uint32]bool{cmd: true}
	h.device.mu.Unlock()

	src := router.SourcePin{Matrix: 1, Level: 2, Source: 2}
	if _, err := h.plugin.SetRoute(context.Background(), r, 1, 2, 3, src, 0); err == nil {
		t.Error("a set answered with something that is not a crosspoint should fail")
	}
}

func TestAFileCommandThatNamesNothing(t *testing.T) {
	// A level with no alternate names simply has none, and an empty value says
	// so rather than failing.
	_, r, err := tryRouter(t, func(f *fakeRouter) {
		f.data(uint32(router.CmdDeviceNamesFile), nil)
	})
	if err != nil {
		t.Fatalf("RouterAt: %v", err)
	}
	if !r.DeviceNames.Empty() {
		t.Errorf("device names = %+v, want nothing", r.DeviceNames)
	}
}

func TestANamesFileTooShortForItsLevel(t *testing.T) {
	h, r := routerHarness(t, nil)

	lv, err := r.Level(1, 1)
	if err != nil {
		t.Fatalf("Level: %v", err)
	}
	h.device.mu.Lock()
	h.device.files[lv.Names.Name] = []byte{0x01, 0x02}
	h.device.mu.Unlock()

	// Too short to hold the names the level declares. Falling back to one
	// command per name is what is left, and it works.
	names, err := h.plugin.LevelNames(context.Background(), r, 1, 1, router.NameWidth32)
	if err != nil {
		t.Fatalf("LevelNames: %v", err)
	}
	if got := names.Src(1); got != "matrix1 level1 source1" {
		t.Errorf("source 1 = %q", got)
	}
}

func TestATallyPushThatWillNotDecode(t *testing.T) {
	h, r := routerHarness(t, nil)
	ctx := context.Background()

	changes := make(chan Crosspoint, 4)
	if err := h.plugin.WatchRoutes(ctx, r, func(x Crosspoint) {
		select {
		case changes <- x:
		default:
		}
	}); err != nil {
		t.Fatalf("WatchRoutes: %v", err)
	}

	// A push on a destination's own command whose data is not Data Transfer
	// Params. It is a crosspoint by its number and not one by its content, and
	// neither delivering nonsense nor stopping is right: it is dropped.
	cmd, err := r.destCommand(1, 1, 8, router.OffDestRoutedSrc)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}
	bad, err := codec.Value{Command: cmd, Mode: codec.ModeData, Data: []byte{0xFF, 0xFF}}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	h.device.push(2, codec.MsgRetValue, bad)

	// A real change afterwards still arrives.
	src := router.SourcePin{Matrix: 1, Level: 1, Source: 9}
	if _, err := h.plugin.SetRoute(ctx, r, 1, 1, 9, src, 0); err != nil {
		t.Fatalf("SetRoute: %v", err)
	}

	select {
	case got := <-changes:
		if got.Dest.Source != 9 {
			t.Errorf("the first tally named destination %d, want 9", got.Dest.Source)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no tally arrived")
	}
}

func TestAMatrixStepSmallerThanItsTable(t *testing.T) {
	// A controller whose matrix step is smaller than the table it documents has
	// matrices overlapping in the command space. Reading past the step would
	// read the next matrix's fields, so discovery stops rather than reporting
	// a model built from the wrong numbers.
	_, _, err := tryRouter(t, func(f *fakeRouter) {
		f.num(uint32(router.CmdMatrixStep), 1)
	})
	if err == nil {
		t.Error("a matrix step smaller than the matrix table should be refused")
	}
}

func TestATallyThatCannotBeAcknowledged(t *testing.T) {
	// Every push is a request and the acknowledgement is what asks for the
	// next, so a client that cannot acknowledge is a client that will hear
	// nothing more. The watcher ends rather than spinning.
	h, blocked := newBlockedHarness(t, func(d *device) {
		d.ports = 3
		f := newFakeRouter(2, testRouterShape())
		d.routers[2] = f
		for name, body := range f.files {
			d.files[name] = body
		}
	})

	ctx := context.Background()
	r, err := h.plugin.RouterAt(ctx, 2)
	if err != nil {
		t.Fatalf("RouterAt: %v", err)
	}

	seen := make(chan struct{}, 4)
	if err := h.plugin.WatchRoutes(ctx, r, func(Crosspoint) {
		seen <- struct{}{}
	}); err != nil {
		t.Fatalf("WatchRoutes: %v", err)
	}

	// The socket goes away after the push has been sent but before it can be
	// answered.
	cmd, err := r.destCommand(1, 1, 2, router.OffDestRoutedSrc)
	if err != nil {
		t.Fatalf("destCommand: %v", err)
	}
	pin := router.SourcePin{Matrix: 1, Level: 1, Source: 6}
	data, err := dtp.Encode(dtp.Params{dtp.Uint(pin.Pack())}, false)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	payload, err := codec.Value{Command: cmd, Mode: codec.ModeData, Data: data}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	(*blocked).failing.Store(true)
	h.device.push(2, codec.MsgRetValue, payload)

	select {
	case <-seen:
	case <-time.After(2 * time.Second):
		t.Fatal("the change was never delivered")
	}

	// Nothing more can arrive, because nothing more will be asked for.
	h.device.push(2, codec.MsgRetValue, payload)
	select {
	case <-seen:
		t.Error("a second push was taken after the first could not be acknowledged")
	case <-time.After(200 * time.Millisecond):
	}
}
