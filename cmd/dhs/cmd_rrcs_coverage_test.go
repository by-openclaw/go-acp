package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The catalogue is what says "covered": it must name each method once,
// with a known kind, and every request the read verbs send must be in it
// under a verb, not under call.
func TestRRCSCatalog(t *testing.T) {
	byName := map[string]rrcsMethod{}
	for _, m := range rrcsCatalog {
		if _, dup := byName[m.Name]; dup {
			t.Errorf("%s is listed twice", m.Name)
		}
		byName[m.Name] = m
		switch m.Kind {
		case "read", "write", "register", "notify":
		default:
			t.Errorf("%s: kind %q", m.Name, m.Kind)
		}
		if m.Verb == "call" {
			t.Errorf("%s has no verb of its own", m.Name)
		}
		if m.Verb == "" || m.Real == "" || m.Section == "" {
			t.Errorf("%s: incomplete entry %+v", m.Name, m)
		}
		// A method that only reads has a name that says so; anything
		// else must not be reachable without the write guard.
		if m.Kind == "read" && !rrcsReadOnlyMethod(m.Name) {
			t.Errorf("%s is listed as a read but the write guard would stop it", m.Name)
		}
		if (m.Kind == "write" || m.Kind == "register") && rrcsReadOnlyMethod(m.Name) {
			t.Errorf("%s changes something but the write guard lets it through", m.Name)
		}
	}
	for _, op := range rrcsOps {
		m, ok := byName[op.Method]
		if !ok || m.Verb != rrcsOpVerb(op.Method) {
			t.Errorf("%s: catalogue says %+v, the verb is %s", op.Method, m, rrcsOpVerb(op.Method))
		}
		if ok && (m.Kind == "read") != op.Read {
			t.Errorf("%s: the catalogue and the table disagree on whether it reads", op.Method)
		}
	}
	sent := append(append(append([]string{}, rrcsInfoMethods...), rrcsDiscoverMethods...), rrcsWalkLists...)
	for _, name := range sent {
		m, ok := byName[name]
		if !ok {
			t.Errorf("%s is sent by a verb and missing from the catalogue", name)
			continue
		}
		if m.Verb == "call" {
			t.Errorf("%s is sent by a verb and catalogued as call only", name)
		}
	}
}

func TestRRCSCoverageVerb(t *testing.T) {
	out := rrcsRun(t, "coverage")
	rrcsWant(t, out, "SECTION  METHOD", "GetAllPorts", "ConfigurationChangeEx", "stopped RRCS", "KIND      IN THE SPECIFICATION", "total")
	// Every method has a verb of its own: nothing is left to the generic
	// call verb alone.
	if got := strings.TrimSpace(rrcsRun(t, "coverage", "--output", "json", "--only", "call")); got != "[]" {
		t.Errorf("methods without a verb of their own: %s", got)
	}
	var rows []rrcsMethod
	if err := json.Unmarshal([]byte(rrcsRun(t, "coverage", "--output", "json", "--only", "write")), &rows); err != nil || len(rows) == 0 {
		t.Fatalf("json: %v, %d rows", err, len(rows))
	}
	for _, r := range rows {
		if r.Kind != "write" {
			t.Errorf("--only write returned %+v", r)
		}
	}
	if unproven := rrcsRun(t, "coverage", "--only", "unproven"); strings.Contains(unproven, "GetVersion ") {
		t.Errorf("--only unproven lists a proven method")
	}
	rrcsWant(t, rrcsRun(t, "coverage", "--only", "notify"), "CrosspointChange", "PanelSpyKeyEvent")
}
