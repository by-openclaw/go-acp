package audit

import (
	"testing"
)

// TestUndecodableResourcesAreReportedOnce: a collection holding a
// value that is not an object is reported by the core check and then
// ignored by every check that reads a specific resource shape. One
// finding names the defect; the others must neither crash nor invent
// findings about a resource that cannot be read.
func TestUndecodableResourcesAreReportedOnce(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {
			"self":      map[string]any{"id": "11111111-1111-4111-8111-111111111111"},
			"devices":   []any{"just a string"},
			"senders":   []any{42},
			"receivers": []any{true},
		}},
	})

	core := codes(checkIS04Core(h))["NMOS-IS04-UNDECODABLE"]
	if len(core) != 3 {
		t.Fatalf("want one undecodable finding per collection, got %d: %v", len(core), core)
	}

	for name, check := range map[string]checkFn{
		"control advertisement": checkControlAdvertisement,
		"graph":                 checkIS04Graph,
		"manifest":              checkIS04Manifest,
		"group hint":            checkBCP002GroupHint,
		"receiver caps":         checkBCP004ReceiverCaps,
	} {
		if got := check(h); len(got) != 0 {
			t.Errorf("%s check produced findings about unreadable resources: %v", name, codeList(got))
		}
	}
	if s, r, g := hintCounts(h); s != 0 || r != 0 || g != 0 {
		t.Errorf("hintCounts on unreadable resources = %d,%d,%d", s, r, g)
	}
	if rows, fs := checkPlantGroups([]*Harvest{h}); len(rows) != 0 || len(fs) != 0 {
		t.Errorf("group pivot on unreadable resources = %v, %v", rows, codeList(fs))
	}
}
