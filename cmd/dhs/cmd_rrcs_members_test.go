package main

import (
	"sort"
	"strings"
	"testing"

	"dhs/internal/rrcs/codec"
	rrcs "dhs/internal/rrcs/consumer"
)

// rrcsMatrixModel is the oracle with two conferences and one group.
func rrcsMatrixModel(t *testing.T) *rrcsModel {
	t.Helper()
	m := rrcsOracleModel(t)
	m.Objects = map[string][]*rrcs.Object{
		"conference": {{ObjectID: 11, LongName: "DHS CONF 1", Label: "DHSCNF1"}, {ObjectID: 22, LongName: "DHS CONF 2"}},
		"group":      {{ObjectID: 33, LongName: "DHS GROUP 1", Members: []rrcs.MemberRef{{Port: "net.1.node.93.port.8"}}}},
		"ifb":        {{ObjectID: 44, LongName: "DHS IFB 1", Label: "DHSIFB1"}},
	}
	return m
}

func TestRRCSMemberRights(t *testing.T) {
	for cell, want := range map[string][3]bool{
		"talk+listen": {true, true, true}, "on": {true, true, true}, "Listen": {false, true, true},
		"talk": {true, false, true}, "off": {false, false, true}, "maybe": {false, false, false},
	} {
		talk, listen, ok := rrcsMemberRights(cell)
		if [3]bool{talk, listen, ok} != want {
			t.Errorf("%s: %v %v %v", cell, talk, listen, ok)
		}
	}
}

func TestRRCSMembersMatrix(t *testing.T) {
	m := rrcsMatrixModel(t)
	var problems []string
	bad := func(field, format string, a ...any) { problems = append(problems, field) }
	want := &rrcsDesired{
		Conferences: []rrcsDesiredObject{{Name: "NEW CONF"}},
		Members: rrcsMemberMatrix{
			"conference.DHS CONF 1": {"net.1.node.93.port.17": "talk", "net.1.node.93.port.8": "talk+listen"},
			"conference.22":         {"net.1.node.93.port.17": "off"},
			"conference.NEW CONF":   {"net.1.node.93.port.17": "on"}, // created by this run: next pass
			"group.DHS GROUP 1":     {"net.1.node.93.port.17": "on", "net.1.node.93.port.8": "off"},
			"conference.NOPE":       {"net.1.node.93.port.17": "on"},
			"bogus":                 {"net.1.node.93.port.17": "on"},
		},
	}
	out := rrcsWithMembers(m, want, bad)
	keys := []string{}
	for _, k := range out.Keys {
		keys = append(keys, k.Panel+" "+k.Target+" "+k.State)
	}
	sort.Strings(keys)
	got := strings.Join(keys, " | ")
	wantKeys := "net.1.node.93.port.17.in conference.11  | net.1.node.93.port.17.in conference.22 absent | " +
		"net.1.node.93.port.17.out conference.11 absent | net.1.node.93.port.17.out conference.22 absent | " +
		"net.1.node.93.port.8 conference.11 "
	if got != wantKeys {
		t.Errorf("keys:\n got  %s\n want %s", got, wantKeys)
	}
	for _, k := range out.Keys {
		if !k.Additive || k.Key != "vfunc.always" || k.Function != "call-to-conference" {
			t.Errorf("key %+v", k)
		}
	}
	if len(out.Groups) != 1 || strings.Join(out.Groups[0].Members, " ") != "net.1.node.93.port.17.out" {
		t.Errorf("group: %+v", out.Groups)
	}
	sort.Strings(problems)
	if strings.Join(problems, " ") != "members.bogus members.conference.NOPE" {
		t.Errorf("problems: %v", problems)
	}
	if len(want.Keys) != 0 {
		t.Error("the file itself was changed")
	}
}

func TestRRCSMembersOneObjectPort(t *testing.T) {
	m := rrcsMatrixModel(t)
	var problems []string
	bad := func(field, format string, a ...any) { problems = append(problems, field) }
	out := rrcsWithMembers(m, &rrcsDesired{Members: rrcsMemberMatrix{
		"conference.11": {"net.1.node.93.port.8": "talk", "net.1.node.93.port.99": "on", "x": "on", "net.1.node.93.port.17": "loud"}}}, bad)
	if len(out.Keys) != 0 || len(problems) != 4 {
		t.Errorf("keys %v problems %v", out.Keys, problems)
	}
}

func TestRRCSResolveObjectNames(t *testing.T) {
	m := rrcsMatrixModel(t)
	want := &rrcsDesired{Keys: []rrcsDesiredKey{
		{Function: "call-to-conference", Target: "conference.DHS CONF 1"},
		{Function: "call-to-ifb", Target: "ifb.DHSIFB1"},
		{Function: "call-to-group", Target: "group.33"},
		{Function: "listen", Target: "net.1.node.93.port.17"},
		{Function: "call-to-conference", Target: "conference.LATER"},
	}}
	rrcsResolveTalkPaths(m, want)
	got := []string{}
	for _, k := range want.Keys {
		got = append(got, k.Target)
	}
	if strings.Join(got, " ") != "conference.11 ifb.44 group.33 net.1.node.93.port.17.in conference.LATER" {
		t.Errorf("targets: %v", got)
	}
	label := "L"
	if !rrcsCreatedInFile(&rrcsDesired{IFBs: []rrcsDesiredIFB{{Label: &label}}}, "ifb", "L") ||
		!rrcsCreatedInFile(&rrcsDesired{Groups: []rrcsDesiredObject{{Name: "G"}}}, "group", "G") ||
		rrcsCreatedInFile(&rrcsDesired{}, "conference", "C") {
		t.Error("rrcsCreatedInFile")
	}
}

func TestRRCSListenCommandParams(t *testing.T) {
	params, err := rrcsCommandParams("listen-to-port-cmd", "net.1.node.93.port.8.key.0.1.7", "net.1.node.93.port.16.in")
	if err != nil || len(params) != 2 || params[1].Name != "SourcePortAddress" {
		t.Fatalf("%v %v", params, err)
	}
	if rrcsMemberInt(params[1].Value, "Port") != 16 {
		t.Errorf("source %s", rrcsCompact(params[1].Value))
	}
	if _, err := rrcsCommandParams("listen-to-port-cmd", "net.1.node.93.port.8.key.0.1.7", "nope"); err == nil {
		t.Error("bad target taken")
	}
}

func TestRRCSPlanCardMadi(t *testing.T) {
	// As RRCS 9.0.RR1 reads a MADI card set in Director to 64, 01-08,
	// optical, optical.
	have := map[string]any{"ClientCardTypeString": "MADI", "FrameLength": 64.0, "Channels": 1.0, "UpInterface": 1.0, "DownInterface": 1.0, "LongName": ""}
	edit, madi, _, _, problems := rrcsPlanCard(have, rrcsDesiredCard{Madi: &rrcsDesiredMadi{Frame: 64, Channels: "01-08", Up: "optical", Down: "optical"}})
	if len(edit)+len(madi)+len(problems) != 0 {
		t.Errorf("same settings: %v %v %v", edit, madi, problems)
	}
	name := "MADI 1"
	edit, madi, from, to, problems := rrcsPlanCard(have, rrcsDesiredCard{Name: &name, Madi: &rrcsDesiredMadi{Frame: 56, Channels: "17-24", Up: "electrical"}})
	got := map[string]int32{}
	for _, mb := range madi {
		got[mb.Name] = mb.Value.Int
	}
	if len(problems) != 0 || len(edit) != 1 || got["FrameLength"] != 0 || got["Channels"] != 2 || got["UpInterface"] != 1 || len(from) != 4 || len(to) != 4 {
		t.Errorf("edit %v madi %v problems %v", edit, got, problems)
	}
	for _, d := range []rrcsDesiredMadi{{Frame: 48}, {Channels: "1-8"}, {Channels: "57-64", Frame: 0}, {Up: "copper"}} {
		h := map[string]any{"ClientCardTypeString": "MADI", "FrameLength": 56.0}
		if _, _, _, _, p := rrcsPlanCard(h, rrcsDesiredCard{Madi: &d}); len(p) == 0 {
			t.Errorf("%+v taken", d)
		}
	}
	if _, _, _, _, p := rrcsPlanCard(map[string]any{"ClientCardTypeString": "CAT5"}, rrcsDesiredCard{Madi: &rrcsDesiredMadi{Frame: 64}}); len(p) == 0 {
		t.Error("madi settings on a CAT5 card taken")
	}
}

func TestRRCSCardAndPanelHelpers(t *testing.T) {
	if n, b, ok := rrcsCardOfPath("net.1.node.93.card.3"); !ok || n != 93 || b != 3 {
		t.Errorf("card path %d %d %v", n, b, ok)
	}
	for _, p := range []string{"net.1.node.93.port.3", "net.1.node.x.card.3", "card.3"} {
		if _, _, ok := rrcsCardOfPath(p); ok {
			t.Errorf("%s taken", p)
		}
	}
	for v, want := range map[any]string{true: "true", 5.0: "5", "x": "x"} {
		if _, text, ok := rrcsPanelValue(v); !ok || text != want {
			t.Errorf("%v: %q %v", v, text, ok)
		}
	}
	if _, _, ok := rrcsPanelValue(2.5); ok {
		t.Error("2.5 taken")
	}
	if got := rrcsPlain(codec.Struct(codec.Member{Name: "Bay", Value: codec.Int(3)})); jInt(got.(map[string]any), "Bay") != 3 {
		t.Errorf("plain: %v", got)
	}
}

func TestRRCSGenericCommand(t *testing.T) {
	if !rrcsIsCommandType("beep-panel-cmd") || rrcsIsCommandType("panel-key") {
		t.Error("command types")
	}
	params, err := rrcsGenericCommandParams("net.1.node.93.port.8.key.0.1.10", map[string]any{
		"DestinationPortAddress": map[string]any{"IsInput": false, "Node": 93.0, "Port": 8.0},
		"SendString":             "HELLO", "List": []any{1.0, true}})
	if err != nil || len(params) != 4 || params[0].Name != "CommandPosition" || params[1].Name != "DestinationPortAddress" {
		t.Fatalf("%v %v", params, err)
	}
	if rrcsMemberInt(params[1].Value, "Port") != 8 {
		t.Errorf("address %s", rrcsCompact(params[1].Value))
	}
	for _, bad := range []map[string]any{{"X": 1.5}, {"X": nil}, {"X": map[string]any{"Y": 2.5}}, {"X": []any{2.5}}} {
		if _, err := rrcsGenericCommandParams("net.1.node.93.port.8.key.0.1.10", bad); err == nil {
			t.Errorf("%v taken", bad)
		}
	}
	if _, err := rrcsGenericCommandParams("nope", nil); err == nil {
		t.Error("bad position taken")
	}
	if p, err := rrcsCommandParams("beep-panel-cmd", "net.1.node.93.port.8.key.0.1.10", ""); err != nil || len(p) != 1 {
		t.Errorf("place only: %v %v", p, err)
	}
}
