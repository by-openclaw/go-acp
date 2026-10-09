package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"dhs/internal/rrcs/codec"
)

// The stand-in panel 61.1026 holds: key 0.1.1 call to port 61.7.out,
// key 0.1.5 call to group 200, key 0.1.9 a trunk listen, key 0.1.32
// reply. Conference 300 and group 200 (members 61.7 and 99.1) exist.
const rrcsEnsureCfgDesired = `{
  "keys": [
    {"panel": "net.1.node.61.port.1026", "key": "0.1.2", "function": "call-to-port", "target": "net.1.node.61.port.1041"},
    {"panel": "net.1.node.61.port.1026", "key": "0.1.1", "function": "call-to-conference", "target": "conference.300"},
    {"panel": "net.1.node.61.port.1026", "key": "0.1.5", "state": "absent"},
    {"panel": "net.1.node.61.port.1026", "key": "0.1.32", "function": "reply"}
  ],
  "conferences": [{"name": "Conference 040", "label": "CONF 40"}, {"id": 300, "label": "C3"}],
  "groups": [{"id": 200, "members": ["net.1.node.61.port.7.out", "net.1.node.61.port.1041"]},
             {"name": "GROUP BETA"}]
}`

// rrcsCfgCalls runs ensure on the stand-in and returns every
// configuration change it received, as "method changeType objectType".
func rrcsCfgRun(t *testing.T, desired string, extra ...string) (out string, changes []codec.Value, err error) {
	t.Helper()
	var mu sync.Mutex
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if strings.HasPrefix(call.Method, "ConfigurationChange") {
			mu.Lock()
			changes = append(changes, codec.Struct(rrcsMember("Method", codec.String(call.Method)), rrcsMember("Change", call.Params[1].Items[0])))
			mu.Unlock()
			return call.Params[0], true
		}
		return rrcsTreeAnswer(call)
	})
	file := filepath.Join(t.TempDir(), "desired.json")
	if werr := os.WriteFile(file, []byte(desired), 0o600); werr != nil {
		t.Fatal(werr)
	}
	args := append([]string{"ensure", f.addr(), "--file", file, "--output", "json"}, extra...)
	for i, a := range args {
		if a == "HOST" {
			args[i] = f.addr()
		}
	}
	out, err = rrcsStdout(t, func() error { return runRRCS(context.Background(), args) })
	mu.Lock()
	defer mu.Unlock()
	return out, changes, err
}

func TestRRCSEnsureConfigCheck(t *testing.T) {
	out, changes, err := rrcsCfgRun(t, rrcsEnsureCfgDesired, "--check")
	if err != nil {
		t.Fatalf("ensure --check: %v\n%s", err, out)
	}
	if len(changes) != 0 {
		t.Fatalf("--check sent %d change(s)", len(changes))
	}
	var doc struct {
		WouldChange bool `json:"would_change"`
		Diff        []struct {
			Field string `json:"field"`
			From  string `json:"from"`
			To    string `json:"to"`
		} `json:"diff"`
		Failed []rrcsEnsureFailure `json:"failed"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got := map[string]string{}
	for _, d := range doc.Diff {
		got[d.Field+" | "+d.From+" -> "+d.To] = ""
	}
	for _, want := range []string{
		"net.1.node.61.port.1026.key.0.1.2.function |  -> call-to-port-cmd net.1.node.61.port.1041",
		"net.1.node.61.port.1026.key.0.1.1.function | call-to-port-cmd net.1.node.61.port.7.out -> ",
		"net.1.node.61.port.1026.key.0.1.1.function |  -> call-to-conference conference.300",
		"net.1.node.61.port.1026.key.0.1.5.function | call-to-group group.200 -> ",
		"conference.Conference 040 |  -> Conference 040",
		"conference.300 | label Conf 003 -> label C3",
		"group.200 | , member net.1.node.99.port.1.out -> member net.1.node.61.port.1041,",
		"group.GROUP BETA |  -> GROUP BETA",
	} {
		if _, ok := got[want]; !ok {
			t.Errorf("diff lacks %q", want)
		}
	}
	// The reply key is already there: nothing for it.
	for k := range got {
		if strings.Contains(k, "key.0.1.32") {
			t.Errorf("a key that is right was planned: %s", k)
		}
	}
	if !doc.WouldChange || len(doc.Failed) != 0 || t.Failed() {
		t.Errorf("would_change %v failed %v\n%s", doc.WouldChange, doc.Failed, out)
	}
}

// The requests, as the specification prints them (§8.10.1, §8.10.4.12,
// §8.10.4.8, §8.10.4.33, §6.6), and always with ConfigurationChange.
func TestRRCSEnsureConfigRequests(t *testing.T) {
	desired := `{"keys": [
	  {"panel": "net.1.node.61.port.1026", "key": "0.1.2", "function": "call-to-port", "target": "net.1.node.61.port.1041", "label": "CODEC", "mode": "latching"},
	  {"panel": "net.1.node.61.port.1026", "key": "2.1.3", "function": "call-to-conference", "target": "conference.300"},
	  {"panel": "net.1.node.61.port.1026", "key": "0.1.5", "state": "absent"}]}`
	// The stand-in takes nothing: the run must end with not_taken.
	_, changes, err := rrcsCfgRun(t, desired, "--write-to", "HOST")
	if err == nil || !strings.Contains(err.Error(), "could not be brought") {
		t.Errorf("a configuration that did not change passed: %v", err)
	}
	if len(changes) < 4 {
		t.Fatalf("%d changes sent, want at least 4", len(changes))
	}
	type sent struct {
		method, change, object string
		params                 codec.Value
	}
	var all []sent
	for _, c := range changes {
		m, _ := c.Field("Method")
		ch, _ := c.Field("Change")
		ct, _ := ch.Field("ChangeType")
		ot, _ := ch.Field("ObjectType")
		sp, _ := ch.Field("SpecificParams")
		all = append(all, sent{m.Str, ct.Str, ot.Str, sp})
		if m.Str != "ConfigurationChange" {
			t.Errorf("sent with %s", m.Str)
		}
	}
	find := func(change, object string) *sent {
		for i := range all {
			if all[i].change == change && all[i].object == object {
				return &all[i]
			}
		}
		t.Fatalf("no %s %s among %d changes", change, object, len(all))
		return nil
	}
	create := find("create", "call-to-port-cmd")
	pos, _ := create.params.Field("CommandPosition")
	dst, _ := create.params.Field("DestinationPortAddress")
	if typ, _ := pos.Field("PositionType"); typ.Str != "key" || rrcsMemberInt(pos, "Node") != 61 || rrcsMemberInt(pos, "Port") != 1026 ||
		rrcsMemberInt(pos, "Page") != 1 || rrcsMemberInt(pos, "KeyNumber") != 2 || rrcsMemberInt(dst, "Port") != 1041 {
		t.Errorf("create call-to-port: %s", rrcsCompact(create.params))
	}
	if _, has := pos.Field("ExpansionPanel"); has {
		t.Errorf("ExpansionPanel sent for the main panel: %s", rrcsCompact(pos))
	}
	conf := find("create", "call-to-conference")
	cpos, _ := conf.params.Field("CommandPosition")
	if rrcsMemberInt(conf.params, "Conference") != 300 || rrcsMemberInt(cpos, "ExpansionPanel") != 2 || rrcsMemberInt(cpos, "KeyNumber") != 3 {
		t.Errorf("create call-to-conference: %s", rrcsCompact(conf.params))
	}
	del := find("delete", "call-to-group")
	if rrcsMemberInt(del.params, "Group") != 200 {
		t.Errorf("delete call-to-group: %s", rrcsCompact(del.params))
	}
	key := find("edit", "panel-key")
	np, _ := key.params.Field("NewProperties")
	label, _ := np.Field("LabelValue")
	if rrcsMemberInt(key.params, "KeyNumber") != 2 || label.Str != "CODEC" || rrcsMemberInt(np, "KeyMode") != 2 || rrcsFieldBool(np, "AutoLabelFlag") {
		t.Errorf("edit panel-key: %s", rrcsCompact(key.params))
	}
}

func TestRRCSEnsureConfigRefusals(t *testing.T) {
	for name, desired := range map[string]string{
		"no such panel":    `{"keys": [{"panel": "net.1.node.61.port.4000", "key": "0.1.1", "function": "reply"}]}`,
		"bad key":          `{"keys": [{"panel": "net.1.node.61.port.1026", "key": "14", "function": "reply"}]}`,
		"bad function":     `{"keys": [{"panel": "net.1.node.61.port.1026", "key": "0.1.2", "function": "sing"}]}`,
		"bad target":       `{"keys": [{"panel": "net.1.node.61.port.1026", "key": "0.1.2", "function": "call-to-group", "target": "net.1.node.61.port.7"}]}`,
		"unknown id":       `{"groups": [{"id": 999, "label": "X"}]}`,
		"trunk on the key": `{"keys": [{"panel": "net.1.node.61.port.1026", "key": "0.1.9", "state": "absent"}]}`,
	} {
		out, changes, err := rrcsCfgRun(t, desired, "--check")
		if err == nil || len(changes) != 0 || !strings.Contains(out, `"failed":[{`) {
			t.Errorf("%s: err %v, %d changes\n%s", name, err, len(changes), out)
		}
	}
}

func TestRRCSLevelRaw(t *testing.T) {
	for text, want := range map[string]int{"mute": 0, "0": 230, "-14.5": 201, "-20 dB": 190, "12.5": 255, "-114.5": 1} {
		if got, err := rrcsLevelRaw(text); err != nil || got != want {
			t.Errorf("%q = %d, %v; want %d", text, got, err, want)
		}
	}
	for _, text := range []string{"", "loud", "13", "-0.3", "-115"} {
		if _, err := rrcsLevelRaw(text); err == nil {
			t.Errorf("%q accepted", text)
		}
	}
	if rrcsLevelText(201) != "-14.5 dB" || rrcsLevelText(0) != "mute" {
		t.Errorf("text: %q %q", rrcsLevelText(201), rrcsLevelText(0))
	}
}

// An IFB is edited by its number; only what differs is sent.
func TestRRCSEnsureIFB(t *testing.T) {
	// The stand-in IFB (number 0): label "IFB A1", input 61.7, output
	// 61.7, no mix minus.
	desired := `{"ifbs": [{"number": 0, "label": "SPORT", "input": "net.1.node.61.port.7.in",
	  "mix_minus": "net.1.node.61.port.1041", "dim_level": 4}]}`
	_, changes, _ := rrcsCfgRun(t, desired, "--write-to", "HOST")
	if len(changes) == 0 {
		t.Fatal("nothing sent")
	}
	ch, _ := changes[0].Field("Change")
	ot, _ := ch.Field("ObjectType")
	sp, _ := ch.Field("SpecificParams")
	label, _ := sp.Field("Label")
	mm, _ := sp.Field("MixMinus")
	if ot.Str != "ifb" || rrcsMemberInt(sp, "IFBNumber") != 0 || label.Str != "SPORT" || rrcsMemberInt(mm, "Port") != 1041 || rrcsMemberInt(sp, "DimLevel") != 4 {
		t.Errorf("edit ifb: %s", rrcsCompact(sp))
	}
	if _, sent := sp.Field("Input"); sent {
		t.Errorf("the input did not change and was sent: %s", rrcsCompact(sp))
	}
	out, changes, err := rrcsCfgRun(t, `{"ifbs": [{"number": 99, "label": "X"}]}`, "--check")
	if err == nil || len(changes) != 0 || !strings.Contains(out, "cannot be created") {
		t.Errorf("unknown IFB: %v\n%s", err, out)
	}
}

// Label, alias and gains of a port: read, written when they differ, read
// again; a second run changes nothing.
func TestRRCSEnsurePorts(t *testing.T) {
	var mu sync.Mutex
	state := map[string]codec.Value{"Label": codec.String("OLD"), "Alias": codec.String(""), "InputGain": codec.Int(0), "OutputGain": codec.Int(-128)}
	var writes []string
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		defer mu.Unlock()
		k := call.Params[0]
		name := strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(call.Method, "Get"), "Set"), "Port")
		if _, known := state[name]; !known {
			return rrcsTreeAnswer(call)
		}
		if strings.HasPrefix(call.Method, "Get") {
			return codec.Array(k, codec.Int(0), state[name]), true
		}
		// The value is the parameter before the last for label and
		// alias (is input follows), the last for a gain.
		v := call.Params[len(call.Params)-1]
		if name == "Label" || name == "Alias" {
			v = call.Params[len(call.Params)-2]
		}
		state[name] = v
		writes = append(writes, call.Method)
		return codec.Array(k, codec.Int(0)), true
	})
	file := filepath.Join(t.TempDir(), "ports.json")
	_ = os.WriteFile(file, []byte(`{"ports": [{"port": "net.1.node.61.port.1041", "label": "CODEC1", "alias": "C 1",
	  "input_gain": "-3.5", "output_gain": "mute"}]}`), 0o600)
	out := rrcsRun(t, "ensure", f.addr(), "--file", file, "--check")
	rrcsWant(t, out, "would change  net.1.node.61.port.1041.Label", "OLD -> CODEC1", "0.0 dB -> -3.5 dB", "would change 3, failed 0")
	if strings.Contains(out, "OutputGain") {
		t.Errorf("a gain that is already mute was planned:\n%s", out)
	}
	out = rrcsRun(t, "ensure", f.addr(), "--file", file, "--write-to", f.addr())
	rrcsWant(t, out, "changed 3, failed 0")
	mu.Lock()
	if got := strings.Join(writes, ","); got != "SetPortLabel,SetPortAlias,SetInputGain" || state["InputGain"].Int != -7 {
		t.Errorf("writes %s, input gain %d", got, state["InputGain"].Int)
	}
	mu.Unlock()
	rrcsWant(t, rrcsRun(t, "ensure", f.addr(), "--file", file, "--write-to", f.addr()), "changed 0, failed 0")
	for text, want := range map[string]int{"mute": -128, "0": 0, "-18": -36, "18 dB": 36, "0.5": 1} {
		if got, err := rrcsGainRaw(text); err != nil || got != want {
			t.Errorf("gain %q = %d, %v", text, got, err)
		}
	}
	if _, err := rrcsGainRaw("19"); err == nil {
		t.Error("gain 19 accepted")
	}
}

// Every change a converging verb makes, and every one it could not make,
// is a record of the log: what, from what, to what.
func TestRRCSEnsureLogsChanges(t *testing.T) {
	label := "OLD"
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		k := call.Params[0]
		switch call.Method {
		case "GetPortLabel":
			return codec.Array(k, codec.Int(0), codec.String(label)), true
		case "SetPortLabel":
			label = call.Params[3].Str
			return codec.Array(k, codec.Int(0)), true
		case "GetInputGain":
			return codec.Array(k, codec.Int(24)), true // the port is not on line
		}
		return rrcsTreeAnswer(call)
	})
	dir := t.TempDir()
	file, logFile := filepath.Join(dir, "d.json"), filepath.Join(dir, "ensure.log")
	_ = os.WriteFile(file, []byte(`{"ports": [{"port": "net.1.node.61.port.1041", "label": "NEW", "input_gain": "0"}]}`), 0o600)
	_, _ = rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"ensure", f.addr(), "--file", file, "--write-to", f.addr(), "--log", logFile, "--log-format", "json"})
	})
	// The log is one file per day: the date is in its name.
	var raw []byte
	names, _ := filepath.Glob(filepath.Join(dir, "ensure*.log"))
	for _, name := range names {
		b, _ := os.ReadFile(name)
		raw = append(raw, b...)
	}
	for _, want := range []string{
		`"msg":"config_change"`, `"verb":"ensure"`, `"mode":"applied"`, `"path":"net.1.node.61.port.1041.Label"`, `"from":"OLD"`, `"to":"NEW"`,
		`"msg":"config_failed"`, `"path":"net.1.node.61.port.1041.InputGain"`, `"level":"WARN"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("log lacks %s:\n%s", want, raw)
		}
	}
}

// A stream: the ports after the main one are linked to it, channel 2 and
// up, inputs and outputs, with Mode and Selection alone in the request.
func TestRRCSEnsureStreams(t *testing.T) {
	var mu sync.Mutex
	selection := map[string]int32{} // "port/in" → channel; 1 when absent
	var sent []codec.Value
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		mu.Lock()
		defer mu.Unlock()
		k := call.Params[0]
		key := func(port int32, in bool) string {
			if in {
				return strconv.Itoa(int(port)) + "/in"
			}
			return strconv.Itoa(int(port)) + "/out"
		}
		switch call.Method {
		case "GetPort": // net, node, port, is input, pool port
			port, in := call.Params[3].Int, call.Params[4].Bool
			sel, set := selection[key(port, in)]
			if !set {
				sel = 1
			}
			channels := int32(1)
			if port == 1072 && !in {
				channels = 4
			}
			block := "PortAes67Output"
			if in {
				block = "PortAes67Input"
			}
			return codec.Array(k, codec.Int(0), codec.Struct(rrcsMember(block, codec.Struct(
				rrcsMember("Protocol", codec.Int(5)), rrcsMember("Channels", codec.Int(channels)), rrcsMember("Selection", codec.Int(sel)))))), true
		case "ConfigurationChange":
			sp, _ := call.Params[1].Items[0].Field("SpecificParams")
			sent = append(sent, sp)
			addr, _ := sp.Field("PortAddress")
			in := rrcsFieldBool(addr, "IsInput")
			block := "PortAes67Output"
			if in {
				block = "PortAes67Input"
			}
			b, _ := sp.Field(block)
			selection[key(int32(rrcsMemberInt(addr, "Port")), in)] = int32(rrcsMemberInt(b, "Selection"))
			return k, true
		}
		return rrcsTreeAnswer(call)
	})
	file := filepath.Join(t.TempDir(), "s.json")
	_ = os.WriteFile(file, []byte(`{"streams": [{"main": "net.1.node.63.port.1072", "block": 3}]}`), 0o600)

	// Dry run: four links planned (two ports, two directions), nothing sent.
	out, _ := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"ensure", f.addr(), "--file", file, "--check"})
	})
	rrcsWant(t, out, "net.1.node.63.port.1073.in.PortAes67Input.Selection", "channel 1 -> linked to port 1072, channel 2",
		"net.1.node.63.port.1074.out.PortAes67Output.Selection", "linked to port 1072, channel 3", "would change 4, failed 0")
	mu.Lock()
	if len(sent) != 0 {
		t.Fatalf("--check sent %d requests", len(sent))
	}
	mu.Unlock()

	rrcsWant(t, rrcsRun(t, "ensure", f.addr(), "--file", file, "--write-to", f.addr()), "changed 4, failed 0")
	mu.Lock()
	if len(sent) != 4 {
		t.Fatalf("%d requests sent, want 4", len(sent))
	}
	first := sent[0]
	addr, _ := first.Field("PortAddress")
	in, _ := first.Field("PortAes67Input")
	if rrcsMemberInt(addr, "Node") != 63 || rrcsMemberInt(addr, "Port") != 1073 || !rrcsFieldBool(addr, "IsInput") ||
		len(in.Members) != 2 || rrcsMemberInt(in, "Mode") != 1072 || rrcsMemberInt(in, "Selection") != 2 {
		t.Errorf("first link: %s", rrcsCompact(first))
	}
	mu.Unlock()
	// A second run finds every port on its channel.
	rrcsWant(t, rrcsRun(t, "ensure", f.addr(), "--file", file, "--write-to", f.addr()), "changed 0, failed 0")

	// A main output without room for the channels is said, not edited.
	_ = os.WriteFile(file, []byte(`{"streams": [{"main": "net.1.node.63.port.1072", "block": 8, "directions": ["out"]}]}`), 0o600)
	out, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"ensure", f.addr(), "--file", file, "--check"})
	})
	if err == nil || !strings.Contains(out, "the main output has 4 channel(s), 8 wanted") {
		t.Errorf("%v\n%s", err, out)
	}
	for name, bad := range map[string]string{
		"direction in main": `{"streams": [{"main": "net.1.node.63.port.1072.in", "block": 8}]}`,
		"both":              `{"streams": [{"main": "net.1.node.63.port.1072", "block": 8, "linked": ["net.1.node.63.port.1073"]}]}`,
		"neither":           `{"streams": [{"main": "net.1.node.63.port.1072"}]}`,
		"other card":        `{"streams": [{"main": "net.1.node.63.port.1072", "linked": ["net.1.node.61.port.1073"]}]}`,
	} {
		_ = os.WriteFile(file, []byte(bad), 0o600)
		if out, err := rrcsStdout(t, func() error {
			return runRRCS(context.Background(), []string{"ensure", f.addr(), "--file", file, "--check"})
		}); err == nil {
			t.Errorf("%s: accepted\n%s", name, out)
		}
	}
}

// A call over a trunk is named by the far net and port, to place it and
// to remove it.
func TestRRCSCommandParamsTrunk(t *testing.T) {
	params, err := rrcsCommandParams("call-to-port-cmd", "net.1.node.61.port.1026.key.0.1.9", "trunk.2.109")
	if err != nil {
		t.Fatal(err)
	}
	v := codec.Struct(params...)
	if rrcsMemberInt(v, "TrunkingNetAddr") != 2 || rrcsMemberInt(v, "TrunkingPortAddr") != 109 {
		t.Errorf("trunk call: %s", rrcsCompact(v))
	}
	if _, has := v.Field("DestinationPortAddress"); has {
		t.Errorf("a trunk call carries no local destination: %s", rrcsCompact(v))
	}
	if _, err := rrcsCommandParams("call-to-port-cmd", "net.1.node.61.port.1026.key.0.1.9", "trunk.x"); err == nil {
		t.Error("bad trunk target accepted")
	}
}
