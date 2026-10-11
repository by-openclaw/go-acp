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
	if _, err := p.Walk(context.Background(), 0); !errors.Is(err, dhsc.ErrNotImplemented) {
		t.Errorf("Walk: %v, want ErrNotImplemented", err)
	}
}
