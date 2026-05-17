package acp2

import (
	"testing"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
)

// TestSeedTreeFromCachedObjects pins #353: a snapshot's per-object
// Meta carries acp2.objType / acp2.numType / acp2.optionsMap, and
// SeedTreeFromCachedObjects rebuilds the parallel arrays so the
// announce decoder can resolve types + enum labels from the in-memory
// tree before any fresh walk runs.
//
// JSON round-trip note: encoded Meta values come back as float64 +
// map[string]any, so the test exercises BOTH the in-process path
// (uint8 / map[uint32]string) and the post-JSON path
// (float64 / map[string]any) via metaToUint8 + decodeStringlyOptionsMap.
func TestSeedTreeFromCachedObjects_DirectTypes(t *testing.T) {
	p := &Plugin{trees: newWalkedTreeCache(4, 0)}
	objs := []consumer.Object{
		{
			Slot: 1, ID: 17671, Label: "IO Board",
			Meta: map[string]any{
				"acp2.objType": uint8(codec.ObjTypeEnum),
				"acp2.numType": uint8(codec.NumTypePreset),
				"acp2.optionsMap": map[uint32]string{
					16: "NA", 120: "Present",
				},
			},
		},
	}
	p.SeedTreeFromCachedObjects(1, objs)

	tree, ok := p.trees.Get(1)
	if !ok || tree == nil {
		t.Fatal("trees not populated after seed")
	}
	if len(tree.ObjTypes) != 1 || tree.ObjTypes[0] != codec.ObjTypeEnum {
		t.Errorf("ObjTypes[0]=%v want Enum", tree.ObjTypes[0])
	}
	if tree.OptionsMaps[0] == nil || tree.OptionsMaps[0][120] != "Present" {
		t.Errorf("OptionsMap[120] not resolved: %v", tree.OptionsMaps[0])
	}
}

// TestSeedTreeFromCachedObjects_PostJSONShape covers the canonical
// JSON-decoded form (numbers as float64, maps as map[string]any),
// which is what tree_store.LoadByIdentity returns from disk.
func TestSeedTreeFromCachedObjects_PostJSONShape(t *testing.T) {
	p := &Plugin{trees: newWalkedTreeCache(4, 0)}
	objs := []consumer.Object{
		{
			Slot: 1, ID: 21127, Label: "Fan Control",
			Meta: map[string]any{
				"acp2.objType": float64(codec.ObjTypeEnum),
				"acp2.numType": float64(codec.NumTypePreset),
				"acp2.optionsMap": map[string]any{
					"786": "Automatic",
					"791": "Full Speed",
				},
			},
		},
	}
	p.SeedTreeFromCachedObjects(1, objs)

	tree, ok := p.trees.Get(1)
	if !ok || tree == nil {
		t.Fatal("trees not populated after seed")
	}
	if tree.ObjTypes[0] != codec.ObjTypeEnum {
		t.Errorf("ObjTypes[0]=%v want Enum", tree.ObjTypes[0])
	}
	if tree.OptionsMaps[0][786] != "Automatic" || tree.OptionsMaps[0][791] != "Full Speed" {
		t.Errorf("OptionsMap reconstruction failed: %v", tree.OptionsMaps[0])
	}
	if tree.Labels["Fan Control"] != 0 {
		t.Errorf("Labels not populated: %v", tree.Labels)
	}
}
