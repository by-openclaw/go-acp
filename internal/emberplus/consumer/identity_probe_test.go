package emberplus

import (
	"context"
	"strings"
	"sync"
	"testing"

	"dhs/internal/emberplus/codec/glow"
	"dhs/internal/protocol"
)

func newProbeTestPlugin() *Plugin {
	return &Plugin{
		numIndex:   make(map[string]*treeEntry),
		pathIndex:  make(map[string]*treeEntry),
		labelIndex: make(map[string][]*treeEntry),
		treeMu:     sync.RWMutex{},
	}
}

// putIdentityNode registers an identity Node into numIndex + labelIndex.
// schemaIdentifiers is empty for DTD < 2.30 providers (identifier-only
// convention) and non-empty for DTD 2.30+ (schema-based).
func putIdentityNode(p *Plugin, parts []string, schemaIdentifiers string) *treeEntry {
	n := &glow.Node{
		Identifier:        parts[len(parts)-1],
		SchemaIdentifiers: schemaIdentifiers,
	}
	e := &treeEntry{
		obj: protocol.Object{
			Path:  append([]string(nil), parts...),
			Label: n.Identifier,
		},
		glowNode: n,
	}
	p.numIndex[strings.Join(parts, ".")] = e
	p.labelIndex[n.Identifier] = append(p.labelIndex[n.Identifier], e)
	return e
}

// putChildParam adds a child Parameter under parentPath with a string value.
func putChildParam(p *Plugin, parentPath, identifier, value string) {
	parts := append(strings.Split(parentPath, "."), identifier)
	key := strings.Join(parts, ".")
	e := &treeEntry{
		obj: protocol.Object{
			Path:  parts,
			Label: identifier,
			Value: protocol.Value{Kind: protocol.KindString, Str: value},
		},
		glowParam: &glow.Parameter{Identifier: identifier, Value: value},
	}
	p.pathIndex[key] = e
}

// TestIdentityProbe_SchemaIdentifiers — DTD 2.30+ providers where the
// identity Node carries schemaIdentifiers ending in ".identity"
// (e.g. Lawo "de.l-s-b.emberplus.identity"). Spec p.87 NodeContents [4].
func TestIdentityProbe_SchemaIdentifiers(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentityNode(p, []string{"id"}, "de.l-s-b.emberplus.identity")
	putChildParam(p, "id", "product", "Power Core")
	putChildParam(p, "id", "version", "1.21")

	id, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if got, want := id, "Power Core@1.21"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_IdentifierConvention — DTD < 2.30 providers that use
// the libember-cpp / TinyEmberPlus sample convention: a Node named
// "identity" with product/version children.
func TestIdentityProbe_IdentifierConvention(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentityNode(p, []string{"router", "identity"}, "")
	putChildParam(p, "router.identity", "product", "Tiny Ember+ Router")
	putChildParam(p, "router.identity", "version", "1.6.2")

	id, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if got, want := id, "Tiny Ember+ Router@1.6.2"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_SchemaPreferredOverIdentifier — when both detection
// layers match, schemaIdentifiers (strict-spec DTD 2.30+) wins.
func TestIdentityProbe_SchemaPreferredOverIdentifier(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentityNode(p, []string{"router", "identity"}, "")
	putChildParam(p, "router.identity", "product", "LegacyProduct")
	putChildParam(p, "router.identity", "version", "0.0.1")
	putIdentityNode(p, []string{"id"}, "de.l-s-b.emberplus.identity")
	putChildParam(p, "id", "product", "ModernProduct")
	putChildParam(p, "id", "version", "1.21")

	id, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if got, want := id, "ModernProduct@1.21"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_ProductFallbackName — provider whose identity uses
// "name" instead of "product".
func TestIdentityProbe_ProductFallbackName(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentityNode(p, []string{"identity"}, "")
	putChildParam(p, "identity", "name", "DeviceA")
	putChildParam(p, "identity", "version", "2.0")

	id, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if got, want := id, "DeviceA@2.0"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_VersionFallbackSoftwareVersion — provider whose
// identity uses "softwareVersion" instead of "version".
func TestIdentityProbe_VersionFallbackSoftwareVersion(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentityNode(p, []string{"identity"}, "")
	putChildParam(p, "identity", "product", "DeviceB")
	putChildParam(p, "identity", "softwareVersion", "3.4.5")

	id, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if got, want := id, "DeviceB@3.4.5"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_RejectsNonZeroSlot pins the slot=0 invariant —
// Ember+ flattens the Glow tree into one logical slot.
func TestIdentityProbe_RejectsNonZeroSlot(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentityNode(p, []string{"identity"}, "")
	putChildParam(p, "identity", "product", "x")
	putChildParam(p, "identity", "version", "1")

	_, err := p.IdentityProbe(context.Background(), 1)
	if err == nil {
		t.Fatal("want error for slot=1, got nil")
	}
}

// TestIdentityProbe_NotWalkedYet covers the pre-condition: indexes empty.
func TestIdentityProbe_NotWalkedYet(t *testing.T) {
	p := newProbeTestPlugin()

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when no identity node found, got nil")
	}
}

// TestIdentityProbe_MissingProduct — never fabricate identity.
func TestIdentityProbe_MissingProduct(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentityNode(p, []string{"identity"}, "")
	putChildParam(p, "identity", "version", "1.6.2")

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when product is missing, got nil")
	}
	if !strings.Contains(err.Error(), "product") {
		t.Errorf("error should mention product, got %q", err.Error())
	}
}

// TestIdentityProbe_MissingVersion — never fabricate identity.
func TestIdentityProbe_MissingVersion(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentityNode(p, []string{"identity"}, "")
	putChildParam(p, "identity", "product", "Tiny Ember+ Router")

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when version is missing, got nil")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error should mention version, got %q", err.Error())
	}
}

// TestIdentityProbe_EmptyValues rejects empty strings — empty identity
// would collide every such provider into the same cache slot.
func TestIdentityProbe_EmptyValues(t *testing.T) {
	p := newProbeTestPlugin()
	putIdentityNode(p, []string{"identity"}, "")
	putChildParam(p, "identity", "product", "")
	putChildParam(p, "identity", "version", "")

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when identity values are empty, got nil")
	}
}

// TestHasIdentitySchema exercises the newline-separated schemaIdentifiers
// parser per spec p.87 NodeContents [4].
func TestHasIdentitySchema(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"lawo single", "de.l-s-b.emberplus.identity", true},
		{"multi-line first matches", "de.l-s-b.emberplus.identity\nother.thing", true},
		{"multi-line second matches", "other.thing\nde.l-s-b.emberplus.identity", true},
		{"trailing whitespace", "  de.l-s-b.emberplus.identity  ", true},
		{"non-identity schema", "de.l-s-b.emberplus.matrix", false},
		{"partial suffix only", "foo.bar.notidentity", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasIdentitySchema(c.in); got != c.want {
				t.Errorf("hasIdentitySchema(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
