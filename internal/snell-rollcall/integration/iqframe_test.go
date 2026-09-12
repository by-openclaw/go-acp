//go:build integration

// The IQ frame, served from the repository alone.
//
// The IQ modular frame at 10.6.255.113 lists its cards in its own port list:
// five IQDBE00 Nodal cards on the odd ports 01 to 09, three IQMUX42 AES cards
// on 0B, 0C and 0D, under unit 0x0C. Both device models were walked off that
// frame, so this serves real cards at the addresses a client of the real frame
// uses, with no hardware in the room — and fails the day either the placement
// or the identities drift.
//
// The gateway on port 0 here is still ours — the connector's own honest status
// page, by design (see provider/gateway.go). The real controller's 720-object
// menu is served and walked from its own DM in gateway_test.go instead. The
// instance names ("EMB.06 (Nodal)") are in no DM and are not asserted here.
//
// Run with:
//
//	go test -tags integration ./internal/snell-rollcall/integration/... -run IQFrame

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

// iqFrameCards is the real frame's own port list for its cards, as it
// answered on 2026-09-11.
var iqFrameCards = []struct {
	address string
	typeID  string
}{
	{"0000-0C-01", "562"},
	{"0000-0C-03", "562"},
	{"0000-0C-05", "562"},
	{"0000-0C-07", "562"},
	{"0000-0C-09", "562"},
	{"0000-0C-0B", "389"},
	{"0000-0C-0C", "389"},
	{"0000-0C-0D", "389"},
}

// serveIQFrame builds the frame the committed manifest describes, places its
// cards the way the producer does, and serves it on a port the system picks.
func serveIQFrame(t *testing.T) (string, int) {
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

func TestTheIQFrameIsServedAtTheAddressesTheRealOneUses(t *testing.T) {
	host, port := serveIQFrame(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	// The gateway, then its eight cards, in port order.
	if info.NumSlots != 1+len(iqFrameCards) {
		t.Fatalf("the frame lists %d nodes, want the gateway and %d cards",
			info.NumSlots, len(iqFrameCards))
	}

	for i, want := range iqFrameCards {
		si, err := c.GetSlotInfo(ctx, i+1)
		if err != nil {
			t.Fatalf("slot %d: %v", i+1, err)
		}
		addr := si.Identity["address"]
		if !strings.HasPrefix(addr, want.address) {
			t.Errorf("card %d answers at %q, want %s as on the real frame", i+1, addr, want.address)
		}
		if got := si.Identity["type_id"]; got != want.typeID {
			t.Errorf("card at %s answers as type %s, want %s", want.address, got, want.typeID)
		}
	}
}

func TestEveryIQFrameCardServesTheMenuItWasWalkedWith(t *testing.T) {
	host, port := serveIQFrame(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	c := rcconsumer.New(plugin.Deps{}.WithDefaults())
	if err := c.Connect(ctx, host, port); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = c.Disconnect() }()

	// One Nodal and one AES card: the two device models, each served whole.
	// The counts are what the walks off the real frame produced; a DM that
	// quietly lost most of itself would still serve, and this notices.
	for _, c2 := range []struct {
		slot int
		min  int
	}{
		{1, 150}, // IQDBE00, committed with 167 menu lines
		{6, 80},  // IQMUX42, committed with 91 menu lines
	} {
		objs, err := c.Walk(ctx, c2.slot)
		if err != nil {
			t.Fatalf("walk slot %d: %v", c2.slot, err)
		}
		if len(objs) < c2.min {
			t.Errorf("slot %d served %d objects, want at least %d", c2.slot, len(objs), c2.min)
		}
	}
}
