package rollcall

import (
	"context"
	"log/slog"
	"runtime/debug"
	"testing"
	"time"

	"dhs/internal/clock"
	"dhs/internal/plugin"
	"dhs/internal/snell-rollcall/codec"
	"dhs/internal/snell-rollcall/session"
)

// A session negotiates one generation and keeps it. A message from the other
// one is answered anyway — it is well formed and refusing would break a client
// that otherwise works — but it must leave a trace, which is exactly what it
// failed to do while a vendor panel was connecting and failing for reasons no
// log could explain.

func TestA32BitMessageOnA16BitSessionIsCounted(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcMenus|codec.SvcControl) // no LongStr => 16-bit

	// GetMenuCount belongs to the long-string generation.
	if _, err := sess.Do(context.Background(), codec.MsgGetMenuCount,
		codec.MenuReq{MenuIndex: 0}.AppendTo(nil)); err != nil {
		t.Fatalf("the message should still be answered: %v", err)
	}
	if !hasEvent(s.p, EventMixedGeneration) {
		t.Error("a 32-bit message on a 16-bit session went unrecorded")
	}
}

func TestA16BitMessageOnA32BitSessionIsCounted(t *testing.T) {
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	// GetFStat belongs to the 16-bit generation.
	_, _ = sess.Do(context.Background(), codec.MsgGetFStat,
		codec.GetFStat{Command: 1}.AppendTo(nil))

	if !hasEvent(s.p, EventMixedGeneration) {
		t.Error("a 16-bit message on a 32-bit session went unrecorded")
	}
}

func TestAMessageBelongingToBothIsNotCounted(t *testing.T) {
	// Most of the protocol is the same in both generations, and none of it
	// should be reported as mixing.
	s := newServed(t, testTree())
	sess := s.open(1, codec.SvcMenus|codec.SvcControl)
	ctx := context.Background()

	for _, typ := range []codec.PacketType{codec.MsgGetID, codec.MsgGetStat, codec.MsgKeepAlive} {
		if _, err := sess.Do(ctx, typ, nil); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
	}
	if hasEvent(s.p, EventMixedGeneration) {
		t.Error("messages common to both generations were reported as mixed")
	}
}

func TestA16BitFrameWithholdsLongStrings(t *testing.T) {
	// A frame that does not advertise SV_LONGSTR cannot be asked for the newer
	// generation, so every client on it speaks 16-bit. It is how a 16-bit
	// device is emulated from the same tree, without a second implementation
	// to disagree with the first.
	s := newServed(t, testTree())
	s.p.SetLongStrings(false)

	if s.p.served().LongStrings() {
		t.Error("a 16-bit frame still offers the long-string service")
	}
	id, ok := s.p.identityOf(0)
	if !ok {
		t.Fatal("the gateway has no identity")
	}
	if id.Services.LongStrings() {
		t.Error("the gateway still advertises long strings")
	}
	card, ok := s.p.identityOf(1)
	if !ok {
		t.Fatal("card 1 has no identity")
	}
	if card.Services.LongStrings() {
		t.Error("a card still advertises long strings")
	}
	// What it announces has to agree with what it answers.
	if s.p.gatewayInfo().ID.Services.LongStrings() {
		t.Error("the announcement still offers long strings")
	}
}

func TestA32BitFrameOffersLongStrings(t *testing.T) {
	s := newServed(t, testTree())

	if !s.p.served().LongStrings() {
		t.Error("the default frame should offer the newer generation")
	}
	s.p.SetLongStrings(false)
	s.p.SetLongStrings(true)
	if !s.p.served().LongStrings() {
		t.Error("turning it back on did not restore it")
	}
}

func TestARackHoldsCardsOfDifferentAges(t *testing.T) {
	// Services are advertised per unit and a session is negotiated with the
	// node it is opened on, so an old card that speaks only the 16-bit forms
	// sits behind a gateway that speaks both. A client talking to two cards in
	// one frame is in two generations at once.
	s := newServed(t, testTree())
	s.p.SetLongStringsAt(1, false)

	if !s.p.served().LongStrings() {
		t.Error("the gateway should still offer the newer generation")
	}
	old, _ := s.p.identityOf(1)
	if old.Services.LongStrings() {
		t.Error("the card told to be older still advertises long strings")
	}
	rest, _ := s.p.identityOf(2)
	if !rest.Services.LongStrings() {
		t.Error("a card nobody changed lost its generation")
	}

	// And a session on the old card cannot negotiate what it does not offer.
	if _, err := s.tryOpen(1, codec.SvcMenus|codec.SvcLongStr, codec.LevelSupervisor); err == nil {
		t.Error("the old card granted a long-string session")
	}
	sess, err := s.tryOpen(2, codec.SvcMenus|codec.SvcLongStr, codec.LevelSupervisor)
	if err != nil {
		t.Errorf("the newer card refused a long-string session: %v", err)
	} else {
		_ = sess.Close()
	}
}

func TestAServiceACardDoesNotHaveIsRefused(t *testing.T) {
	// The map is the gateway's, not a card's. Checking a call against the
	// frame's services rather than the node's granted a client something the
	// node it asked does not serve.
	s := newServed(t, testTree())

	if _, err := s.tryOpen(1, codec.SvcMap, codec.LevelSupervisor); err == nil {
		t.Error("a card granted a map session")
	}
	sess, err := s.tryOpen(0, codec.SvcMap, codec.LevelSupervisor)
	if err != nil {
		t.Errorf("the gateway refused a map session: %v", err)
	} else {
		_ = sess.Close()
	}
}

func TestTheGatewayHasAPageOfItsOwn(t *testing.T) {
	// A gateway is a unit and a panel expects to open it. Ours had no menu at
	// all, so selecting it offered nothing to read. A real controller does the
	// opposite: the Nucleus template the Centra simulator ships draws a Unit
	// Setup page of exactly this shape.
	s := newServed(t, testTree())

	gw := s.p.model.port(0)
	if gw == nil {
		t.Fatal("the gateway has no port")
	}
	lines := gw.menu(true)
	if len(lines) == 0 {
		t.Fatal("the gateway has no menu")
	}

	want := map[string]bool{
		"Ethernet": false, "IP Address": false, "IPShare Port": false,
		"RollCall": false, "Generation": false, "Cards": false,
		"Software": false, "Version": false,
		"Logging": false, "Debug Logging": false,
	}
	for _, l := range lines {
		if _, ok := want[l.Text]; ok {
			want[l.Text] = true
		}
		// One line may be written and the rest may not: a connector that let a
		// panel change where it listens would answer the question by cutting
		// the wire, but turning its own logging up is exactly what an operator
		// watching it misbehave wants.
		writable := l.Command == cmdGatewayDebugLog
		if l.Command != 0 && !l.Style.Disabled() != writable {
			t.Errorf("%q writable=%v, want %v", l.Text, !l.Style.Disabled(), writable)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("the gateway page does not show %q", name)
		}
	}
}

func TestTheGatewayIsNotACardSlot(t *testing.T) {
	// It has a page, but a port list enumerates card slots and the gateway is
	// not one. Putting it in the order would have every client counting a card
	// that is not there.
	s := newServed(t, testTree())

	for _, n := range s.p.model.portNumbers() {
		if n == 0 {
			t.Error("the gateway appears in the card enumeration")
		}
	}
	if _, ok := s.p.templates[0]; !ok {
		t.Error("the gateway serves no template")
	}
}

func TestTheGatewayPageSaysWhereItIsListening(t *testing.T) {
	for _, tc := range []struct {
		addr string
		host string
		port int
	}{
		{"", "not listening", 0},
		{"10.6.239.107:2061", "10.6.239.107", 2061},
		{"[::]:2061", "all interfaces", 2061},
		{":2061", "all interfaces", 2061},
		{"[fe80::1]:2050", "fe80::1", 2050},
		{"nonsense", "nonsense", 0},
		{"host:notaport", "host", 0},
	} {
		host, port := splitListen(tc.addr)
		if host != tc.host || port != tc.port {
			t.Errorf("splitListen(%q) = %q,%d; want %q,%d",
				tc.addr, host, port, tc.host, tc.port)
		}
	}
}

func TestTheGatewayPageIsRefreshedWhenRead(t *testing.T) {
	// It shows what the connector is doing now, not what it was doing when the
	// tree loaded.
	s := newServed(t, testTree())
	gw := s.p.model.port(0)

	s.p.mu.Lock()
	s.p.addr = "10.6.239.107:2061"
	s.p.mu.Unlock()
	s.p.refreshGateway()

	v, ok := gw.value(cmdGatewayAddress)
	if !ok || v.Text != "10.6.239.107" {
		t.Errorf("address = %q", v.Text)
	}
	if v, ok := gw.value(cmdGatewayPort); !ok || v.Val != 2061 {
		t.Errorf("port = %d", v.Val)
	}
	if v, ok := gw.value(cmdGatewayCards); !ok || v.Val != 2 {
		t.Errorf("cards = %d, want the two the tree has", v.Val)
	}
	if v, ok := gw.value(cmdGatewayGeneration); !ok || v.Text != "32-bit" {
		t.Errorf("generation = %q", v.Text)
	}

	// And it follows the frame it belongs to.
	s.p.SetLongStrings(false)
	s.p.refreshGateway()
	if v, _ := gw.value(cmdGatewayGeneration); v.Text != "16-bit" {
		t.Errorf("generation after the frame changed = %q", v.Text)
	}
}

func TestRefreshingAGatewayThatIsNotThere(t *testing.T) {
	// A model with no gateway port is not a reason to panic.
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())
	p.model.mu.Lock()
	delete(p.model.ports, 0)
	p.model.mu.Unlock()

	p.refreshGateway()
}

func TestReadingTheGatewayPageOverASession(t *testing.T) {
	// What a panel does when it opens the controller: a session on port zero
	// and a read of its lines. The page is refreshed as it is read, so the
	// listening address is the one in force now.
	s := newServed(t, testTree())
	s.p.mu.Lock()
	s.p.addr = "10.6.239.107:2061"
	s.p.mu.Unlock()

	sess := s.open(0, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	reply, err := sess.Do(context.Background(), codec.MsgGetValue,
		codec.GetValue{Command: cmdGatewayAddress}.AppendTo(nil))
	if err != nil {
		t.Fatalf("read the gateway's address: %v", err)
	}
	v, err := codec.DecodeValue(reply.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Text != "10.6.239.107" {
		t.Errorf("the page says %q, want where it is listening", v.Text)
	}
}

func TestWhatTheGatewayPageSaysAboutTheBuild(t *testing.T) {
	// It comes from the binary rather than a constant, so the page cannot
	// claim a version the code is not.
	for _, tc := range []struct {
		name string
		in   *debug.BuildInfo
		want facts
	}{
		{
			"a released build",
			&debug.BuildInfo{
				Main: debug.Module{Version: "v0.21.1"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "8af6dcce20d12c76528f7af493a8a3e7690fa269"},
					{Key: "vcs.time", Value: "2026-09-08T15:10:10Z"},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			facts{version: "v0.21.1", commit: "8af6dcc", built: "2026-09-08T15:10:10Z"},
		},
		{
			"a build with uncommitted changes",
			&debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "8af6dcce20d12c76528f7af493a8a3e7690fa269"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			// It is not the commit it names, and the page says so.
			facts{version: "devel", commit: "8af6dcc-dirty", built: "unknown"},
		},
		{
			"a short revision",
			&debug.BuildInfo{
				Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}},
			},
			facts{version: "devel", commit: "abc", built: "unknown"},
		},
		{
			"no version control at all",
			&debug.BuildInfo{},
			facts{version: "devel", commit: "unknown", built: "unknown"},
		},
		{
			"no build record at all",
			nil,
			facts{version: "devel", commit: "unknown", built: "unknown"},
		},
	} {
		got := factsFrom(tc.in)
		if got.version != tc.want.version || got.commit != tc.want.commit || got.built != tc.want.built {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
		if got.goVersion == "" {
			t.Errorf("%s: no Go version", tc.name)
		}
	}
}

func TestTheGatewayPageReportsUptime(t *testing.T) {
	s := newServed(t, testTree())

	if got := s.p.uptime(); got != "not started" {
		t.Errorf("uptime before serving = %q", got)
	}

	s.p.mu.Lock()
	s.p.started = s.clk.Now()
	s.p.mu.Unlock()
	s.clk.Advance(90 * time.Second)

	if got := s.p.uptime(); got != "1m30s" {
		t.Errorf("uptime = %q, want 1m30s", got)
	}
}

func TestTheGatewayPageNests(t *testing.T) {
	// A container's step is the size of its whole subtree, not the count of
	// its immediate children. Written without one, every group was empty and a
	// client drew the page flat.
	s := newServed(t, testTree())
	lines := s.p.model.port(0).menu(true)

	spans := map[string]uint32{}
	for _, l := range lines {
		if l.Command == 0 {
			spans[l.Text] = l.Step
		}
	}
	for name, want := range map[string]uint32{
		"Ethernet": 2, "RollCall": 3, "Software": 4, "Logging": 1, "Status": 3,
	} {
		if spans[name] != want {
			t.Errorf("%q spans %d lines, want %d", name, spans[name], want)
		}
	}

	// And every line under a group is inside its span.
	for i, l := range lines {
		if l.Command != 0 {
			continue
		}
		for j := i + 1; j <= i+int(l.Step) && j < len(lines); j++ {
			if lines[j].Command == 0 {
				t.Errorf("%q contains the container %q", l.Text, lines[j].Text)
			}
		}
	}
}

func TestTheProviderListsItself(t *testing.T) {
	// Both devices we can measure put themselves at the head of their own port
	// list: the IQ frame reports 0000-0C-00 before its cards, the Centra
	// 0000-08-00 before its units. A provider that left itself out was the
	// only thing on the network doing so.
	s := newServed(t, testTree())
	sess := s.open(0, codec.SvcMenus|codec.SvcMap)

	var names []string
	err := session.Walk(context.Background(), sess, codec.MsgGetDevList, []byte{0, 0},
		func(_ int, f codec.Frame) error {
			info, err := codec.DecodeDeviceInfo(f.Payload)
			if err != nil {
				return err
			}
			names = append(names, info.ID.Name)
			return nil
		})
	if err != nil {
		t.Fatalf("port list: %v", err)
	}
	if len(names) != 3 || names[0] != "dhs rollcall" {
		t.Errorf("port list = %v, want the gateway then its cards", names)
	}
}

func TestTurningLoggingUpFromThePage(t *testing.T) {
	// The control has to do the thing rather than remember that somebody asked.
	lvl := new(slog.LevelVar)
	lvl.Set(slog.LevelInfo)

	deps := testDeps(clock.NewFake(time.Time{}))
	deps.LogLevel = lvl
	p := New(deps, testTree())

	if err := p.setDebugLogging(true); err != nil {
		t.Fatalf("turning logging up: %v", err)
	}
	if lvl.Level() != slog.LevelDebug {
		t.Errorf("level = %v, want debug", lvl.Level())
	}

	// And the page reads back what is in force.
	p.refreshGateway()
	if v, ok := p.model.port(0).value(cmdGatewayDebugLog); !ok || v.Val != 1 {
		t.Errorf("the page says %d, want 1", v.Val)
	}

	if err := p.setDebugLogging(false); err != nil {
		t.Fatalf("turning logging down: %v", err)
	}
	if lvl.Level() != slog.LevelInfo {
		t.Errorf("level = %v, want info", lvl.Level())
	}
	p.refreshGateway()
	if v, _ := p.model.port(0).value(cmdGatewayDebugLog); v.Val != 0 {
		t.Errorf("the page says %d, want 0", v.Val)
	}
}

func TestAProcessThatFixedItsLogLevelRefuses(t *testing.T) {
	// A caller that kept no level has nothing to move, and the line says so
	// rather than pretending it worked.
	p := New(testDeps(clock.NewFake(time.Time{})), testTree())

	if err := p.setDebugLogging(true); err == nil {
		t.Error("a fixed log level should refuse the write")
	}
	p.refreshGateway()
	if v, _ := p.model.port(0).value(cmdGatewayDebugLog); v.Val != 0 {
		t.Errorf("the page claims logging is up: %d", v.Val)
	}
}

func TestWritingTheLoggingControlOverASession(t *testing.T) {
	// What a panel does: tick the box.
	lvl := new(slog.LevelVar)
	lvl.Set(slog.LevelInfo)

	s := newServedDeps(t, testTree(), func(d *plugin.Deps) { d.LogLevel = lvl })
	sess := s.open(0, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	payload, err := codec.Value{
		Command: cmdGatewayDebugLog, Mode: codec.ModeValue, Val: 1,
	}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := sess.Do(context.Background(), codec.MsgSetValue, payload); err != nil {
		t.Fatalf("write the logging control: %v", err)
	}
	if lvl.Level() != slog.LevelDebug {
		t.Errorf("level = %v, want debug", lvl.Level())
	}
}

func TestTheLoggingControlRefusesOverASession(t *testing.T) {
	// A process that fixed its level refuses the write on the wire too, rather
	// than storing a value that changed nothing.
	s := newServed(t, testTree())
	sess := s.open(0, codec.SvcMenus|codec.SvcControl|codec.SvcLongStr)

	payload, err := codec.Value{
		Command: cmdGatewayDebugLog, Mode: codec.ModeValue, Val: 1,
	}.AppendTo(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := sess.Do(context.Background(), codec.MsgSetValue, payload); err == nil {
		t.Error("the write should have been refused")
	}
}
