package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
