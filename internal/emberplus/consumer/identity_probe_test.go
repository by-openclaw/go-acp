package emberplus

import (
	"context"
	"strings"
	"sync"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/emberplus/codec/glow"
)

func newProbeTestPlugin() *Plugin {
	return &Plugin{
		numIndex:   make(map[string]*treeEntry),
		pathIndex:  make(map[string]*treeEntry),
		labelIndex: make(map[string][]*treeEntry),
		treeMu:     sync.RWMutex{},
	}
}

// addIdentityNode registers a Node entry whose numericPath, label path,
// glowNode.Identifier and (optional) schemaIdentifiers reflect what a
// real walk would produce.
func addIdentityNode(p *Plugin, oid []int32, path []string, schemaIdentifiers string) *treeEntry {
	n := &glow.Node{
		Identifier:        path[len(path)-1],
		SchemaIdentifiers: schemaIdentifiers,
	}
	e := &treeEntry{
		obj: consumer.Object{
			Path:  append([]string(nil), path...),
			Label: n.Identifier,
		},
		glowNode:    n,
		numericPath: append([]int32(nil), oid...),
	}
	p.numIndex[numericKey(oid)] = e
	p.pathIndex[strings.Join(path, ".")] = e
	p.labelIndex[n.Identifier] = append(p.labelIndex[n.Identifier], e)
	return e
}

// addChildParam registers a child Parameter under parent at sub-index
// childNum, with the given identifier and string value.
func addChildParam(p *Plugin, parent *treeEntry, childNum int32, identifier, value string) *treeEntry {
	oid := append(append([]int32(nil), parent.numericPath...), childNum)
	path := append(append([]string(nil), parent.obj.Path...), identifier)
	e := &treeEntry{
		obj: consumer.Object{
			Path:  path,
			Label: identifier,
			Value: consumer.Value{Kind: consumer.KindString, Str: value},
		},
		glowParam:   &glow.Parameter{Identifier: identifier, Value: value},
		numericPath: oid,
	}
	p.numIndex[numericKey(oid)] = e
	p.pathIndex[strings.Join(path, ".")] = e
	p.labelIndex[identifier] = append(p.labelIndex[identifier], e)
	return e
}

// TestIdentityProbe_SchemaIdentifiers — DTD 2.30+ providers where the
// identity Node carries schemaIdentifiers ending in ".identity"
// (e.g. Lawo "de.l-s-b.emberplus.identity"). Spec p.87 NodeContents [4].
func TestIdentityProbe_SchemaIdentifiers(t *testing.T) {
	p := newProbeTestPlugin()
	id := addIdentityNode(p, []int32{1}, []string{"id"}, "de.l-s-b.emberplus.identity")
	addChildParam(p, id, 1, "product", "Power Core")
	addChildParam(p, id, 2, "version", "1.21")

	got, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if want := "Power Core@1.21"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_LowercaseIdentifier — TinyEmberPlus / libember-cpp
// sample convention: Node named "identity" (lowercase) with
// product/version children.
func TestIdentityProbe_LowercaseIdentifier(t *testing.T) {
	p := newProbeTestPlugin()
	router := addIdentityNode(p, []int32{1}, []string{"router"}, "")
	router.glowNode.Identifier = "router" // overwrite; this isn't the identity node
	id := addIdentityNode(p, []int32{1, 1}, []string{"router", "identity"}, "")
	addChildParam(p, id, 1, "product", "Tiny Ember+ Router")
	addChildParam(p, id, 2, "version", "1.6.2")

	got, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if want := "Tiny Ember+ Router@1.6.2"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_CapitalisedIdentifier_DHD — DHD audio devices use
// a capitalised "Identity" node (case differs from libember sample).
// Children also capitalised: "Product", "Firmwareversion". Probe must
// match case-insensitively and recognise "Firmwareversion" as a
// version candidate.
func TestIdentityProbe_CapitalisedIdentifier_DHD(t *testing.T) {
	p := newProbeTestPlugin()
	device := addIdentityNode(p, []int32{1}, []string{"Device"}, "")
	device.glowNode.Identifier = "Device"
	id := addIdentityNode(p, []int32{1, 1}, []string{"Device", "Identity"}, "")
	addChildParam(p, id, 1, "Company", "DHD audio GmbH")
	addChildParam(p, id, 2, "Series", "Series52")
	addChildParam(p, id, 3, "Product", "52-7520")
	addChildParam(p, id, 4, "Firmwareversion", "10.1.7.1")

	got, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if want := "52-7520@10.1.7.1"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_NestedIdentity_PowerCore — Lawo PowerCore nests
// "identity" under a "PowerCore" Node at depth 2. Verifies the probe
// finds identity at any depth, not only at root.
func TestIdentityProbe_NestedIdentity_PowerCore(t *testing.T) {
	p := newProbeTestPlugin()
	powerCore := addIdentityNode(p, []int32{1}, []string{"PowerCore"}, "")
	powerCore.glowNode.Identifier = "PowerCore"
	id := addIdentityNode(p, []int32{1, 30}, []string{"PowerCore", "identity"}, "")
	addChildParam(p, id, 1, "product", "PowerCore Rev3 (710/13)")
	addChildParam(p, id, 2, "company", "Lawo AG / DSA-Volgmann")
	addChildParam(p, id, 3, "serial", "00-59-E3-88-31-27-FB-0B")
	addChildParam(p, id, 4, "version", "8.2.93")
	addChildParam(p, id, 5, "role", "br-r-pclaw-001")

	got, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if want := "PowerCore Rev3 (710/13)@8.2.93"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_RealLawo_mc2 — Lawo mc² 56 mk3 layout observed
// from a live capture: identity Node at the root, lowercase
// identifiers, "version" is the canonical version field.
func TestIdentityProbe_RealLawo_mc2(t *testing.T) {
	p := newProbeTestPlugin()
	id := addIdentityNode(p, []int32{1}, []string{"identity"}, "")
	addChildParam(p, id, 1, "product", "mc2_56_mk3")
	addChildParam(p, id, 2, "company", "Lawo AG")
	addChildParam(p, id, 3, "role", "MXALAW-241-primary")
	addChildParam(p, id, 4, "version", "12-2-0-0 build 45")
	addChildParam(p, id, 5, "productVersion", "12.2.0.3")

	got, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if want := "mc2_56_mk3@12-2-0-0 build 45"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_SchemaPreferredOverIdentifier — when both
// detection layers match, schemaIdentifiers (strict-spec DTD 2.30+)
// wins.
func TestIdentityProbe_SchemaPreferredOverIdentifier(t *testing.T) {
	p := newProbeTestPlugin()
	legacy := addIdentityNode(p, []int32{1}, []string{"identity"}, "")
	addChildParam(p, legacy, 1, "product", "LegacyProduct")
	addChildParam(p, legacy, 2, "version", "0.0.1")
	modern := addIdentityNode(p, []int32{2}, []string{"id"}, "de.l-s-b.emberplus.identity")
	addChildParam(p, modern, 1, "product", "ModernProduct")
	addChildParam(p, modern, 2, "version", "1.21")

	got, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if want := "ModernProduct@1.21"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_ProductFallback_Name — provider whose identity
// uses "name" rather than "product".
func TestIdentityProbe_ProductFallback_Name(t *testing.T) {
	p := newProbeTestPlugin()
	id := addIdentityNode(p, []int32{1}, []string{"identity"}, "")
	addChildParam(p, id, 1, "name", "DeviceA")
	addChildParam(p, id, 2, "version", "2.0")

	got, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if want := "DeviceA@2.0"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_VersionFallback_SoftwareVersion — provider whose
// identity uses "softwareVersion" rather than "version".
func TestIdentityProbe_VersionFallback_SoftwareVersion(t *testing.T) {
	p := newProbeTestPlugin()
	id := addIdentityNode(p, []int32{1}, []string{"identity"}, "")
	addChildParam(p, id, 1, "product", "DeviceB")
	addChildParam(p, id, 2, "softwareVersion", "3.4.5")

	got, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if want := "DeviceB@3.4.5"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_RejectsNonZeroSlot pins the slot=0 invariant —
// Ember+ flattens the Glow tree into one logical slot.
func TestIdentityProbe_RejectsNonZeroSlot(t *testing.T) {
	p := newProbeTestPlugin()
	id := addIdentityNode(p, []int32{1}, []string{"identity"}, "")
	addChildParam(p, id, 1, "product", "x")
	addChildParam(p, id, 2, "version", "1")

	_, err := p.IdentityProbe(context.Background(), 1)
	if err == nil {
		t.Fatal("want error for slot=1, got nil")
	}
}

// TestIdentityProbe_NoIdentityNode — the indexes are populated but no
// Node satisfies either detection layer.
func TestIdentityProbe_NoIdentityNode(t *testing.T) {
	p := newProbeTestPlugin()
	root := addIdentityNode(p, []int32{1}, []string{"router"}, "")
	root.glowNode.Identifier = "router"
	addChildParam(p, root, 1, "product", "x")

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when no identity node found, got nil")
	}
}

// TestIdentityProbe_MissingProduct — never fabricate identity.
func TestIdentityProbe_MissingProduct(t *testing.T) {
	p := newProbeTestPlugin()
	id := addIdentityNode(p, []int32{1}, []string{"identity"}, "")
	addChildParam(p, id, 1, "version", "1.6.2")

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
	id := addIdentityNode(p, []int32{1}, []string{"identity"}, "")
	addChildParam(p, id, 1, "product", "Tiny Ember+ Router")

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when version is missing, got nil")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("error should mention version, got %q", err.Error())
	}
}

// TestIdentityProbe_EmptyValues rejects empty strings — empty
// identity would collide every such provider into the same cache slot.
func TestIdentityProbe_EmptyValues(t *testing.T) {
	p := newProbeTestPlugin()
	id := addIdentityNode(p, []int32{1}, []string{"identity"}, "")
	addChildParam(p, id, 1, "product", "")
	addChildParam(p, id, 2, "version", "")

	_, err := p.IdentityProbe(context.Background(), 0)
	if err == nil {
		t.Fatal("want error when identity values are empty, got nil")
	}
}

// TestIdentityProbe_NoIdentityWrapper_EMTWO — vendor that omits the
// "identity" wrapper entirely and puts identity fields directly on a
// "Device" Node, with spaces in identifier names. Probe must fall
// through to Layer 3 (children-matching) and normalise identifier
// names ("Hardware Name" → "hardwarename") to find the match.
func TestIdentityProbe_NoIdentityWrapper_EMTWO(t *testing.T) {
	p := newProbeTestPlugin()
	device := addIdentityNode(p, []int32{1}, []string{"Device"}, "")
	device.glowNode.Identifier = "Device"
	addChildParam(p, device, 1, "Hardware Name", "EMTWO")
	addChildParam(p, device, 2, "Software Version", "4.8.0.1745420015")
	addChildParam(p, device, 3, "Serial Number", "12507150001")
	addChildParam(p, device, 4, "Device Name", "bm-r-rfus3-010")
	addChildParam(p, device, 5, "PTP Status", "locked")
	addChildParam(p, device, 6, "BESS Version", "Version 1.1a")

	got, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if want := "EMTWO@4.8.0.1745420015"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestIdentityProbe_Layer2_BeatsLayer3 — when a strict "identity"
// Node is present alongside a Layer-3-matching candidate, the strict
// match must win.
func TestIdentityProbe_Layer2_BeatsLayer3(t *testing.T) {
	p := newProbeTestPlugin()
	device := addIdentityNode(p, []int32{1}, []string{"Device"}, "")
	device.glowNode.Identifier = "Device"
	addChildParam(p, device, 1, "Hardware Name", "WrongProduct")
	addChildParam(p, device, 2, "Software Version", "wrong.version")
	id := addIdentityNode(p, []int32{2}, []string{"identity"}, "")
	addChildParam(p, id, 1, "product", "RightProduct")
	addChildParam(p, id, 2, "version", "1.0.0")

	got, err := p.IdentityProbe(context.Background(), 0)
	if err != nil {
		t.Fatalf("IdentityProbe: %v", err)
	}
	if want := "RightProduct@1.0.0"; got != want {
		t.Errorf("identity = %q, want %q", got, want)
	}
}

// TestNormalizeIdentifier covers the identifier normalisation used
// by the Layer-3 children matcher.
func TestNormalizeIdentifier(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"product", "product"},
		{"Product", "product"},
		{"Hardware Name", "hardwarename"},
		{"hardware_name", "hardwarename"},
		{"Hardware-Name", "hardwarename"},
		{"Software Version", "softwareversion"},
		{"  Trimmed Spaces  ", "trimmedspaces"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeIdentifier(c.in); got != c.want {
			t.Errorf("normalizeIdentifier(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestHasIdentitySchema exercises the newline-separated
// schemaIdentifiers parser per spec p.87 NodeContents [4].
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
