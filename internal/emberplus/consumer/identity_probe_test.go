package emberplus

import (
	"context"
	"strings"
	"sync"
	"testing"

	"dhs/internal/protocol"
)

// newProbeTestPlugin returns a Plugin with the in-RAM tree shape needed
// by IdentityProbe — only pathIndex is populated; numIndex / labelIndex
// stay nil because IdentityProbe never reads them.
func newProbeTestPlugin() *Plugin {
	return &Plugin{
		pathIndex: make(map[string]*treeEntry),
		treeMu:    sync.RWMutex{},
	}
}

func putIdentity(p *Plugin, path, value string) {
	p.pathIndex[path] = &treeEntry{
		obj: protocol.Object{
			Path: strings.Split(path, "."),
			Value: protocol.Value{
				Kind: protocol.KindString,
				Str:  value,
			},
		},
	}
}

// TestIdentityProbe_HappyPath asserts the canonical "<Product>@<Version>"
// shape for a provider that exposes the full router.identity subtree.
// Mirrors TinyEmberPlusRouter 1.6.2 and Lawo Power Core wire shape.
func TestIdentityProbe_HappyPath(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentity(p, "router.identity.product", "Tiny Ember+ Router")
	putIdentity(p, "router.identity.version", "1.6.2")

	id, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if got, want := id, "Tiny Ember+ Router@1.6.2"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_RejectsNonZeroSlot pins the slot=0 invariant —
// Ember+ flattens the Glow tree into one logical slot.
func TestIdentityProbe_RejectsNonZeroSlot(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentity(p, "router.identity.product", "x")
	putIdentity(p, "router.identity.version", "1")

	_, err := p.IdentityProbe(context.Background(), 1)
	if err == nil {
		t.Fatal("want error for slot=1, got nil")
	}
}

// TestIdentityProbe_MissingProduct verifies we never fabricate identity —
// per ADR-0022, providers that omit identity fall back to the IP-keyed
// cache path (legacy behaviour).
func TestIdentityProbe_MissingProduct(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentity(p, "router.identity.version", "1.6.2")

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when product is missing, got nil")
	}
	if !strings.Contains(err.Error(), "product") {
		t.Errorf("error should mention product, got %q", err.Error())
	}
}

// TestIdentityProbe_MissingVersion mirrors MissingProduct for the other
// half of the identity pair.
func TestIdentityProbe_MissingVersion(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentity(p, "router.identity.product", "Tiny Ember+ Router")

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when version is missing, got nil")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error should mention version, got %q", err.Error())
	}
}

// TestIdentityProbe_EmptyValues rejects empty strings rather than
// returning "@" as the cache key — empty identity would collide every
// such provider into the same cache slot.
func TestIdentityProbe_EmptyValues(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentity(p, "router.identity.product", "")
	putIdentity(p, "router.identity.version", "")

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when identity values are empty, got nil")
	}
}

// TestIdentityProbe_NotWalkedYet covers the pre-condition: IdentityProbe
// is called by cmd/dhs after a successful Walk; if pathIndex is empty
// the probe must error rather than return a bogus identity.
func TestIdentityProbe_NotWalkedYet(t *testing.T) {
	p := newProbeTestPlugin()

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when pathIndex is empty (walk not yet run), got nil")
	}
}
