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

func rrcsMember(name string, v codec.Value) codec.Member { return codec.Member{Name: name, Value: v} }

// rrcsWalkAnswer is a small system: one node, two ports (a panel with
// both directions, an output), one conference that is also listed as a
// group so the property request must not be repeated.
func rrcsWalkAnswer(call codec.Call) (codec.Value, bool) {
	k := call.Params[0]
	switch call.Method {
	case "GetVersion":
		return codec.Array(k, codec.Int(0), codec.String("9.0.1")), true
	case "GetAllNodes":
		return codec.Array(k, codec.Array(codec.Struct(rrcsMember("NodeAddress", codec.Int(60))))), true
	case "GetAllPorts":
		panel := codec.Struct(rrcsMember("Net", codec.Int(1)), rrcsMember("Node", codec.Int(61)), rrcsMember("Port", codec.Int(1040)),
			rrcsMember("Input", codec.Bool(true)), rrcsMember("Output", codec.Bool(true)))
		out := codec.Struct(rrcsMember("Net", codec.Int(1)), rrcsMember("Node", codec.Int(62)), rrcsMember("Port", codec.Int(7)),
			rrcsMember("Input", codec.Bool(false)), rrcsMember("Output", codec.Bool(true)))
		return codec.Array(k, codec.Array(panel, out)), true
	case "GetLicenseInfo":
		return codec.Array(k, codec.Struct(rrcsMember("PortsLicensed", codec.Int(1024)))), true
	case "GetObjectList":
		switch call.Params[1].Str {
		case "conference", "group":
			return codec.Struct(rrcsMember("TransKey", k), rrcsMember("ObjectList", codec.Array(
				codec.Struct(rrcsMember("ObjectID", codec.Int(77)), rrcsMember("LongName", codec.String("Conf 001")))))), true
		case "user":
			return codec.Struct(rrcsMember("TransKey", k), rrcsMember("ObjectList", codec.Array(
				codec.Struct(rrcsMember("ObjectID", codec.Int(-5)), rrcsMember("LongName", codec.String("ghost")))))), true
		case "ifb":
			return codec.Struct(rrcsMember("TransKey", k), rrcsMember("ObjectList", codec.Array())), true
		}
		return codec.Value{}, false
	case "GetObjectProperty":
		if call.Params[1].Int == -5 {
			return codec.Array(k, codec.Int(22)), true // object does not exist
		}
		if call.Params[2].Str == "Label" {
			return codec.Struct(rrcsMember("TransKey", k), rrcsMember("Label", codec.String("Conf 001"))), true
		}
		return codec.Struct(rrcsMember("TransKey", k), rrcsMember("Label", codec.String("Conf 001")), rrcsMember("Owner", codec.Int(3))), true
	case "GetObjectPropertyNames":
		return codec.Struct(rrcsMember("TransKey", k), rrcsMember("PropertyNames", codec.Array(codec.String("Label"), codec.String("Owner")))), true
	case "GetAllCaps":
		return codec.Struct(rrcsMember("ErrorCode", codec.Int(0)), rrcsMember("TransKey", k), rrcsMember("port count", codec.Int(2)),
			rrcsMember("port#1", codec.Array(codec.Int(1), codec.Int(61), codec.Int(1040), codec.Int(-1))),
			rrcsMember("port#2", codec.Array(codec.Int(1), codec.Int(62), codec.Int(7), codec.Int(2)))), true
	case "GetPortsCommandLists":
		return codec.Struct(rrcsMember("TransKey", k), rrcsMember("CommandLists", codec.Array())), true
	}
	return codec.Value{}, false
}

func TestRRCSWalk(t *testing.T) {
	f := newRRCSFake(t, rrcsWalkAnswer)
	dir := t.TempDir()
	out := filepath.Join(dir, "deep", "walk.json")
	capture := filepath.Join(dir, "walk.jsonl")
	text, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"walk", f.addr(), "--out", out, "--capture", capture})
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var snap rrcsWalkSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !snap.Complete || snap.Proto != "rrcs" || len(snap.Status) != len(rrcsInfoMethods) ||
		len(snap.Lists) != len(rrcsDiscoverMethods)+len(rrcsWalkLists)-2 || len(snap.ObjectLists) != len(rrcsObjectTypes) {
		t.Errorf("snapshot shape: %+v", snap)
	}
	if len(snap.Licenses) != 1 || snap.Licenses[0].Args[0] != float64(60) {
		t.Errorf("licences: %+v", snap.Licenses)
	}
	// One request per port and pool port: the panel has no pool port
	// (-1), the output has two (0 and 1).
	if len(snap.Commands) != 3 {
		t.Fatalf("command lists: %d, want 3", len(snap.Commands))
	}
	if got := snap.Commands[0].Args; len(got) != 5 || got[1] != float64(61) || got[2] != float64(1040) || got[3] != false || got[4] != float64(-1) {
		t.Errorf("first command request: %v", got)
	}
	if a, b := snap.Commands[1].Args, snap.Commands[2].Args; a[4] != float64(0) || b[4] != float64(1) || a[3] != false {
		t.Errorf("pool port requests: %v %v", a, b)
	}
	conf := snap.Objects["conference"]
	if len(conf) != 1 || conf[0].ObjectID != 77 || conf[0].LongName != "Conf 001" || conf[0].Properties == nil {
		t.Errorf("conference: %+v", conf)
	}
	if ghost := snap.Objects["user"]; len(ghost) != 1 || !strings.Contains(ghost[0].Error, "code 22") {
		t.Errorf("user: %+v", ghost)
	}
	if _, ok := snap.Objects["ifb"]; !ok {
		t.Error("an empty object list was left out")
	}
	// Object 77 is listed twice and asked once.
	asked := 0
	for _, m := range f.methods() {
		if m == "GetObjectProperty" {
			asked++
		}
	}
	if asked != 2 {
		t.Errorf("GetObjectProperty sent %d times, want 2", asked)
	}
	if snap.Requests != len(f.methods()) || snap.Failed == 0 {
		t.Errorf("counts: %d requests (%d sent), %d failed", snap.Requests, len(f.methods()), snap.Failed)
	}
	for _, want := range []string{"conference           1", "command lists        3", "snapshot"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary lacks %q:\n%s", want, text)
		}
	}
	if trace, _ := os.ReadFile(capture); !strings.Contains(string(trace), `"note":"rrcs walk: objects conference`) {
		t.Error("the capture lacks the notes of the walk")
	}
}

func TestRRCSWalkSkipAndJSON(t *testing.T) {
	f := newRRCSFake(t, rrcsWalkAnswer)
	out := filepath.Join(t.TempDir(), "walk.json")
	text, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"walk", f.addr(), "--out", out, "--skip", "both", "--output", "json"})
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	var snap rrcsWalkSnapshot
	if err := json.Unmarshal([]byte(text), &snap); err != nil {
		t.Fatalf("stdout is not the snapshot: %v", err)
	}
	if len(snap.Commands) != 0 || snap.Objects["conference"][0].Properties != nil {
		t.Errorf("skipped parts were walked: %+v", snap)
	}
	for _, m := range f.methods() {
		if m == "GetObjectProperty" || m == "GetPortsCommandLists" {
			t.Errorf("%s sent with --skip both", m)
		}
	}
}

func TestRRCSWalkInterruptedAndRefusals(t *testing.T) {
	f := newRRCSFake(t, rrcsWalkAnswer)
	out := filepath.Join(t.TempDir(), "walk.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A cancelled context: nothing answers, the walk says so.
	if err := runRRCS(ctx, []string{"walk", f.addr(), "--out", out}); err == nil {
		t.Error("no error")
	}
	var val *consumer.ValidationError
	for name, args := range map[string][]string{
		"walk no host":  {"walk"},
		"walk bad skip": {"walk", "h", "--skip", "everything"},
		"get no host":   {"get", "--id", "1"},
		"get no id":     {"get", "h"},
		"get bad names": {"get", "h", "--id", "1", "--names", "maybe"},
	} {
		if err := runRRCS(context.Background(), args); !errors.As(err, &val) {
			t.Errorf("%s: got %v, want a validation error", name, err)
		}
	}
}

func TestRRCSGet(t *testing.T) {
	f := newRRCSFake(t, rrcsWalkAnswer)
	ctx := context.Background()
	all, err := rrcsStdout(t, func() error { return runRRCS(ctx, []string{"get", f.addr(), "--id", "77"}) })
	if err != nil || !strings.Contains(all, `Label                        "Conf 001"`) || !strings.Contains(all, "Owner                        3") {
		t.Errorf("all: %v\n%s", err, all)
	}
	one, err := rrcsStdout(t, func() error {
		return runRRCS(ctx, []string{"get", f.addr(), "--id", "77", "--prop", "Label", "--output", "json"})
	})
	if err != nil || strings.TrimSpace(one) != `{"Label":"Conf 001"}` {
		t.Errorf("one: %v\n%s", err, one)
	}
	names, err := rrcsStdout(t, func() error { return runRRCS(ctx, []string{"get", f.addr(), "--id", "77", "--names", "yes"}) })
	if err != nil || !strings.Contains(names, `["Label","Owner"]`) {
		t.Errorf("names: %v\n%s", err, names)
	}
	if err := runRRCS(ctx, []string{"get", f.addr(), "--id", "-5"}); !errors.Is(err, &codec.CodeError{Code: codec.ErrorCode(22)}) {
		t.Errorf("missing object: %v", err)
	}
}

// walk reads label, alias and gains per port; a port that is not online
// is counted apart and is not a failure.
func TestRRCSWalkPortValues(t *testing.T) {
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		k := call.Params[0]
		switch call.Method {
		case "GetPortLabel": // node, port, input
			return codec.Array(k, codec.Int(0), codec.String("LBL")), true
		case "GetPortAlias": // net, node, port, input
			if call.Params[3].Int == 1026 {
				return codec.Array(k, codec.Int(0), codec.String("PNL TWO")), true
			}
			return codec.Array(k, codec.Int(0), codec.String("")), true
		case "GetInputGain":
			if call.Params[3].Int == 1041 {
				return codec.Array(k, codec.Int(24)), true // port is not online
			}
			return codec.Array(k, codec.Int(0), codec.Int(-12)), true
		case "GetOutputGain":
			if call.Params[3].Int == 1041 {
				return codec.Array(k, codec.Int(24)), true
			}
			return codec.Array(k, codec.Int(0), codec.Int(-128)), true
		case "GetAllKeyConfiguration":
			// Node, Port, IsInput, PoolPort: a net in front is refused.
			if len(call.Params) != 5 || call.Params[3].Kind != codec.KindBool || call.Params[1].Int != 61 || call.Params[4].Int != -1 {
				return codec.Value{}, false
			}
			return codec.Array(k, codec.Array()), true
		case "GetTrunklineSetup", "GetTrunklineActivities":
			t.Errorf("%s asked on a system without trunk ports", call.Method)
			return codec.Array(k, codec.Int(99)), true
		}
		return rrcsTreeAnswer(call)
	})
	dir := t.TempDir()
	snapFile := filepath.Join(dir, "walk.json")
	text := rrcsRun(t, "walk", f.addr(), "--out", snapFile, "--skip", "properties,commands")
	rrcsWant(t, text, "port values          14", "port not online      2")
	raw, _ := os.ReadFile(snapFile)
	var snap rrcsWalkSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	// Four ports: label and alias each, an input gain for three, an
	// output gain for three.
	if len(snap.PortValues) != 14 {
		t.Errorf("port value requests: %d", len(snap.PortValues))
	}
	if snap.NotOnline != 2 {
		t.Errorf("not online: %d", snap.NotOnline)
	}
	if len(snap.KeyConfigs) != 1 || snap.KeyConfigs[0].Error != "" {
		t.Errorf("key configurations: %+v", snap.KeyConfigs)
	}
	for _, c := range snap.PortValues {
		if strings.Contains(c.Error, "code 24") {
			continue
		}
		if c.Error != "" {
			t.Errorf("%s %v: %s", c.Method, c.Args, c.Error)
		}
	}
	ports := rrcsRun(t, "list", "ports", "--from", snapFile)
	rrcsWant(t, ports, "ALIAS    GAIN IN  GAIN OUT", "PANEL-02   PNL TWO  -6.0     mute")
	rrcsWant(t, rrcsRun(t, "get", "--from", snapFile, "--path", "net.1.node.61.port.1026", "--prop", "InputGain"), "InputGain                    -12")
	out := rrcsRun(t, "export", "--from", snapFile, "--format", "csv", "--path", "port.1026.InputGain,port.1026.Alias")
	rrcsWant(t, out, "net.1.node.61.port.1026.Alias,100,Alias,string,R--,PNL TWO", "InputGain,int,R--,-12,,0.5 dB,-36,36")

	// --skip values leaves them out; an unknown part is refused.
	rrcsRun(t, "walk", f.addr(), "--out", snapFile, "--skip", "properties, commands ,values")
	raw, _ = os.ReadFile(snapFile)
	snap = rrcsWalkSnapshot{}
	_ = json.Unmarshal(raw, &snap)
	if len(snap.PortValues) != 0 {
		t.Errorf("--skip values still asked %d", len(snap.PortValues))
	}
	var val *consumer.ValidationError
	if err := runRRCS(context.Background(), []string{"walk", "h", "--skip", "values,everything"}); !errors.As(err, &val) {
		t.Errorf("bad --skip: %v", err)
	}
}

// A panel answers "port address invalid" to a gain request: counted
// apart, not a failure. And the trunk line requests are asked where
// there are trunk ports.
func TestRRCSWalkNoGainAndTrunks(t *testing.T) {
	asked := map[string]int{}
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		k := call.Params[0]
		asked[call.Method]++
		switch call.Method {
		case "GetInputGain", "GetOutputGain":
			return codec.Array(k, codec.Int(4)), true
		case "GetPortLabel", "GetPortAlias":
			return codec.Array(k, codec.Int(0), codec.String("")), true
		case "GetAllKeyConfiguration":
			return codec.Array(k, codec.Array()), true
		case "GetTrunkPorts":
			return codec.Array(k, codec.Array(codec.Struct(rrcsMember("Port", codec.Int(1))))), true
		case "GetTrunklineSetup", "GetTrunklineActivities":
			return codec.Array(k, codec.Array()), true
		}
		return rrcsTreeAnswer(call)
	})
	file := filepath.Join(t.TempDir(), "walk.json")
	rrcsWant(t, rrcsRun(t, "walk", f.addr(), "--out", file, "--skip", "properties,commands"), "port without gain    6")
	raw, _ := os.ReadFile(file)
	var snap rrcsWalkSnapshot
	_ = json.Unmarshal(raw, &snap)
	if snap.NoGain != 6 || snap.NotOnline != 0 {
		t.Errorf("no gain %d, not online %d", snap.NoGain, snap.NotOnline)
	}
	if asked["GetTrunklineSetup"] != 1 || asked["GetTrunklineActivities"] != 1 {
		t.Errorf("trunk line requests: %v", asked)
	}
}
