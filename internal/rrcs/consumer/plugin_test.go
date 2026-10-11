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
	if _, err := p.GetValue(context.Background(), dhsc.ValueRequest{}); !errors.Is(err, dhsc.ErrNotImplemented) {
		t.Errorf("GetValue: %v, want ErrNotImplemented", err)
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

// An empty bay and the node itself give no objects and send nothing
// that is not needed.
func TestPluginWalkEmpty(t *testing.T) {
	f := walkFake(t)
	p := pluginConnect(t, f)
	ctx := context.Background()

	if objs, err := p.Walk(ctx, 0); err != nil || len(objs) != 0 {
		t.Errorf("Walk(0) = %d objects, %v", len(objs), err)
	}
	if got := f.methods(); got != "GetVersion" {
		t.Errorf("Walk(0) sent %q, want nothing", got)
	}
	if objs, err := p.Walk(ctx, 5); err != nil || len(objs) != 0 {
		t.Errorf("Walk(5) = %d objects, %v", len(objs), err)
	}
}
