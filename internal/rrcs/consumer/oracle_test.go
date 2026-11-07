package consumer

import (
	"encoding/json"
	"testing"
)

// oracleSnapshot is the shape a real RRCS 9.0.RR1 answered on 2026-11-07:
// a panel (93.8, one object for both directions), a MADI channel as
// FOUR_WIRE_SPLIT (93.17.in and 93.17.out, two objects), and on the panel
// key 0.1.6 a call to IFB 1062278048 whose IfbName comes back empty.
const oracleSnapshot = `{
 "lists": [{"method": "GetAllPorts", "payload": [[
   {"Net": 1, "Node": 93, "Port": 8,  "Input": true,  "Output": true,  "PortType": "RSP-1216HL", "Label": "YOUSSEF"},
   {"Net": 1, "Node": 93, "Port": 17, "Input": true,  "Output": false, "PortType": "Input (MADI)",  "Label": "I.3.2"},
   {"Net": 1, "Node": 93, "Port": 17, "Input": false, "Output": true,  "PortType": "Output (MADI)", "Label": "O.3.2"}
 ]]}],
 "commands": [{"payload": {"CommandLists": [
   {"CommandPosition": {"ExpansionPanel": 0, "IsInput": false, "KeyNumber": 6, "Net": 1, "Node": 93, "Page": 1, "Port": 8, "PositionType": "key"},
    "CommandList": [{"CommandType": "call-to-ifb", "IFBObjectID": 1062278048, "IfbName": "", "ObjectID": 231112008}]}
 ]}}]
}`

func oracleModel(t *testing.T) *Model {
	t.Helper()
	var snap map[string]any
	if err := json.Unmarshal([]byte(oracleSnapshot), &snap); err != nil {
		t.Fatal(err)
	}
	return BuildModel(snap)
}

func TestModelReadsTheIFBOfACallToIFBKey(t *testing.T) {
	m := oracleModel(t)
	if len(m.Keys) != 1 {
		t.Fatalf("keys: %d", len(m.Keys))
	}
	k := m.Keys[0]
	if k.CommandType != "call-to-ifb" || k.Target != "ifb.1062278048" {
		t.Errorf("key 0.1.6: %s %q, want call-to-ifb ifb.1062278048", k.CommandType, k.Target)
	}
}

func TestModelSplitPortHalves(t *testing.T) {
	m := oracleModel(t)
	if p := m.Find(93, 17, false); p == nil || p.Path != "net.1.node.93.port.17.out" {
		t.Errorf("93.17 output: %+v", p)
	}
	if p := m.Find(93, 8, false); p == nil || p.Path != "net.1.node.93.port.8" {
		t.Errorf("panel 93.8: %+v", p)
	}
}

func TestVolumeRaw(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"mute", 0, true},
		{"MUTE", 0, true},
		{"-10dB", 210, true},
		{"-14.5dB", 201, true},
		{"0dB", 230, true},
		{"12.5dB", 255, true},
		{"-114.5dB", 1, true},
		{" -10 dB ", 210, true},
		{"210", 210, true},
		{"0", 0, true},
		{"255", 255, true},
		{"-10.25dB", 0, false},
		{"13dB", 0, false},
		{"-115dB", 0, false},
		{"xdB", 0, false},
		{"-10", 0, false},
		{"256", 0, false},
		{"loud", 0, false},
	} {
		got, err := VolumeRaw(tc.in)
		if (err == nil) != tc.ok || (tc.ok && got != tc.want) {
			t.Errorf("VolumeRaw(%q) = %d, %v; want %d ok=%v", tc.in, got, err, tc.want, tc.ok)
		}
	}
}

func TestCardOfPortOnAFrameWhoseCardsShareItsNode(t *testing.T) {
	m := &Model{Cards: []*Card{{Node: 93, Bay: 1, Type: "AES67"}, {Node: 93, Bay: 2, Type: "CAT5"}, {Node: 93, Bay: 3, Type: "MADI"}}}
	for port, want := range map[int]string{0: "AES67", 7: "AES67", 8: "CAT5", 16: "MADI", 23: "MADI", 24: ""} {
		got := ""
		if c := m.CardOfPort(nil, &Port{Node: 93, Port: port}); c != nil {
			got = c.Type
		}
		if got != want {
			t.Errorf("port %d: %q, want %q", port, got, want)
		}
	}
	// An Artist-1024: the card is the one of the port's own node.
	sic := &Card{Node: 60, Bay: 4, Type: "AES67"}
	if c := m.CardOfPort(map[int]*Card{61: sic}, &Port{Node: 61, Port: 1030}); c != sic {
		t.Errorf("node card: %+v", c)
	}
}
