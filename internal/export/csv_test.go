package export

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"dhs/internal/consumer"
)

// csvRows parses WriteCSV output back with the stdlib reader and
// returns one name→cell map per data row, so assertions read by
// header name (the documented contract) rather than by position.
func csvRows(t *testing.T, data []byte) []map[string]string {
	t.Helper()
	recs, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(recs) == 0 {
		t.Fatal("no header row")
	}
	var rows []map[string]string
	for _, rec := range recs[1:] {
		m := map[string]string{}
		for i, h := range recs[0] {
			m[h] = rec[i]
		}
		rows = append(rows, m)
	}
	return rows
}

func kindsSnapshot() *Snapshot {
	return &Snapshot{
		Device:    DeviceInfo{IP: "10.6.250.101", Protocol: "acp1"},
		CreatedAt: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC),
		Slots: []SlotDump{{Slot: 1, Objects: []consumer.Object{
			{Path: []string{"control"}, ID: 1, Label: "Temp", Kind: consumer.KindInt, Access: 0x07, Unit: "C",
				Min: int64(-10), Max: int64(90), Step: int64(1), Def: int64(20),
				Value: consumer.Value{Kind: consumer.KindInt, Int: -3}},
			{Path: []string{"control"}, ID: 2, Label: "Count", Kind: consumer.KindUint, Access: 0x01,
				Min: uint64(0), Max: uint64(10),
				Value: consumer.Value{Kind: consumer.KindUint, Uint: 4}},
			{Path: []string{"control"}, ID: 3, Label: "Level", Kind: consumer.KindFloat, Access: 0x03,
				Min: float64(0.5), Max: float64(1.5),
				Value: consumer.Value{Kind: consumer.KindFloat, Float: 1.25}},
			{Path: []string{"control"}, ID: 4, Label: "Mode", Kind: consumer.KindEnum, Access: 0x03,
				EnumItems: []string{"Off", "On"}, Def: uint64(1),
				Value: consumer.Value{Kind: consumer.KindEnum, Enum: 1, Str: "On"}},
			{Path: []string{"identity"}, ID: 5, Label: "Card name", Kind: consumer.KindString, Access: 0x01, MaxLen: 8,
				Value: consumer.Value{Kind: consumer.KindString, Str: "RRS18"}},
			{Path: []string{"network"}, ID: 6, Label: "IP", Kind: consumer.KindIPAddr, Access: 0x01,
				Value: consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 6, 250, 101}}},
			{Path: []string{"control"}, ID: 7, Label: "Flag", Kind: consumer.KindBool, Access: 0x03,
				Value: consumer.Value{Kind: consumer.KindBool, Bool: true}},
			{Path: []string{"alarm"}, ID: 8, Label: "Fan", Kind: consumer.KindAlarm, Access: 0x01,
				AlarmPriority: 2, AlarmTag: 0x1F, AlarmOnMsg: "Fan failed", AlarmOffMsg: "Fan ok",
				Value: consumer.Value{Kind: consumer.KindAlarm}},
			{Path: []string{"frame"}, ID: 9, Label: "Slots", Kind: consumer.KindFrame, Access: 0x01,
				Value: consumer.Value{Kind: consumer.KindFrame, SlotStatus: []consumer.SlotStatus{consumer.SlotPresent, consumer.SlotError}}},
			{Path: []string{"ROOT_NODE_V2", "BOARD"}, ID: 10, Label: "BOARD", Kind: consumer.KindRaw},
			{Group: "misc", ID: 11, Label: "Odd", Kind: consumer.KindUnknown, Min: int(3)},
			{ID: 12, Label: "Orphan", Kind: consumer.KindString, Access: 0x01, Value: consumer.Value{Kind: consumer.KindString, Str: "x"}},
		}}},
	}
}

// TestWriteCSV_Cells — one row per value-bearing object, every column
// rendered the way the reader expects it back; containers (kind=raw)
// are tree structure, not rows.
func TestWriteCSV_Cells(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCSV(&buf, kindsSnapshot()); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	rows := csvRows(t, buf.Bytes())
	if len(rows) != 11 {
		t.Fatalf("rows got %d, want 11 (12 objects minus the raw container)", len(rows))
	}
	byLabel := map[string]map[string]string{}
	for _, r := range rows {
		byLabel[r["label"]] = r
		if r["ip"] != "10.6.250.101" || r["protocol"] != "acp1" || r["slot"] != "1" {
			t.Errorf("device columns wrong on %q: %v", r["label"], r)
		}
	}
	if _, ok := byLabel["BOARD"]; ok {
		t.Error("raw container must not become a row")
	}
	want := map[string]map[string]string{
		"Temp":      {"path": "control", "id": "1", "kind": "int", "access": "RWD", "value": "-3", "unit": "C", "min": "-10", "max": "90", "step": "1", "default": "20"},
		"Count":     {"kind": "uint", "access": "R--", "value": "4", "min": "0", "max": "10", "step": "", "default": ""},
		"Level":     {"kind": "float", "access": "RW-", "value": "1.25", "min": "0.5", "max": "1.5"},
		"Mode":      {"kind": "enum", "value": "1", "value_name": "On", "enum_items": "Off|On", "default": "1"},
		"Card name": {"path": "identity", "kind": "string", "value": "RRS18", "max_len": "8"},
		"IP":        {"kind": "ipaddr", "value": "10.6.250.101"},
		"Flag":      {"kind": "bool", "value": "", "value_name": ""},
		"Fan":       {"kind": "alarm", "value": "", "alarm_priority": "2", "alarm_tag": "0x1F", "alarm_on": "Fan failed", "alarm_off": "Fan ok"},
		"Slots":     {"kind": "frame", "value": "", "slot_status": "present|error"},
		"Odd":       {"path": "misc", "kind": "unknown", "access": "---", "min": "3", "alarm_priority": ""},
		"Orphan":    {"path": "", "kind": "string", "value": "x"},
	}
	// Group-less, path-less objects are still exported (bucketed under
	// "other" for ordering) — with an empty path cell, never an invented one.
	if last := rows[len(rows)-1]; last["label"] != "Orphan" {
		t.Errorf("group-less object must sort last (\"other\" bucket), got %q", last["label"])
	}
	for label, cells := range want {
		row, ok := byLabel[label]
		if !ok {
			t.Errorf("row %q missing", label)
			continue
		}
		for col, v := range cells {
			if row[col] != v {
				t.Errorf("%s.%s got %q, want %q", label, col, row[col], v)
			}
		}
	}
}

// TestWriteCSV_SinkErrors — the header, a row, and the final flush
// each report a dead sink with the failing row identified.
func TestWriteCSV_SinkErrors(t *testing.T) {
	t.Run("header when the sink already failed", func(t *testing.T) {
		orig := newCSVWriter
		newCSVWriter = func(w io.Writer) *csv.Writer {
			cw := csv.NewWriter(w)
			// Overflow the 4 KiB bufio buffer so the writer records the
			// sink failure before WriteCSV writes its first field.
			_ = cw.Write([]string{strings.Repeat("x", 5000)})
			return cw
		}
		t.Cleanup(func() { newCSVWriter = orig })
		err := WriteCSV(brokenSink{}, kindsSnapshot())
		if !errors.Is(err, errSink) || !strings.HasPrefix(err.Error(), "csv header:") {
			t.Errorf("got %v, want csv header: sink closed", err)
		}
	})
	t.Run("row that overflows the buffer names its slot and id", func(t *testing.T) {
		big := &Snapshot{Slots: []SlotDump{{Slot: 3}}}
		for i := 0; i < 40; i++ {
			big.Slots[0].Objects = append(big.Slots[0].Objects, consumer.Object{
				Path: []string{"g"}, ID: 100 + i, Label: strings.Repeat("L", 200), Kind: consumer.KindInt,
			})
		}
		err := WriteCSV(brokenSink{}, big)
		if !errors.Is(err, errSink) || !strings.HasPrefix(err.Error(), "csv row slot=3 id=") {
			t.Errorf("got %v, want csv row slot=3 id=N: sink closed", err)
		}
	})
	t.Run("final flush", func(t *testing.T) {
		err := WriteCSV(brokenSink{}, kindsSnapshot())
		if !errors.Is(err, errSink) {
			t.Errorf("got %v, want sink closed from the final flush", err)
		}
	})
}

// TestReadCSV_Columns — hand-written CSV covering the columns the
// writer's own fixtures do not: the legacy group column, a status
// column, alarm fields, max_len and a single-segment path that must
// repopulate Group for the ACP1 resolver.
func TestReadCSV_Columns(t *testing.T) {
	in := "ip,protocol,slot,status,group,id,label,kind,access,value,value_name,unit,min,max,step,default,enum_items,max_len,alarm_priority,alarm_tag,alarm_on,alarm_off\n" +
		"10.0.0.1,acp1,2,present,control,7,GainA,float,RWD,1.5,,dB,-20,20,0.5,0,,,,,,\n" +
		"10.0.0.1,acp1,2,present,control,4,Mode,enum,RW-,1,On,,,,,,Off|On,,,,,\n" +
		"10.0.0.1,acp1,2,present,identity,0,Card name,string,R--,RRS18,,,,,,,,8,,,,\n" +
		"10.0.0.1,acp1,2,present,alarm,9,Fan,alarm,R--,,,,,,,,,,2,0x1F,Fan failed,Fan ok\n" +
		"10.0.0.1,acp1,2,present,control,5,Count,uint,R--,42,,,0,100,,,,,,,,\n" +
		"10.0.0.1,acp1,2,present,,6,Loose,int,R--,1,,,,,,,,,,,,\n"
	snap, err := ReadCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	if snap.Device.IP != "10.0.0.1" || snap.Device.Protocol != "acp1" {
		t.Errorf("device header: %+v", snap.Device)
	}
	if len(snap.Slots) != 1 || snap.Slots[0].Slot != 2 || snap.Slots[0].Status != "present" {
		t.Fatalf("slots: %+v", snap.Slots)
	}
	objs := map[string]consumer.Object{}
	for _, o := range snap.Slots[0].Objects {
		objs[o.Label] = o
	}
	if len(objs) != 6 {
		t.Fatalf("objects got %d, want 6", len(objs))
	}
	g := objs["GainA"]
	if g.Group != "control" || len(g.Path) != 1 || g.Path[0] != "control" {
		t.Errorf("legacy group column must populate Group and Path: %+v", g)
	}
	if g.ID != 7 || g.Access != 0x07 || g.Unit != "dB" || g.Min != float64(-20) || g.Max != float64(20) || g.Step != float64(0.5) || g.Def != float64(0) {
		t.Errorf("GainA fields: %+v", g)
	}
	if g.Value.Kind != consumer.KindFloat || g.Value.Float != 1.5 {
		t.Errorf("GainA value: %+v", g.Value)
	}
	m := objs["Mode"]
	if len(m.EnumItems) != 2 || m.Value.Enum != 1 || m.Value.Str != "On" {
		t.Errorf("Mode: %+v", m)
	}
	if c := objs["Card name"]; c.MaxLen != 8 || c.Value.Str != "RRS18" {
		t.Errorf("Card name: %+v", c)
	}
	f := objs["Fan"]
	if f.Kind != consumer.KindAlarm || f.AlarmPriority != 2 || f.AlarmOnMsg != "Fan failed" || f.AlarmOffMsg != "Fan ok" {
		t.Errorf("Fan: %+v", f)
	}
	if c := objs["Count"]; c.Min != uint64(0) || c.Max != uint64(100) || c.Value.Uint != 42 {
		t.Errorf("Count: %+v", c)
	}
	if l := objs["Loose"]; l.Group != "" || l.Path != nil || l.Min != nil {
		t.Errorf("pathless row must stay pathless: %+v", l)
	}
}

// TestReadCSV_Malformed — an empty file has no header; a bare quote in
// a row is a CSV syntax error, each reported with its stage.
func TestReadCSV_Malformed(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty input has no header", "", "csv: read header"},
		{"bare quote inside a row", "ip,protocol,slot,path,id,label,kind,access,value\n10.0.0.1,acp1,1,control,7,Ga\"in,int,RW-,1\n", "csv: read row"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ReadCSV(strings.NewReader(c.in))
			if err == nil || !strings.HasPrefix(err.Error(), c.want) {
				t.Errorf("got %v, want prefix %q", err, c.want)
			}
		})
	}
}

// TestParseCSVValue — the (value, value_name) cell pair back into a
// typed Value. An unparseable cell must come back as KindUnknown so
// the importer refuses it instead of writing a zero.
func TestParseCSVValue(t *testing.T) {
	cases := []struct {
		name string
		kind consumer.ValueKind
		val  string
		want consumer.Value
	}{
		{"int", consumer.KindInt, "-3", consumer.Value{Kind: consumer.KindInt, Int: -3}},
		{"int garbage is unknown", consumer.KindInt, "x", consumer.Value{Kind: consumer.KindUnknown}},
		{"int empty keeps kind", consumer.KindInt, "", consumer.Value{Kind: consumer.KindInt}},
		{"uint", consumer.KindUint, "4", consumer.Value{Kind: consumer.KindUint, Uint: 4}},
		{"uint garbage is unknown", consumer.KindUint, "-1", consumer.Value{Kind: consumer.KindUnknown}},
		{"float", consumer.KindFloat, "1.25", consumer.Value{Kind: consumer.KindFloat, Float: 1.25}},
		{"float garbage is unknown", consumer.KindFloat, "x", consumer.Value{Kind: consumer.KindUnknown}},
		{"enum carries its name", consumer.KindEnum, "1", consumer.Value{Kind: consumer.KindEnum, Enum: 1, Str: "On"}},
		{"enum garbage is unknown", consumer.KindEnum, "On", consumer.Value{Kind: consumer.KindUnknown}},
		{"string verbatim", consumer.KindString, "x", consumer.Value{Kind: consumer.KindString, Str: "x"}},
		{"ipaddr dotted quad", consumer.KindIPAddr, "10.6.250.101", consumer.Value{Kind: consumer.KindIPAddr, IPAddr: [4]byte{10, 6, 250, 101}}},
		{"ipaddr short is unknown", consumer.KindIPAddr, "10.6", consumer.Value{Kind: consumer.KindUnknown}},
		{"ipaddr empty keeps kind", consumer.KindIPAddr, "", consumer.Value{Kind: consumer.KindIPAddr}},
		{"bool has no cell form", consumer.KindBool, "true", consumer.Value{Kind: consumer.KindBool}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseCSVValue(c.kind, c.val, "On", []string{"Off", "On"})
			if got.Kind != c.want.Kind || got.Int != c.want.Int || got.Uint != c.want.Uint ||
				got.Float != c.want.Float || got.Enum != c.want.Enum || got.Str != c.want.Str || got.IPAddr != c.want.IPAddr {
				t.Errorf("parseCSVValue(%v, %q) = %+v, want %+v", c.kind, c.val, got, c.want)
			}
		})
	}
}

// TestKindAndAccessSpelling — kind and access cells are the CLI's
// spelling, and the reader inverts them exactly.
func TestKindAndAccessSpelling(t *testing.T) {
	kinds := map[consumer.ValueKind]string{
		consumer.KindBool: "bool", consumer.KindInt: "int", consumer.KindUint: "uint", consumer.KindFloat: "float",
		consumer.KindEnum: "enum", consumer.KindString: "string", consumer.KindIPAddr: "ipaddr", consumer.KindAlarm: "alarm",
		consumer.KindFrame: "frame", consumer.KindRaw: "raw",
	}
	for k, name := range kinds {
		if got := kindName(k); got != name {
			t.Errorf("kindName(%v) = %q, want %q", k, got, name)
		}
		if got := parseValueKind(strings.ToUpper(name)); got != k {
			t.Errorf("parseValueKind(%q) = %v, want %v (case-insensitive)", name, got, k)
		}
	}
	if kindName(consumer.KindUnknown) != "unknown" || parseValueKind("unknown") != consumer.KindUnknown || parseValueKind("") != consumer.KindUnknown {
		t.Error("unknown kind must spell as \"unknown\" and never parse to a real kind")
	}
	for bits, s := range map[uint8]string{0: "---", 1: "R--", 2: "-W-", 4: "--D", 3: "RW-", 7: "RWD"} {
		if got := accessStr(bits); got != s {
			t.Errorf("accessStr(%d) = %q, want %q", bits, got, s)
		}
		if got := parseAccess(s); got != bits {
			t.Errorf("parseAccess(%q) = %d, want %d", s, got, bits)
		}
	}
}

// TestParseNum — numeric constraint cells take the Go type of their
// object's kind; non-numeric kinds carry no constraints.
func TestParseNum(t *testing.T) {
	cases := []struct {
		name string
		s    string
		kind consumer.ValueKind
		want any
	}{
		{"empty is absent", "", consumer.KindInt, nil},
		{"int", "-5", consumer.KindInt, int64(-5)},
		{"uint", "5", consumer.KindUint, uint64(5)},
		{"float", "0.5", consumer.KindFloat, float64(0.5)},
		{"string kind has no bounds", "5", consumer.KindString, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseNum(c.s, c.kind); got != c.want {
				t.Errorf("parseNum(%q, %v) = %#v, want %#v", c.s, c.kind, got, c.want)
			}
		})
	}
}
