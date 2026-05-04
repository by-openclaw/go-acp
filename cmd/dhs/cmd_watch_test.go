package main

import "testing"

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
