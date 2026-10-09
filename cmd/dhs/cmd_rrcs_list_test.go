package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

// rrcsTreeAnswer is a small Artist-1024 the way a real 9.0 answers: one
// node 60, ports on node 61, an input and an output sharing number 7, a
// panel with two keys and a 4-wire with a virtual function.
func rrcsTreeAnswer(call codec.Call) (codec.Value, bool) {
	k := call.Params[0]
	st := codec.Struct
	mem := rrcsMember
	stream := func(proto int32, mcast string) codec.Value {
		return st(mem("Protocol", codec.Int(proto)), mem("Multicast", codec.String(mcast)), mem("MulticastPort", codec.Int(5004)),
			mem("Multicast2", codec.String("0.0.0.0")), mem("MulticastPort2", codec.Int(5004)), mem("SourceIp", codec.String("10.0.0.9")),
			mem("Channels", codec.Int(1)), mem("BitDepth", codec.Int(24)), mem("PacketTime", codec.Int(1000)), mem("PayloadType", codec.Int(97)))
	}
	port := func(n, p int32, in, out bool, typ, label, long string, id, keys int32) codec.Value {
		if typ == "Output (AES67)" {
			return st(mem("Net", codec.Int(1)), mem("Node", codec.Int(n)), mem("Port", codec.Int(p)),
				mem("Input", codec.Bool(in)), mem("Output", codec.Bool(out)), mem("PortType", codec.String(typ)),
				mem("Label", codec.String(label)), mem("LongName", codec.String(long)), mem("ObjectID", codec.Int(id)),
				mem("KeyCount", codec.Int(keys)), mem("PageCount", codec.Int(keys/16)), mem("PortAes67Output", stream(2, "239.1.2.3")))
		}
		if typ == "4-Wire (AES67)" {
			return st(mem("Net", codec.Int(1)), mem("Node", codec.Int(n)), mem("Port", codec.Int(p)),
				mem("Input", codec.Bool(in)), mem("Output", codec.Bool(out)), mem("PortType", codec.String(typ)),
				mem("Label", codec.String(label)), mem("LongName", codec.String(long)), mem("ObjectID", codec.Int(id)),
				mem("KeyCount", codec.Int(keys)), mem("PageCount", codec.Int(keys/16)),
				mem("PortAes67Input", stream(5, "0.0.0.0")), mem("PortAes67Output", stream(9, "0.0.0.0")))
		}
		return st(mem("Net", codec.Int(1)), mem("Node", codec.Int(n)), mem("Port", codec.Int(p)),
			mem("Input", codec.Bool(in)), mem("Output", codec.Bool(out)), mem("PortType", codec.String(typ)),
			mem("Label", codec.String(label)), mem("LongName", codec.String(long)), mem("ObjectID", codec.Int(id)),
			mem("KeyCount", codec.Int(keys)), mem("PageCount", codec.Int(keys/16)))
	}
	addr := func(n, p int32, in bool) codec.Value {
		return st(mem("IsInput", codec.Bool(in)), mem("Net", codec.Int(1)), mem("Node", codec.Int(n)), mem("Port", codec.Int(p)))
	}
	pos := func(p, key int32, typ string) codec.Value {
		m := []codec.Member{mem("ExpansionPanel", codec.Int(0)), mem("IsInput", codec.Bool(false)), mem("KeyNumber", codec.Int(key)),
			mem("Net", codec.Int(1)), mem("Node", codec.Int(61)), mem("Page", codec.Int(1)), mem("Port", codec.Int(p)),
			mem("PositionType", codec.String(typ))}
		if typ != "key" {
			m = append(m, mem("VirtualFunctionType", codec.String("always")))
		}
		return st(m...)
	}
	entry := func(position codec.Value, cmd ...codec.Member) codec.Value {
		return st(mem("CommandList", codec.Array(st(cmd...))), mem("CommandPosition", position))
	}
	switch call.Method {
	case "GetVersion":
		return codec.Array(k, codec.Int(0), codec.String("9.0.1")), true
	case "GetState":
		return codec.Array(k, codec.Int(0), codec.String("Working")), true
	case "GetAllNodes":
		return codec.Array(k, codec.Array(st(mem("NodeAddress", codec.Int(60)), mem("LongName", codec.String("FRAME A")),
			mem("NodeTypeString", codec.String("ARTIST_1024")), mem("ObjectID", codec.Int(9))))), true
	case "GetAllClientCards":
		return codec.Array(k, codec.Array(st(mem("Node", codec.Int(60)), mem("Bay", codec.Int(1)),
			mem("ClientCardTypeString", codec.String("AES67")), mem("LongName", codec.String("CARD 1")), mem("ObjectID", codec.Int(8))))), true
	case "GetAllPorts":
		return codec.Array(k, codec.Array(
			port(61, 1026, true, true, "RSP-1232HL", "PNL2", "PANEL-02", 100, 32),
			port(61, 7, false, true, "Output (AES67)", "O.-7", "Out seven", 101, 0),
			port(61, 7, true, false, "Input (AES67)", "I.-7", "In seven", 102, 0),
			port(61, 1041, true, true, "4-Wire (AES67)", "CODEC", "Codec IP", 103, 0),
		)), true
	case "GetAllCaps":
		return st(mem("ErrorCode", codec.Int(0)), mem("TransKey", k)), true
	case "GetAllConferences":
		return codec.Array(k, codec.Array(st(mem("ObjectID", codec.Int(300)), mem("Label", codec.String("Conf 003")),
			mem("LongName", codec.String("Conference 003")), mem("MemberList", codec.Array(st(mem("Node", codec.Int(61)),
				mem("Port", codec.Int(1041)), mem("Talk", codec.Bool(true)), mem("Listen", codec.Bool(true)))))))), true
	case "GetAllGroups":
		return codec.Array(k, codec.Array(st(mem("ObjectID", codec.Int(200)), mem("Label", codec.String("GRP ALF")),
			mem("LongName", codec.String("GROUP ALPHA")), mem("MemberList", codec.Array(
				st(mem("Node", codec.Int(61)), mem("Port", codec.Int(7))), st(mem("Node", codec.Int(99)), mem("Port", codec.Int(1)))))))), true
	case "GetAllIFBs":
		return codec.Array(k, codec.Array(st(mem("ObjectID", codec.Int(400)), mem("Label", codec.String("IFB A1")),
			mem("LongName", codec.String("IFB one")), mem("Input", addr(61, 7, true)), mem("Output", addr(61, 7, false)),
			mem("MixMinus", addr(0, 0, false))))), true
	case "GetAllActiveXps":
		return st(mem("ErrorCode", codec.Int(0)), mem("TransKey", k), mem("XP Count", codec.Int(2)),
			mem("XP#1", codec.Array(codec.Int(1), codec.Int(61), codec.Int(1026), codec.Int(1), codec.Int(61), codec.Int(7))),
			mem("XP#2", codec.Array(codec.Int(1), codec.Int(61), codec.Int(7), codec.Int(1), codec.Int(61), codec.Int(1026)))), true
	case "GetAllLogicSources_v2":
		return st(mem("ErrorCode", codec.Int(0)), mem("TransKey", k), mem("LogicSourceCount", codec.Int(1)),
			mem("LogicSource#1", codec.Array(codec.String("Studio On-Air"), codec.String("On-Air"), codec.Int(500), codec.Bool(true)))), true
	case "GetPortsCommandLists":
		switch call.Params[3].Int {
		case 1026:
			return st(mem("TransKey", k), mem("CommandLists", codec.Array(
				entry(pos(1026, 5, "key"), mem("CommandType", codec.String("call-to-group")), mem("Description", codec.String("Call to Group ")),
					mem("Group", codec.Int(200)), mem("GroupName", codec.String("GROUP ALPHA")), mem("ObjectID", codec.Int(601))),
				entry(pos(1026, 1, "key"), mem("CommandType", codec.String("call-to-port-cmd")), mem("Description", codec.String("Call to out seven")),
					mem("DestinationPortAddress", addr(61, 7, false)), mem("ObjectID", codec.Int(600))),
				entry(pos(1026, 9, "key"), mem("CommandType", codec.String("listen-to-port-cmd")), mem("Description", codec.String("Listen")),
					mem("TrunkingNetAddr", codec.Int(2)), mem("TrunkingPortAddr", codec.Int(109)), mem("ObjectID", codec.Int(603))),
				entry(pos(1026, 32, "key"), mem("CommandType", codec.String("reply-cmd")), mem("Description", codec.String("Reply")),
					mem("ObjectID", codec.Int(604))),
			))), true
		case 1041:
			return st(mem("TransKey", k), mem("CommandLists", codec.Array(
				entry(pos(1041, 0, "virtual-function"), mem("CommandType", codec.String("call-to-conference")), mem("Description", codec.String("Conf")),
					mem("Conference", codec.Int(300)), mem("ConferenceName", codec.String("Conference 003")), mem("ObjectID", codec.Int(602))),
			))), true
		}
		return st(mem("TransKey", k), mem("CommandLists", codec.Array())), true
	}
	return codec.Value{}, false
}

// rrcsRun runs a verb and returns its standard output.
func rrcsRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := rrcsStdout(t, func() error { return runRRCS(context.Background(), args) })
	if err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return out
}

func rrcsWant(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}

func TestRRCSTreeLive(t *testing.T) {
	f := newRRCSFake(t, rrcsTreeAnswer)
	out := rrcsRun(t, "tree", f.addr(), "--keys", "all")
	rrcsWant(t, out,
		"Version=9.0.1  State=Working",
		"net.1.node.60  ARTIST_1024  FRAME A  oid=9  (4 ports)",
		"  card.1  AES67  CARD 1  oid=8  (4 ports, addressed as node 61)",
		"    node.61.port.1026      in+out  RSP-1232HL                 PNL2      PANEL-02  oid=100",
		"port.7.in",
		"port.7.out",
		"key.0.1.1        call-to-port-cmd     net.1.node.61.port.7.out (O.-7)",
		"key.0.1.5        call-to-group        group.200 (GROUP ALPHA)",
		"key.0.1.9        listen-to-port-cmd   trunk.2.109",
		"vfunc.always.0   call-to-conference   conference.300 (Conference 003)",
		"conference  (1)",
		"net.1.node.61.port.1041            CODEC     talk+listen",
		"net.1.node.99.port.1.out",
		"net.1.node.61.port.7.in            I.-7      input",
		"logic.500",
	)
	if strings.Contains(out, "mixminus") {
		t.Errorf("an unassigned IFB leg was printed:\n%s", out)
	}
	// Keys come in key order, not in the order RRCS sent them.
	if strings.Index(out, "key.0.1.1 ") > strings.Index(out, "key.0.1.5 ") {
		t.Errorf("keys are not sorted:\n%s", out)
	}
	// One command request per port, in the direction the port has.
	asked := 0
	for _, m := range f.methods() {
		if m == "GetPortsCommandLists" {
			asked++
		}
		if m == "GetObjectList" || m == "GetLicenseInfo" || m == "GetObjectProperty" {
			t.Errorf("tree sent %s", m)
		}
	}
	if asked != 4 {
		t.Errorf("GetPortsCommandLists sent %d times, want 4", asked)
	}

	noKeys := rrcsRun(t, "tree", f.addr(), "--keys", "no", "--type", "rsp")
	if strings.Contains(noKeys, "key.0.1.1") || strings.Contains(noKeys, "port.7") || strings.Contains(noKeys, "conference") {
		t.Errorf("--keys no --type rsp:\n%s", noKeys)
	}
}

func TestRRCSListAndGetFromSnapshot(t *testing.T) {
	f := newRRCSFake(t, rrcsTreeAnswer)
	snap := filepath.Join(t.TempDir(), "walk.json")
	rrcsRun(t, "walk", f.addr(), "--out", snap)
	sent := len(f.methods())

	rrcsWant(t, rrcsRun(t, "list", "nodes", "--from", snap), "net.1.node.60  ARTIST_1024  0", "net.1.node.61", "2 nodes")
	rrcsWant(t, rrcsRun(t, "list", "cards", "--from", snap), "net.1.node.60.card.1  AES67  8", "1 cards")
	rrcsWant(t, rrcsRun(t, "list", "ports", "--from", snap), "net.1.node.61.port.7.in", "in+out  RSP-1232HL", "4 ports")
	rrcsWant(t, rrcsRun(t, "list", "ports", "--from", snap, "--type", "aes67", "--match", "seven"), "2 ports")
	rrcsWant(t, rrcsRun(t, "list", "ports", "--from", snap, "--node", "62"), "0 ports")
	rrcsWant(t, rrcsRun(t, "list", "panels", "--from", snap), "PNL2", "1 panels")
	rrcsWant(t, rrcsRun(t, "list", "keys", "--from", snap), "5 keys", "net.1.node.61.port.1041.vfunc.always.0")
	rrcsWant(t, rrcsRun(t, "list", "keys", "--from", snap, "--match", "group"), "1 keys")
	rrcsWant(t, rrcsRun(t, "list", "keys", "--from", snap, "--type", "4-wire"), "1 keys")
	rrcsWant(t, rrcsRun(t, "list", "streams", "--from", snap), "3 streams",
		"net.1.node.61.port.7.out  out          101  sender    O.-7   Out seven  Manual  1   0    239.1.2.3:5004",
		"net.1.node.61.port.1041   in   -7.18   103  receiver  CODEC  Codec IP   NMOS    1   0    0.0.0.0:5004    0.0.0.0:5004  10.0.0.9  24    1000   97",
		"sender    CODEC  Codec IP   9 ")
	// The same rows as CSV, with the columns named as in the JSON.
	rrcsWant(t, rrcsRun(t, "list", "streams", "--from", snap, "--output", "csv"),
		"path,dir,number,oid,role,label,long_name,mode,ch,sel,multicast,multicast_2,source,bits,ptime,pt",
		"net.1.node.61.port.1041,in,-7.18,103,receiver,CODEC,Codec IP,NMOS,1,0,0.0.0.0:5004,0.0.0.0:5004,10.0.0.9,24,1000,97")
	rrcsWant(t, rrcsRun(t, "list", "streams", "--from", snap, "--type", "output"), "1 streams")
	rrcsWant(t, rrcsRun(t, "list", "sources", "--from", snap), "1  net.1.node.61.port.7.in", "3 sources")
	rrcsWant(t, rrcsRun(t, "list", "dests", "--from", snap), "net.1.node.61.port.7.out", "3 dests")
	rrcsWant(t, rrcsRun(t, "list", "xp", "--from", snap),
		"net.1.node.61.port.7.in  I.-7       net.1.node.61.port.1026   PNL2",
		"net.1.node.61.port.1026  PNL2       net.1.node.61.port.7.out  O.-7",
		"2 active crosspoints, of 3 sources x 3 destinations")
	rrcsWant(t, rrcsRun(t, "list", "xp", "--from", snap, "--match", "O.-7"), "1 active crosspoints")
	if got := strings.TrimSpace(rrcsRun(t, "list", "xp", "--from", snap, "--node", "99", "--output", "json")); got != "[]" {
		t.Errorf("no crosspoint as JSON: %s", got)
	}
	rrcsWant(t, rrcsRun(t, "list", "conferences", "--from", snap), "conference.300", "net.1.node.61.port.1041 (CODEC) talk+listen")
	rrcsWant(t, rrcsRun(t, "list", "groups", "--from", snap), "group.200", "net.1.node.61.port.7.out (O.-7), net.1.node.99.port.1.out")
	rrcsWant(t, rrcsRun(t, "list", "ifbs", "--from", snap), "ifb.400", "input", "output")
	rrcsWant(t, rrcsRun(t, "list", "logic", "--from", snap), "logic.500", "on", "1 logic")

	var keys []map[string]any
	if err := json.Unmarshal([]byte(rrcsRun(t, "list", "keys", "--from", snap, "--output", "json")), &keys); err != nil || len(keys) != 5 {
		t.Fatalf("keys as JSON: %v, %d rows", err, len(keys))
	}
	if keys[0]["path"] != "net.1.node.61.port.1026.key.0.1.1" || keys[0]["target"] != "net.1.node.61.port.7.out" {
		t.Errorf("first key: %v", keys[0])
	}
	if got := strings.TrimSpace(rrcsRun(t, "list", "ports", "--from", snap, "--node", "62", "--output", "json")); got != "[]" {
		t.Errorf("empty list as JSON: %s", got)
	}
	var tree map[string]any
	if err := json.Unmarshal([]byte(rrcsRun(t, "tree", "--from", snap, "--output", "json")), &tree); err != nil || tree["target"] == "" {
		t.Errorf("tree as JSON: %v", err)
	}

	rrcsWant(t, rrcsRun(t, "get", "--from", snap, "--path", "net.1.node.61.port.1026"), `LongName                     "PANEL-02"`, "KeyCount                     32")
	rrcsWant(t, rrcsRun(t, "get", "--from", snap, "--path", "net.1.node.61.port.1026.key.0.1.5"), `GroupName                    "GROUP ALPHA"`)
	rrcsWant(t, rrcsRun(t, "get", "--from", snap, "--path", "group.200", "--prop", "Label", "--output", "json"), `{"Label":"GRP ALF"}`)
	rrcsWant(t, rrcsRun(t, "get", "--from", snap, "--path", "net.1.node.60"), `"FRAME A"`)
	rrcsWant(t, rrcsRun(t, "get", "--from", snap, "--path", "net.1.node.61"), "Ports                        4")
	rrcsWant(t, rrcsRun(t, "get", "--from", snap, "--path", "net.1.node.60.card.1"), `"CARD 1"`)
	rrcsWant(t, rrcsRun(t, "get", "--from", snap, "--path", "logic.500"), "State                        true")

	if len(f.methods()) != sent {
		t.Errorf("reading a snapshot sent %d requests to the gateway", len(f.methods())-sent)
	}
}

// get --path on a live gateway asks the keys of that one port only.
func TestRRCSGetPathLive(t *testing.T) {
	f := newRRCSFake(t, rrcsTreeAnswer)
	rrcsWant(t, rrcsRun(t, "get", f.addr(), "--path", "net.1.node.61.port.1026.key.0.1.1"), `"call-to-port-cmd"`)
	asked := 0
	for _, m := range f.methods() {
		if m == "GetPortsCommandLists" {
			asked++
		}
	}
	if asked != 1 {
		t.Errorf("GetPortsCommandLists sent %d times, want 1", asked)
	}
	before := len(f.methods())
	rrcsWant(t, rrcsRun(t, "get", f.addr(), "--path", "net.1.node.61.port.7.in"), `"In seven"`)
	for _, m := range f.methods()[before:] {
		if m == "GetPortsCommandLists" {
			t.Error("a port path asked for the keys")
		}
	}
}

func TestRRCSListRefusals(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	notRRCS := filepath.Join(dir, "other.json")
	_ = os.WriteFile(notRRCS, []byte(`{"proto":"acp1"}`), 0o644)
	broken := filepath.Join(dir, "broken.json")
	_ = os.WriteFile(broken, []byte(`{`), 0o644)
	good := filepath.Join(dir, "good.json")
	_ = os.WriteFile(good, []byte(`{"proto":"rrcs","target":"x"}`), 0o644)

	var val *consumer.ValidationError
	for name, args := range map[string][]string{
		"no kind":            {"list"},
		"unknown kind":       {"list", "widgets", "--from", good},
		"no source":          {"list", "ports"},
		"host and from":      {"list", "ports", "h", "--from", good},
		"bad output":         {"list", "ports", "--from", good, "--output", "xml"},
		"tree bad keys":      {"tree", "--from", good, "--keys", "maybe"},
		"get id and path":    {"get", "h", "--id", "1", "--path", "group.1"},
		"get id from file":   {"get", "--id", "1", "--from", good},
		"get neither":        {"get", "h"},
		"get path two hosts": {"get", "a", "b", "--path", "group.1"},
	} {
		if err := runRRCS(ctx, args); !errors.As(err, &val) {
			t.Errorf("%s: got %v, want a validation error", name, err)
		}
	}
	for name, args := range map[string][]string{
		"missing file":  {"list", "ports", "--from", filepath.Join(dir, "none.json")},
		"other proto":   {"list", "ports", "--from", notRRCS},
		"broken file":   {"tree", "--from", broken},
		"unknown path":  {"get", "--from", good, "--path", "group.1"},
		"unknown prop":  {"get", "--from", good, "--path", "group.1", "--prop", "x"},
		"dead gateway":  {"list", "ports", "127.0.0.1:1", "--timeout", "300ms"},
		"dead for tree": {"tree", "127.0.0.1:1", "--timeout", "300ms"},
	} {
		if err := runRRCS(ctx, args); err == nil || errors.As(err, &val) {
			t.Errorf("%s: got %v, want a runtime error", name, err)
		}
	}
	// An empty snapshot is an empty system, not an error.
	rrcsWant(t, rrcsRun(t, "list", "keys", "--from", good), "0 keys")
	rrcsWant(t, rrcsRun(t, "tree", "--from", good), "rrcs x")
}

func TestRRCSPortOfPath(t *testing.T) {
	node, port, in, ok := rrcsPortOfPath("net.1.node.61.port.7.in")
	if !ok || node != 61 || port != 7 || !in {
		t.Errorf("got %d %d %v %v", node, port, in, ok)
	}
	if _, _, in, ok := rrcsPortOfPath("net.1.node.61.port.1026.key.0.1.1"); !ok || in {
		t.Errorf("key path: in=%v ok=%v", in, ok)
	}
	for _, bad := range []string{"group.200", "net.1.node.61", "net.1.node.x.port.7", "net.1.node.61.port.y", "a.b.c.d.e.f"} {
		if _, _, _, ok := rrcsPortOfPath(bad); ok {
			t.Errorf("%q read as a port path", bad)
		}
	}
}

// A read verb sends the requests its answer is made of, and no other.
func TestRRCSAtomicReads(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"list conferences": {[]string{"list", "conferences"}, "GetAllConferences"},
		"list groups":      {[]string{"list", "groups"}, "GetAllGroups"},
		"list ifbs":        {[]string{"list", "ifbs"}, "GetAllIFBs"},
		"list cards":       {[]string{"list", "cards"}, "GetAllClientCards"},
		"list ports":       {[]string{"list", "ports"}, "GetAllPorts"},
		"list streams":     {[]string{"list", "streams"}, "GetAllPorts"},
		"list xp":          {[]string{"list", "xp"}, "GetAllActiveXps,GetAllPorts"},
		// The panel is the one port with keys: one request for its keys.
		"list keys":      {[]string{"list", "keys"}, "GetAllPorts,GetAllCaps,GetPortsCommandLists"},
		"get conference": {[]string{"get", "--path", "conference.300"}, "GetAllConferences"},
		"tree":           {[]string{"tree"}, "GetVersion,GetState,IsConnectedToArtist,GetConfigurationID,GetAllNodes,GetAllClientCards,GetAllDevices,GetAllPorts,GetAllConferences,GetAllGroups,GetAllIFBs,GetAllLogicSources_v2,GetAllGpIns,GetAllGpOuts,GetAllActiveXps,GetAllCaps,GetPortsCommandLists"},
	} {
		f := newRRCSFake(t, rrcsTreeAnswer)
		args := append(append([]string{}, tc.args[:1]...), tc.args[1:]...)
		if tc.args[0] == "list" {
			args = []string{"list", tc.args[1], f.addr()}
		} else {
			args = append(args, f.addr())
		}
		rrcsRun(t, args...)
		if got := strings.Join(f.methods(), ","); got != tc.want {
			t.Errorf("%s sent %s, want %s", name, got, tc.want)
		}
	}
	// One port, one request.
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "GetPort" {
			return codec.Array(call.Params[0], codec.Int(0), codec.Struct(rrcsMember("LongName", codec.String("In seven")))), true
		}
		return rrcsTreeAnswer(call)
	})
	rrcsWant(t, rrcsRun(t, "get", f.addr(), "--path", "net.1.node.61.port.7.in"), `LongName                     "In seven"`)
	if got := strings.Join(f.methods(), ","); got != "GetPort" {
		t.Errorf("get of a port sent %s", got)
	}
}
