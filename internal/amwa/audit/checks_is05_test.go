package audit

import (
	"strings"
	"testing"
)

// TestEndpointsReadHighestUsableMinor pins how the IS-05 reader treats
// the same sender captured at several minors: one endpoint, from the
// highest minor that actually holds a decodable payload. A `null`
// capture (the request failed) at v1.1 must fall through to v1.0
// rather than hide the sender, and a payload that is not an object is
// skipped rather than crashing the audit.
func TestEndpointsReadHighestUsableMinor(t *testing.T) {
	const twice, fell, junk = "aaaa", "bbbb", "cccc"
	on := true
	h := mk("node", map[string]map[string]map[string]any{
		"connection": {
			"v1.1": {
				"senders/" + twice + "/active": map[string]any{"master_enable": on},
				"senders/" + fell + "/active":  nil,
				"senders/" + junk + "/active":  "not an object",
			},
			"v1.0": {
				"senders/" + twice + "/active": map[string]any{"master_enable": !on},
				"senders/" + fell + "/active":  map[string]any{"master_enable": on},
			},
		},
	})
	eps := h.endpoints("senders", "active")
	got := map[string]string{}
	for _, e := range eps {
		got[e.ID] = e.Ver
	}
	if len(eps) != 2 {
		t.Fatalf("endpoints = %v, want exactly the two decodable senders", got)
	}
	if got[twice] != "v1.1" {
		t.Errorf("a sender captured at two minors must be read once, from the highest: %v", got)
	}
	if got[fell] != "v1.0" {
		t.Errorf("a null capture at v1.1 must fall through to v1.0: %v", got)
	}
	if _, ok := got[junk]; ok {
		t.Error("an undecodable payload was returned as an endpoint")
	}
}

// TestRedundancySubnetIgnoresUnaddressedLegs: a leg with no address is
// not on any subnet. Both legs of an idle sender read as 0.0.0.0, and
// calling that "the same /24" turned every idle sender in a plant into
// a redundancy finding — 3048 of them on one real 44-node capture.
func TestRedundancySubnetIgnoresUnaddressedLegs(t *testing.T) {
	cases := []struct {
		name string
		legs []any
	}{
		{"legs without a destination_ip field", []any{
			map[string]any{"destination_port": 20000}, map[string]any{"destination_port": 20000},
		}},
		{"legs at the unspecified address", []any{
			map[string]any{"destination_ip": "0.0.0.0"}, map[string]any{"destination_ip": "0.0.0.0"},
		}},
		{"legs at IPv6 groups have no /24", []any{
			map[string]any{"destination_ip": "ff3e::1"}, map[string]any{"destination_ip": "ff3e::1"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := mk("node", active(sID, map[string]any{"master_enable": true, "transport_params": tc.legs}))
			hasNot(t, checkIS05TransportParams(h), "NMOS-2022-7-SAME-SUBNET")
		})
	}
}

// mkIdle builds a node with n senders all master-enabled and unset.
func mkIdle(n int) *Harvest {
	bucket := map[string]any{}
	for i := 0; i < n; i++ {
		id := strings.Repeat(string(rune('a'+i)), 4)
		bucket["senders/"+id+"/active"] = map[string]any{
			"master_enable": true,
			"transport_params": []any{
				map[string]any{"destination_ip": "0.0.0.0", "destination_port": 12700},
			},
		}
	}
	return mk("node", map[string]map[string]map[string]any{"connection": {"v1.1": bucket}})
}

// TestGroupedFindingNamesThreeExamples: a grouped finding names up to
// three senders and counts the rest, so a 176-sender device stays
// readable while still telling the operator where to look.
func TestGroupedFindingNamesThreeExamples(t *testing.T) {
	f := has(t, checkIS05Active(mkIdle(5)), "NMOS-IS05-NO-DESTINATION")
	if !strings.Contains(f.Detail, "sender/aaaa, sender/bbbb, sender/cccc (+2 more)") {
		t.Errorf("examples should be capped at three with a count: %q", f.Detail)
	}
	if !strings.Contains(f.Detail, "5 of 5 sender(s)") {
		t.Errorf("the count should be stated: %q", f.Detail)
	}
}
