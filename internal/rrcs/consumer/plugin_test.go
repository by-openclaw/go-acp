package consumer

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	dhsc "dhs/internal/consumer"
	"dhs/internal/plugin"
	"dhs/internal/rrcs/codec"
)

// pluginCard is one entry of a GetAllClientCards answer.
func pluginCard(node, bay int32) codec.Value {
	return codec.Struct(
		codec.Member{Name: "Node", Value: codec.Int(node)},
		codec.Member{Name: "Bay", Value: codec.Int(bay)},
		codec.Member{Name: "ClientCardTypeString", Value: codec.String("AES67-108 G2")},
		codec.Member{Name: "LongName", Value: codec.String("card")},
		codec.Member{Name: "ObjectID", Value: codec.Int(1000 + bay)},
	)
}

// pluginFake answers the two requests the session part of the plugin
// sends, with the client cards given.
func pluginFake(t *testing.T, cards ...codec.Value) *fakeRRCS {
	t.Helper()
	return newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		key := call.Params[0]
		switch call.Method {
		case "GetVersion":
			return codec.Array(key, codec.String("9.0.1")), nil
		case "GetAllClientCards":
			return codec.Array(key, codec.Array(cards...)), nil
		}
		return codec.Value{}, &codec.Fault{Code: 1, String: "unexpected " + call.Method}
	})
}

func pluginConnect(t *testing.T, f *fakeRRCS) *Plugin {
	t.Helper()
	p, ok := (&Factory{}).New(plugin.Deps{}).(*Plugin)
	if !ok {
		t.Fatal("Factory.New: not a *Plugin")
	}
	host, portText, err := net.SplitHostPort(f.addr())
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	if err := p.Connect(context.Background(), host, port); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Disconnect() })
	return p
}

func (f *fakeRRCS) methods() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var names []string
	for _, c := range f.calls {
		names = append(names, c.Method)
	}
	return strings.Join(names, ",")
}

// The footprint is part of the contract: Connect is one request, the
// cards are read once whatever is asked after.
func TestPluginSessionRequests(t *testing.T) {
	f := pluginFake(t, pluginCard(60, 1), pluginCard(60, 2), pluginCard(60, 4))
	p := pluginConnect(t, f)
	if got := f.methods(); got != "GetVersion" {
		t.Fatalf("Connect sent %q, want GetVersion alone", got)
	}

	ctx := context.Background()
	info, err := p.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("GetDeviceInfo: %v", err)
	}
	if info.NumSlots != 5 || info.DtdVersion != "9.0.1" {
		t.Errorf("GetDeviceInfo = %+v, want 5 slots (node and bays 1 to 4) and version 9.0.1", info)
	}

	want := map[int]dhsc.SlotStatus{0: dhsc.SlotPresent, 1: dhsc.SlotPresent, 3: dhsc.SlotNoCard, 4: dhsc.SlotPresent, 9: dhsc.SlotNoCard}
	for slot, status := range want {
		si, err := p.GetSlotInfo(ctx, slot)
		if err != nil {
			t.Fatalf("GetSlotInfo(%d): %v", slot, err)
		}
		if si.Status != status {
			t.Errorf("slot %d = %s, want %s", slot, si.Status, status)
		}
	}
	if got := f.methods(); got != "GetVersion,GetAllClientCards" {
		t.Errorf("session sent %q, want the cards read once", got)
	}
}

// Cards of several nodes: the bays are not numbered as slots.
func TestPluginSeveralNodes(t *testing.T) {
	f := pluginFake(t, pluginCard(60, 1), pluginCard(70, 1))
	p := pluginConnect(t, f)
	ctx := context.Background()

	info, err := p.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.NumSlots != 1 {
		t.Errorf("NumSlots = %d, want 1", info.NumSlots)
	}
	si, _ := p.GetSlotInfo(ctx, 1)
	if si.Status != dhsc.SlotNoCard {
		t.Errorf("slot 1 = %s, want no_card", si.Status)
	}
}

func TestPluginNotConnected(t *testing.T) {
	p := (&Factory{}).New(plugin.Deps{})
	if _, err := p.GetDeviceInfo(context.Background()); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Errorf("GetDeviceInfo before Connect: %v, want ErrNotConnected", err)
	}
	if _, err := p.GetValue(context.Background(), dhsc.ValueRequest{}); !errors.Is(err, dhsc.ErrNotConnected) {
		t.Errorf("GetValue: %v, want ErrNotConnected", err)
	}
}

// walkFake answers like the production gateway does: one node (60), its
// client cards in bays 1 and 2, and the ports on node numbers of their
// own (61 and 62), never on 60.
func walkFake(t *testing.T) *fakeRRCS {
	t.Helper()
	port := func(node, number, id int32, label string) codec.Value {
		return codec.Struct(
			codec.Member{Name: "Net", Value: codec.Int(1)},
			codec.Member{Name: "Node", Value: codec.Int(node)},
			codec.Member{Name: "Port", Value: codec.Int(number)},
			codec.Member{Name: "Input", Value: codec.Bool(true)},
			codec.Member{Name: "Output", Value: codec.Bool(true)},
			codec.Member{Name: "Label", Value: codec.String(label)},
			codec.Member{Name: "ObjectID", Value: codec.Int(id)},
			codec.Member{Name: "PortAes67Input", Value: codec.Struct(
				codec.Member{Name: "Protocol", Value: codec.Int(5)},
				codec.Member{Name: "Channels", Value: codec.Int(1)},
			)},
		)
	}
	return newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		key := call.Params[0]
		switch call.Method {
		case "GetVersion":
			return codec.Array(key, codec.String("9.0.1")), nil
		case "GetAllNodes":
			return codec.Array(key, codec.Array(codec.Struct(
				codec.Member{Name: "NodeAddress", Value: codec.Int(60)},
				codec.Member{Name: "NodeTypeString", Value: codec.String("ARTIST_1024")},
				codec.Member{Name: "ObjectID", Value: codec.Int(221474832)},
			))), nil
		case "GetAllClientCards":
			return codec.Array(key, codec.Array(pluginCard(60, 1), pluginCard(60, 2))), nil
		case "GetAllPorts":
			return codec.Array(key, codec.Array(
				port(61, 1024, 7001, "NOC1"), port(61, 1026, 7002, "NOC2"), port(62, 1024, 7003, "MCR"),
			)), nil
		case "GetAllConferences":
			return codec.Array(key, codec.Array(codec.Struct(
				codec.Member{Name: "Label", Value: codec.String("CONF1")},
				codec.Member{Name: "LongName", Value: codec.String("Conf 1")},
				codec.Member{Name: "ObjectID", Value: codec.Int(555)},
				codec.Member{Name: "MemberList", Value: codec.Array(codec.Struct(
					codec.Member{Name: "Node", Value: codec.Int(61)},
					codec.Member{Name: "Port", Value: codec.Int(1024)},
					codec.Member{Name: "Talk", Value: codec.Bool(true)},
					codec.Member{Name: "Listen", Value: codec.Bool(true)},
				))},
			))), nil
		case "GetAllGroups", "GetAllIFBs":
			return codec.Array(key, codec.Array()), nil
		case "GetAllLogicSources_v2":
			return codec.Array(key, codec.Struct()), nil
		}
		return codec.Value{}, &codec.Fault{Code: 1, String: "unexpected " + call.Method}
	})
}

// A walk of one bay is three requests and gives the card and the ports
// of that card only, each with the path RRCS uses and the object ID.
func TestPluginWalkSlot(t *testing.T) {
	f := walkFake(t)
	p := pluginConnect(t, f)

	objs, err := p.Walk(context.Background(), 1)
	if err != nil {
		t.Fatalf("Walk(1): %v", err)
	}
	if got := f.methods(); got != "GetVersion,GetAllNodes,GetAllClientCards,GetAllPorts" {
		t.Errorf("Walk(1) sent %q, want the three lists", got)
	}

	byPath := map[string]dhsc.Object{}
	for _, o := range objs {
		byPath[strings.Join(o.Path, ".")] = o
		if o.Slot != 1 {
			t.Errorf("%s: slot %d, want 1", strings.Join(o.Path, "."), o.Slot)
		}
		if strings.HasPrefix(strings.Join(o.Path, "."), "net.1.node.62.") {
			t.Errorf("%s belongs to bay 2, not to bay 1", strings.Join(o.Path, "."))
		}
	}

	label, ok := byPath["net.1.node.61.port.1024.Label"]
	if !ok {
		t.Fatalf("no object for the label of port 1024; got %d objects", len(objs))
	}
	if label.OID != "7001" || label.ID != 7001 || label.Value.Str != "NOC1" || label.Kind != dhsc.KindString {
		t.Errorf("label = %+v, want oid 7001 and value NOC1", label)
	}

	proto, ok := byPath["net.1.node.61.port.1024.PortAes67Input.Protocol"]
	if !ok {
		t.Fatal("no object for PortAes67Input.Protocol")
	}
	if proto.Value.Int != 5 || proto.Access&accessWrite == 0 || len(proto.EnumItems) != 3 {
		t.Errorf("protocol = %+v, want 5, writable, three named values", proto)
	}

	if _, ok := byPath["net.1.node.60.card.1.Bay"]; !ok {
		t.Error("no object for the card of bay 1")
	}
}

// Slot 0 is the node and what belongs to no card: six requests, the
// node's properties and the conference with its member. An empty bay
// gives no objects.
func TestPluginWalkSlotZero(t *testing.T) {
	f := walkFake(t)
	p := pluginConnect(t, f)
	ctx := context.Background()

	objs, err := p.Walk(ctx, 0)
	if err != nil {
		t.Fatalf("Walk(0): %v", err)
	}
	if got := f.methods(); got != "GetVersion,"+strings.Join(slotZeroLists, ",") {
		t.Errorf("Walk(0) sent %q, want the six lists", got)
	}
	byPath := map[string]dhsc.Object{}
	for _, o := range objs {
		if o.Slot != 0 {
			t.Errorf("%s: slot %d, want 0", strings.Join(o.Path, "."), o.Slot)
		}
		byPath[strings.Join(o.Path, ".")] = o
	}
	nodeType, ok := byPath["net.1.node.60.NodeTypeString"]
	if !ok || nodeType.Value.Str != "ARTIST_1024" || nodeType.OID != "221474832" {
		t.Errorf("node type = %+v (found %v), want ARTIST_1024 with the node's oid", nodeType, ok)
	}
	label, ok := byPath["conference.555.Label"]
	if !ok || label.Value.Str != "CONF1" || label.OID != "555" {
		t.Errorf("conference label = %+v (found %v)", label, ok)
	}
	member, ok := byPath["conference.555.member.1"]
	if !ok || !strings.Contains(member.Value.Str, "net.1.node.61.port.1024") {
		t.Errorf("conference member = %+v (found %v), want the port path", member, ok)
	}

	if objs, err := p.Walk(ctx, 5); err != nil || len(objs) != 0 {
		t.Errorf("Walk(5) = %d objects, %v", len(objs), err)
	}
}

// §8.1: the six numbers of a crosspoint come from two port paths, the
// methods send them in the order of the specification, and the active
// crosspoints come back with the paths of their ports.
func TestCrosspoints(t *testing.T) {
	var sent []codec.Call
	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		sent = append(sent, call)
		key := call.Params[0]
		switch call.Method {
		case "GetVersion":
			return codec.Array(key, codec.String("9.0.1")), nil
		case "SetXp", "KillXp", "SetXpPrio", "SetXpDestructive":
			return codec.Array(key, codec.Int(0)), nil
		case "GetXpStatus":
			return codec.Array(key, codec.Int(0), codec.Bool(true)), nil
		case "GetAllActiveXps":
			return codec.Struct(
				codec.Member{Name: "XP Count", Value: codec.Int(1)},
				codec.Member{Name: "XP#1", Value: codec.Array(codec.Int(1), codec.Int(61), codec.Int(1024), codec.Int(1), codec.Int(62), codec.Int(1024))},
			), nil
		case "GetAllPorts":
			port := func(node int32, label string) codec.Value {
				return codec.Struct(
					codec.Member{Name: "Net", Value: codec.Int(1)},
					codec.Member{Name: "Node", Value: codec.Int(node)},
					codec.Member{Name: "Port", Value: codec.Int(1024)},
					codec.Member{Name: "Input", Value: codec.Bool(true)},
					codec.Member{Name: "Output", Value: codec.Bool(true)},
					codec.Member{Name: "Label", Value: codec.String(label)},
				)
			}
			return codec.Array(key, codec.Array(port(61, "NOC1"), port(62, "MCR"))), nil
		}
		return codec.Value{}, &codec.Fault{Code: 1, String: "unexpected " + call.Method}
	})
	p := pluginConnect(t, f)
	ctx := context.Background()

	if _, err := XpOfPaths("net.1.node.61.port.1024", "conference.3"); err == nil {
		t.Error("XpOfPaths took a path that is not a port")
	}
	a, err := XpOfPaths("net.1.node.61.port.1024.in", "net.1.node.62.port.1024.out")
	if err != nil {
		t.Fatal(err)
	}
	if want := (XpAddress{1, 61, 1024, 1, 62, 1024}); a != want {
		t.Fatalf("XpOfPaths = %+v, want %+v", a, want)
	}

	if _, err := p.client.SetXp(ctx, a); err != nil {
		t.Fatalf("SetXp: %v", err)
	}
	if _, err := p.client.SetXpPrio(ctx, a, 3); err != nil {
		t.Fatalf("SetXpPrio: %v", err)
	}
	on, err := p.client.XpStatus(ctx, a)
	if err != nil || !on {
		t.Fatalf("XpStatus = %v, %v, want made", on, err)
	}
	if _, err := p.client.KillXp(ctx, a); err != nil {
		t.Fatalf("KillXp: %v", err)
	}
	numbers := func(call codec.Call) string {
		var out []string
		for _, v := range call.Params[1:] {
			out = append(out, strconv.Itoa(int(v.Int)))
		}
		return call.Method + " " + strings.Join(out, " ")
	}
	want := []string{"SetXp 1 61 1024 1 62 1024", "SetXpPrio 1 61 1024 1 62 1024 3", "GetXpStatus 1 61 1024 1 62 1024", "KillXp 1 61 1024 1 62 1024"}
	for i, w := range want {
		if got := numbers(sent[i+1]); got != w {
			t.Errorf("request %d = %q, want %q", i+1, got, w)
		}
	}

	active, err := p.client.ActiveXps(ctx)
	if err != nil || len(active) != 1 || active[0] != a {
		t.Fatalf("ActiveXps = %+v, %v, want the one crosspoint", active, err)
	}
	xps, err := p.Crosspoints(ctx)
	if err != nil || len(xps) != 1 {
		t.Fatalf("Crosspoints = %+v, %v", xps, err)
	}
	if xps[0].Source != "net.1.node.61.port.1024" || xps[0].SourceLabel != "NOC1" ||
		xps[0].Destination != "net.1.node.62.port.1024" || xps[0].DestinationLabel != "MCR" {
		t.Errorf("crosspoint = %+v, want NOC1 > MCR by path", xps[0])
	}
}

// Any method of the table is sent by name with named arguments, its
// parameters in the order of the specification.
func TestDoByName(t *testing.T) {
	var last codec.Call
	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		last = call
		return codec.Array(call.Params[0], codec.Int(0)), nil
	})
	c := f.client(t, Config{})
	ctx := context.Background()

	if _, err := c.Do(ctx, "SetInputGain", map[string]string{"port-path": "net.1.node.63.port.1064.in", "gain": "-12"}, nil); err != nil {
		t.Fatalf("SetInputGain: %v", err)
	}
	got := []int32{last.Params[1].Int, last.Params[2].Int, last.Params[3].Int, last.Params[4].Int}
	if want := []int32{1, 63, 1064, -12}; got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Errorf("SetInputGain sent %v, want %v", got, want)
	}

	if _, err := c.Do(ctx, "SetInputGain", map[string]string{"port-path": "net.1.node.63.port.1064.in"}, nil); err == nil {
		t.Error("SetInputGain without a gain was sent")
	}
	if _, err := c.Do(ctx, "NoSuchMethod", nil, nil); err == nil {
		t.Error("a method that is not in the table was sent")
	}

	// Every entry builds its parameters from arguments of the right kind.
	for i := range Ops {
		op := &Ops[i]
		args := map[string]string{"src": "net.1.node.61.port.1024", "dst": "net.1.node.62.port.1024"}
		for _, p := range op.Params {
			switch p.Kind {
			case "int":
				args[p.Flag] = "1"
			case "bool":
				args[p.Flag] = "yes"
			case "text":
				args[p.Flag] = "x"
			case "netnodeport", "nodeport", "portaddr":
				args[p.Flag] = "net.1.node.61.port.1024"
			case "key", "cmdpos":
				args[p.Flag] = "net.1.node.61.port.1024.key.0.1.2"
			case "changes":
				args[p.Flag] = "[]"
			}
		}
		if _, err := op.Values(args, []string{"net.1.node.61.port.1024>net.1.node.62.port.1024"}); err != nil {
			t.Errorf("%s: %v", op.Method, err)
		}
	}
}

// GetValue and SetValue: one property through the plugin — a port read
// in one request, a direct write of §8.3 to §8.5 read back through its
// own request, and a configuration-change property refused towards the
// guarded verbs.
func TestPluginValues(t *testing.T) {
	gain := int32(-128)
	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		key := call.Params[0]
		switch call.Method {
		case "GetVersion":
			return codec.Array(key, codec.String("9.0.1")), nil
		case "GetPort":
			return codec.Array(key, codec.Struct(
				codec.Member{Name: "Label", Value: codec.String("NOC1")},
				codec.Member{Name: "PortAes67Input", Value: codec.Struct(
					codec.Member{Name: "Protocol", Value: codec.Int(5)},
				)},
			)), nil
		case "SetInputGain":
			gain = call.Params[4].Int
			return codec.Array(key, codec.Int(0)), nil
		case "GetInputGain":
			return codec.Array(key, codec.Int(0), codec.Int(gain)), nil
		}
		return codec.Value{}, &codec.Fault{Code: 1, String: "unexpected " + call.Method}
	})
	p := pluginConnect(t, f)
	ctx := context.Background()

	v, err := p.GetValue(ctx, dhsc.ValueRequest{Path: "net.1.node.61.port.1024.Label"})
	if err != nil || v.Str != "NOC1" {
		t.Fatalf("GetValue label = %+v, %v", v, err)
	}
	v, err = p.GetValue(ctx, dhsc.ValueRequest{Path: "net.1.node.61.port.1024.PortAes67Input.Protocol"})
	if err != nil || v.Int != 5 {
		t.Fatalf("GetValue protocol = %+v, %v", v, err)
	}

	v, err = p.SetValue(ctx, dhsc.ValueRequest{Path: "net.1.node.61.port.1024.InputGain"},
		dhsc.Value{Kind: dhsc.KindInt, Int: -12})
	if err != nil || v.Int != -12 || gain != -12 {
		t.Fatalf("SetValue gain = %+v, %v (sent %d)", v, err, gain)
	}

	if _, err := p.SetValue(ctx, dhsc.ValueRequest{Path: "net.1.node.61.port.1024.PortAes67Input.Protocol"},
		dhsc.Value{Kind: dhsc.KindInt, Int: 2}); err == nil || !strings.Contains(err.Error(), "ADR-0035") {
		t.Fatalf("a stream property went through the plugin: %v", err)
	}
}

// Subscribe registers once, events reach the callback decoded, and the
// last Unsubscribe removes the registration.
func TestPluginSubscribe(t *testing.T) {
	registered := 0
	f := newFakeRRCS(t, func(call codec.Call) (codec.Value, *codec.Fault) {
		key := call.Params[0]
		switch call.Method {
		case "GetVersion":
			return codec.Array(key, codec.String("9.0.1")), nil
		case "RegisterForAllEvents":
			registered++
			return codec.Array(key, codec.Int(0)), nil
		case "UnregisterForAllEvents":
			registered--
			return codec.Array(key, codec.Int(0)), nil
		}
		return codec.Value{}, &codec.Fault{Code: 1, String: "unexpected " + call.Method}
	})
	p := pluginConnect(t, f)

	got := make(chan dhsc.Event, 4)
	if err := p.Subscribe(dhsc.ValueRequest{}, func(e dhsc.Event) { got <- e }); !errors.Is(err, dhsc.ErrNotImplemented) {
		t.Fatalf("Subscribe without a callback: %v", err)
	}
	p.SetCallback(8195, "")
	if err := p.Subscribe(dhsc.ValueRequest{}, func(e dhsc.Event) { got <- e }); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := p.Subscribe(dhsc.ValueRequest{Path: "logic"}, func(dhsc.Event) {}); err != nil {
		t.Fatalf("second Subscribe: %v", err)
	}
	if registered != 1 {
		t.Fatalf("RegisterForAllEvents sent %d times, want 1", registered)
	}

	// A PortInactive notification, as RRCS posts it to the listener.
	p.EventHandler().OnEvent(Event{Method: "PortInactive", Params: []codec.Value{
		codec.Int(1), codec.Int(61), codec.Int(1024),
	}})
	e := <-got
	if e.Path != "net.1.node.61.port.1024.Online" || e.Value.Bool || e.Value.Kind != dhsc.KindBool {
		t.Errorf("event = %+v, want Online false", e)
	}

	if err := p.Unsubscribe(dhsc.ValueRequest{Path: "logic"}); err != nil || registered != 1 {
		t.Fatalf("Unsubscribe of one path: %v, registered %d", err, registered)
	}
	if err := p.Unsubscribe(dhsc.ValueRequest{}); err != nil || registered != 0 {
		t.Fatalf("last Unsubscribe: %v, registered %d", err, registered)
	}
}
