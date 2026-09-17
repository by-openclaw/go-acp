package main

import (
	"bytes"
	"strings"
	"testing"

	"dhs/internal/consumer"
)

// fixtureEmberPlusTree returns a 3-element synthetic Ember+ tree
// shaped like the integration-test identity-strict DM. Used to pin
// the PlantUML mindmap output independently of any live device.
func fixtureEmberPlusTree() []consumer.Object {
	return []consumer.Object{
		{
			OID:   "1",
			Label: "dhs-emberplus-integration",
			Path:  []string{"dhs-emberplus-integration"},
			Kind:  consumer.KindRaw,
		},
		{
			OID:   "1.0",
			Label: "identity",
			Path:  []string{"dhs-emberplus-integration", "identity"},
			Kind:  consumer.KindRaw,
		},
		{
			OID:   "1.0.1",
			Label: "product",
			Path:  []string{"dhs-emberplus-integration", "identity", "product"},
			Kind:  consumer.KindString,
			Value: consumer.Value{Kind: consumer.KindString, Str: "Tiny Ember+ Router"},
		},
	}
}

// TestRenderPlantUML_StartEndMarkers pins the document envelope —
// PlantUML's `plantuml.jar` rejects anything without `@startmindmap`
// and `@endmindmap`.
func TestRenderPlantUML_StartEndMarkers(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTreePlantUML(&buf, fixtureEmberPlusTree(), treeRenderOpts{}); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "@startmindmap\n") {
		t.Errorf("missing @startmindmap header:\n%s", out)
	}
	if !strings.Contains(out, "\n@endmindmap\n") {
		t.Errorf("missing @endmindmap footer:\n%s", out)
	}
}

// TestRenderPlantUML_DepthEncodedByStars pins the mindmap depth
// notation: root is `*`, identity is `**`, product is `***`.
func TestRenderPlantUML_DepthEncodedByStars(t *testing.T) {
	var buf bytes.Buffer
	if err := renderTreePlantUML(&buf, fixtureEmberPlusTree(), treeRenderOpts{}); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"* device",                             // synthetic root
		"** dhs-emberplus-integration [oid=1]", // top-level node
		"*** identity [oid=1.0]",               // child node
		"**** product (string) = ",             // leaf with kind + value
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing line %q in:\n%s", want, out)
		}
	}
}

// TestRenderPlantUML_FocusPath drops ancestor siblings — the PlantUML
// path must mirror the ASCII renderer's focus-and-descend behavior so
// docs use the same rendering.
func TestRenderPlantUML_FocusPath(t *testing.T) {
	objs := append(fixtureEmberPlusTree(),
		consumer.Object{
			OID:   "1.1",
			Label: "oneToN",
			Path:  []string{"dhs-emberplus-integration", "oneToN"},
			Kind:  consumer.KindRaw,
		})
	var buf bytes.Buffer
	err := renderTreePlantUML(&buf, objs, treeRenderOpts{
		FromPath: "dhs-emberplus-integration.identity",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "identity") {
		t.Errorf("focused subtree missing identity:\n%s", out)
	}
	if strings.Contains(out, "oneToN") {
		t.Errorf("oneToN sibling must not appear when focused on identity:\n%s", out)
	}
}

// TestRenderPlantUML_DepthCap drops descendants past the cap. With
// depth=1 from the focus the renderer must include direct children
// but no grandchildren.
func TestRenderPlantUML_DepthCap(t *testing.T) {
	var buf bytes.Buffer
	err := renderTreePlantUML(&buf, fixtureEmberPlusTree(), treeRenderOpts{
		FromPath: "dhs-emberplus-integration",
		Depth:    1,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "identity") {
		t.Errorf("depth=1 must include direct child identity:\n%s", out)
	}
	if strings.Contains(out, "product") {
		t.Errorf("depth=1 must not include grandchild product:\n%s", out)
	}
}

// TestRenderPlantUML_FilterDropsNonMatching pins the --filter
// behavior: lines without the substring are dropped, but the
// document envelope still bookends the output.
func TestRenderPlantUML_FilterDropsNonMatching(t *testing.T) {
	var buf bytes.Buffer
	err := renderTreePlantUML(&buf, fixtureEmberPlusTree(), treeRenderOpts{
		Filter: "product",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "@startmindmap\n") {
		t.Errorf("envelope missing after filter:\n%s", out)
	}
	if !strings.Contains(out, "product") {
		t.Errorf("matching line dropped:\n%s", out)
	}
}

// TestRenderPlantUML_ConflictingFocusFlags pins the mutual-exclusion
// guard: passing both --from-path and --from-oid is a usage error.
func TestRenderPlantUML_ConflictingFocusFlags(t *testing.T) {
	var buf bytes.Buffer
	err := renderTreePlantUML(&buf, fixtureEmberPlusTree(), treeRenderOpts{
		FromPath: "identity",
		FromOID:  "1.0",
	})
	if err == nil {
		t.Fatal("expected error for conflicting flags")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error message lost intent: %v", err)
	}
}
