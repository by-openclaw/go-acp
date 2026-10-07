package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/consumer"
	"dhs/internal/rrcs/codec"
)

func TestRRCSSplitPath(t *testing.T) {
	tests := []struct{ in, object, block, field string }{
		{"net.1.node.61.port.7.out.PortAes67Output.Multicast", "net.1.node.61.port.7.out", "PortAes67Output", "Multicast"},
		{"net.1.node.60.card.1.Ptp.PTP", "net.1.node.60.card.1", "Ptp", "PTP"},
		{"net.1.node.61.port.1026.LongName", "net.1.node.61.port.1026", "", "LongName"},
	}
	for _, tc := range tests {
		object, block, field, ok := rrcsSplitPath(tc.in)
		if !ok || object != tc.object || block != tc.block || field != tc.field {
			t.Errorf("%s: %q %q %q %v", tc.in, object, block, field, ok)
		}
	}
	for _, bad := range []string{"net.1.node.61.port.7", "Ptp.PTP", "net.1.node.60.card.1.A.B.C", ""} {
		if _, _, _, ok := rrcsSplitPath(bad); ok {
			t.Errorf("%q split", bad)
		}
	}
}

// readCSV returns the rows of an export keyed by path.
func rrcsReadExport(t *testing.T, file string) (header []string, rows map[string][]string) {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	all, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	rows = map[string][]string{}
	for _, r := range all[1:] {
		rows[r[4]] = r
	}
	return all[0], rows
}

func TestRRCSExport(t *testing.T) {
	f := newRRCSFake(t, rrcsTreeAnswer)
	dir := t.TempDir()
	file := filepath.Join(dir, "x.csv")
	rrcsRun(t, "export", f.addr(), "--out", file)
	header, rows := rrcsReadExport(t, file)
	// The header of the acp connectors, column for column.
	if strings.Join(header, ",") != "ip,protocol,slot,oid,path,id,label,kind,access,value,value_name,unit,min,max,step,default,enum_items,max_len,alarm_priority,alarm_tag,alarm_on,alarm_off,slot_status" {
		t.Errorf("header %v", header)
	}
	mc := rows["net.1.node.61.port.7.out.PortAes67Output.Multicast"]
	if mc == nil || mc[0] != "127.0.0.1" || mc[1] != "rrcs" || mc[5] != "101" || mc[6] != "Multicast" || mc[7] != "string" || mc[8] != "RW-" || mc[9] != "239.1.2.3" {
		t.Errorf("multicast row %v", mc)
	}
	proto := rows["net.1.node.61.port.7.out.PortAes67Output.Protocol"]
	if proto == nil || proto[7] != "enum" || proto[9] != "2" || proto[10] != "Manual" || proto[16] != "2=Manual|3=RTSP|5=NMOS" {
		t.Errorf("protocol row %v", proto)
	}
	if long := rows["net.1.node.61.port.1026.LongName"]; long == nil || long[8] != "R--" || long[9] != "PANEL-02" {
		t.Errorf("long name row %v", long)
	}
	if src := rows["net.1.node.61.port.1041.PortAes67Input.SourceIp"]; src == nil || src[8] != "RW-" {
		t.Errorf("receiver source row %v", src)
	}
	if card := rows["net.1.node.60.card.1.LongName"]; card == nil || card[9] != "CARD 1" {
		t.Errorf("card row %v", card)
	}

	// --path narrows; json by extension; stdout when no --out.
	js := filepath.Join(dir, "x.json")
	rrcsRun(t, "export", f.addr(), "--out", js, "--path", "PortAes67Output,card")
	var list []rrcsRow
	raw, _ := os.ReadFile(js)
	if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
		t.Fatalf("json export: %v, %d rows", err, len(list))
	}
	for _, r := range list {
		if !strings.Contains(r.Path, "PortAes67Output") && !strings.Contains(r.Path, "card") {
			t.Errorf("row outside --path: %s", r.Path)
		}
	}
	if out := rrcsRun(t, "export", f.addr(), "--format", "csv", "--path", "nothing-matches"); strings.Count(out, "\n") != 1 {
		t.Errorf("empty csv export:\n%s", out)
	}
	if out := rrcsRun(t, "export", f.addr(), "--path", "nothing-matches"); strings.TrimSpace(out) != "[]" {
		t.Errorf("empty json export: %s", out)
	}
	// From a snapshot too.
	snap := filepath.Join(dir, "walk.json")
	rrcsRun(t, "walk", f.addr(), "--out", snap, "--skip", "both")
	if out := rrcsRun(t, "export", "--from", snap, "--format", "csv", "--path", "port.7.out.PortAes67Output.Multicast,"); !strings.Contains(out, "239.1.2.3") {
		t.Errorf("export from a snapshot:\n%s", out)
	}
}

// edit rewrites one value of an exported CSV.
func rrcsEditCSV(t *testing.T, file, path, value string, sep rune) {
	t.Helper()
	raw, _ := os.ReadFile(file)
	r := csv.NewReader(strings.NewReader(string(raw)))
	if first, _, _ := strings.Cut(string(raw), "\n"); strings.Count(first, ";") > strings.Count(first, ",") {
		r.Comma = ';'
	}
	all, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range all[1:] {
		if r[4] == path {
			r[9], found = value, true
		}
	}
	if !found {
		t.Fatalf("no row %s", path)
	}
	out, _ := os.Create(file)
	w := csv.NewWriter(out)
	w.Comma = sep
	_ = w.WriteAll(all)
	w.Flush()
	_ = out.Close()
}

// The whole loop: export, edit one cell, dry run, apply, and a second
// import that finds nothing left to do.
func TestRRCSImportRoundTrip(t *testing.T) {
	f, changes := rrcsSetFake(t, true)
	file := filepath.Join(t.TempDir(), "x.csv")
	rrcsRun(t, "export", f.addr(), "--out", file)

	// Unedited: nothing to apply, read-only rows counted.
	out := rrcsRun(t, "import", f.addr(), "--file", file, "--dry-run")
	rrcsWant(t, out, "would apply 0, unchanged", "read_only (")
	if strings.Contains(out, "net.1.node.61.port.1026.LongName") {
		t.Errorf("read-only rows were listed one by one:\n%s", out)
	}

	rrcsEditCSV(t, file, "net.1.node.61.port.7.out.PortAes67Output.Multicast", "239.9.9.9", ';')
	rrcsEditCSV(t, file, "net.1.node.61.port.1026.LongName", "RENAMED", ';') // read-only: ignored

	dry := rrcsRun(t, "import", "--dry-run", f.addr(), "--file", file)
	rrcsWant(t, dry, "would_apply net.1.node.61.port.7.out.PortAes67Output.Multicast", "239.1.2.3 -> 239.9.9.9", "would apply 1,")
	if len(*changes) != 0 {
		t.Fatalf("--dry-run sent %d changes", len(*changes))
	}

	done := rrcsRun(t, "import", f.addr(), "--file", file, "--write-to", f.addr())
	rrcsWant(t, done, "applied     net.1.node.61.port.7.out.PortAes67Output.Multicast", "applied 1,", "failed 0")
	if len(*changes) != 1 {
		t.Fatalf("%d changes sent, want 1", len(*changes))
	}
	again := rrcsRun(t, "import", f.addr(), "--file", file, "--write-to", f.addr())
	rrcsWant(t, again, "applied 0,")
	if len(*changes) != 1 {
		t.Errorf("a second import sent a change again")
	}
}

// RRCS accepts and the value does not move: failed, not applied.
func TestRRCSImportNotTaken(t *testing.T) {
	f, _ := rrcsSetFake(t, false)
	file := filepath.Join(t.TempDir(), "x.csv")
	rrcsRun(t, "export", f.addr(), "--out", file, "--path", "port.7.out")
	rrcsEditCSV(t, file, "net.1.node.61.port.7.out.PortAes67Output.Multicast", "239.9.9.9", ',')
	out, err := rrcsStdout(t, func() error {
		return runRRCS(context.Background(), []string{"import", f.addr(), "--file", file, "--output", "json", "--write-to", f.addr()})
	})
	if err == nil || !strings.Contains(out, `"result":"not_taken"`) || !strings.Contains(out, `"failed":1`) {
		t.Errorf("%v\n%s", err, out)
	}
}

func TestRRCSImportSkipsAndRefusals(t *testing.T) {
	f, changes := rrcsSetFake(t, true)
	dir := t.TempDir()
	file := filepath.Join(dir, "hand.csv")
	hand := "path,value,value_name\n" +
		"net.1.node.61.port.7.out.PortAes67Output.Protocol,,NMOS\n" + // the word alone
		"net.1.node.61.port.7.out.PortAes67Output.MulticastPort,abc,\n" + // not a number
		"net.1.node.61.port.999.PortAes67Output.Multicast,239.1.1.1,\n" + // no such port
		"net.1.node.61.port.7.out.PortAes67Output.Channels,1,\n" + // unchanged
		"net.1.node.61.port.7.out.PortAes67Output.Channels,2,\n" + // changed
		"net.1.node.61.port.7.out.PortAes67Output.Channels,3,\n" + // given twice
		"net.1.node.61.port.1041.PortAes67Input.Protocol,,Telepathy\n" + // not a value
		"just-text,1,\n"
	_ = os.WriteFile(file, []byte(hand), 0o644)
	out := rrcsRun(t, "import", f.addr(), "--file", file, "--dry-run")
	rrcsWant(t, out,
		"would_apply net.1.node.61.port.7.out.PortAes67Output.Protocol", "2 -> 5",
		"would_apply net.1.node.61.port.7.out.PortAes67Output.Channels", "1 -> 2",
		"would apply 2, unchanged 1, skipped 5, failed 0",
		"bad_path (1)", "bad_value (2)", "duplicate (1)", "not_on_device (1)")
	// --path narrows the apply set.
	rrcsWant(t, rrcsRun(t, "import", f.addr(), "--file", file, "--dry-run", "--path", "Protocol"), "would apply 1,")
	if len(*changes) != 0 {
		t.Errorf("dry runs sent %d changes", len(*changes))
	}

	ctx := context.Background()
	var val *consumer.ValidationError
	for name, args := range map[string][]string{
		"no host":       {"import", "--file", file},
		"no file":       {"import", "h"},
		"export format": {"export", "h", "--format", "yaml"},
	} {
		if err := runRRCS(ctx, args); !errors.As(err, &val) {
			t.Errorf("%s: got %v, want a validation error", name, err)
		}
	}
	noPath := filepath.Join(dir, "nopath.csv")
	_ = os.WriteFile(noPath, []byte("label,value\nx,1\n"), 0o644)
	noValue := filepath.Join(dir, "novalue.csv")
	_ = os.WriteFile(noValue, []byte("path,label\nx,1\n"), 0o644)
	empty := filepath.Join(dir, "empty.csv")
	_ = os.WriteFile(empty, nil, 0o644)
	badJSON := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(badJSON, []byte("{"), 0o644)
	for name, args := range map[string][]string{
		"missing file": {"import", f.addr(), "--file", filepath.Join(dir, "none.csv")},
		"no path col":  {"import", f.addr(), "--file", noPath},
		"no value col": {"import", f.addr(), "--file", noValue},
		"empty file":   {"import", f.addr(), "--file", empty},
		"bad json":     {"import", f.addr(), "--file", badJSON},
		"dead gateway": {"import", "127.0.0.1:1", "--file", file, "--timeout", "300ms"},
		"bad out":      {"export", f.addr(), "--out", filepath.Join(dir, "no", "such", "dir", "x.csv")},
	} {
		if args[0] == "import" {
			args = append(args, "--dry-run")
		}
		if err := runRRCS(ctx, args); err == nil || errors.As(err, &val) {
			t.Errorf("%s: got %v, want a runtime error", name, err)
		}
	}
}

// A JSON export imports the same way.
func TestRRCSImportJSON(t *testing.T) {
	f, changes := rrcsSetFake(t, true)
	file := filepath.Join(t.TempDir(), "x.json")
	rows := []rrcsRow{{Path: "net.1.node.61.port.7.out.PortAes67Output.Multicast", Value: "239.9.9.9"}}
	raw, _ := json.Marshal(rows)
	_ = os.WriteFile(file, raw, 0o644)
	rrcsWant(t, rrcsRun(t, "import", f.addr(), "--file", file, "--write-to", f.addr()), "applied 1,")
	if len(*changes) != 1 {
		t.Errorf("%d changes", len(*changes))
	}
}

// The export carries, under each panel, every key and the command on it,
// each with its own object ID.
func TestRRCSExportKeysAndCommands(t *testing.T) {
	f := newRRCSFake(t, func(call codec.Call) (codec.Value, bool) {
		if call.Method == "GetAllKeyConfiguration" {
			key := func(n int32, id int32, label string) codec.Value {
				return codec.Struct(
					rrcsMember("KeyPosition", codec.Struct(rrcsMember("ExpansionPanel", codec.Int(0)), rrcsMember("KeyNumber", codec.Int(n)),
						rrcsMember("Page", codec.Int(1)), rrcsMember("PositionType", codec.String("key")))),
					rrcsMember("KeyProperties", codec.Struct(rrcsMember("KeyMode", codec.Int(2)), rrcsMember("LabelValue", codec.String(label)),
						rrcsMember("ObjectID", codec.Int(id)))))
			}
			return codec.Struct(rrcsMember("TransKey", call.Params[0]), rrcsMember("KeyList", codec.Array(key(5, 901, "GRP ALF"), key(6, 902, "")))), true
		}
		return rrcsTreeAnswer(call)
	})
	file := filepath.Join(t.TempDir(), "x.csv")
	rrcsRun(t, "export", f.addr(), "--out", file, "--path", "node.61.port.1026")
	header, rows := rrcsReadExport(t, file)
	col := map[string]int{}
	for i, h := range header {
		col[h] = i
	}
	// The key itself, assigned or not.
	label := rows["net.1.node.61.port.1026.key.0.1.5.LabelValue"]
	if label == nil || label[col["oid"]] != "901" || label[col["value"]] != "GRP ALF" || label[col["access"]] != "R--" {
		t.Errorf("key label row %v", label)
	}
	if empty := rows["net.1.node.61.port.1026.key.0.1.6.KeyMode"]; empty == nil || empty[col["oid"]] != "902" {
		t.Errorf("an unassigned key is missing: %v", empty)
	}
	// The command on it, with the object ID of the command and its target.
	typ := rows["net.1.node.61.port.1026.key.0.1.5.cmd.CommandType"]
	if typ == nil || typ[col["oid"]] != "601" || typ[col["value"]] != "call-to-group" {
		t.Errorf("command row %v", typ)
	}
	if target := rows["net.1.node.61.port.1026.key.0.1.5.cmd.Target"]; target == nil || target[col["value"]] != "group.200" {
		t.Errorf("target row %v", target)
	}
	if target := rows["net.1.node.61.port.1026.key.0.1.1.cmd.Target"]; target == nil || target[col["value"]] != "net.1.node.61.port.7.out" {
		t.Errorf("port target row %v", target)
	}
	// An import of that file changes nothing: none of it is writable.
	rrcsWant(t, rrcsRun(t, "import", f.addr(), "--file", file, "--dry-run"), "would apply 0,")
}
