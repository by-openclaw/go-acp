package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The oracle of 2026-11-07: panel 93.8, MADI channel 93.17 as
// FOUR_WIRE_SPLIT (two objects, .in and .out), no IFB yet.
const rrcsOracleSnap = `{
 "lists": [{"method": "GetAllPorts", "payload": [[
   {"Net": 1, "Node": 93, "Port": 8,  "Input": true,  "Output": true,  "PortType": "RSP-1216HL"},
   {"Net": 1, "Node": 93, "Port": 17, "Input": true,  "Output": false, "PortType": "Input (MADI)"},
   {"Net": 1, "Node": 93, "Port": 17, "Input": false, "Output": true,  "PortType": "Output (MADI)"}
 ]]}]
}`

func rrcsOracleModel(t *testing.T) *rrcsModel {
	t.Helper()
	var snap map[string]any
	if err := json.Unmarshal([]byte(rrcsOracleSnap), &snap); err != nil {
		t.Fatal(err)
	}
	return rrcsBuildModel(snap)
}

func TestRRCSResolveTalkPathsSplitPort(t *testing.T) {
	m := rrcsOracleModel(t)
	want := &rrcsDesired{
		Keys: []rrcsDesiredKey{
			{Panel: "net.1.node.93.port.8", Key: "0.1.1", Function: "call-to-port", Target: "net.1.node.93.port.17"},
			{Panel: "net.1.node.93.port.8", Key: "0.1.2", Function: "call-to-port", Target: "net.1.node.93.port.17.in"},
			{Panel: "net.1.node.93.port.8", Key: "0.1.3", Function: "call-to-conference", Target: "conference.5"},
		},
		Groups: []rrcsDesiredObject{{Name: "G", Members: []string{"net.1.node.93.port.8", "net.1.node.93.port.17", "net.1.node.93.port.99"}}},
	}
	rrcsResolveTalkPaths(m, want)
	if got := want.Keys[0].Target; got != "net.1.node.93.port.17.out" {
		t.Errorf("bare split port: %s", got)
	}
	if got := want.Keys[1].Target; got != "net.1.node.93.port.17.in" {
		t.Errorf("explicit half kept: %s", got)
	}
	if got := want.Keys[2].Target; got != "conference.5" {
		t.Errorf("conference target kept: %s", got)
	}
	got := strings.Join(want.Groups[0].Members, " ")
	if got != "net.1.node.93.port.8 net.1.node.93.port.17.out net.1.node.93.port.99" {
		t.Errorf("members: %s", got)
	}
}

func TestRRCSPlanIFBCreatesAnAbsentNumber(t *testing.T) {
	m := rrcsOracleModel(t)
	label, name, in := "DHSIFB2", "DHS IFB 2", "net.1.node.93.port.17.in"
	steps, problems := rrcsPlanIFBs(m, &rrcsDesired{IFBs: []rrcsDesiredIFB{{Number: 2, Label: &label, Name: &name, Input: &in}}})
	if len(problems) != 0 || len(steps) != 1 {
		t.Fatalf("steps %d problems %v", len(steps), problems)
	}
	s := steps[0]
	if s.changeType != "create" || s.objectType != "ifb" {
		t.Fatalf("step: %s %s", s.changeType, s.objectType)
	}
	names := []string{}
	for _, p := range s.params {
		names = append(names, p.Name)
	}
	if strings.Join(names, " ") != "IFBNumber Label LongName" {
		t.Errorf("create params: %v", names)
	}
}

func TestRRCSBufferable(t *testing.T) {
	for _, tc := range []struct {
		method, body string
		ok           bool
	}{
		{"BufferConfigurationChange", `[{"ObjectType":"group"},{"ObjectType":"panel-key"}]`, true},
		{"BufferConfigurationChange", `[{"ObjectType":"Group"}]`, true},
		{"BufferConfigurationChange", `[{"ObjectType":"conference"}]`, false},
		{"BufferConfigurationChange", `[{"ObjectType":"ifb"}]`, false},
		{"BufferConfigurationChange", `[{"ObjectType":"portex"}]`, false},
		{"BufferConfigurationChangeEx", `[{"ObjectType":"portex"}]`, true},
		{"BufferConfigurationChangeEx", `[{"ObjectType":"monitor-port-cmd"}]`, true},
		{"BufferConfigurationChangeEx", `[{"ObjectType":"gpio"}]`, false},
		{"BufferConfigurationChange", `{"ObjectType":"group"}`, false},
	} {
		err := rrcsBufferable(tc.method, []byte(tc.body))
		if (err == nil) != tc.ok {
			t.Errorf("%s %s: %v, want ok=%v", tc.method, tc.body, err, tc.ok)
		}
	}
}
