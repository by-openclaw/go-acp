package consumer

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"dhs/internal/clock"
	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
	"dhs/internal/snmp/provider"
	"dhs/internal/transport"
)

// The plugin is the agent seen as any other device: one slot, a tree,
// values by name. These tests drive it against a real agent in this
// process, because the only thing worth asserting about a manager is
// what comes back over the wire.

func pluginUnder(t *testing.T, communities provider.Communities) (*Plugin, string, int) {
	t.Helper()
	addr := agentUnder(t, communities)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port := 0
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}
	f := &Factory{}
	p, ok := f.New(plugin.Deps{Logger: quiet(), Clock: clock.System()}).(*Plugin)
	if !ok {
		t.Fatal("the factory must build a *Plugin")
	}
	return p, host, port
}

func connected(t *testing.T, communities provider.Communities) *Plugin {
	t.Helper()
	p, host, port := pluginUnder(t, communities)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func TestFactoryRegistersTheProtocol(t *testing.T) {
	f := &Factory{}
	if m := f.Meta(); m.Name != Name || m.DefaultPort != DefaultPort || m.Description == "" {
		t.Errorf("meta = %+v", m)
	}
	if _, err := dhsc.Get(Name); err != nil {
		t.Errorf("%s is not in the registry: %v", Name, err)
	}
}

func TestPluginReadsTheDeviceAsAnyOther(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	ctx := context.Background()

	info, err := p.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots != 1 || info.DtdVersion != "1.3.6.1.4.1.54981.1" {
		t.Errorf("info = %+v", info)
	}

	// One slot, and it is present; anything else is not a slot.
	si, err := p.GetSlotInfo(ctx, 0)
	if err != nil || si.Status != dhsc.SlotPresent || !si.IsOnline {
		t.Fatalf("slot 0 = %+v, %v", si, err)
	}
	if si, _ := p.GetSlotInfo(ctx, 3); si.Status != dhsc.SlotNoCard {
		t.Errorf("slot 3 = %+v", si)
	}

	objs, err := p.Walk(ctx, 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objs) < 6 {
		t.Fatalf("the system group alone is six objects: %d", len(objs))
	}
	var sawSystem, sawEnterprise bool
	for _, o := range objs {
		switch {
		case strings.HasPrefix(o.Label, "sysDescr"):
			sawSystem = true
			if o.Value.Str != "dhs SNMP agent under test" {
				t.Errorf("sysDescr = %q", o.Value.Str)
			}
			if len(o.Path) == 0 || o.Path[0] != "system" {
				t.Errorf("path = %v", o.Path)
			}
		case strings.HasPrefix(o.OID, "1.3.6.1.4.1.54981."):
			sawEnterprise = true
		}
	}
	if !sawSystem || !sawEnterprise {
		t.Errorf("a walk covers the system group AND the device's own branch (%v, %v)", sawSystem, sawEnterprise)
	}
	if _, err := p.Walk(ctx, 2); err == nil {
		t.Error("an agent has one slot")
	}
}

func TestPluginNamesObjectsThreeWays(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	ctx := context.Background()

	for _, name := range []string{
		"sysDescr.0",        // the MIB's name, with its instance
		"1.3.6.1.2.1.1.1.0", // the number
		"system.Descr",      // the path a walk prints
		"sysDescr",          // the name, instance implied
	} {
		v, err := p.GetValue(ctx, dhsc.ValueRequest{Path: name})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v.Str != "dhs SNMP agent under test" {
			t.Errorf("%s = %q", name, v.Str)
		}
	}
	// A label names it too — an operator filters on what they see.
	if v, err := p.GetValue(ctx, dhsc.ValueRequest{Label: "sysName.0"}); err != nil || v.Str != "agent-under-test" {
		t.Errorf("by label = %q, %v", v.Str, err)
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{}); err == nil {
		t.Error("an unnamed object must be refused")
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "nothing.like.this"}); err == nil {
		t.Error("a name no MIB knows must be refused")
	}
	if !p.PathNative() {
		t.Error("the MIB is the map; the CLI must not walk to resolve a name")
	}
}

func TestPluginWritesWithTheWriteCommunity(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public", Write: "private"})
	ctx := context.Background()
	req := dhsc.ValueRequest{Path: "1.3.6.1.4.1.54981.3.0"}

	// The read community cannot write, and the agent says so.
	if _, err := p.SetValue(ctx, req, dhsc.Value{Kind: dhsc.KindInt, Int: 7}); err == nil {
		t.Fatal("a write on the read community must be refused")
	}

	p.SetCommunity("public", "private")
	got, err := p.SetValue(ctx, req, dhsc.Value{Kind: dhsc.KindInt, Int: 7})
	if err != nil {
		t.Fatalf("SetValue: %v", err)
	}
	// Confirmed by a read-back, not by the agent's echo.
	if got.Int != 7 {
		t.Errorf("confirmed value = %+v", got)
	}
	if v, err := p.GetValue(ctx, req); err != nil || v.Int != 7 {
		t.Errorf("read back = %+v, %v", v, err)
	}
	// A value that is neither a number nor an enum name is refused
	// before anything goes on the wire.
	if _, err := p.SetValue(ctx, req, dhsc.Value{Kind: dhsc.KindString, Str: "sideways"}); err == nil {
		t.Error("a non-numeric value for an INTEGER must be refused")
	}
}

func TestPluginRefusesWorkWithoutASession(t *testing.T) {
	f := &Factory{}
	p := f.New(plugin.Deps{Logger: quiet()}).(*Plugin)
	ctx := context.Background()

	for name, err := range map[string]error{
		"GetDeviceInfo": firstErr(func() error { _, e := p.GetDeviceInfo(ctx); return e }),
		"GetSlotInfo":   firstErr(func() error { _, e := p.GetSlotInfo(ctx, 0); return e }),
		"Walk":          firstErr(func() error { _, e := p.Walk(ctx, 0); return e }),
		"GetValue":      firstErr(func() error { _, e := p.GetValue(ctx, dhsc.ValueRequest{Path: "sysDescr.0"}); return e }),
		"SetValue": firstErr(func() error {
			_, e := p.SetValue(ctx, dhsc.ValueRequest{Path: "sysDescr.0"}, dhsc.Value{})
			return e
		}),
		"Subscribe": p.Subscribe(dhsc.ValueRequest{}, func(dhsc.Event) {}),
		"WalkUnder": firstErr(func() error { _, e := p.WalkUnder(ctx, "system"); return e }),
	} {
		if !errors.Is(err, dhsc.ErrNotConnected) {
			t.Errorf("%s without a session = %v", name, err)
		}
	}
	// Disconnecting a plugin that never connected is not an error.
	if err := p.Disconnect(); err != nil {
		t.Errorf("Disconnect: %v", err)
	}
}

func TestConnectSaysWhichVersionsItTried(t *testing.T) {
	f := &Factory{}
	p := f.New(plugin.Deps{Logger: quiet()}).(*Plugin)
	// Port 1 on the loopback answers nothing at all.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := p.Connect(ctx, "127.0.0.1", 1)
	if err == nil {
		t.Fatal("a silent address is not a device")
	}
	if !strings.Contains(err.Error(), "neither v2c nor v1") {
		t.Errorf("err = %v", err)
	}
}

func TestWalkUnderScopesTheBranch(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	objs, err := p.WalkUnder(context.Background(), "system")
	if err != nil {
		t.Fatalf("WalkUnder: %v", err)
	}
	if len(objs) == 0 {
		t.Fatal("the system group is not empty")
	}
	for _, o := range objs {
		if !strings.HasPrefix(o.OID, "1.3.6.1.2.1.1.") {
			t.Errorf("%s is not under the system group", o.OID)
		}
	}
	if _, err := p.WalkUnder(context.Background(), "no.such.branch"); err == nil {
		t.Error("a branch no MIB knows must be refused")
	}
}

func TestPollProfileFollowsTheOperatorsInterval(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	ctx := context.Background()

	p.SetPollInterval(0) // ignored: zero is not a cadence
	p.SetPollInterval(3 * time.Second)

	prof, err := p.pollProfileFor(ctx, dhsc.ValueRequest{Path: "system"})
	if err != nil {
		t.Fatalf("pollProfileFor: %v", err)
	}
	if len(prof.Entries) == 0 {
		t.Fatal("nothing to poll under the system group")
	}
	for _, e := range prof.Entries {
		if time.Duration(e.Interval) != 3*time.Second {
			t.Errorf("%s polls every %v", e.Path, time.Duration(e.Interval))
		}
		// Every sample is published: a measurement is judged per
		// sample, and the display skips the repeats.
		if e.OnChange == nil || *e.OnChange {
			t.Errorf("%s is on_change", e.Path)
		}
	}
	// An unscoped profile covers the whole walked model.
	all, err := p.pollProfileFor(ctx, dhsc.ValueRequest{})
	if err != nil || len(all.Entries) < len(prof.Entries) {
		t.Errorf("unscoped profile = %d entries, %v", len(all.Entries), err)
	}
	if _, err := p.pollProfileFor(ctx, dhsc.ValueRequest{Path: "1.3.6.1.4.1.99999"}); err == nil {
		t.Error("a scope with no objects must say so")
	}
}

func TestSubscribePollsAndUnsubscribeStops(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	p.SetPollInterval(50 * time.Millisecond)

	events := make(chan dhsc.Event, 64)
	req := dhsc.ValueRequest{Path: "system"}
	if err := p.Subscribe(req, func(ev dhsc.Event) {
		select {
		case events <- ev:
		default:
		}
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	select {
	case ev := <-events:
		if len(ev.Path) == 0 {
			t.Errorf("event carries no path: %+v", ev)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no poll arrived")
	}
	if err := p.Unsubscribe(req); err != nil {
		t.Errorf("Unsubscribe: %v", err)
	}
}

func TestValuesComeBackUnderTheirMIBNames(t *testing.T) {
	// An enumeration reads as the word the manual uses, not the
	// ordinal, because that is what an operator and an alarm rule both
	// say. Everything else keeps its own shape.
	p := connected(t, provider.Communities{Read: "public"})
	def, _ := mib.Compiled().Lookup(codec.MustParseOID("1.3.6.1.4.1.54981.3.0"))
	if def == nil {
		t.Skip("the DHS-MIB is not in the compiled table")
	}
	v := named(def, dhsc.Value{Kind: dhsc.KindInt, Int: 1})
	if len(def.Enums) > 0 && v.Kind != dhsc.KindEnum {
		t.Errorf("an enumerated integer must come back named: %+v", v)
	}
	// A value with no enumeration is untouched, and so is a non-integer.
	if got := named(nil, dhsc.Value{Kind: dhsc.KindInt, Int: 5}); got.Int != 5 || got.Kind != dhsc.KindInt {
		t.Errorf("no definition = %+v", got)
	}
	if got := named(def, dhsc.Value{Kind: dhsc.KindString, Str: "x"}); got.Str != "x" {
		t.Errorf("a string is not an enum: %+v", got)
	}
	_ = p
}

func TestToNeutralCoversEverySyntax(t *testing.T) {
	for _, c := range []struct {
		in   codec.Value
		want dhsc.ValueKind
	}{
		{codec.Int(7), dhsc.KindInt},
		{codec.Gauge32(7), dhsc.KindUint},
		{codec.Counter32(7), dhsc.KindUint},
		{codec.TimeTicks(7), dhsc.KindUint},
		{codec.Counter64(7), dhsc.KindUint},
		{codec.String("x"), dhsc.KindString},
		{codec.Opaque([]byte("x")), dhsc.KindString},
		{codec.ObjectID(codec.OID{1, 3, 6}), dhsc.KindString},
		{codec.IPAddress(net.IPv4(10, 6, 255, 114)), dhsc.KindIPAddr},
		{codec.IPAddress(net.ParseIP("2001:db8::1")), dhsc.KindUnknown}, // not four octets
		{codec.NoSuchObject(), dhsc.KindUnknown},
	} {
		if got := toNeutral(c.in); got.Kind != c.want {
			t.Errorf("toNeutral(%v) = %v, want %v", c.in.Type, got.Kind, c.want)
		}
	}
	if got := toNeutral(codec.IPAddress(net.IPv4(10, 6, 255, 114))); got.IPAddr != [4]byte{10, 6, 255, 114} {
		t.Errorf("address = %v", got.IPAddr)
	}
}

func TestPathsReadLikeTheVendorWroteThem(t *testing.T) {
	p := &Plugin{}
	for _, c := range []struct{ oid, want string }{
		{"1.3.6.1.2.1.1.1.0", "system.Descr"},
		{"1.3.6.1.2.1.1.5.0", "system.Name"},
		{"9.9.9.9", "9.9.9.9"}, // no MIB knows it: the number is the path
	} {
		if got := strings.Join(p.pathFor(codec.MustParseOID(c.oid)), "."); got != c.want {
			t.Errorf("pathFor(%s) = %q, want %q", c.oid, got, c.want)
		}
	}
	// A parent's name is trimmed off its child, cut at a word boundary
	// so a longer word is never sliced in half.
	for _, c := range []struct{ parent, name, want string }{
		{"", "dr5000", "dr5000"},
		{"dr5000Status", "dr5000StatusInput", "Input"},
		{"dr5000AutomaticTable", "dr5000AutomaticEntry", "Entry"},
		// Cut at a word boundary, never mid-word: "ellite" would be
		// unreadable, "Satellite" is the sibling's own word.
		{"dr5000Sat", "dr5000Satellite", "Satellite"},
		{"abc", "abc", "abc"},
	} {
		if got := trimParent(c.parent, c.name); got != c.want {
			t.Errorf("trimParent(%q, %q) = %q, want %q", c.parent, c.name, got, c.want)
		}
	}
}

func firstErr(f func() error) error { return f() }

// deadSession points a connected plugin at an address nothing answers,
// so the paths that handle "the agent stopped talking" are exercised
// without stopping the agent other tests share.
func deadSession(t *testing.T, p *Plugin) {
	t.Helper()
	sess, err := Dial(context.Background(), Options{
		Addr: "127.0.0.1:1", Version: codec.Version2c, Timeout: 200 * time.Millisecond, Retries: 0,
	}, plugin.Deps{Logger: quiet()})
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.sess, p.writeSess = sess, sess
	p.mu.Unlock()
	t.Cleanup(func() { _ = sess.Close() })
}

func TestASilentAgentIsAnErrorNotAPanic(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	ctx := context.Background()
	req := dhsc.ValueRequest{Path: "sysDescr.0"}
	deadSession(t, p)

	if _, err := p.GetDeviceInfo(ctx); err == nil {
		t.Error("GetDeviceInfo must report the silence")
	}
	if _, err := p.GetValue(ctx, req); err == nil {
		t.Error("GetValue must report the silence")
	}
	if _, err := p.SetValue(ctx, req, dhsc.Value{Kind: dhsc.KindString, Str: "x"}); err == nil {
		t.Error("SetValue must report the silence")
	}
	if _, err := p.Walk(ctx, 0); err == nil {
		t.Error("Walk must report the silence")
	}
	// A reply with no varbind in it has said nothing.
	if _, err := firstBind(nil, codec.OID{1, 3, 6}, "reply"); err == nil {
		t.Error("an empty reply is not a value")
	}
	if v, err := firstBind([]codec.VarBind{{Value: codec.Int(4)}}, codec.OID{1, 3, 6}, "reply"); err != nil || v.Int != 4 {
		t.Errorf("firstBind = %+v, %v", v, err)
	}
}

func TestConfirmReadFailureIsNotASuccessfulWrite(t *testing.T) {
	// The SET lands on the agent, the read-back does not: the operator
	// must not be told the value is in place.
	p := connected(t, provider.Communities{Read: "public", Write: "private"})
	p.SetCommunity("public", "private")
	ctx := context.Background()
	req := dhsc.ValueRequest{Path: "1.3.6.1.4.1.54981.3.0"}
	if _, err := p.SetValue(ctx, req, dhsc.Value{Kind: dhsc.KindInt, Int: 3}); err != nil {
		t.Fatalf("the write itself: %v", err)
	}

	// Break only the read session; the write one still answers.
	dead, err := Dial(ctx, Options{Addr: "127.0.0.1:1", Version: codec.Version2c,
		Timeout: 200 * time.Millisecond, Retries: 0}, plugin.Deps{Logger: quiet()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dead.Close() }()
	p.mu.Lock()
	p.sess = dead
	p.mu.Unlock()

	_, err = p.SetValue(ctx, req, dhsc.Value{Kind: dhsc.KindInt, Int: 4})
	if err == nil || !strings.Contains(err.Error(), "confirm read failed") {
		t.Errorf("err = %v", err)
	}
}

func TestWireValueSpeaksTheSyntaxTheMIBDeclares(t *testing.T) {
	p := &Plugin{}
	cases := []struct {
		oid  string
		in   dhsc.Value
		want codec.ValueType
	}{
		{"1.3.6.1.2.1.1.6.0", dhsc.Value{Kind: dhsc.KindString, Str: "TEC RACK 23"}, codec.TypeOctetString},
		{"1.3.6.1.2.1.1.3.0", dhsc.Value{Kind: dhsc.KindUint, Uint: 42}, codec.TypeTimeTicks},
		{"1.3.6.1.4.1.54981.3.0", dhsc.Value{Kind: dhsc.KindInt, Int: 2}, codec.TypeInteger},
		// A float that is a whole number is a number; a bool is a word,
		// and the refusal names the object rather than guessing.
		{"9.9.9.9", dhsc.Value{Kind: dhsc.KindFloat, Float: 12}, codec.TypeInteger},
		{"9.9.9.9", dhsc.Value{Kind: dhsc.KindUint, Uint: 12}, codec.TypeInteger},
		{"9.9.9.9", dhsc.Value{Kind: dhsc.KindBool, Bool: true}, codec.TypeInteger},
	}
	for _, c := range cases {
		got, err := p.wireValue(codec.MustParseOID(c.oid), c.in)
		if c.in.Kind == dhsc.KindBool {
			if err == nil {
				t.Errorf("%s: a word is not an integer", c.oid)
			}
			continue
		}
		if err != nil || got.Type != c.want {
			t.Errorf("%s = %v (%v), want %v", c.oid, got.Type, err, c.want)
		}
	}
	// An enumerated object takes the word from its own item list.
	def, _ := mib.Compiled().Lookup(codec.MustParseOID("1.3.6.1.4.1.54981.3.0"))
	if def != nil && len(def.Enums) > 0 {
		v, err := p.wireValue(codec.MustParseOID("1.3.6.1.4.1.54981.3.0"),
			dhsc.Value{Kind: dhsc.KindString, Str: def.Enums[0].Name})
		if err != nil || v.Int != def.Enums[0].Value {
			t.Errorf("by enum name = %+v, %v", v, err)
		}
	}
	// An OBJECT IDENTIFIER is written as one, and a malformed one is
	// refused before it reaches the wire.
	if v, err := p.wireValue(codec.MustParseOID("1.3.6.1.2.1.1.2.0"),
		dhsc.Value{Kind: dhsc.KindString, Str: "1.3.6.1.4.1.54981"}); err != nil || v.Type != codec.TypeOID {
		t.Errorf("sysObjectID = %+v, %v", v, err)
	}
	if _, err := p.wireValue(codec.MustParseOID("1.3.6.1.2.1.1.2.0"),
		dhsc.Value{Kind: dhsc.KindString, Str: "not an oid"}); err == nil {
		t.Error("a malformed OID must be refused")
	}
}

func TestAnObjectNoMIBKnowsStillHasAValue(t *testing.T) {
	p := &Plugin{}
	o := p.objectFor(codec.VarBind{
		Name:  codec.MustParseOID("2.9.9.9.1"), // under no root any MIB names
		Value: codec.String("something"),
	})
	if o.Value.Str != "something" || o.Kind != dhsc.KindString {
		t.Errorf("value = %+v", o.Value)
	}
	// Nothing names it, so the number is the path — the deepest true
	// thing that can be said about it.
	if len(o.Path) != 1 || o.Path[0] != "2.9.9.9.1" {
		t.Errorf("path = %v", o.Path)
	}
	if o.Access != 1 {
		t.Errorf("an unknown object is read-only until a MIB says otherwise: %d", o.Access)
	}
}

func TestEnterpriseRootIsAskedForWhenItIsNotKnown(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	sess, err := p.session()
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.sysOID = nil
	p.mu.Unlock()
	if got := p.enterpriseRoot(context.Background(), sess); got.String() != "1.3.6.1.4.1.54981" {
		t.Errorf("enterprise root = %v", got)
	}
	// An agent whose sysObjectID is not under an enterprise has no
	// branch of its own, and the system group is the whole model.
	p.mu.Lock()
	p.sysOID = codec.MustParseOID("1.3.6.1.2.1.1")
	p.mu.Unlock()
	if got := p.enterpriseRoot(context.Background(), sess); got != nil {
		t.Errorf("non-enterprise sysObjectID = %v", got)
	}
}

func TestWalkSaysWhenItTruncated(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	old := walkLimit
	walkLimit = 2
	defer func() { walkLimit = old }()

	objs, err := p.Walk(context.Background(), 0)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(objs) != 2 {
		t.Errorf("a truncated walk returns what it read: %d", len(objs))
	}
	if _, err := p.walkUnder(context.Background(), codec.MustParseOID("1.3.6.1.2.1.1")); err != nil {
		t.Errorf("walkUnder: %v", err)
	}
}

func TestConnectTakesTheDefaultPort(t *testing.T) {
	f := &Factory{}
	p := f.New(plugin.Deps{Logger: quiet()}).(*Plugin)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Nothing answers on the loopback's 161 in a test environment; what
	// matters is that port 0 meant 161 and not port 0.
	if err := p.Connect(ctx, "127.0.0.1", 0); err == nil {
		t.Skip("something is serving SNMP on this host")
	} else if !strings.Contains(err.Error(), ":161") {
		t.Errorf("err = %v", err)
	}
}

func TestWalkOutcomeTellsFatalFromIncomplete(t *testing.T) {
	stop := errors.New("limit")
	boom := errors.New("the agent went away")
	for _, c := range []struct {
		name  string
		objs  int
		err   error
		warn  bool
		fatal error
	}{
		{"read it all", 12, nil, false, nil},
		{"hit the cap", 12, stop, true, nil},
		{"nothing at all", 0, boom, false, boom},
		{"part of it", 12, boom, true, nil},
	} {
		warn, fatal := walkOutcome(c.objs, c.err, stop)
		if warn != c.warn || !errors.Is(fatal, c.fatal) && fatal != c.fatal {
			t.Errorf("%s = warn %v, fatal %v", c.name, warn, fatal)
		}
	}
}

func TestConnectReportsAnUnreachableName(t *testing.T) {
	f := &Factory{}
	p := f.New(plugin.Deps{Logger: quiet()}).(*Plugin)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err := p.Connect(ctx, "no.such.host.invalid", 161)
	if err == nil {
		t.Fatal("a name that does not resolve is not a device")
	}
	if !strings.Contains(err.Error(), "neither v2c nor v1") {
		t.Errorf("err = %v", err)
	}
}

func TestEnterpriseRootSaysNothingWhenTheAgentDoes(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	deadSession(t, p)
	sess, err := p.session()
	if err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	p.sysOID = nil
	p.mu.Unlock()
	if got := p.enterpriseRoot(context.Background(), sess); got != nil {
		t.Errorf("a silent agent has no branch: %v", got)
	}
}

func TestObjectForCarriesWhatTheMIBDeclares(t *testing.T) {
	p := &Plugin{}
	// An ATEME status object: enumerated, and the MIB marks it
	// read-write even though it is a reading.
	o := p.objectFor(codec.VarBind{
		Name:  codec.MustParseOID("1.3.6.1.4.1.27338.5.5.3.3.1"),
		Value: codec.Int(1),
	})
	if len(o.EnumItems) != 2 || o.EnumItems[0] != "true" {
		t.Errorf("enum items = %v", o.EnumItems)
	}
	if o.Access&2 == 0 {
		t.Errorf("access = %d, and that MIB says read-write", o.Access)
	}
	if o.Value.Kind != dhsc.KindEnum || o.Value.Str != "true" {
		t.Errorf("value = %+v", o.Value)
	}
	if strings.Join(o.Path, ".") != "ateme.dr5000.Status.Input.Sat.Locked" {
		t.Errorf("path = %v", o.Path)
	}
}

func TestWireValueCountsAndGauges(t *testing.T) {
	p := &Plugin{}
	for _, c := range []struct {
		oid  string
		want codec.ValueType
	}{
		{"1.3.6.1.2.1.2.2.1.5", codec.TypeGauge32},    // ifSpeed
		{"1.3.6.1.2.1.2.2.1.10", codec.TypeCounter32}, // ifInOctets
	} {
		got, err := p.wireValue(codec.MustParseOID(c.oid), dhsc.Value{Kind: dhsc.KindUint, Uint: 9})
		if err != nil || got.Type != c.want {
			t.Errorf("%s = %v (%v), want %v", c.oid, got.Type, err, c.want)
		}
	}
}

func TestAnUnnameableObjectIsRefusedBeforeTheWire(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public", Write: "private"})
	ctx := context.Background()
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "nothing.like.this"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 1}); err == nil {
		t.Error("a name no MIB knows must be refused")
	}
	// And after Disconnect there is no session to refuse it with.
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "sysDescr.0"}); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Errorf("after Disconnect = %v", err)
	}
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "sysDescr.0"}, dhsc.Value{}); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Errorf("after Disconnect = %v", err)
	}
}

func TestOIDFromBindsRefusesTheWrongSyntax(t *testing.T) {
	if got := oidFromBinds(nil); got != nil {
		t.Errorf("no binds = %v", got)
	}
	if got := oidFromBinds([]codec.VarBind{{Value: codec.String("not an oid")}}); got != nil {
		t.Errorf("a string sysObjectID = %v", got)
	}
	got := oidFromBinds([]codec.VarBind{{Value: codec.ObjectID(codec.OID{1, 3, 6, 1, 4, 1, 27338})}})
	if got.String() != "1.3.6.1.4.1.27338" {
		t.Errorf("oid = %v", got)
	}
}

// scriptedNet is an agent that answers whatever a test needs it to:
// an empty reply, a sysObjectID that is not an OID, a socket that
// refuses to open. A real agent cannot be made to misbehave on demand,
// and these are the paths that only ever run when one does.
type scriptedNet struct {
	transport.Net
	dialErr error
	reply   func(codec.Message) codec.Message
}

func (s *scriptedNet) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if s.dialErr != nil {
		return nil, s.dialErr
	}
	mine, theirs := net.Pipe()
	go func() {
		defer func() { _ = theirs.Close() }()
		buf := make([]byte, 65535)
		for {
			n, err := theirs.Read(buf)
			if err != nil {
				return
			}
			req, err := codec.Decode(buf[:n])
			if err != nil {
				return
			}
			out, err := codec.Encode(s.reply(req))
			if err != nil {
				return
			}
			if _, err := theirs.Write(out); err != nil {
				return
			}
		}
	}()
	return mine, nil
}

// emptyReply answers every request with no varbinds at all.
func emptyReply(req codec.Message) codec.Message {
	return codec.Message{
		Version: req.Version, Community: req.Community,
		PDU: &codec.PDU{Type: codec.PDUTypeResponse, RequestID: req.PDU.RequestID},
	}
}

func scriptedPlugin(t *testing.T, n *scriptedNet) *Plugin {
	t.Helper()
	f := &Factory{}
	p := f.New(plugin.Deps{Logger: quiet(), Net: n, Clock: clock.System()}).(*Plugin)
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func TestAnAgentThatAnswersNothingIsNotAValue(t *testing.T) {
	p := scriptedPlugin(t, &scriptedNet{Net: transport.New(transport.Config{}), reply: emptyReply})
	ctx := context.Background()
	// Connect succeeds: the agent replied, it simply said nothing.
	if err := p.Connect(ctx, "127.0.0.1", 161); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "sysDescr.0"}); err == nil ||
		!strings.Contains(err.Error(), "no varbind in the reply") {
		t.Errorf("GetValue = %v", err)
	}
	p.SetCommunity("public", "private")
	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "sysLocation.0"},
		dhsc.Value{Kind: dhsc.KindString, Str: "x"}); err == nil ||
		!strings.Contains(err.Error(), "no varbind in the confirm read") {
		t.Errorf("SetValue = %v", err)
	}
	// With nothing under any root, a walk has no model to return.
	if objs, err := p.Walk(ctx, 0); err != nil || len(objs) != 0 {
		t.Errorf("Walk = %d objects, %v", len(objs), err)
	}
	if _, err := p.pollProfileFor(ctx, dhsc.ValueRequest{}); err == nil {
		t.Error("a device with no objects has nothing to poll")
	}
}

func TestAWriteSessionThatWillNotOpen(t *testing.T) {
	n := &scriptedNet{Net: transport.New(transport.Config{}), reply: emptyReply}
	p := scriptedPlugin(t, n)
	if err := p.Connect(context.Background(), "127.0.0.1", 161); err != nil {
		t.Fatal(err)
	}
	p.SetCommunity("public", "private")
	n.dialErr = errors.New("no socket today")
	if _, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "sysLocation.0"},
		dhsc.Value{Kind: dhsc.KindString, Str: "x"}); err == nil {
		t.Error("a write session that will not open must be reported")
	}
}

func TestPollProfileReportsAScopeThatWillNotWalk(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	deadSession(t, p)
	if _, err := p.pollProfileFor(context.Background(), dhsc.ValueRequest{Path: "system"}); err == nil {
		t.Error("a scope whose walk fails must be reported, not polled empty")
	}
}

func TestSmallPiecesSayWhatTheyMean(t *testing.T) {
	// branchOf: a scalar's instance is not part of its branch.
	if got := branchOfFor("1.3.6.1.2.1.1.1.0"); got != "1.3.6.1.2.1.1.1" {
		t.Errorf("branchOf(scalar) = %s", got)
	}
	if got := branchOfFor("1.3.6.1.2.1.1.1"); got != "1.3.6.1.2.1.1.1" {
		t.Errorf("branchOf(branch) = %s", got)
	}
	// named: an integer no item matches keeps its own shape.
	def := &mib.Object{Enums: []mib.Enum{{Name: "up", Value: 1}}}
	if got := named(def, dhsc.Value{Kind: dhsc.KindInt, Int: 9}); got.Kind != dhsc.KindInt || got.Int != 9 {
		t.Errorf("unmatched ordinal = %+v", got)
	}
	// wireValue: an object no MIB knows takes a string as a string.
	p := &Plugin{}
	if v, err := p.wireValue(codec.MustParseOID("2.9.9.9"),
		dhsc.Value{Kind: dhsc.KindString, Str: "hello"}); err != nil || v.Type != codec.TypeOctetString {
		t.Errorf("unknown object = %+v, %v", v, err)
	}
}

func branchOfFor(s string) string {
	p := &Plugin{}
	return p.branchOf(codec.MustParseOID(s)).String()
}

func TestAWalkedPathIsWhatTheDeviceSaidItWas(t *testing.T) {
	// After a walk the device's own answer is the index: an agent may
	// publish an instance no MIB row predicted, and the path a walk
	// printed must still resolve to it.
	p := connected(t, provider.Communities{Read: "public"})
	ctx := context.Background()
	objs, err := p.Walk(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	var path string
	for _, o := range objs {
		if strings.HasPrefix(o.OID, "1.3.6.1.4.1.54981.") {
			path = strings.Join(o.Path, ".")
			break
		}
	}
	if path == "" {
		t.Skip("the agent published nothing under its own branch")
	}
	if _, err := p.GetValue(ctx, dhsc.ValueRequest{Path: path}); err != nil {
		t.Errorf("a walked path must resolve: %s: %v", path, err)
	}
}

func TestWriteAnEnumByTheWordTheManualUses(t *testing.T) {
	p := &Plugin{}
	// The ATEME IRD's lock state: "true" and "false" are its own words.
	v, err := p.wireValue(codec.MustParseOID("1.3.6.1.4.1.27338.5.5.3.3.1"),
		dhsc.Value{Kind: dhsc.KindString, Str: "true"})
	if err != nil || v.Type != codec.TypeInteger || v.Int != 1 {
		t.Fatalf("by name = %+v, %v", v, err)
	}
	// Case does not matter; a word that is in no item list does.
	if v, err := p.wireValue(codec.MustParseOID("1.3.6.1.4.1.27338.5.5.3.3.1"),
		dhsc.Value{Kind: dhsc.KindString, Str: "FALSE"}); err != nil || v.Int != 2 {
		t.Errorf("case-insensitive = %+v, %v", v, err)
	}
	if _, err := p.wireValue(codec.MustParseOID("1.3.6.1.4.1.27338.5.5.3.3.1"),
		dhsc.Value{Kind: dhsc.KindString, Str: "maybe"}); err == nil {
		t.Error("a word no item list has must be refused")
	}
}

func TestWalkUnderNeedsASession(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public"})
	if err := p.Disconnect(); err != nil {
		t.Fatal(err)
	}
	if _, err := p.walkUnder(context.Background(), codec.MustParseOID("1.3.6.1.2.1.1")); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Errorf("walkUnder without a session = %v", err)
	}
}

func TestTheWriteLandsButTheReadSessionIsGone(t *testing.T) {
	// The confirm read is not optional: with no session to read back
	// on, a SET is not a value in place.
	p := connected(t, provider.Communities{Read: "public", Write: "private"})
	p.SetCommunity("public", "private")
	ctx := context.Background()
	if _, err := p.writeSession(ctx); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	sess := p.sess
	p.sess = nil
	p.mu.Unlock()
	defer func() { _ = sess.Close() }()

	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "1.3.6.1.4.1.54981.3.0"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 1}); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Errorf("err = %v", err)
	}
}

func TestPollProfileNeedsAModelItCanRead(t *testing.T) {
	// No scope, no walk yet, and the device has stopped answering:
	// there is no plan to build, and saying so beats polling nothing.
	p := connected(t, provider.Communities{Read: "public"})
	deadSession(t, p)
	if _, err := p.pollProfileFor(context.Background(), dhsc.ValueRequest{}); err == nil {
		t.Error("a model that cannot be read is not an empty model")
	}
}

func TestWalkOfASilentBranch(t *testing.T) {
	// The system group answers, the device's own branch does not: the
	// model is incomplete, not absent, and the walk says so rather
	// than failing.
	p := connected(t, provider.Communities{Read: "public"})
	p.mu.Lock()
	p.sysOID = codec.MustParseOID("1.3.6.1.2.1.1") // no enterprise branch
	p.mu.Unlock()
	objs, err := p.Walk(context.Background(), 0)
	if err != nil || len(objs) == 0 {
		t.Fatalf("Walk = %d objects, %v", len(objs), err)
	}
}

func TestAValueTheSyntaxCannotCarryNeverReachesTheWire(t *testing.T) {
	p := connected(t, provider.Communities{Read: "public", Write: "private"})
	p.SetCommunity("public", "private")
	// sysUpTime is TimeTicks: a word is not one, and the refusal comes
	// from here rather than from the agent.
	_, err := p.SetValue(context.Background(), dhsc.ValueRequest{Path: "sysUpTime.0"},
		dhsc.Value{Kind: dhsc.KindString, Str: "sideways"})
	if err == nil || !strings.Contains(err.Error(), "is not a number") {
		t.Errorf("err = %v", err)
	}
}
