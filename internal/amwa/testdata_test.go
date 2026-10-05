package amwa_test

// ADR-0025 deliverable 6: the replay set under testdata/. Every message
// kind has its capture, its tree through wireshark/dhs_nmos.lua and its
// page, and the tree carries what the dissector says of that kind.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kinds is the replay set: folder → what its tree must say.
var kinds = map[string][]string{
	"dnssd-mdns":             {"AMWA NMOS (dhs — discovery layer)"},
	"registration-resource":  {"IS-04 Registration", "POST type="},
	"registration-heartbeat": {"IS-04 Registration", "POST heartbeat node="},
	"registration-delete":    {"IS-04 Registration", "DELETE "},
	"query-list":             {"IS-04 Query", "?paging.limit=1000"},
	"query-subscription":     {"IS-04 Query", "POST subscription resource_path="},
	"query-grain-ws":         {"IS-04 Query grain topic="},
	"node-api":               {"IS-04 Node", "GET self"},
	"connection-single":      {"IS-05 Connection", "PATCH receiver ", "mode=activate_"},
	"connection-bulk":        {"IS-05 Connection", "POST bulk receivers items=2"},
	"events-ws":              {"IS-07 command subscription sources=1", "IS-07 state source="},
	"events-mqtt":            {"IS-07 MQTT source=", "message=state", "message=connection_status"},
	"channelmapping":         {"IS-08 Channel Mapping", "POST map/activations mode=activate_immediate channels=1"},
	"system":                 {"IS-09 System", "GET global"},
	"streamcompatibility":    {"IS-11 Stream Compatibility", "constraints/active/ constraint_sets=1 parameters=2", "state=constrained"},
	"control-ws":             {"IS-12 Command commands=1", "IS-12 CommandResponse responses=1", "IS-12 Notification notifications=1"},
	"configuration":          {"IS-14 Configuration", "PUT role=root.ExampleControl properties/1p6/value/"},
	"configuration-bulk":     {"IS-14 Configuration", "PATCH role=root.ExampleControl bulkProperties/"},
}

func TestEveryMessageKindHasItsReplay(t *testing.T) {
	root := filepath.Join("testdata", "protocol_types")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("the replay set is missing: %v", err)
	}
	for _, e := range entries {
		if _, known := kinds[e.Name()]; e.IsDir() && !known {
			t.Errorf("%s is in the replay set and this test does not say what its tree must carry", e.Name())
		}
	}
	for kind, want := range kinds {
		dir := filepath.Join(root, kind)
		for _, f := range []string{"capture.pcapng", "README.md"} {
			if st, err := os.Stat(filepath.Join(dir, f)); err != nil || st.Size() == 0 {
				t.Errorf("%s: %s is missing or empty", kind, f)
			}
		}
		if st, err := os.Stat(filepath.Join(dir, "capture.pcapng")); err == nil && st.Size() > 100*1024 {
			t.Errorf("%s: capture is %d bytes, over the 100 KB a committed capture may be", kind, st.Size())
		}
		tree, err := os.ReadFile(filepath.Join(dir, "tshark.tree"))
		if err != nil {
			t.Errorf("%s: %v", kind, err)
			continue
		}
		for _, w := range want {
			if !strings.Contains(string(tree), w) {
				t.Errorf("%s: the tree does not carry %q", kind, w)
			}
		}
		if strings.Contains(string(tree), "Lua Error") {
			t.Errorf("%s: the dissector raised an error on this capture", kind)
		}
	}
}

// The golden scenario and the Controller's plant are there too.
func TestTheFixturesAndExportsAreCommitted(t *testing.T) {
	for _, f := range []string{
		"fixtures/node-registers.pcapng", "fixtures/node-registers.tree",
		"exports/reference-registry.json", "exports/reference-node.json",
		"integration-test/README.md", "README.md",
	} {
		if st, err := os.Stat(filepath.Join("testdata", filepath.FromSlash(f))); err != nil || st.Size() == 0 {
			t.Errorf("testdata/%s is missing or empty", f)
		}
	}
	tree, _ := os.ReadFile(filepath.Join("testdata", "fixtures", "node-registers.tree"))
	for _, w := range []string{"POST type=node", "POST type=device", "POST type=source", "POST type=flow"} {
		if !strings.Contains(string(tree), w) {
			t.Errorf("the golden scenario's tree does not carry %q", w)
		}
	}
}
