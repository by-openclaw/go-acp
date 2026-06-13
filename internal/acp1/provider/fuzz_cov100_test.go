package acp1

import (
	"context"
	"math/rand"
	"testing"

	"dhs/internal/acp1/codec"
	"dhs/internal/export/canonical"
)

// TestRunFuzz_DefaultsRateAndSeed: cfg with Rate<=0 and Seed==0 takes both
// default arms. An immediately-cancelled context exits before any tick.
func TestRunFuzz_DefaultsRateAndSeed(t *testing.T) {
	s := newTestServer(t)
	announceCounter(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Rate 0 → default 1.0; Seed 0 → time-based seed. Slot -1 / ID -1 so a
	// target exists and RunFuzz doesn't error out early.
	if err := s.RunFuzz(ctx, FuzzConfig{Slot: -1, ID: -1}); err != nil {
		t.Fatalf("RunFuzz: %v", err)
	}
}

// TestCollectFuzzTargets_IDFilterAndNonFuzzable: the ID filter and the
// non-fuzzable-type skip both run.
func TestCollectFuzzTargets_IDFilterAndNonFuzzable(t *testing.T) {
	s := newMutServer()
	// A writable integer (fuzzable) at id 0 and a writable string (not
	// fuzzable) at id 1, plus a third integer at id 2 to test the ID filter.
	s.tree.entries[objectKey{slot: 1, group: codec.GroupControl, id: 0}] = &entry{
		acpType: codec.TypeInteger, access: 0x02, param: &canonical.Parameter{Value: int64(0)}}
	s.tree.entries[objectKey{slot: 1, group: codec.GroupControl, id: 1}] = &entry{
		acpType: codec.TypeString, access: 0x02, param: &canonical.Parameter{Value: "x"}}
	s.tree.entries[objectKey{slot: 1, group: codec.GroupControl, id: 2}] = &entry{
		acpType: codec.TypeInteger, access: 0x02, param: &canonical.Parameter{Value: int64(0)}}

	// ID filter = 0 → only the id-0 integer survives (id 2 filtered, id 1
	// also filtered by ID; the non-fuzzable skip is exercised when ID=-1).
	got := s.collectFuzzTargets(FuzzConfig{Slot: -1, ID: 0})
	if len(got) != 1 {
		t.Fatalf("ID filter: got %d targets, want 1", len(got))
	}
	// ID=-1 → integers fuzzable (2), string skipped by fuzzableType.
	all := s.collectFuzzTargets(FuzzConfig{Slot: -1, ID: -1})
	if len(all) != 2 {
		t.Fatalf("non-fuzzable skip: got %d targets, want 2", len(all))
	}
}

// TestSynthInteger_DegenerateSpan covers the span<=0 arm (min == max means
// span 1, so use min > max via crafted bounds → readIntBounds clamps, but a
// min==max+1 isn't possible; instead force the non-edge default branch with
// equal min/max so span==1 and then a min>max via direct entry).
func TestSynthInteger_DegenerateSpan(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	// Entry with Minimum > Maximum so readIntBounds returns min>max, making
	// span = max-min+1 <= 0 → the v = min arm.
	e := &entry{acpType: codec.TypeInteger, param: &canonical.Parameter{
		Value: int64(0), Minimum: int64(100), Maximum: int64(-100),
	}}
	out := synthInteger(rng, e, false, 2)
	if len(out) != 2 {
		t.Fatalf("synthInteger width 2 → %d bytes", len(out))
	}
}

// TestFuzzTick_SynthError: calling fuzzTick with a non-fuzzable target makes
// synthesiseValue return an error → the skip-target arm.
func TestFuzzTick_SynthError(t *testing.T) {
	s := newMutServer()
	s.logger = discardLogger()
	rng := rand.New(rand.NewSource(1))
	// A String target is not fuzzable → synthesiseValue errors.
	target := fuzzTarget{
		key: objectKey{slot: 1, group: codec.GroupControl, id: 0},
		e:   &entry{acpType: codec.TypeString, param: &canonical.Parameter{Value: "x"}},
	}
	s.fuzzTick(rng, []fuzzTarget{target}, FuzzConfig{Slot: -1, ID: -1}, 0)
}

// TestReadFloatBounds_IntConstraints covers the int64/int min/max arms.
func TestReadFloatBounds_IntConstraints(t *testing.T) {
	e := &entry{param: &canonical.Parameter{Minimum: int(0), Maximum: int(10)}}
	min, max := readFloatBounds(e)
	if min != 0 || max != 10 {
		t.Fatalf("int constraints → %v..%v, want 0..10", min, max)
	}
	e2 := &entry{param: &canonical.Parameter{Minimum: int64(-5), Maximum: int64(5)}}
	min2, max2 := readFloatBounds(e2)
	if min2 != -5 || max2 != 5 {
		t.Fatalf("int64 constraints → %v..%v, want -5..5", min2, max2)
	}
}
