package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/datastore"
	"dhs/internal/manifest"
	"dhs/internal/rrcs/codec"
)

// rrcsExtractAnswer is a gateway with one node, one card, a split port
// and a panel with one key: what extract needs, and nothing it does not
// ask for.
func rrcsExtractAnswer(t *testing.T) func(codec.Call) (codec.Value, bool) {
	t.Helper()
	of := func(text string) codec.Value {
		v, err := rrcsValueOfJSON(text)
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
		return v
	}
	list := func(k codec.Value, text string) (codec.Value, bool) { return codec.Array(k, of(text)), true }
	st := func(k codec.Value, name, text string) (codec.Value, bool) {
		return codec.Struct(rrcsMember("TransKey", k), rrcsMember("ErrorCode", codec.Int(0)), rrcsMember(name, of(text))), true
	}
	return func(call codec.Call) (codec.Value, bool) {
		k := call.Params[0]
		switch call.Method {
		case "GetVersion":
			return codec.Array(k, codec.Int(0), codec.String("9.0.RR1-11.f33d55f")), true
		case "GetAllNodes":
			return list(k, `[{"NodeAddress": 60, "LongName": "FRAME A", "NodeTypeString": "ARTIST_1024", "ObjectID": 901}]`)
		case "GetAllClientCards":
			return list(k, `[{"Node": 60, "Bay": 4, "ClientCardTypeString": "AES67", "LongName": "AES67 C4", "ObjectID": 44, "Ptp": {"PTP": 100}}]`)
		case "GetAllPorts":
			return list(k, `[
			 {"Net": 1, "Node": 63, "Port": 1064, "Input": true, "Output": false, "PortType": "Input (AES67)", "PortExType": 3, "Label": "I.-7.41", "LongName": "In. -7.41", "ObjectID": 101064, "KeyCount": 0, "PageCount": 0,
			  "PortAes67Input": {"Protocol": 5, "Channels": 8, "Selection": 1, "Multicast": "0.0.0.0", "MulticastPort": 5004, "PayloadType": 96, "BitDepth": 24, "PacketTime": 1000}},
			 {"Net": 1, "Node": 63, "Port": 1064, "Input": false, "Output": true, "PortType": "Output (AES67)", "PortExType": 3, "Label": "O.-7.41", "LongName": "Out. -7.41", "ObjectID": 201064, "KeyCount": 0, "PageCount": 0,
			  "PortAes67Output": {"Protocol": 5, "Channels": 8, "Selection": 1, "Multicast": "0.0.0.0", "MulticastPort": 5004, "PayloadType": 97, "BitDepth": 24, "PacketTime": 1000}},
			 {"Net": 1, "Node": 63, "Port": 1024, "Input": true, "Output": true, "PortType": "RSP-1232HL", "PortExType": 11, "Label": "NOC1", "LongName": "BM NOC 1", "ObjectID": 101024, "KeyCount": 32, "PageCount": 2,
			  "ConnectedExpansions": [1], "PortAes67": {"IpAddress": "10.5.62.66", "ListenPort": 6060}}]`)
		case "GetAllConferences":
			return list(k, `[{"ObjectID": 300, "Label": "CONF", "LongName": "Conference", "MemberList": [{"Node": 63, "Port": 1024, "Talk": true, "Listen": true}]}]`)
		case "GetAllGroups", "GetAllIFBs":
			return list(k, `[]`)
		case "GetAllLogicSources_v2":
			return st(k, "LogicSource#1", `["Studio On-Air", "On-Air", 500, true]`)
		case "GetPortExTypeList":
			return list(k, `[{"PortExType": 3, "PortExTypeString": "FOUR_WIRE_SPLIT"}, {"PortExType": 11, "PortExTypeString": "RSP_1232HL"}]`)
		case "GetPortsCommandLists":
			if call.Params[3].Int != 1024 {
				return st(k, "CommandLists", `[]`)
			}
			return st(k, "CommandLists", `[{"CommandPosition": {"ExpansionPanel": 0, "IsInput": false, "KeyNumber": 1, "Net": 1, "Node": 63, "Page": 1, "Port": 1024, "PositionType": "key"},
			 "CommandList": [{"CommandType": "call-to-port-cmd", "Description": "Call", "DestinationPortAddress": {"IsInput": false, "Net": 1, "Node": 63, "Port": 1064}, "ObjectID": 600, "Priority": 1}]}]`)
		case "GetAllKeyConfiguration":
			return st(k, "KeyList", `[{"KeyPosition": {"ExpansionPanel": 0, "KeyNumber": 1, "Page": 1, "PositionType": "key"},
			 "KeyProperties": {"ActionByKeyPressed": 1, "KeyMode": 2, "LabelValue": "MCR", "ObjectID": 629}}]`)
		}
		return codec.Value{}, false
	}
}

func TestRRCSExtract(t *testing.T) {
	prev := treeStore
	treeStore = datastore.NewTreeStore(t.TempDir())
	t.Cleanup(func() { treeStore = prev })
	f := newRRCSFake(t, rrcsExtractAnswer(t))

	out := rrcsRun(t, "extract", f.addr())
	for _, want := range []string{"version:  9.0.RR1-11 (FRAME A)", "requests: 12", "model written:", "manifest:", "(4 entries)", "FOUR_WIRE_SPLIT", "RSP_1232HL", "call-to-port-cmd"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// One request per list, the version, the commands of one port per
	// type and the keys of the panel: nothing per port.
	count := map[string]int{}
	for _, m := range f.methods() {
		count[m]++
	}
	if count["GetVersion"] != 1 || count["GetAllPorts"] != 1 || count["GetPortsCommandLists"] != 2 || count["GetAllKeyConfiguration"] != 1 || count["GetPort"] != 0 {
		t.Errorf("requests %v", count)
	}

	// The card's model: its templates are the port types; the node's
	// the system's objects, the key and the commands.
	var card datastore.DM
	raw, err := os.ReadFile(treeStore.IdentityPath("rrcs", "AES67@9.0.RR1-11"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &card); err != nil {
		t.Fatal(err)
	}
	if card.Model != "AES67" || card.SwRev != "9.0.RR1-11" || card.Root == nil || len(card.Templates) != 2 ||
		card.Templates[0].Identifier != "FOUR_WIRE_SPLIT" || card.Templates[1].Identifier != "RSP_1232HL" {
		t.Errorf("card model %s %s templates %d", card.Model, card.SwRev, len(card.Templates))
	}
	var node datastore.DM
	raw, err = os.ReadFile(treeStore.IdentityPath("rrcs", "ARTIST_1024@9.0.RR1-11"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range node.Templates {
		names[e.Identifier] = true
	}
	if !names["conference"] || !names["key"] || !names["call-to-port-cmd"] || !names["reply-cmd"] {
		t.Errorf("node templates %v", names)
	}

	// The inventory: node, card, two ports; the split port with its two
	// object IDs and its roles, the panel with its keys.
	mf, err := manifest.Load(filepath.Join(treeStore.BaseDir(), "manifest", "rrcs", "127.0.0.1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if mf.Device.Name != "FRAME A" || mf.Device.Protocol != "rrcs" || len(mf.Frames) != 1 || len(mf.Frames[0].Slots) != 4 {
		t.Fatalf("manifest %+v", mf)
	}
	for _, s := range mf.Frames[0].Slots {
		switch s.Addr["port"] {
		case float64(1064):
			role := s.Addr["role"].(map[string]any)["in"].(map[string]any)
			if s.DM != "AES67@9.0.RR1-11" || s.Addr["template"] != "FOUR_WIRE_SPLIT" || role["kind"] != "stream-block" || s.Addr["bay"] != float64(4) {
				t.Errorf("1064 %+v", s.Addr)
			}
		case float64(1024):
			if s.Addr["template"] != "RSP_1232HL" || s.Addr["keys"] != float64(32) || s.Addr["role"].(map[string]any)["kind"] != "panel" {
				t.Errorf("1024 %+v", s.Addr)
			}
		}
	}

	// A second run keeps the models and rewrites the inventory; --refresh
	// rewrites the models.
	if out := rrcsRun(t, "extract", f.addr()); strings.Count(out, "model kept:") != 2 || !strings.Contains(out, "manifest:") {
		t.Errorf("second run:\n%s", out)
	}
	if out := rrcsRun(t, "extract", f.addr(), "--refresh"); strings.Count(out, "model written:") != 2 {
		t.Errorf("--refresh:\n%s", out)
	}
	// From a walk, without a gateway: the same models and inventory.
	snap := filepath.Join(t.TempDir(), "walk.json")
	rrcsRun(t, "walk", f.addr(), "--out", snap, "--skip", "both")
	before := len(f.methods())
	if out := rrcsRun(t, "extract", "--from", snap, "--refresh"); !strings.Contains(out, "version:  9.0.RR1-11 (FRAME A)") || !strings.Contains(out, "source:   "+snap) || strings.Count(out, "model written:") != 2 {
		t.Errorf("--from:\n%s", out)
	}
	if len(f.methods()) != before {
		t.Errorf("--from asked the gateway")
	}
	// The account as JSON.
	var account struct {
		Version string           `json:"version"`
		Models  []map[string]any `json:"models"`
		Types   []map[string]any `json:"types"`
	}
	if err := json.Unmarshal([]byte(rrcsRun(t, "extract", f.addr(), "--output", "json")), &account); err != nil {
		t.Fatal(err)
	}
	if account.Version != "9.0.RR1-11" || len(account.Models) != 2 || len(account.Types) < 30 {
		t.Errorf("json account: %s %d models %d types", account.Version, len(account.Models), len(account.Types))
	}
}
