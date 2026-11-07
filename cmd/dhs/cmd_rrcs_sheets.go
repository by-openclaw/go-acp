package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The sheets are the desired state as CSV files, one per topic, for a
// person with a spreadsheet and for Ansible: `rrcs sheets` writes them
// from the system as it is, `rrcs ensure --sheets DIR` brings the system
// to them. A blank cell leaves the system as it is.
//
//	cards.csv        card, type, name, frame, channels, up, down
//	ports.csv        port, type, label, alias
//	conferences.csv  name, label
//	groups.csv       name, label
//	ifbs.csv         number, label, name, input, output, mix_minus, dim_level
//	members.csv      port, label, conference.<name>…, group.<name>…   (the matrix:
//	                 talk+listen | talk | listen | off, on | off for a group,
//	                 "key" = joins when a key of the port is pressed, left alone)
//	keys.csv         panel, key, function, target, label, mode
var rrcsSheetHeaders = map[string][]string{
	"cards.csv":       {"card", "type", "name", "frame", "channels", "up", "down"},
	"ports.csv":       {"port", "type", "label", "alias"},
	"conferences.csv": {"name", "label"},
	"groups.csv":      {"name", "label"},
	"ifbs.csv":        {"number", "label", "name", "input", "output", "mix_minus", "dim_level"},
	"members.csv":     {"port", "label"},
	"keys.csv":        {"panel", "key", "function", "target", "label", "mode"},
}

// rrcsKeyFunctionWords is the file's word for each command type.
var rrcsKeyFunctionWords = func() map[string]string {
	out := map[string]string{}
	for word, t := range rrcsKeyFunctions {
		out[t] = word
	}
	return out
}()

func rrcsSheets(ctx context.Context, args []string) error {
	const verb = "sheets"
	fs := flag.NewFlagSet("rrcs "+verb, flag.ContinueOnError)
	src := newRRCSSource(fs)
	out := fs.String("out", "auto", "folder the CSV files are written to. \"auto\" = snapshots/rrcs/<host>/sheets/")
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "Usage: dhs consumer rrcs sheets <host>[:port] [--out DIR]\n\n"+
			"Writes the system as CSV sheets, one per topic — cards, ports, conferences,\n"+
			"groups, IFBs, the member matrix, the keys — the files ensure --sheets reads\n"+
			"back. Edit a cell, run ensure: only that cell changes. It only reads.\n\n")
		fs.PrintDefaults()
	}
	if err := parseVerbFlags(fs, reorderFlagsFirst(args)); err != nil {
		return err
	}
	snap, err := src.snapshot(ctx, verb, fs.Args(), rrcsCollectOpts{Values: true, Commands: true, PanelCommands: false})
	if err != nil {
		return err
	}
	m := rrcsBuildModel(snap)
	dir := *out
	if dir == "auto" {
		key := "from-file"
		if len(fs.Args()) == 1 {
			key = strings.Split(fs.Arg(0), ":")[0]
		}
		dir = filepath.Join(snapshotDir("rrcs", key), "sheets")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sheets := rrcsSheetsOf(m)
	names := make([]string, 0, len(sheets))
	for n := range sheets {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if err := rrcsWriteSheet(filepath.Join(dir, n), sheets[n]); err != nil {
			return err
		}
		fmt.Printf("%-16s %4d rows  %s\n", n, len(sheets[n])-1, filepath.Join(dir, n))
	}
	return nil
}

// snapshot reads the system from the gateway or from --from.
func (s *rrcsSource) snapshot(ctx context.Context, verb string, hosts []string, opts rrcsCollectOpts) (map[string]any, error) {
	var doc []byte
	switch {
	case *s.from != "" && len(hosts) > 0:
		return nil, rrcsValErr(verb, "give a host or --from, not both")
	case *s.from != "":
		raw, err := os.ReadFile(*s.from)
		if err != nil {
			return nil, fmt.Errorf("rrcs %s: %w", verb, err)
		}
		doc = raw
	case len(hosts) != 1:
		return nil, rrcsValErr(verb, "want one host[:port] argument, or --from FILE")
	default:
		client, _, closeFn, err := s.cf.open(verb, hosts[0])
		if err != nil {
			return nil, err
		}
		defer closeFn()
		snap, _, err := rrcsCollect(ctx, client, opts)
		if err != nil {
			return nil, err
		}
		if doc, err = json.Marshal(snap); err != nil {
			return nil, err
		}
	}
	var snap map[string]any
	if err := json.Unmarshal(doc, &snap); err != nil {
		return nil, fmt.Errorf("rrcs %s: %w", verb, err)
	}
	return snap, nil
}

// rrcsBasePort drops the .in / .out of a half.
func rrcsBasePort(path string) string {
	return strings.TrimSuffix(strings.TrimSuffix(path, ".in"), ".out")
}

// rrcsSheetsOf lays the model out as the sheets, header row first.
func rrcsSheetsOf(m *rrcsModel) map[string][][]string {
	out := map[string][][]string{}
	for n, h := range rrcsSheetHeaders {
		out[n] = [][]string{append([]string(nil), h...)}
	}
	add := func(n string, row ...string) { out[n] = append(out[n], row) }

	for _, c := range m.Cards {
		row := []string{c.Path, c.Type, c.LongName, "", "", "", ""}
		if c.Type == "MADI" {
			if f := jInt(c.Raw, "FrameLength"); f != 0 {
				row[3] = strconv.Itoa(f)
			}
			if ch := jInt(c.Raw, "Channels"); ch >= 1 && ch <= len(rrcsMadiBlocks) {
				row[4] = rrcsMadiBlocks[ch-1]
			}
			side := func(v int) string { return map[int]string{1: "optical", 2: "electrical"}[v] }
			row[5], row[6] = side(jInt(c.Raw, "UpInterface")), side(jInt(c.Raw, "DownInterface"))
		}
		add("cards.csv", row...)
	}
	for _, p := range m.Ports {
		add("ports.csv", p.Path, p.Type, p.Label, p.Alias)
	}
	for _, o := range m.Objects["conference"] {
		add("conferences.csv", o.LongName, o.Label)
	}
	for _, o := range m.Objects["group"] {
		add("groups.csv", o.LongName, o.Label)
	}
	leg := func(v any, input bool) string {
		a := jMap(v)
		// "-" is none; a blank cell would leave the leg as it is.
		if jInt(a, "Node") == 0 && jInt(a, "Port") == 0 {
			return "-"
		}
		if p := m.Find(jInt(a, "Node"), jInt(a, "Port"), input); p != nil {
			return p.Path
		}
		return "-"
	}
	for _, o := range m.Objects["ifb"] {
		add("ifbs.csv", strconv.Itoa(jInt(o.Raw, "Number")), o.Label, o.LongName,
			leg(o.Raw["Input"], true), leg(o.Raw["Output"], false), leg(o.Raw["MixMinus"], true),
			strconv.Itoa(jInt(o.Raw, "DimLevel")))
	}

	// The matrix: a row per port that is a member somewhere or a panel, a
	// column per conference and group.
	type cell = map[string]string
	rows := map[string]cell{}
	labels := map[string]string{}
	var columns []string
	for _, kind := range []string{"conference", "group"} {
		for _, o := range m.Objects[kind] {
			col := kind + "." + o.LongName
			columns = append(columns, col)
			for _, mb := range o.Members {
				base := rrcsBasePort(mb.Port)
				if rows[base] == nil {
					rows[base] = cell{}
				}
				if labels[base] == "" {
					labels[base] = mb.Label
				}
				if kind == "group" {
					rows[base][col] = "on"
					continue
				}
				for _, f := range strings.Split(mb.Flags, "+") {
					if f == "" {
						continue
					}
					now := rows[base][col]
					switch {
					case now == "" || now == f:
						rows[base][col] = f
					case now != f:
						rows[base][col] = "talk+listen"
					}
				}
			}
		}
	}
	// A port that is in a conference or group because one of its keys
	// calls it joins when the key is pressed: "key", which ensure leaves
	// alone (the key itself is in keys.csv).
	viaKey := map[string]bool{}
	for _, k := range m.Keys {
		if k.Position != "key" {
			continue
		}
		for _, kind := range []string{"conference", "group"} {
			for _, o := range m.Objects[kind] {
				if k.Target == kind+"."+strconv.Itoa(o.ObjectID) {
					viaKey[rrcsBasePort(k.Port)+"|"+kind+"."+o.LongName] = true
				}
			}
		}
	}
	for base, cells := range rows {
		for col := range cells {
			if viaKey[base+"|"+col] && strings.HasPrefix(col, "conference.") {
				cells[col] = "key"
			}
		}
	}
	for _, p := range m.Ports {
		if p.KeyCount > 0 && rows[p.Path] == nil {
			rows[p.Path], labels[p.Path] = cell{}, p.Label
		}
	}
	out["members.csv"][0] = append(out["members.csv"][0], columns...)
	ports := make([]string, 0, len(rows))
	for p := range rows {
		ports = append(ports, p)
	}
	sort.Strings(ports)
	for _, p := range ports {
		row := []string{p, labels[p]}
		for _, c := range columns {
			row = append(row, rows[p][c])
		}
		add("members.csv", row...)
	}

	// Keys, with their label and mode. A target object is named by name.
	names := map[string]string{}
	for _, kind := range []string{"conference", "group", "ifb"} {
		for _, o := range m.Objects[kind] {
			name := o.LongName
			if kind == "ifb" && o.Label != "" {
				name = o.Label
			}
			names[kind+"."+strconv.Itoa(o.ObjectID)] = kind + "." + name
		}
	}
	modeWord := map[int]string{}
	for w, v := range rrcsKeyModes {
		modeWord[v+1] = w
	}
	for _, k := range m.Keys {
		word := rrcsKeyFunctionWords[k.CommandType]
		// A virtual function is membership, kept in members.csv: as a key
		// row it would hold one command per place, and a port in two
		// conferences would lose one.
		if word == "" || k.Position != "key" {
			continue
		}
		target := k.Target
		if n, ok := names[target]; ok {
			target = n
		}
		var label, mode string
		for _, p := range m.Ports {
			if p.Path != k.Port {
				continue
			}
			for _, kc := range p.KeyConfigs {
				if kc.Path == k.Path || strings.HasPrefix(k.Path, kc.Path+".") {
					if !jBool(kc.Props, "AutoLabelFlag") {
						label = jStr(kc.Props, "LabelValue")
					}
					mode = modeWord[jInt(kc.Props, "KeyMode")]
				}
			}
		}
		key := strings.TrimPrefix(k.Path, k.Port+".")
		key = strings.TrimPrefix(key, "key.")
		add("keys.csv", k.Port, key, word, target, label, mode)
	}
	return out
}

func rrcsWriteSheet(path string, rows [][]string) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	if err := w.WriteAll(rows); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// rrcsReadSheets reads the sheets of a folder into a desired state. A
// file that is not there, and a blank cell, leave the system as it is.
func rrcsReadSheets(dir string) (*rrcsDesired, error) {
	want := &rrcsDesired{}
	read := func(name string) ([]map[string]string, []string, error) {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, nil
		}
		if err != nil {
			return nil, nil, err
		}
		recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), string([]byte{0xef, 0xbb, 0xbf})))).ReadAll()
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
		if len(recs) == 0 {
			return nil, nil, nil
		}
		head := recs[0]
		var rows []map[string]string
		for _, r := range recs[1:] {
			row := map[string]string{}
			for i, h := range head {
				if i < len(r) {
					row[strings.TrimSpace(h)] = strings.TrimSpace(r[i])
				}
			}
			rows = append(rows, row)
		}
		return rows, head, nil
	}
	opt := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	leg := func(s string) *string {
		switch s {
		case "":
			return nil
		case "-", "none":
			none := ""
			return &none
		}
		return &s
	}

	rows, _, err := read("cards.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		c := rrcsDesiredCard{Card: r["card"], Name: opt(r["name"])}
		if r["frame"]+r["channels"]+r["up"]+r["down"] != "" {
			frame, _ := strconv.Atoi(r["frame"])
			c.Madi = &rrcsDesiredMadi{Frame: frame, Channels: r["channels"], Up: r["up"], Down: r["down"]}
		}
		if c.Name != nil || c.Madi != nil {
			want.Cards = append(want.Cards, c)
		}
	}
	if rows, _, err = read("ports.csv"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r["label"]+r["alias"] != "" {
			want.Ports = append(want.Ports, rrcsDesiredPort{Port: r["port"], Label: opt(r["label"]), Alias: opt(r["alias"])})
		}
	}
	for _, kind := range []string{"conferences.csv", "groups.csv"} {
		if rows, _, err = read(kind); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r["name"] == "" {
				continue
			}
			o := rrcsDesiredObject{Name: r["name"], Label: opt(r["label"])}
			if kind == "conferences.csv" {
				want.Conferences = append(want.Conferences, o)
			} else {
				want.Groups = append(want.Groups, o)
			}
		}
	}
	if rows, _, err = read("ifbs.csv"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		n, err := strconv.Atoi(r["number"])
		if err != nil {
			return nil, fmt.Errorf("ifbs.csv: number %q", r["number"])
		}
		f := rrcsDesiredIFB{Number: n, Label: opt(r["label"]), Name: opt(r["name"]),
			Input: leg(r["input"]), Output: leg(r["output"]), MixMinus: leg(r["mix_minus"])}
		if r["dim_level"] != "" {
			d, err := strconv.Atoi(r["dim_level"])
			if err != nil {
				return nil, fmt.Errorf("ifbs.csv: dim_level %q", r["dim_level"])
			}
			f.DimLevel = &d
		}
		want.IFBs = append(want.IFBs, f)
	}
	rows, head, err := read("members.csv")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		for _, col := range head {
			col = strings.TrimSpace(col)
			if col == "port" || col == "label" || r[col] == "" || strings.EqualFold(r[col], "key") {
				continue // blank: untouched; key: joins through its key (keys.csv)
			}
			if want.Members == nil {
				want.Members = rrcsMemberMatrix{}
			}
			if want.Members[col] == nil {
				want.Members[col] = map[string]string{}
			}
			want.Members[col][r["port"]] = r[col]
		}
	}
	if rows, _, err = read("keys.csv"); err != nil {
		return nil, err
	}
	for _, r := range rows {
		k := rrcsDesiredKey{Panel: r["panel"], Key: r["key"], Function: r["function"], Target: r["target"], Label: opt(r["label"]), Mode: r["mode"]}
		if k.Function == "absent" || k.Function == "off" {
			k.Function, k.State = "", "absent"
		}
		if k.Function == "" && k.State == "" && k.Label == nil && k.Mode == "" {
			continue
		}
		want.Keys = append(want.Keys, k)
	}
	return want, nil
}

// rrcsMergeDesired adds the sheets to what a --file gave.
func rrcsMergeDesired(into, from *rrcsDesired) {
	into.Cards = append(into.Cards, from.Cards...)
	into.Ports = append(into.Ports, from.Ports...)
	into.Conferences = append(into.Conferences, from.Conferences...)
	into.Groups = append(into.Groups, from.Groups...)
	into.IFBs = append(into.IFBs, from.IFBs...)
	into.Keys = append(into.Keys, from.Keys...)
	for col, cells := range from.Members {
		if into.Members == nil {
			into.Members = rrcsMemberMatrix{}
		}
		if into.Members[col] == nil {
			into.Members[col] = map[string]string{}
		}
		for p, v := range cells {
			into.Members[col][p] = v
		}
	}
}
