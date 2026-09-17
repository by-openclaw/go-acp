//go:build integration

// Our own IPShare in front of several frames at once.
//
// The vendor RollProxy exists "to enable connection to more than one Ethernet
// enabled IQ chassis": one connection for the client, one subnet per chassis.
// This fronts two frames served in-process — the committed IQ frame at 2100
// and our own router (a frame with a matrix) at 4000 — behind one proxy, and
// drives our consumer against it: both chains, both gateways, and a card and
// the router's nodes reached at their routes.
//
// Run with:
//
//	go test -tags integration ./internal/snell-rollcall/integration/... -run IPShareFrames
package rollcall_integration

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dhs/internal/export/canonical"
	"dhs/internal/plugin"
	rcconsumer "dhs/internal/snell-rollcall/consumer"
	rcprovider "dhs/internal/snell-rollcall/provider"
)

// serveRouter serves the router export — a frame with two cards and a matrix —
// as unit 0x20 on a port the system picks.
func serveRouter(t *testing.T) (string, int) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "testdata", "exports", "router_tree.json"))
	if err != nil {
		t.Fatalf("read the router export: %v", err)
	}
	var tree canonical.Export
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("decode the router export: %v", err)
	}

	p := rcprovider.New(plugin.Deps{}.WithDefaults(), &tree)
	p.SetUnit(0x20)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if cerr := ln.Close(); cerr != nil {
		t.Fatalf("close the probe listener: %v", cerr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Serve(ctx, addr) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitForPort(t, addr)

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)
	return host, port
}

// waitForPort blocks until something accepts on addr.
func waitForPort(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = c.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing came up on %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOurIPShareFrontsSeveralFrames(t *testing.T) {
	iqHost, iqPort := serveIQFrame(t)
	rtHost, rtPort := serveRouter(t)

	p := rcprovider.New(plugin.Deps{}.WithDefaults(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := p.SetProxy(ctx, rcprovider.ProxyConfig{Unit: 0xFF, Frames: []rcprovider.ProxyFrame{
		{Subnet: 0x2100, Upstream: net.JoinHostPort(iqHost, strconv.Itoa(iqPort))},
		{Subnet: 0x4000, Upstream: net.JoinHostPort(rtHost, strconv.Itoa(rtPort))},
	}})
	if err != nil {
		t.Fatalf("front both frames: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	serveCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Serve(serveCtx, addr) }()
	t.Cleanup(func() {
		stop()
		<-done
	})
	waitForPort(t, addr)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	// Everything the consumer reaches through the proxy, by address.
	c := rcconsumer.New(plugin.Deps{}.WithDefaults())
	if err := c.Connect(ctx, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = c.Disconnect() }()
	info, err := c.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("device info: %v", err)
	}
	seen := make(map[string]string, info.NumSlots)
	routerSlot := -1
	for i := 0; i < info.NumSlots; i++ {
		si, err := c.GetSlotInfo(ctx, i)
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		seen[si.Identity["address"]] = si.Identity["type_id"]
		if routerSlot < 0 && strings.HasPrefix(si.Identity["address"], "4000-20-01") {
			routerSlot = i
		}
	}

	// Both chains from the one map, and both frames at their routes.
	for _, want := range []string{"0000-02-00", "2000-01-00", "0000-04-00", "2100-0C-00", "4000-20-00"} {
		if addrType(seen, want) == "" {
			t.Errorf("%s was not reached through the proxy: %v", want, seen)
		}
	}
	// The IQ frame's cards behind 2100.
	for _, want := range iqFrameCards {
		routed := "2100" + strings.TrimPrefix(want.address, "0000")
		if got := addrType(seen, routed); got != want.typeID {
			t.Errorf("card %s answers as type %q, want %s", routed, got, want.typeID)
		}
	}
	// Our router's first node behind 4000, walked.
	if routerSlot < 0 {
		t.Fatalf("the router's first port was not reached at 4000-20-01: %v", seen)
	}
	objs, err := c.Walk(ctx, routerSlot)
	if err != nil {
		t.Fatalf("walk the router through the proxy: %v", err)
	}
	if len(objs) == 0 {
		t.Error("the router served nothing through the proxy")
	}
}
