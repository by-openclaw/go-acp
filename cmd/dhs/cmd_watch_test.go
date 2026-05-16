package main

import (
	"testing"

	"dhs/internal/consumer"
)

// TestWatchCacheKey_DistinguishesGroupsForSharedID pins the fix for #236:
// ACP1 groups (control / status / alarm / identity / file / frame) re-use
// the same object-id space within one slot, so a flat ID-only cache map
// flattens distinct labels onto the same key. The watch verb keys by
// "<group>.<id>" so a live announce on s1.control.0 (IO-Ctrl) is no longer
// mis-labelled with the s1.status.0 entry (sInp1) loaded from disk cache.
func TestWatchCacheKey_DistinguishesGroupsForSharedID(t *testing.T) {
	ctrl := watchCacheKey("control", 0)
	stat := watchCacheKey("status", 0)
	if ctrl == stat {
		t.Fatalf("cache key collision: control.0 == status.0 (%q == %q)", ctrl, stat)
	}

	cache := map[string]string{
		watchCacheKey("control", 0):  "IO-Ctrl",
		watchCacheKey("status", 0):   "sInp1",
		watchCacheKey("control", 3):  "#Inp_SelA",
		watchCacheKey("status", 3):   "sInp1_WSS-Extd",
		watchCacheKey("control", 10): "#Out-Mode",
		watchCacheKey("status", 10):  "sInp3",
	}

	cases := []struct {
		group string
		id    int
		want  string
	}{
		{"control", 0, "IO-Ctrl"},
		{"status", 0, "sInp1"},
		{"control", 3, "#Inp_SelA"},
		{"status", 3, "sInp1_WSS-Extd"},
		{"control", 10, "#Out-Mode"},
		{"status", 10, "sInp3"},
	}
	for _, c := range cases {
		if got := cache[watchCacheKey(c.group, c.id)]; got != c.want {
			t.Errorf("lookup %s.%d: got %q want %q", c.group, c.id, got, c.want)
		}
	}
}

// TestWatchCacheKey_ACP2EmptyGroup verifies the same helper stays correct
// for ACP2 events (no group concept; ev.Group is empty). Distinct ids
// must yield distinct keys.
func TestWatchCacheKey_ACP2EmptyGroup(t *testing.T) {
	if a, b := watchCacheKey("", 1), watchCacheKey("", 2); a == b {
		t.Fatalf("ACP2 ids collapsed: id=1 key %q == id=2 key %q", a, b)
	}
}

// TestFrameStatusDelta_BaselineThenSingleChange covers the common watch
// flow: first event seeds the baseline (no transitions emitted, caller
// prints baseline), second event with one slot delta emits one line.
func TestFrameStatusDelta_BaselineThenSingleChange(t *testing.T) {
	// Helper using the protocol's SlotStatus constants directly.
	P := consumer.SlotPresent
	N := consumer.SlotNoCard
	U := consumer.SlotPowerUp

	// First call: prev nil → no transitions returned (baseline path).
	if got := frameStatusDelta(nil, []consumer.SlotStatus{P, N, N, P}); len(got) != 0 {
		t.Errorf("baseline: got %d transitions, want 0: %v", len(got), got)
	}

	// Single slot transition: slot 2 N → U.
	got := frameStatusDelta(
		[]consumer.SlotStatus{P, N, N, P},
		[]consumer.SlotStatus{P, N, U, P},
	)
	if len(got) != 1 {
		t.Fatalf("got %d transitions, want 1: %v", len(got), got)
	}
	if got[0] != "slot 2: no_card -> power_up" {
		t.Errorf("got %q, want %q", got[0], "slot 2: no_card -> power_up")
	}
}

// TestFrameStatusDelta_NoChange covers re-broadcast suppression: same
// strip in/out yields zero transitions so watch prints nothing.
func TestFrameStatusDelta_NoChange(t *testing.T) {
	strip := []consumer.SlotStatus{
		consumer.SlotPresent, consumer.SlotPresent, consumer.SlotNoCard,
	}
	if got := frameStatusDelta(strip, strip); len(got) != 0 {
		t.Errorf("idempotent re-broadcast emitted %d transitions: %v", len(got), got)
	}
}

// TestFrameStatusDelta_FullCycle replays the live 6-state cycle the user
// observed on slot 19 (no_card → power_up → error → removed → boot →
// present) to lock the slot-only diff output.
func TestFrameStatusDelta_FullCycle(t *testing.T) {
	mkStrip := func(slot19 consumer.SlotStatus) []consumer.SlotStatus {
		s := make([]consumer.SlotStatus, 31)
		s[0] = consumer.SlotPresent
		s[1] = consumer.SlotPresent
		s[19] = slot19
		return s
	}
	steps := []struct {
		from, to consumer.SlotStatus
		want     string
	}{
		{consumer.SlotNoCard, consumer.SlotPowerUp, "slot 19: no_card -> power_up"},
		{consumer.SlotPowerUp, consumer.SlotError, "slot 19: power_up -> error"},
		{consumer.SlotError, consumer.SlotRemoved, "slot 19: error -> removed"},
		{consumer.SlotRemoved, consumer.SlotBootMode, "slot 19: removed -> boot_mode"},
		{consumer.SlotBootMode, consumer.SlotPresent, "slot 19: boot_mode -> present"},
	}
	for _, st := range steps {
		out := frameStatusDelta(mkStrip(st.from), mkStrip(st.to))
		if len(out) != 1 || out[0] != st.want {
			t.Errorf("step %s -> %s: got %v want [%q]", st.from, st.to, out, st.want)
		}
	}
}

// TestFrameStatusDelta_LengthMismatch verifies short/long slice handling:
// a frame whose slot count grew gets the new positions reported as
// no_card -> X transitions.
func TestFrameStatusDelta_LengthMismatch(t *testing.T) {
	prev := []consumer.SlotStatus{consumer.SlotPresent}
	cur := []consumer.SlotStatus{consumer.SlotPresent, consumer.SlotPresent}
	got := frameStatusDelta(prev, cur)
	if len(got) != 1 || got[0] != "slot 1: no_card -> present" {
		t.Errorf("length-grow: got %v want [\"slot 1: no_card -> present\"]", got)
	}
}
