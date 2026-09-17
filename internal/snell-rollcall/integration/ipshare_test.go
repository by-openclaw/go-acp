//go:build integration

// Our own IPShare in front of a frame.
//
// The provider fronts a frame reached over the network — not a tree it serves —
// behind a RollCall IP Proxy at a network address: a client sees the proxy
// unit, its virtual routing nodes and, behind them, the frame's own gateway and
// cards at the subnet route, with every routed request carried to the frame and
// its answers carried back (provider/proxy_relay.go).
//
// Two tiers. The loopback fronts the committed IQ frame served in-process, so
// it runs from the repository alone and fails the day the relay stops carrying
// a session, a port list or a menu. The live one fronts the real IQ frame at
// ROLLCALL_TEST_HOST, read-only, and is what proves the relay against a frame
// that spoofs and zeroes addresses the way the vendor library does rather than
// the way this provider does.
//
// Run with:
//
//	go test -tags integration ./internal/snell-rollcall/integration/... -run IPShare
//	ROLLCALL_TEST_HOST=10.6.255.113 go test -tags integration ./internal/snell-rollcall/integration/... -run IPShare
package rollcall_integration

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"dhs/internal/plugin"
	rcconsumer "dhs/internal/snell-rollcall/consumer"
	rcprovider "dhs/internal/snell-rollcall/provider"
)

// ipshareSubnet is the route our IPShare fronts the frame at: 2100, two hops,
// chosen so it never collides with the vendor proxy's 1100 for the same rack
// when a client holds both.
const ipshareSubnet = 0x2100

// serveIPShare fronts the frame at upstream behind our IPShare on a port the
// system picks, and returns where a client connects.
func serveIPShare(t *testing.T, upstream string) (string, int) {
	t.Helper()

	// No tree: what it serves is the frame it fronts.
	p := rcprovider.New(plugin.Deps{}.WithDefaults(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.SetProxy(ctx, rcprovider.ProxyConfig{Unit: 0xFF, Subnet: ipshareSubnet, Upstream: upstream}); err != nil {
		t.Fatalf("front the frame at %s: %v", upstream, err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if cerr := ln.Close(); cerr != nil {
		t.Fatalf("close the probe listener: %v", cerr)
	}

	serveCtx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Serve(serveCtx, addr) }()
	t.Cleanup(func() {
		stop()
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
			t.Fatalf("the proxy never came up on %s: %v", addr, derr)
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

// walkThroughIPShare connects a consumer to our IPShare, enumerates everything
// it reaches by address, and walks the first card found at the subnet route.
func walkThroughIPShare(t *testing.T, host string, port int, cardPrefix string, minObjects int) map[string]string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

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
	card := -1
	for i := 0; i < info.NumSlots; i++ {
		si, err := c.GetSlotInfo(ctx, i)
		if err != nil {
			t.Fatalf("slot %d: %v", i, err)
		}
		seen[si.Identity["address"]] = si.Identity["type_id"]
		if card < 0 && strings.HasPrefix(si.Identity["address"], cardPrefix) {
			card = i
		}
	}
	if card < 0 {
		t.Fatalf("no card at %s was reached through our IPShare: %v", cardPrefix, seen)
	}

	objs, err := c.Walk(ctx, card)
	if err != nil {
		t.Fatalf("walk the card at %s through our IPShare: %v", cardPrefix, err)
	}
	if len(objs) < minObjects {
		t.Errorf("the card at %s served %d objects through our IPShare, want at least %d", cardPrefix, len(objs), minObjects)
	}
	return seen
}

func TestOurIPShareFrontsTheServedIQFrame(t *testing.T) {
	frameHost, framePort := serveIQFrame(t)
	host, port := serveIPShare(t, net.JoinHostPort(frameHost, strconv.Itoa(framePort)))

	seen := walkThroughIPShare(t, host, port, "2100-0C-01", 150)

	// The frame gateway and all eight cards, at the subnet route, with the
	// identities the real frame answers.
	if got := addrType(seen, "2100-0C-00"); got == "" {
		t.Errorf("the frame gateway at 2100-0C-00 was not reached through our IPShare: %v", seen)
	}
	for _, want := range iqFrameCards {
		routed := "2100" + strings.TrimPrefix(want.address, "0000")
		got := addrType(seen, routed)
		if got == "" {
			t.Errorf("card %s was not reached through our IPShare", routed)
			continue
		}
		if got != want.typeID {
			t.Errorf("card %s answers as type %s, want %s", routed, got, want.typeID)
		}
	}
}

func TestOurIPShareFrontsTheRealFrame(t *testing.T) {
	rack := os.Getenv("ROLLCALL_TEST_HOST")
	if rack == "" {
		t.Skip("ROLLCALL_TEST_HOST not set: no real frame to front")
	}
	if _, _, err := net.SplitHostPort(rack); err != nil {
		port := os.Getenv("ROLLCALL_TEST_PORT")
		if port == "" {
			port = strconv.Itoa(rcprovider.DefaultPort)
		}
		rack = net.JoinHostPort(rack, port)
	}

	host, port := serveIPShare(t, rack)

	// Read-only: enumerate the rack through our IPShare and walk its first card.
	// The real frame's first Nodal card is on port 01 (docs/testbed.md).
	seen := walkThroughIPShare(t, host, port, "2100-0C-01", 150)
	if got := addrType(seen, "2100-0C-00"); got == "" {
		t.Errorf("the real frame's gateway at 2100-0C-00 was not reached through our IPShare: %v", seen)
	}
	t.Logf("reached %d nodes through our IPShare in front of %s: %v", len(seen), rack, seen)
}
