package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rrcs "dhs/internal/rrcs/consumer"
)

func TestRRCSSheetsOfTheOracle(t *testing.T) {
	m := rrcsMatrixModel(t)
	m.Objects["conference"][0].Members = []rrcs.MemberRef{
		{Port: "net.1.node.93.port.8", Label: "YOUSSEF", Flags: "talk+listen"},
		{Port: "net.1.node.93.port.17.in", Label: "I.3.2", Flags: "talk"},
		{Port: "net.1.node.93.port.17.out", Label: "O.3.2", Flags: "listen"},
	}
	m.Objects["ifb"][0].Raw = map[string]any{"Number": 1.0, "DimLevel": 4.0,
		"Input": map[string]any{"Node": 93.0, "Port": 17.0}, "Output": map[string]any{"Node": 0.0, "Port": 0.0}}
	m.Keys = []*rrcs.Key{
		{Port: "net.1.node.93.port.8", Path: "net.1.node.93.port.8.key.0.1.4", Position: "key", CommandType: "call-to-conference", Target: "conference.11"},
		{Port: "net.1.node.93.port.8", Path: "net.1.node.93.port.8.key.0.1.6", Position: "key", CommandType: "call-to-ifb", Target: "ifb.44"},
		{Port: "net.1.node.93.port.17.in", Path: "net.1.node.93.port.17.in.vfunc.always.0", Position: "virtual-function", CommandType: "call-to-conference", Target: "conference.11"},
		{Port: "net.1.node.93.port.8", Path: "net.1.node.93.port.8.key.0.1.9", Position: "key", CommandType: "edit-ifb-cmd"},
	}
	m.Cards = []*rrcs.Card{{Path: "net.1.node.93.card.3", Type: "MADI", Raw: map[string]any{"FrameLength": 64.0, "Channels": 1.0, "UpInterface": 1.0, "DownInterface": 2.0}}}
	for _, p := range m.Ports {
		switch p.Path {
		case "net.1.node.93.port.17.in":
			p.InputGain = "3.0"
		case "net.1.node.93.port.17.out":
			p.OutputGain = "mute"
		}
	}
	sheets := rrcsSheetsOf(m)
	if ports := strings.Join(sheets["ports.csv"][0], ","); ports != "port,type,label,alias,input_gain,output_gain" {
		t.Errorf("ports header: %s", ports)
	}
	for _, row := range sheets["ports.csv"][1:] {
		line := strings.Join(row, ",")
		switch row[0] {
		case "net.1.node.93.port.17.in":
			if line != "net.1.node.93.port.17.in,Input (MADI),,,3.0," {
				t.Errorf("gain row: %s", line)
			}
		case "net.1.node.93.port.17.out":
			if line != "net.1.node.93.port.17.out,Output (MADI),,,,mute" {
				t.Errorf("mute row: %s", line)
			}
		}
	}
	text := func(n string) string {
		var b strings.Builder
		for _, r := range sheets[n] {
			b.WriteString(strings.Join(r, ",") + "\n")
		}
		return b.String()
	}
	for name, want := range map[string]string{
		"members.csv": "port,label,conference.DHS CONF 1,conference.DHS CONF 2,group.DHS GROUP 1\n" +
			"net.1.node.93.port.17,I.3.2,talk+listen,,\nnet.1.node.93.port.8,YOUSSEF,key,,on\n",
		"keys.csv": "panel,key,function,target,label,mode\n" +
			"net.1.node.93.port.8,0.1.4,call-to-conference,conference.DHS CONF 1,,\n" +
			"net.1.node.93.port.8,0.1.6,call-to-ifb,ifb.DHSIFB1,,\n",
		"cards.csv": "card,type,name,frame,channels,up,down\nnet.1.node.93.card.3,MADI,,64,01-08,optical,electrical\n",
		"ifbs.csv":  "number,label,name,input,output,mix_minus,dim_level\n1,DHSIFB1,DHS IFB 1,net.1.node.93.port.17.in,-,-,4\n",
	} {
		if got := text(name); got != want {
			t.Errorf("%s:\n%s\nwant\n%s", name, got, want)
		}
	}
}

func TestRRCSReadSheets(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cards.csv", "card,type,name,frame,channels,up,down\nnet.1.node.93.card.3,MADI,,64,01-08,optical,optical\nnet.1.node.93.card.2,CAT5,,,,,\n")
	write("ports.csv", "\ufeffport,type,label,alias,input_gain,output_gain\nnet.1.node.93.port.16.in,MADI,I.3.1,,,\nnet.1.node.93.port.17.in,MADI,,,-3.5,\nnet.1.node.93.port.18.in,MADI,,,,\n")
	write("conferences.csv", "name,label\nDHS CONF 1,DHSCNF1\n,\n")
	write("groups.csv", "name,label\nDHS GROUP 1,\n")
	write("ifbs.csv", "number,label,name,input,output,mix_minus,dim_level\n1,L,,net.1.node.93.port.16.in,-,,4\n")
	write("members.csv", "port,label,conference.DHS CONF 1,group.DHS GROUP 1\nnet.1.node.93.port.20,x,talk,\nnet.1.node.93.port.8,y,key,on\n")
	write("keys.csv", "panel,key,function,target,label,mode\nnet.1.node.93.port.8,0.1.1,call-to-port,net.1.node.93.port.17,M,latching\nnet.1.node.93.port.8,0.1.2,off,,,\nnet.1.node.93.port.8,0.1.3,,,,\n")
	want, err := rrcsReadSheets(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(want.Cards) != 1 || want.Cards[0].Madi.Channels != "01-08" || want.Cards[0].Madi.Frame != 64 {
		t.Errorf("cards %+v", want.Cards)
	}
	if len(want.Ports) != 2 || *want.Ports[0].Label != "I.3.1" || want.Ports[0].Alias != nil || want.Ports[1].InputGain != "-3.5" || want.Ports[1].Label != nil {
		t.Errorf("ports %+v", want.Ports)
	}
	if len(want.Conferences) != 1 || len(want.Groups) != 1 || want.Groups[0].Label != nil {
		t.Errorf("objects %+v %+v", want.Conferences, want.Groups)
	}
	f := want.IFBs[0]
	if f.Number != 1 || *f.Input != "net.1.node.93.port.16.in" || *f.Output != "" || f.MixMinus != nil || *f.DimLevel != 4 {
		t.Errorf("ifb %+v", f)
	}
	if want.Members["conference.DHS CONF 1"]["net.1.node.93.port.20"] != "talk" || len(want.Members["conference.DHS CONF 1"]) != 1 ||
		want.Members["group.DHS GROUP 1"]["net.1.node.93.port.8"] != "on" {
		t.Errorf("members %+v", want.Members)
	}
	if len(want.Keys) != 2 || want.Keys[1].State != "absent" || want.Keys[0].Mode != "latching" {
		t.Errorf("keys %+v", want.Keys)
	}

	// Merged with a file: the cells add to what the file gives.
	into := &rrcsDesired{Members: rrcsMemberMatrix{"conference.X": {"p": "on"}}}
	rrcsMergeDesired(into, want)
	if len(into.Members) != 3 || len(into.Keys) != 2 {
		t.Errorf("merge %+v", into)
	}

	for name, body := range map[string]string{"ifbs.csv": "number\nx\n", "keys.csv": "\"a\n"} {
		bad := t.TempDir()
		_ = os.WriteFile(filepath.Join(bad, name), []byte(body), 0o644)
		if _, err := rrcsReadSheets(bad); err == nil {
			t.Errorf("%s %q taken", name, body)
		}
	}
	empty := t.TempDir()
	if w, err := rrcsReadSheets(empty); err != nil || len(w.Keys)+len(w.Members) != 0 {
		t.Errorf("empty folder: %+v %v", w, err)
	}
}

// The verb writes the sheets of a snapshot and ensure reads them back.
func TestRRCSSheetsVerbFromASnapshot(t *testing.T) {
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap.json")
	if err := os.WriteFile(snap, []byte(rrcsOracleSnap), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "sheets")
	if _, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"sheets", "--from", snap, "--out", out})
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "ports.csv"))
	if err != nil || !strings.Contains(string(raw), "net.1.node.93.port.17.out") {
		t.Errorf("ports.csv: %s %v", raw, err)
	}
	if _, err := rrcsReadSheets(out); err != nil {
		t.Errorf("read back: %v", err)
	}
	if err := runRRCS(context.Background(), []string{"sheets"}); err == nil {
		t.Error("no host taken")
	}
}
