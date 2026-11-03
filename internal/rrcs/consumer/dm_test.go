package consumer

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"dhs/internal/export/canonical"
	"dhs/internal/manifest"
)

// dmSnapshot is a small system as a walk writes it: one node, two
// cards, a split port with its linked neighbour, a panel with one key,
// a Bolero beltpack whose block carries a property the tables do not
// print, one conference.
const dmSnapshot = `{
 "proto": "rrcs", "target": "192.0.2.10:8193",
 "lists": [
  {"method": "GetAllNodes", "payload": [[{"NodeAddress": 60, "LongName": "FRAME A", "NodeTypeString": "ARTIST_1024", "ObjectID": 901, "PrimaryNicAvailable": true}]]},
  {"method": "GetAllClientCards", "payload": [[
   {"Node": 60, "Bay": 1, "ClientCardTypeString": "AES67", "LongName": "AES67 C1", "ObjectID": 11, "Ptp": {"PTP": 100, "PtpMode": 0}},
   {"Node": 60, "Bay": 4, "ClientCardTypeString": "AES67", "LongName": "AES67 C4", "ObjectID": 44, "Ptp": {"PTP": 100, "PtpMode": 0}}]]},
  {"method": "GetAllPorts", "payload": [[
   {"Net": 1, "Node": 63, "Port": 1064, "Input": true, "Output": false, "PortType": "Input (AES67)", "PortExType": 3, "Label": "I.-7.41", "LongName": "In. -7.41", "ObjectID": 101064, "KeyCount": 0, "PageCount": 0,
    "PortAes67Input": {"Protocol": 5, "Channels": 8, "Selection": 1, "Multicast": "0.0.0.0", "MulticastPort": 5004, "PayloadType": 96, "BitDepth": 24, "PacketTime": 1000}},
   {"Net": 1, "Node": 63, "Port": 1064, "Input": false, "Output": true, "PortType": "Output (AES67)", "PortExType": 3, "Label": "O.-7.41", "LongName": "Out. -7.41", "ObjectID": 201064, "KeyCount": 0, "PageCount": 0,
    "PortAes67Output": {"Protocol": 5, "Channels": 8, "Selection": 1, "Multicast": "0.0.0.0", "MulticastPort": 5004, "PayloadType": 97, "BitDepth": 24, "PacketTime": 1000}},
   {"Net": 1, "Node": 63, "Port": 1065, "Input": true, "Output": false, "PortType": "Input (AES67)", "PortExType": 3, "Label": "I.-7.42", "LongName": "In. -7.42", "ObjectID": 1, "KeyCount": 0, "PageCount": 0,
    "PortAes67Input": {"Protocol": 5, "Channels": 1, "Selection": 2, "Multicast": "0.0.0.0", "MulticastPort": 5004, "PayloadType": 96, "BitDepth": 24, "PacketTime": 1000}},
   {"Net": 1, "Node": 63, "Port": 1065, "Input": false, "Output": true, "PortType": "Output (AES67)", "PortExType": 3, "Label": "O.-7.42", "LongName": "Out. -7.42", "ObjectID": 2, "KeyCount": 0, "PageCount": 0,
    "PortAes67Output": {"Protocol": 5, "Channels": 1, "Selection": 2, "Multicast": "0.0.0.0", "MulticastPort": 5004, "PayloadType": 96, "BitDepth": 24, "PacketTime": 1000}},
   {"Net": 1, "Node": 61, "Port": 1024, "Input": true, "Output": true, "PortType": "RSP-1232HL", "PortExType": 11, "Label": "NOC1", "LongName": "BM NOC 1", "ObjectID": 101024, "KeyCount": 32, "PageCount": 2,
    "ConnectedExpansions": [1, 2], "PortAes67": {"IpAddress": "10.5.62.66", "ListenPort": 6060}},
   {"Net": 1, "Node": 61, "Port": 2000, "Input": true, "Output": true, "PortType": "Bolero Wireless Beltpack", "PortExType": 33, "Label": "BP1", "LongName": "Beltpack 1", "ObjectID": 777, "KeyCount": 0, "PageCount": 0,
    "PortBolero": {"BoleroUserId": 5, "Foo": 1}}]]},
  {"method": "GetAllConferences", "payload": [[{"ObjectID": 300, "Label": "CONF", "LongName": "Conference", "MemberList": [{"Node": 61, "Port": 1024, "Talk": true, "Listen": true}]}]]},
  {"method": "GetAllGroups", "payload": [[]]},
  {"method": "GetAllIFBs", "payload": [[]]},
  {"method": "GetAllLogicSources_v2", "payload": {"LogicSource#1": ["Studio On-Air", "On-Air", 500, true]}},
  {"method": "GetPortExTypeList", "payload": [[{"PortExType": 3, "PortExTypeString": "FOUR_WIRE_SPLIT"}, {"PortExType": 11, "PortExTypeString": "RSP_1232HL"}, {"PortExType": 33, "PortExTypeString": "BOLERO_BP"}]]}
 ],
 "commands": [
  {"method": "GetPortsCommandLists", "args": [1, 61, 1024, false, -1], "payload": {"CommandLists": [
   {"CommandPosition": {"ExpansionPanel": 0, "IsInput": false, "KeyNumber": 1, "Net": 1, "Node": 61, "Page": 1, "Port": 1024, "PositionType": "key"},
    "CommandList": [{"CommandType": "call-to-port-cmd", "Description": "Call", "DestinationPortAddress": {"IsInput": false, "Net": 1, "Node": 63, "Port": 1064}, "ObjectID": 600, "Priority": 1}]}]}}
 ],
 "key_configs": [
  {"method": "GetAllKeyConfiguration", "args": [61, 1024, false, -1], "payload": {"KeyList": [
   {"KeyPosition": {"ExpansionPanel": 0, "KeyNumber": 1, "Page": 1, "PositionType": "key"},
    "KeyProperties": {"ActionByKeyPressed": 1, "KeyMode": 2, "LabelValue": "MCR", "ObjectID": 629, "Zzz": true}}]}}
 ]
}`

func dmModel(t *testing.T) *Model {
	t.Helper()
	var snap map[string]any
	if err := json.Unmarshal([]byte(dmSnapshot), &snap); err != nil {
		t.Fatal(err)
	}
	return BuildModel(snap)
}

func childOf(t *testing.T, el canonical.Element, ident string) canonical.Element {
	t.Helper()
	for _, c := range el.Common().Children {
		if c.Common().Identifier == ident {
			return c
		}
	}
	t.Fatalf("%s has no child %q", el.Common().Path, ident)
	return nil
}

func templateOf(t *testing.T, x canonical.Export, ident string) *canonical.TemplateEntry {
	t.Helper()
	for _, e := range x.Templates {
		if e.Identifier == ident {
			return e
		}
	}
	t.Fatalf("no template %q", ident)
	return nil
}

func TestVersionAndTypeName(t *testing.T) {
	for in, want := range map[string]string{"9.0.RR1-11.f33d55f": "9.0.RR1-11", "9.0.1": "9.0.1", " 8.4 ": "8.4", "": ""} {
		if got := Version(in); got != want {
			t.Errorf("Version(%q) = %q, want %q", in, got, want)
		}
	}
	names := map[int]string{3: "FOUR_WIRE_SPLIT", 11: "RSP_1232HL"}
	for _, tc := range []struct {
		ex   int
		str  string
		want string
	}{
		{3, "Input (AES67)", "FOUR_WIRE_SPLIT"},
		{11, "RSP-1232HL", "RSP_1232HL"},
		{33, "Bolero Wireless Beltpack", "BOLERO_WIRELESS_BELTPACK"}, // not in the list: from the string
		{48, "4-Wire Unicast (AES67)", "4_WIRE_UNICAST_AES67"},
	} {
		if got := TypeName(tc.ex, tc.str, names); got != tc.want {
			t.Errorf("TypeName(%d, %q) = %q, want %q", tc.ex, tc.str, got, tc.want)
		}
	}
}

func TestBuildDMs(t *testing.T) {
	m := dmModel(t)
	var snap map[string]any
	_ = json.Unmarshal([]byte(dmSnapshot), &snap)
	set, err := BuildDMs(m, "9.0.RR1-11.f33d55f", ExTypeNames(snap))
	if err != nil {
		t.Fatal(err)
	}
	if set.Node.Identity != "ARTIST_1024@9.0.RR1-11" || len(set.Cards) != 1 || set.Cards[0].Identity != "AES67@9.0.RR1-11" {
		t.Fatalf("identities: node %q, cards %d", set.Node.Identity, len(set.Cards))
	}
	card := set.Cards[0].Export
	var names []string
	for _, e := range card.Templates {
		names = append(names, e.Identifier)
	}
	if len(names) != 3 || names[0] != "BOLERO_BP" || names[1] != "FOUR_WIRE_SPLIT" || names[2] != "RSP_1232HL" {
		t.Errorf("card templates %v", names)
	}
	// The card's own properties are its root.
	if ptp := childOf(t, childOf(t, card.Root, "Ptp"), "PTP").(*canonical.Parameter); ptp.Type != canonical.ParamInteger || ptp.Access != canonical.AccessReadWrite {
		t.Errorf("card Ptp.PTP %+v", ptp)
	}
	// A split port: a half per direction, the blocks typed by the tables.
	split := templateOf(t, card, "FOUR_WIRE_SPLIT")
	in := childOf(t, split.Template, "in")
	ch := childOf(t, childOf(t, in, "PortAes67Input"), "Channels").(*canonical.Parameter)
	if ch.Type != canonical.ParamInteger || ch.Minimum != int64(1) || ch.Maximum != int64(16) || ch.Access != canonical.AccessReadWrite || ch.Value != nil {
		t.Errorf("Channels %+v", ch)
	}
	proto := childOf(t, childOf(t, in, "PortAes67Input"), "Protocol").(*canonical.Parameter)
	if proto.Type != canonical.ParamEnum || len(proto.EnumMap) != 3 || proto.EnumMap[2].Key != "NMOS" || proto.EnumMap[2].Value != 5 {
		t.Errorf("Protocol %+v", proto)
	}
	childOf(t, childOf(t, childOf(t, split.Template, "out"), "PortAes67Output"), "Selection")
	if lbl := childOf(t, in, "Label").(*canonical.Parameter); lbl.Type != canonical.ParamString || lbl.Description == nil || *lbl.Description != "max_len 8" {
		t.Errorf("Label %+v", lbl)
	}
	// A panel carries keys; a Bolero block has a property beyond the tables.
	childOf(t, templateOf(t, card, "RSP_1232HL").Template, "key")
	foo := childOf(t, childOf(t, templateOf(t, card, "BOLERO_BP").Template, "PortBolero"), "Foo").(*canonical.Parameter)
	if foo.Description == nil || *foo.Description != "observed: not in the specification's tables" {
		t.Errorf("Foo %+v", foo)
	}
	stats := map[string]TypeStat{}
	for _, st := range set.Types {
		stats[st.Name] = st
	}
	if st := stats["BOLERO_BP"]; st.ExType != 33 || st.Count != 1 || len(st.Observed) != 1 || st.Observed[0] != "PortBolero.Foo" || len(st.Hidden) == 0 {
		t.Errorf("Bolero stat %+v", st)
	}
	if st := stats["FOUR_WIRE_SPLIT"]; st.Count != 2 || st.Roles["stream-block"] != 2 || st.Roles["stream-linked"] != 2 || len(st.Strings) != 2 {
		t.Errorf("split stat %+v", st)
	}
	// The node: the system's types as templates.
	node := set.Node.Export
	conf := templateOf(t, node, "conference")
	childOf(t, childOf(t, conf.Template, "member"), "port")
	key := templateOf(t, node, "key")
	childOf(t, childOf(t, key.Template, "cmd"), "CommandType")
	if z := childOf(t, key.Template, "Zzz").(*canonical.Parameter); z.Description == nil {
		t.Errorf("Zzz is observed: %+v", z)
	}
	if st := stats["call-to-port-cmd"]; st.Count != 1 || st.Props == 0 {
		t.Errorf("call-to-port-cmd stat %+v", st)
	}
	if st := stats["reply-cmd"]; st.Count != 0 || st.Props == 0 {
		t.Errorf("reply-cmd from the tables alone %+v", st)
	}
	if n := len(node.Templates); n < 4+1+len(CommandTypes()) {
		t.Errorf("%d node templates", n)
	}
	// The files round-trip through the canonical reader.
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	var back canonical.Export
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("card DM does not read back: %v", err)
	}
	if len(back.Templates) != 3 {
		t.Errorf("%d templates read back", len(back.Templates))
	}
}

// The slot of a row: 0 for the node and the system objects, the bay of
// the card for a card and for the ports on its node — every port's rows,
// not only the first port's.
func TestRowsSlot(t *testing.T) {
	m := dmModel(t)
	slots := map[string]string{}
	for _, r := range Rows(m) {
		slots[r.Path] = r.Slot
	}
	for path, want := range map[string]string{
		"net.1.node.60.LongName":                                "0",
		"conference.300.Label":                                  "0",
		"net.1.node.60.card.1.LongName":                         "1",
		"net.1.node.60.card.4.LongName":                         "4",
		"net.1.node.61.port.1024.Label":                         "1",
		"net.1.node.61.port.1024.key.0.1.1.LabelValue":          "1",
		"net.1.node.61.port.1024.key.0.1.1.cmd.CommandType":     "1",
		"net.1.node.61.port.2000.Label":                         "1",
		"net.1.node.63.port.1064.in.PortAes67Input.Channels":    "4",
		"net.1.node.63.port.1065.out.PortAes67Output.Selection": "4",
	} {
		if got, ok := slots[path]; !ok || got != want {
			t.Errorf("%s: slot %q (present %v), want %q", path, got, ok, want)
		}
	}
}

func TestManifest(t *testing.T) {
	m := dmModel(t)
	var snap map[string]any
	_ = json.Unmarshal([]byte(dmSnapshot), &snap)
	set, err := BuildDMs(m, "9.0.RR1-11", ExTypeNames(snap))
	if err != nil {
		t.Fatal(err)
	}
	mf := Manifest(m, "192.0.2.10", 8193, set)
	if mf.Device.Name != "FRAME A" || mf.Device.IP != "192.0.2.10" || mf.Device.Endpoints[0].Port != 8193 {
		t.Errorf("device %+v", mf.Device)
	}
	if len(mf.Frames) != 1 || len(mf.Frames[0].Slots) != 1+2+4 {
		t.Fatalf("frames %+v", mf.Frames)
	}
	byPort := map[int]manifest.Slot{}
	for _, s := range mf.Frames[0].Slots {
		if p, ok := s.Addr["port"].(int); ok {
			byPort[p] = s
		}
	}
	main := byPort[1064]
	if main.DM != "AES67@9.0.RR1-11" || main.Addr["template"] != "FOUR_WIRE_SPLIT" || main.Addr["bay"] != 4 || main.Addr["type"] != 3 {
		t.Errorf("1064 %+v", main.Addr)
	}
	oids, _ := main.Addr["oid"].(map[string]any)
	roles, _ := main.Addr["role"].(map[string]any)
	if oids["in"] != 101064 || oids["out"] != 201064 || roles["in"].(map[string]any)["kind"] != "stream-block" || roles["out"].(map[string]any)["channels"] != 8 {
		t.Errorf("1064 oid %v role %v", oids, roles)
	}
	linked, _ := byPort[1065].Addr["role"].(map[string]any)
	if linked["in"].(map[string]any)["kind"] != "stream-linked" || linked["in"].(map[string]any)["channel"] != 2 {
		t.Errorf("1065 role %v", linked)
	}
	panel := byPort[1024]
	if panel.Addr["template"] != "RSP_1232HL" || panel.Addr["keys"] != 32 || panel.Addr["pages"] != 2 || panel.Addr["expansions"] != 2 || panel.Addr["bay"] != 1 ||
		panel.Addr["role"].(map[string]any)["kind"] != "panel" || panel.Addr["oid"] != 101024 {
		t.Errorf("1024 %+v", panel.Addr)
	}
	if bp := byPort[2000]; bp.Addr["template"] != "BOLERO_BP" || bp.Addr["role"].(map[string]any)["kind"] != "port" {
		t.Errorf("2000 %+v", bp.Addr)
	}
	if s := mf.Frames[0].Slots[0]; s.DM != "ARTIST_1024@9.0.RR1-11" || s.Addr["bay"] != 0 || s.Addr["oid"] != 901 {
		t.Errorf("node entry %+v", s)
	}
	if s := mf.Frames[0].Slots[2]; s.Addr["bay"] != 4 || s.Addr["node"] != 63 || s.Addr["card_node"] != 60 {
		t.Errorf("card entry %+v", s.Addr)
	}
	// What the shared writer makes of it reads back as a valid manifest.
	dir := t.TempDir()
	path, err := manifest.Write(dir, mf)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "192.0.2.10.json" {
		t.Errorf("manifest file %s", path)
	}
	back, err := manifest.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Frames[0].Slots) != 7 {
		t.Errorf("%d entries read back", len(back.Frames[0].Slots))
	}
}
