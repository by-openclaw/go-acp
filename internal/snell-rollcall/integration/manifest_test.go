//go:build integration

// The producer builds its frame from the repository and nothing else.
//
// That is the whole point of the committed manifest: a fixture that needs a
// device is a fixture nobody can run, and a provider that cannot be started
// without one cannot be tested in CI. What is served here is a real IQDBE00
// card — walked once off the IQ modular frame at 10.6.255.113, committed, and
// replayed ever since with no hardware in the room.
//
// Run with:
//
//	go test -tags integration ./internal/snell-rollcall/integration/... -run Manifest

package rollcall_integration

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"dhs/internal/export/canonical"
	"dhs/internal/manifest"
	"dhs/internal/plugin"
	rcconsumer "dhs/internal/snell-rollcall/consumer"
	rcprovider "dhs/internal/snell-rollcall/provider"
)

const (
	fixtureRoot  = "../testdata/integration-test"
	fixtureModel = "IQDBE00"
)

// serveFixture starts a provider built from the committed manifest and returns
// the address it is listening on.
func serveFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(fixtureRoot, "manifest", "rollcall-integration.json")
	tree, err := treeFromFixture(path)
	if err != nil {
		t.Fatalf("build the tree from the committed manifest: %v", err)
	}

	deps := plugin.Deps{}.WithDefaults()
	p := rcprovider.New(deps, tree)

	// A port the operating system picks, so two runs of the suite never argue
	// over one.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if cerr := ln.Close(); cerr != nil {
		t.Fatalf("close the probe listener: %v", cerr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- p.Serve(ctx, addr) }()

	// Serve binds before it accepts, and a client that dials the instant this
	// returns would race the bind. Dialling until it answers is what a client
	// does anyway.
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

	t.Cleanup(func() {
		cancel()
		<-done
	})
	return addr
}

func TestManifestBuildsAFrameFromTheRepositoryAlone(t *testing.T) {
	addr := serveFixture(t)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("port: %v", err)
	}

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
	// The manifest names two slots and the gateway makes a third node, which
	// is what a client enumerating this frame finds.
	if info.NumSlots < 2 {
		t.Fatalf("the frame reports %d nodes, want at least the two the manifest names",
			info.NumSlots)
	}

	objs, err := c.Walk(ctx, 1)
	if err != nil {
		t.Fatalf("walk slot 1: %v", err)
	}
	if len(objs) == 0 {
		t.Fatal("slot 1 walked to nothing; the DM did not reach the served frame")
	}

	// The card is the one that was captured, named as the device named itself
	// rather than as the manifest labelled it.
	var found bool
	for _, o := range objs {
		if o.Label == fixtureModel {
			found = true
			break
		}
		for _, seg := range o.Path {
			if seg == fixtureModel {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("walked %d objects and none belongs to %s", len(objs), fixtureModel)
	}

	// And the second slot carries the same card, because one DM describes a
	// product rather than a position: a frame with two of them is two slots
	// pointing at one file.
	second, err := c.Walk(ctx, 2)
	if err != nil {
		t.Fatalf("walk slot 2: %v", err)
	}
	if len(second) != len(objs) {
		t.Errorf("slot 2 walked to %d objects and slot 1 to %d, from the same DM",
			len(second), len(objs))
	}
}

func TestTheCommittedDMIsTheOneTheDeviceGave(t *testing.T) {
	// A DM trimmed to nothing would still serve; what makes this fixture worth
	// keeping is that it is the card's whole menu, as walked. The count is the
	// cheapest thing that notices a fixture quietly losing most of itself.
	path := filepath.Join(fixtureRoot, "manifest", "rollcall-integration.json")
	tree, err := treeFromFixture(path)
	if err != nil {
		t.Fatalf("build the tree: %v", err)
	}

	root := tree.Root.Common()
	if len(root.Children) != 2 {
		t.Fatalf("the frame carries %d slots, want the two the manifest names",
			len(root.Children))
	}
	card := root.Children[0].Common()
	if n := elementCount(root.Children[0]); n < 100 {
		t.Errorf("%s carries %d elements; it was committed with 168", card.Identifier, n)
	}
}

// treeFromFixture assembles the frame the committed manifest describes.
func treeFromFixture(path string) (*canonical.Export, error) {
	m, err := manifest.Load(path)
	if err != nil {
		return nil, err
	}
	return manifest.BuildExport(m, fixtureRoot)
}

// elementCount is how many elements hang off one, including itself.
func elementCount(e canonical.Element) int {
	n := 1
	for _, k := range e.Common().Children {
		n += elementCount(k)
	}
	return n
}
