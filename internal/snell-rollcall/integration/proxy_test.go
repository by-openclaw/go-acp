//go:build integration

// Our own IPShare emulator, served from the repository alone.
//
// The provider fronts the committed IQ frame behind a RollCall IP Proxy at a
// network address, the way the vendor RollProxy does: a client sees the proxy
// unit, its virtual routing nodes, and behind them the frame's gateway and
// cards at the subnet route. This drives our own consumer against it — the same
// consumer that walks the real vendor proxy — and asserts it reaches the whole
// frame two hops out, with no proxy hardware in the room.
//
// Run with:
//
//	go test -tags integration ./internal/snell-rollcall/integration/... -run Proxy
package rollcall_integration

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dhs/internal/manifest"
	"dhs/internal/plugin"
	rcconsumer "dhs/internal/snell-rollcall/consumer"
	rcprovider "dhs/internal/snell-rollcall/provider"
)

// proxyFrameCards is the IQ frame's cards as they answer through the proxy: the
// same ports and types as on the real frame, but at the subnet route 1100.
var proxyFrameCards = []struct {
	address string
	typeID  string
}{
	{"1100-0C-01", "562"},
	{"1100-0C-03", "562"},
	{"1100-0C-05", "562"},
	{"1100-0C-07", "562"},
	{"1100-0C-09", "562"},
	{"1100-0C-0B", "389"},
	{"1100-0C-0C", "389"},
	{"1100-0C-0D", "389"},
}

// serveProxiedFrame serves the committed IQ frame behind our IPShare proxy on a
// port the system picks.
func serveProxiedFrame(t *testing.T) (string, int) {
	t.Helper()

	m, err := manifest.Load(filepath.Join(fixtureRoot, "manifest", "iq-frame-12.json"))
	if err != nil {
		t.Fatalf("load the manifest: %v", err)
	}
	tree, err := manifest.BuildExport(m, fixtureRoot)
	if err != nil {
		t.Fatalf("build the tree: %v", err)
	}

	p := rcprovider.New(plugin.Deps{}.WithDefaults(), tree)
	var ports []uint8
	var dms []string
	for _, s := range m.SlotDMs() {
		ports = append(ports, uint8(s.Slot))
		dms = append(dms, s.DM)
	}
	if err := p.SetCards(ports, dms); err != nil {
		t.Fatalf("place the cards: %v", err)
	}
	p.SetLongStrings(false) // the real frame advertises no long strings
	// Front it behind the proxy at subnet 1100, the frame on unit 0x0C, exactly
	// as the vendor RollProxy presented the real frame.
	if err := p.SetProxy(rcprovider.ProxyConfig{Unit: 0xFF, Subnet: 0x1100, Frame: 0x0C}); err != nil {
		t.Fatalf("configure the proxy: %v", err)
	}

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

	deadline := time.Now().Add(10 * time.Second)
	for {
		c, derr := net.DialTimeout("tcp", addr, time.Second)
		if derr == nil {
			_ = c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the provider never came up on %s: %v", addr, derr)
		}
		time.Sleep(20 * time.Millisecond)
	}

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	return host, port
}

func TestOurProxyIsWalkedLikeTheVendorBox(t *testing.T) {
	host, port := serveProxiedFrame(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := rcconsumer.New(plugin.Deps{}.WithDefaults())
	if err := c.Connect(ctx, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = c.Disconnect() }()

	// Everything the consumer enumerates through the proxy, by address.
	info, err := c.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("device info: %v", err)
	}
	seen := make(map[string]string, info.NumSlots)
	for i := 0; i < info.NumSlots; i++ {
		si, err := c.GetSlotInfo(ctx, i)
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		seen[si.Identity["address"]] = si.Identity["type_id"]
	}

	// The frame gateway and all eight cards, reached at the subnet route.
	if got := addrType(seen, "1100-0C-00"); got == "" {
		t.Errorf("the frame gateway at 1100-0C-00 was not reached through the proxy: %v", seen)
	}
	for _, want := range proxyFrameCards {
		got := addrType(seen, want.address)
		if got == "" {
			t.Errorf("card %s was not reached through the proxy", want.address)
			continue
		}
		if got != want.typeID {
			t.Errorf("card %s answers as type %s, want %s", want.address, got, want.typeID)
		}
	}
}

func TestACardIsWalkedThroughOurProxy(t *testing.T) {
	host, port := serveProxiedFrame(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := rcconsumer.New(plugin.Deps{}.WithDefaults())
	if err := c.Connect(ctx, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = c.Disconnect() }()

	// Find the slot the first Nodal card landed on and walk it through the proxy.
	info, err := c.GetDeviceInfo(ctx)
	if err != nil {
		t.Fatalf("device info: %v", err)
	}
	slot := -1
	for i := 0; i < info.NumSlots; i++ {
		si, err := c.GetSlotInfo(ctx, i)
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		if strings.HasPrefix(si.Identity["address"], "1100-0C-01") {
			slot = i
			break
		}
	}
	if slot < 0 {
		t.Fatal("the first card was not enumerated through the proxy")
	}

	objs, err := c.Walk(ctx, slot)
	if err != nil {
		t.Fatalf("walk the card through the proxy: %v", err)
	}
	if len(objs) < 150 {
		t.Errorf("the card served %d objects through the proxy, want at least 150", len(objs))
	}
}

// addrType returns the type id enumerated at an address prefix, or "".
func addrType(seen map[string]string, prefix string) string {
	for addr, ty := range seen {
		if strings.HasPrefix(addr, prefix) {
			return ty
		}
	}
	return ""
}
