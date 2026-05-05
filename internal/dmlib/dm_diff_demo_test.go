package dmlib_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"dhs/internal/dmlib"
	"dhs/internal/export"
)

// TestDM_Diff_LiveCapture demonstrates dmlib.Diff against the schemas
// extracted from the simulator's nine unique cards. Surfaces per-slot
// Added/Removed/Changed object labels for every cross-pair.
//
// Run with `go test ./internal/dmlib/... -v -run TestDM_Diff_LiveCapture`.
// Skips unless the captured fixtures exist (i.e. only meaningful after
// the integration extraction has run).
func TestDM_Diff_LiveCapture(t *testing.T) {
	root := filepath.Join("..", "..", "tests", "fixtures", "products", "axon", "synapse")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("fixtures absent: %v", err)
	}

	type card struct {
		dir   string
		slot  int
		label string
	}
	cards := []card{
		{"RRS18-1601", 0, "RRS18@1601"},
		{"2GS110-2728", 1, "2GS110@2728"},
		{"2GS110-2929", 5, "2GS110@2929"},
		{"2HF110-4326", 2, "2HF110@4326"},
		{"GED130-2522", 3, "GED130@2522"},
		{"SDB20-0806", 9, "SDB20@0806"},
		{"HPD130-2120", 13, "HPD130@2120"},
		{"GJA840-0101", 16, "GJA840@0101"},
		{"HRB990-1010", 19, "HRB990@1010"},
	}
	loaded := map[string]*dmlib.Schema{}
	for _, c := range cards {
		path := filepath.Join(root, c.dir, "acp1", fmt.Sprintf("slot_%d.json", c.slot))
		f, err := os.Open(path)
		if err != nil {
			t.Logf("skip %s: %v", c.label, err)
			continue
		}
		snap, err := export.ReadJSON(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("%s: %v", c.label, err)
		}
		// Normalise to slot 1: both the map key AND every Object.Slot
		// field. Without this the per-object diff sees Slot=N on one
		// side and Slot=M on the other and reports every object as
		// changed even when the schema is identical.
		for i := range snap.Slots {
			snap.Slots[i].Slot = 1
			for j := range snap.Slots[i].Objects {
				snap.Slots[i].Objects[j].Slot = 1
			}
		}
		// Parse "MODEL@REV" so the dmlib.Diff (Model, Proto) gate can
		// reject cross-model pairs as Mismatch. Diff is only valid
		// between firmware revisions of the same product.
		// fmt.Sscanf does NOT support POSIX character classes in Go;
		// using strings.SplitN keeps the parse unambiguous.
		parts := strings.SplitN(c.label, "@", 2)
		if len(parts) != 2 {
			t.Fatalf("bad label %q", c.label)
		}
		loaded[c.label] = &dmlib.Schema{
			Fingerprint: dmlib.Fingerprint{
				Model: parts[0],
				SwRev: parts[1],
				Proto: "acp1",
			},
			Slots: map[int]*export.Snapshot{1: snap},
		}
	}

	r := dmlib.New(t.TempDir())

	// Same-model firmware diffs (the only meaningful case). The
	// simulator only has the 2GS110 family with two firmware revs;
	// other models have a single rev each so no diff to run.
	demo := []struct {
		title       string
		before, aft string
	}{
		{"firmware bump 2GS110: 2728 -> 2929", "2GS110@2728", "2GS110@2929"},
	}
	for _, d := range demo {
		t.Run(d.title, func(t *testing.T) {
			before, ok1 := loaded[d.before]
			after, ok2 := loaded[d.aft]
			if !ok1 || !ok2 {
				t.Skipf("schema missing: before=%v after=%v", ok1, ok2)
			}
			diff := r.Diff(before, after)
			if diff.Mismatch {
				t.Fatalf("%s: dmlib.Diff reports Mismatch — Models differ", d.title)
			}
			fmt.Printf("\n=== %s ===\n", d.title)
			fmt.Printf("AddedSlots:   %v\n", diff.AddedSlots)
			fmt.Printf("RemovedSlots: %v\n", diff.RemovedSlots)
			slots := make([]int, 0, len(diff.PerSlot))
			for s := range diff.PerSlot {
				slots = append(slots, s)
			}
			sort.Ints(slots)
			for _, s := range slots {
				sd := diff.PerSlot[s]
				fmt.Printf("Slot %d: +%d / -%d / ~%d\n", s, len(sd.Added), len(sd.Removed), len(sd.Changed))
				if len(sd.Added) > 0 {
					fmt.Printf("  added (%d, first 5): %v\n", len(sd.Added), firstN(sd.Added, 5))
				}
				if len(sd.Removed) > 0 {
					fmt.Printf("  removed (%d, first 5): %v\n", len(sd.Removed), firstN(sd.Removed, 5))
				}
				if len(sd.Changed) > 0 {
					fmt.Printf("  changed (%d, first 5): %v\n", len(sd.Changed), firstN(sd.Changed, 5))
				}
			}
		})
	}

	// Negative case: cross-model diff must report Mismatch, not pretend
	// to compute a delta.
	t.Run("cross-model rejected as Mismatch", func(t *testing.T) {
		rrs, ok1 := loaded["RRS18@1601"]
		gjA, ok2 := loaded["2GS110@2728"]
		if !ok1 || !ok2 {
			t.Skip("schema missing")
		}
		diff := r.Diff(rrs, gjA)
		if !diff.Mismatch {
			t.Fatalf("cross-model diff did not flag Mismatch; got %+v", diff)
		}
		fmt.Println("\n=== cross-model: RRS18 -> 2GS110 ===")
		fmt.Println("Mismatch=true (rejected: Models differ)")
	})
}

func firstN(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return xs[:n]
}
