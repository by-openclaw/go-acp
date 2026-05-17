package acp2

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"dhs/internal/acp2/codec"
	"dhs/internal/consumer"
)

// TestSeedTreeFromCachedObjects pins the disk-cache fast-path for
// watch (#323): a Plugin instance that hasn't done a live Walk should
// still serve a populated WalkedTree out of trees.Get(slot) after the
// CLI calls SeedTreeFromCachedObjects with the disk snapshot.
//
// The walker stashes acp2.objType / acp2.numType / acp2.optionsMap in
// obj.Meta on a normal walk; those keys round-trip through the disk
// JSON cache. SeedTreeFromCachedObjects reads them back to rebuild
// the parallel ObjTypes / NumTypes / OptionsMaps slices Subscribe
// needs to decode announces.
func TestSeedTreeFromCachedObjects(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	// Build snapshot-shaped objects: live-walker shape (uint8 in Meta).
	objs := []consumer.Object{
		{
			Slot: 1, ID: 5, Label: "Volume",
			Meta: map[string]any{
				"acp2.objType": uint8(codec.ObjTypeNumber),
				"acp2.numType": uint8(codec.NumTypeS32),
			},
		},
		{
			Slot: 1, ID: 6, Label: "Mute",
			Meta: map[string]any{
				"acp2.objType":    uint8(codec.ObjTypeEnum),
				"acp2.numType":    uint8(codec.NumTypePreset),
				"acp2.optionsMap": map[string]string{"0": "Off", "1": "On"},
			},
		},
	}

	p.SeedTreeFromCachedObjects(1, objs)

	tree, ok := p.trees.Get(1)
	if !ok || tree == nil {
		t.Fatal("trees.Get(1) returned no tree after seed")
	}
	if len(tree.Objects) != 2 {
		t.Fatalf("Objects len=%d want 2", len(tree.Objects))
	}
	if tree.ObjTypes[0] != codec.ObjTypeNumber {
		t.Errorf("ObjTypes[0]=%v want Number", tree.ObjTypes[0])
	}
	if tree.NumTypes[0] != codec.NumTypeS32 {
		t.Errorf("NumTypes[0]=%v want S32", tree.NumTypes[0])
	}
	if tree.ObjTypes[1] != codec.ObjTypeEnum {
		t.Errorf("ObjTypes[1]=%v want Enum", tree.ObjTypes[1])
	}
	if tree.OptionsMaps[1] == nil {
		t.Fatal("OptionsMaps[1] is nil; expected {0:Off, 1:On}")
	}
	if tree.OptionsMaps[1][0] != "Off" || tree.OptionsMaps[1][1] != "On" {
		t.Errorf("OptionsMaps[1]=%v want {0:Off, 1:On}", tree.OptionsMaps[1])
	}
	if tree.Labels["Volume"] != 0 {
		t.Errorf("Labels[Volume]=%d want 0", tree.Labels["Volume"])
	}
}

// TestSeedTreeFromCachedObjects_JSONRoundTrip simulates the disk
// reload path: Meta values arrive as JSON-decoded `float64` (numbers)
// and `map[string]any` (nested map). SeedTreeFromCachedObjects must
// coerce both shapes back into typed values.
func TestSeedTreeFromCachedObjects_JSONRoundTrip(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	objs := []consumer.Object{
		{
			Slot: 1, ID: 5, Label: "Vol",
			Meta: map[string]any{
				// JSON unmarshal into any → float64.
				"acp2.objType": float64(codec.ObjTypeNumber),
				"acp2.numType": float64(codec.NumTypeS32),
			},
		},
		{
			Slot: 1, ID: 6, Label: "Mode",
			Meta: map[string]any{
				"acp2.objType": float64(codec.ObjTypeEnum),
				"acp2.numType": float64(codec.NumTypePreset),
				// JSON-decoded options map: map[string]any with string values.
				"acp2.optionsMap": map[string]any{"42": "A", "99": "Z"},
			},
		},
	}
	p.SeedTreeFromCachedObjects(1, objs)
	tree, _ := p.trees.Get(1)
	if tree == nil {
		t.Fatal("no tree after seed (json round-trip)")
	}
	if tree.ObjTypes[0] != codec.ObjTypeNumber {
		t.Errorf("ObjTypes[0]=%v want Number (float64 coercion failed)", tree.ObjTypes[0])
	}
	if tree.NumTypes[0] != codec.NumTypeS32 {
		t.Errorf("NumTypes[0]=%v want S32", tree.NumTypes[0])
	}
	if tree.OptionsMaps[1] == nil {
		t.Fatal("OptionsMaps[1] nil after JSON-shaped seed")
	}
	if tree.OptionsMaps[1][42] != "A" || tree.OptionsMaps[1][99] != "Z" {
		t.Errorf("OptionsMaps[1]=%v want {42:A, 99:Z}", tree.OptionsMaps[1])
	}
}

// TestSeedTreeFromCachedObjects_TimingFastPath asserts the seed
// path doesn't take longer than the implicit Walk would on a 50k
// tree. This is a smoke test: 5 000 objects must seed in under 50 ms
// on a typical workstation; cache.Put is O(1) and our parallel
// slice fills are O(n).
func TestSeedTreeFromCachedObjects_TimingFastPath(t *testing.T) {
	p := &Plugin{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	objs := make([]consumer.Object, 5000)
	for i := range objs {
		objs[i] = consumer.Object{
			Slot: 1, ID: i + 1, Label: "x",
			Meta: map[string]any{
				"acp2.objType": uint8(codec.ObjTypeNumber),
				"acp2.numType": uint8(codec.NumTypeS32),
			},
		}
	}
	start := time.Now()
	p.SeedTreeFromCachedObjects(1, objs)
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Errorf("seed took %v; want < 50 ms (background walk would take seconds)", elapsed)
	}
	tree, _ := p.trees.Get(1)
	if tree == nil || len(tree.Objects) != 5000 {
		t.Errorf("expected 5000 seeded objects; got tree=%v", tree)
	}
}
