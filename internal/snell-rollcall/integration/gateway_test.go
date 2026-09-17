//go:build integration

// The real controller's own menu, served from the repository alone.
//
// The IQ frame's controller is an IQH3UM4-S gateway board (unit type 429). Its
// menu is not the thirteen honest lines this connector serves about itself on
// port 0 — it is a 720-object control surface with the pages an operator drives
// the frame from, including the restart command a power cycle goes through.
// That menu was walked off the real controller at 10.6.255.113 and filed as
// IQH3UM4-S@5.25.cs21, and this serves it back and re-walks it whole.
//
// It is the controller analogue of TestEveryIQFrameCardServesTheMenuItWasWalked
// With: a Tier-4 loopback that fails the day the committed DM quietly loses most
// of itself — a DM stripped to a handful of lines would still serve, and a walk
// that came back short is what notices. The paged wire form the real controller
// answers in is walked against the device itself and pinned by the codec's unit
// tests; this pins the scale.
//
// Run with:
//
//	go test -tags integration ./internal/snell-rollcall/integration/... -run Gateway

package rollcall_integration

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"dhs/internal/manifest"
	"dhs/internal/plugin"
	rcconsumer "dhs/internal/snell-rollcall/consumer"
	rcprovider "dhs/internal/snell-rollcall/provider"
)

// serveGateway serves the one-slot manifest that carries the real controller's
// menu, on a port the system picks.
func serveGateway(t *testing.T) (string, int) {
	t.Helper()

	m, err := manifest.Load(filepath.Join(fixtureRoot, "manifest", "iq-gateway.json"))
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
		t.Fatalf("place the controller: %v", err)
	}
	p.SetUnit(0x0C)
	p.SetLongStrings(false) // the real frame advertises no long strings

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

func TestTheGatewayAnswersAsTheControllerTypeItReallyIs(t *testing.T) {
	host, port := serveGateway(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := rcconsumer.New(plugin.Deps{}.WithDefaults())
	if err := c.Connect(ctx, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = c.Disconnect() }()

	// The controller is served on one slot, and it answers as unit type 429 —
	// the IQH3UM4-S gateway board — not as the provider's own name.
	si, err := c.GetSlotInfo(ctx, 1)
	if err != nil {
		t.Fatalf("slot 1: %v", err)
	}
	if got := si.Identity["type_id"]; got != "429" {
		t.Errorf("the controller answers as type %s, want 429 (IQH3UM4-S)", got)
	}
}

func TestTheControllerServesTheWholeMenuItWasWalkedWith(t *testing.T) {
	host, port := serveGateway(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := rcconsumer.New(plugin.Deps{}.WithDefaults())
	if err := c.Connect(ctx, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = c.Disconnect() }()

	// The controller was committed with 720 menu objects. A DM that lost most of
	// itself would still serve; a walk that came back short is what notices.
	objs, err := c.Walk(ctx, 1)
	if err != nil {
		t.Fatalf("walk the controller: %v", err)
	}
	if len(objs) < 700 {
		t.Errorf("the controller served %d objects, want at least 700 of the 720 it was walked with", len(objs))
	}
}
