package audit

import (
	"strings"
	"testing"
)

// hinted builds a sender or receiver carrying group hints.
func hinted(id string, hints ...string) map[string]any {
	return sender(id, map[string]any{
		"tags": map[string]any{groupHintTag: hints},
	})
}

// TestGroupPivotBuildsLevels is the shape a controller needs: one group
// holding several roles, which is what makes "route all levels" and a
// breakaway possible.
func TestGroupPivotBuildsLevels(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {
			"senders": []any{
				hinted("44444444-4444-4444-8444-444444444444", "CAM-01:Video 1"),
				hinted("55555555-5555-4555-8555-555555555555", "CAM-01:Audio 1"),
				hinted("66666666-6666-4666-8666-666666666666", "CAM-01:Anc 1"),
			},
			"receivers": []any{},
		}},
	})

	rows, findings := checkPlantGroups([]*Harvest{h})
	if len(rows) != 1 {
		t.Fatalf("want 1 group, got %d: %v", len(rows), rows)
	}
	g := rows[0]
	if g.Name != "CAM-01" {
		t.Errorf("group name = %q", g.Name)
	}
	if g.Senders != 3 || g.Receivers != 0 {
		t.Errorf("counts = %d/%d, want 3/0", g.Senders, g.Receivers)
	}
	// Roles read video → audio → anc, the order an operator thinks in,
	// not alphabetically.
	if got := strings.Join(g.Roles, ","); got != "Video 1,Audio 1,Anc 1" {
		t.Errorf("roles = %q, want video then audio then anc", got)
	}
	// A properly grouped signal is not a finding.
	for _, f := range findings {
		if strings.HasPrefix(f.Code, "NMOS-BCP002-GROUP") {
			t.Errorf("a well-formed group produced %s: %s", f.Code, f.Detail)
		}
	}
}

// TestSingleRoleGroupsReported is the real EVS Neuron pathology: every
// hint present, syntactically valid, and grouping nothing — each
// essence is its own group named after itself.
func TestSingleRoleGroupsReported(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {
			"senders": []any{
				hinted("44444444-4444-4444-8444-444444444444", "ARX-01:s2110-30"),
				hinted("55555555-5555-4555-8555-555555555555", "ARX-02:s2110-30"),
				hinted("66666666-6666-4666-8666-666666666666", "ARX-03:s2110-30"),
			},
			"receivers": []any{},
		}},
	})

	rows, findings := checkPlantGroups([]*Harvest{h})
	if len(rows) != 3 {
		t.Fatalf("want 3 single-role groups, got %d", len(rows))
	}

	// One finding per device, not one per group: a 176-sender node would
	// otherwise emit 176 lines saying the same thing.
	var got []Finding
	for _, f := range findings {
		if f.Code == "NMOS-BCP002-GROUP-SINGLE-ROLE" {
			got = append(got, f)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want 1 grouped finding, got %d", len(got))
	}
	if !strings.Contains(got[0].Detail, "3 group(s)") {
		t.Errorf("the finding should name the count: %q", got[0].Detail)
	}
	if !strings.Contains(got[0].Hint, "breakaway") {
		t.Errorf("the hint should say what it costs the operator: %q", got[0].Hint)
	}
}

// TestCrossDeviceGroupReported covers both readings of the same
// observation, because a capture cannot tell them apart.
func TestCrossDeviceGroupReported(t *testing.T) {
	a := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {
			"senders":   []any{hinted("44444444-4444-4444-8444-444444444444", "device_1:source_1")},
			"receivers": []any{},
		}},
	})
	a.Label = "node-a"
	b := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {
			"senders":   []any{hinted("55555555-5555-4555-8555-555555555555", "device_1:source_2")},
			"receivers": []any{},
		}},
	})
	b.Label = "node-b"

	rows, findings := checkPlantGroups([]*Harvest{a, b})
	if len(rows) != 1 || len(rows[0].Devices) != 2 {
		t.Fatalf("want one group across two devices, got %v", rows)
	}
	f := has(t, findings, "NMOS-BCP002-GROUP-CROSS-DEVICE")
	for _, want := range []string{"node-a", "node-b", "device_1"} {
		if !strings.Contains(f.Detail, want) {
			t.Errorf("detail %q missing %q", f.Detail, want)
		}
	}
	// Both readings must be offered — the name-collision one is the
	// dangerous half.
	if !strings.Contains(f.Hint, "not unique") {
		t.Errorf("the hint must offer the name-collision reading: %q", f.Hint)
	}
}

// TestRegistryViewDoesNotDoubleCount is the bug this check shipped with
// and had to have fixed: a registry's catalogue is a VIEW of its nodes,
// so counting both turned 4-receiver services into 7-receiver ones and
// invented 170 cross-device groups on one real plant.
func TestRegistryViewDoesNotDoubleCount(t *testing.T) {
	const sid = "44444444-4444-4444-8444-444444444444"

	node := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {
			"senders":   []any{hinted(sid, "Service 01:Video 1")},
			"receivers": []any{},
		}},
	})
	node.Label = "the-node"

	reg := mk("registry", map[string]map[string]map[string]any{
		"query": {"v1.3": {
			"senders":   []any{hinted(sid, "Service 01:Video 1")},
			"receivers": []any{},
		}},
	})
	reg.Label = "the-registry"

	// Registry first in the slice, to prove the ordering is enforced by
	// the check and not by luck.
	rows, findings := checkPlantGroups([]*Harvest{reg, node})
	if len(rows) != 1 {
		t.Fatalf("want 1 group, got %d", len(rows))
	}
	if rows[0].Senders != 1 {
		t.Errorf("sender count = %d, want 1 — the registry view was counted again", rows[0].Senders)
	}
	if len(rows[0].Devices) != 1 || rows[0].Devices[0] != "the-node" {
		t.Errorf("devices = %v, want just the node that owns the resource", rows[0].Devices)
	}
	for _, f := range findings {
		if f.Code == "NMOS-BCP002-GROUP-CROSS-DEVICE" {
			t.Errorf("a registry view produced a phantom cross-device finding: %s", f.Detail)
		}
	}
}

// TestRegistryOnlyCaptureStillGroups: when nothing but a registry was
// captured, its catalogue is all there is and must still be used.
func TestRegistryOnlyCaptureStillGroups(t *testing.T) {
	reg := mk("registry", map[string]map[string]map[string]any{
		"query": {"v1.3": {
			"senders": []any{
				hinted("44444444-4444-4444-8444-444444444444", "CAM-01:Video 1"),
				hinted("55555555-5555-4555-8555-555555555555", "CAM-01:Audio 1"),
			},
			"receivers": []any{},
		}},
	})
	rows, _ := checkPlantGroups([]*Harvest{reg})
	if len(rows) != 1 || rows[0].Senders != 2 {
		t.Fatalf("a registry-only capture must still group: %v", rows)
	}
}

// TestHintCountsMeasureCoverage is the "which nodes expose BCP-002"
// inventory column.
func TestHintCountsMeasureCoverage(t *testing.T) {
	h := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {
			"senders": []any{
				hinted("44444444-4444-4444-8444-444444444444", "CAM-01:Video 1"),
				sender("55555555-5555-4555-8555-555555555555", nil), // no hint
			},
			"receivers": []any{hinted("88888888-8888-4888-8888-888888888888", "CAM-01:Audio 1")},
		}},
	})
	s, r, groups := hintCounts(h)
	if s != 1 || r != 1 || groups != 1 {
		t.Errorf("hintCounts = %d,%d,%d; want 1,1,1", s, r, groups)
	}

	// A device with no hints at all reports zeroes rather than being
	// omitted — "publishes none" is a finding, not an absence.
	bare := mk("node", map[string]map[string]map[string]any{
		"node": {"v1.3": {"senders": []any{sender(sID, nil)}, "receivers": []any{}}},
	})
	if s, r, g := hintCounts(bare); s != 0 || r != 0 || g != 0 {
		t.Errorf("hintCounts on an unhinted device = %d,%d,%d", s, r, g)
	}
}

// TestHintSplitsOnLastColon: a role may contain spaces and a group name
// may contain colons, so splitting on the FIRST colon would turn
// "RACK:1:video" into a group called "RACK".
func TestHintSplitsOnLastColon(t *testing.T) {
	acc := newGroupAccumulator()
	acc.add("RACK:1:Video 1", "dev", "senders")
	rows := acc.rows()
	if len(rows) != 1 {
		t.Fatalf("want 1 group, got %v", rows)
	}
	if rows[0].Name != "RACK:1" {
		t.Errorf("group name = %q, want RACK:1", rows[0].Name)
	}
	if len(rows[0].Roles) != 1 || rows[0].Roles[0] != "Video 1" {
		t.Errorf("roles = %v, want [Video 1]", rows[0].Roles)
	}
}

// TestMalformedHintsIgnored: a hint with no colon groups nothing, and
// must not create a group keyed on the whole string.
func TestMalformedHintsIgnored(t *testing.T) {
	acc := newGroupAccumulator()
	acc.add("no-colon-here", "dev", "senders")
	acc.add(":only-a-role", "dev", "senders")
	acc.add("", "dev", "senders")
	if rows := acc.rows(); len(rows) != 0 {
		t.Errorf("malformed hints produced groups: %v", rows)
	}
}

// TestGroupsAppearInEveryRenderer proves the pivot reaches the operator
// in each output form.
func TestGroupsAppearInEveryRenderer(t *testing.T) {
	res := loadPlant(t)
	// The fixture plant has one hinted sender: cam-01 iso, "cam-01:video".
	if len(res.Groups) == 0 {
		t.Fatal("the fixture plant has a group hint; the pivot found none")
	}

	var b strings.Builder
	if err := RenderText(&b, res); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	for _, want := range []string{"GROUPING (BCP-002-01)", "ROLES (levels)", "cam-01"} {
		if !strings.Contains(s, want) {
			t.Errorf("text report missing %q", want)
		}
	}
	// Grouping is printed before the findings, because it changes how
	// they read.
	if strings.Index(s, "GROUPING") > strings.Index(s, "FINDINGS") {
		t.Error("grouping must come before the findings")
	}
}

// TestNoHintsAnywhereSaysSo: a plant with no grouping at all is the
// worst case for a controller, and the report must state it rather than
// print an empty table.
func TestNoHintsAnywhereSaysSo(t *testing.T) {
	var b strings.Builder
	res := Result{
		Counts:    map[string]int{},
		Inventory: []Inventory{{Target: "h:1", Senders: 4}},
	}
	if err := RenderText(&b, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "no group hints anywhere") {
		t.Errorf("an ungrouped plant should say so explicitly:\n%s", b.String())
	}
	// The coverage column reads 0/4, not blank.
	if !strings.Contains(b.String(), "0/4") {
		t.Error("the inventory should show 0/4 rather than an empty cell")
	}
}

func TestOfTotal(t *testing.T) {
	if got := ofTotal(0, 0); got != "-" {
		t.Errorf("nothing to cover should read %q, got %q", "-", got)
	}
	if got := ofTotal(3, 176); got != "3/176" {
		t.Errorf("ofTotal = %q", got)
	}
}

func TestRoleOrdering(t *testing.T) {
	got := sortedRoles(map[string]bool{"Anc 1": true, "Audio 2": true, "Video 1": true, "zzz": true, "Audio 1": true})
	want := []string{"Video 1", "Audio 1", "Audio 2", "Anc 1", "zzz"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedRoles = %v, want %v", got, want)
		}
	}
}
