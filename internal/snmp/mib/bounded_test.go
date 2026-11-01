package mib

import (
	"bytes"
	"strings"
	"testing"

	"dhs/internal/snmp/codec"
)

func parsed(t *testing.T, rows string) *Table {
	t.Helper()
	tb, err := ParseTable(bytes.NewReader(gz(t, TableHeader+"\n"+rows)))
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

// row is one table line, with only what the rule reads.
func row(oid, name, base, index, max string) string {
	return strings.Join([]string{oid, name, "M", "object-type", base, "", "read-only", "", index, max}, "\t") + "\n"
}

// The DR5000 is the one table the rule was written from. Derived from its
// compiled table, it finds the two tables the device pads, and no others;
// no device name is involved.
func TestTheRuleFindsTheFilledTablesOfTheDR5000(t *testing.T) {
	dr5000, err := Load("ateme-dr5000")
	if err != nil {
		t.Fatal(err)
	}
	got := dr5000.Bounded()
	if len(got) != 2 {
		t.Fatalf("%d tables: %+v", len(got), got)
	}
	stream, sub := got[0], got[1]
	if stream.Table.String() != "1.3.6.1.4.1.27338.5.5.1.5.2.1.1" || stream.Count.String() != "1.3.6.1.4.1.27338.5.5.1.5.1.1.12" {
		t.Errorf("%+v", stream)
	}
	if sub.Table.String() != "1.3.6.1.4.1.27338.5.5.1.5.2.2.1.1" || sub.Count.String() != "1.3.6.1.4.1.27338.5.5.1.5.2.1.1.10" {
		t.Errorf("%+v", sub)
	}
	if len(stream.Columns) != 12 || len(sub.Columns) != 5 {
		t.Errorf("columns %d %d", len(stream.Columns), len(sub.Columns))
	}
	// RowIndex states 1..2147483647, which is no limit: the row bound holds.
	if stream.Max != MaxRows {
		t.Errorf("max %d", stream.Max)
	}
	index := 0
	for _, c := range sub.Columns {
		if c.Index {
			index++
		}
	}
	if index != 3 {
		t.Errorf("%d index columns in the subtitle table", index)
	}
}

// A device with no such table walks as it always did, and the answer is
// remembered for the rest of the run.
func TestATableWithNoCountHasNoBoundedTables(t *testing.T) {
	if got := Base().Bounded(); got != nil {
		t.Errorf("%+v", got)
	}
}

func TestEveryTableDerives(t *testing.T) {
	for _, name := range TableNames() {
		tb, err := Load(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range tb.Bounded() {
			if b.Max <= 0 || b.Max > MaxRows || len(b.Columns) == 0 {
				t.Errorf("%s: %+v", name, b)
			}
		}
	}
}

// The rule on a small tree, one line per way to match or miss.
func TestTheRule(t *testing.T) {
	const parent = "1.3.9.1.1\tportEntry\tM\tobject-type\t\t\tnot-accessible\t\tportIndex\t\n"
	child := func(index string) string {
		return "1.3.9.2.1\tportLaneEntry\tM\tobject-type\t\t\tnot-accessible\t\t" + index + "\t\n" +
			row("1.3.9.2.1.1", "portLaneIndex", "Integer32", "", "16") +
			row("1.3.9.2.1.2", "portLaneValue", "Integer32", "", "")
	}
	const both = "portIndex,portLaneIndex"
	for _, tc := range []struct {
		name string
		rows string
		want string // the count column, empty for no match
	}{
		{"stem + Count", parent + row("1.3.9.1.1.5", "portLaneCount", "Integer32", "", "") + child(both), "1.3.9.1.1.5"},
		{"N + name", parent + row("1.3.9.1.1.5", "portNLane", "Gauge32", "", "") + child(both), "1.3.9.1.1.5"},
		{"a name that counts nothing", parent + row("1.3.9.1.1.5", "portSpeed", "Integer32", "", "") + child(both), ""},
		{"a count of something else", parent + row("1.3.9.1.1.5", "portOtherCount", "Integer32", "", "") + child(both), ""},
		{"not an integer", parent + row("1.3.9.1.1.5", "portLaneCount", "DisplayString", "", "") + child(both), ""},
		{"an index that is not the parent's", parent + row("1.3.9.1.1.5", "portLaneCount", "Integer32", "", "") + child("other,portLaneIndex"), ""},
		{"two more indexes than the parent", parent + row("1.3.9.1.1.5", "portLaneCount", "Integer32", "", "") + child("portIndex,x,portLaneIndex"), ""},
		{"one index: nothing above it", parent + row("1.3.9.1.1.5", "portLaneCount", "Integer32", "", "") + child("portLaneIndex"), ""},
		{"index objects declared again in each table",
			"1.3.9.1.1\tportEntry\tM\tobject-type\t\t\tnot-accessible\t\tportSlotIdx\t\n" +
				row("1.3.9.1.1.5", "portLaneCount", "Integer32", "", "") +
				"1.3.9.2.1\tportLaneEntry\tM\tobject-type\t\t\tnot-accessible\t\tportLaneSlotIdx,portLaneLaneIdx\t\n" +
				row("1.3.9.2.1.2", "portLaneValue", "Integer32", "", ""), "1.3.9.1.1.5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := parsed(t, tc.rows).Bounded()
			switch {
			case tc.want == "" && len(got) != 0:
				t.Errorf("%+v", got)
			case tc.want != "" && (len(got) != 1 || got[0].Count.String() != tc.want):
				t.Errorf("%+v", got)
			}
		})
	}
}

func TestAnEnumeratedColumnCountsNothing(t *testing.T) {
	tb := parsed(t, "1.3.9.1.1\tportEntry\tM\tobject-type\t\t\tnot-accessible\t\tportIndex\t\n"+
		"1.3.9.1.1.5\tportLaneCount\tM\tobject-type\tINTEGER\t\tread-only\tone=1\t\t\n"+
		"1.3.9.2.1\tportLaneEntry\tM\tobject-type\t\t\tnot-accessible\t\tportIndex,portLaneIndex\t\n"+
		row("1.3.9.2.1.2", "portLaneValue", "Integer32", "", ""))
	if got := tb.Bounded(); got != nil {
		t.Errorf("%+v", got)
	}
}

// An entry that is its parent's whole name has no rest to count.
func TestAParentWithTheTablesOwnNameCountsNothing(t *testing.T) {
	tb := parsed(t, "1.3.9.1.1\tportEntry\tM\tobject-type\t\t\tnot-accessible\t\tportIndex\t\n"+
		row("1.3.9.1.1.5", "portCount", "Integer32", "", "")+
		"1.3.9.2.1\tportEntry\tN\tobject-type\t\t\tnot-accessible\t\tportIndex,y\t\n"+
		row("1.3.9.2.1.2", "portValue", "Integer32", "", ""))
	if got := tb.Bounded(); got != nil {
		t.Errorf("%+v", got)
	}
}

// Two modules naming one OID are one column.
func TestARowTwoModulesNameIsOneRow(t *testing.T) {
	tb := parsed(t, "1.3.9.1.1\tportEntry\tM\tobject-type\t\t\tnot-accessible\t\tportIndex\t\n"+
		row("1.3.9.1.1.5", "portLaneCount", "Integer32", "", "")+
		row("1.3.9.1.1.5", "portLaneTally", "Integer32", "", "")+
		"1.3.9.2.1\tportLaneEntry\tM\tobject-type\t\t\tnot-accessible\t\tportIndex,portLaneIndex\t\n"+
		row("1.3.9.2.1.2", "portLaneValue", "Integer32", "", ""))
	if got := tb.Bounded(); len(got) != 1 {
		t.Errorf("%+v", got)
	}
}

// A table whose every column is an index cannot show a row is empty.
func TestAnIndexOnlyTableCannotBeProven(t *testing.T) {
	tb := parsed(t, "1.3.9.1.1\tportEntry\tM\tobject-type\t\t\tnot-accessible\t\tportIndex\t\n"+
		row("1.3.9.1.1.5", "portLaneCount", "Integer32", "", "")+
		"1.3.9.2.1\tportLaneEntry\tM\tobject-type\t\t\tnot-accessible\t\tportIndex,portLaneIndex\t\n"+
		row("1.3.9.2.1.1", "portLaneIndex", "Integer32", "", ""))
	if got := tb.Bounded(); got != nil {
		t.Errorf("%+v", got)
	}
}

func TestTheRowLimitComesFromTheIndexRange(t *testing.T) {
	tb := parsed(t, "1.3.9.1.1\tportEntry\tM\tobject-type\t\t\tnot-accessible\t\tportIndex\t\n"+
		row("1.3.9.1.1.5", "portLaneCount", "Integer32", "", "")+
		"1.3.9.2.1\tportLaneEntry\tM\tobject-type\t\t\tnot-accessible\t\tportIndex,portLaneIndex\t\n"+
		row("1.3.9.2.1.1", "portLaneIndex", "Integer32", "", "16")+
		"1.3.9.2.1.2\tportLaneState\tM\tobject-type\tINTEGER\t\tread-only\tunknown=4;up=1\t\t\n")
	got := tb.Bounded()
	if len(got) != 1 || got[0].Max != 16 {
		t.Fatalf("%+v", got)
	}
	if !got[0].Columns[0].Index || got[0].Columns[1].Index || len(got[0].Columns[1].Unknown) != 1 || got[0].Columns[1].Unknown[0] != 4 {
		t.Errorf("%+v", got[0].Columns)
	}
}

func TestAddress(t *testing.T) {
	inet := &Object{TC: "InetAddress"}
	for _, tc := range []struct {
		in   []byte
		want string
		ok   bool
	}{
		{[]byte{10, 6, 255, 114}, "10.6.255.114", true},
		{[]byte("host"), "104.111.115.116", true}, // four bytes is an address
		{append(bytes.Repeat([]byte{0}, 15), 1), "::1", true},
		{[]byte{10, 0, 0, 1, 0, 0, 0, 3}, "10.0.0.1%3", true},
		{append(append(bytes.Repeat([]byte{0}, 15), 1), 0, 0, 0, 7), "::1%7", true},
		{[]byte("a.example"), "", false},        // nine bytes: no address has them
		{[]byte("sub.example.test"), "", false}, // sixteen printable bytes: a name
		{[]byte{1, 2, 3}, "", false},            // three bytes are nothing
		{nil, "", false},
	} {
		got, ok := inet.Address(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%v = %q %v, want %q %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := (&Object{TC: "DisplayString"}).Address([]byte{1, 2, 3, 4}); ok {
		t.Error("a string is not an address")
	}
	if got, ok := (&Object{Base: "IpAddress"}).Address([]byte{1, 2, 3, 4}); !ok || got != "1.2.3.4" {
		t.Errorf("%q", got)
	}
	if got, ok := (&Object{TC: "Ipv6Address"}).Address(append(bytes.Repeat([]byte{0}, 15), 1)); !ok || got != "::1" {
		t.Errorf("%q", got)
	}
}

func TestAddressBytes(t *testing.T) {
	inet := &Object{TC: "InetAddress"}
	for _, tc := range []struct {
		in   string
		want []byte
		ok   bool
	}{
		{"10.0.0.1", []byte{10, 0, 0, 1}, true},
		{"10.0.0.1%3", []byte{10, 0, 0, 1, 0, 0, 0, 3}, true},
		{"example.test", nil, false},
		{"10.0.0.1%x", nil, false},
	} {
		got, ok := inet.AddressBytes(tc.in)
		if ok != tc.ok || !bytes.Equal(got, tc.want) {
			t.Errorf("%q = %v %v", tc.in, got, ok)
		}
	}
	if _, ok := (&Object{}).AddressBytes("10.0.0.1"); ok {
		t.Error("not an address object")
	}
}

func TestEnterprise(t *testing.T) {
	for number, want := range map[uint32]string{
		30065: "Arista Networks",
		27338: "ATEME",
		7995:  "Snell & Wilcox",
		54981: "BY-SYSTEMS SPRL",
	} {
		if got, ok := Enterprise(number); !ok || !strings.HasPrefix(got, want) {
			t.Errorf("%d = %q %v", number, got, ok)
		}
	}
	if _, ok := Enterprise(4294967295); ok {
		t.Error("no such number")
	}
	if n, ok := EnterpriseOf(codec.MustParseOID("1.3.6.1.4.1.30065.1.3011")); !ok || n != 30065 {
		t.Errorf("%d %v", n, ok)
	}
	if _, ok := EnterpriseOf(Enterprises); ok {
		t.Error("enterprises itself names nobody")
	}
	if _, ok := EnterpriseOf(Mgmt2); ok {
		t.Error("a standard OID names nobody")
	}
}

func TestABrokenRegistryIsABadCommit(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("must panic")
		}
	}()
	mustEnterprises([]byte("not gzip"))
}

func TestParseEnterprisesRefusals(t *testing.T) {
	long := EnterpriseHeader + "x\n1\t" + strings.Repeat("x", 2<<20) + "\n"
	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"not gzip", []byte("text"), "enterprises:"},
		{"no header", gz(t, "1\tx\n"), "first line"},
		{"a bad number", gz(t, EnterpriseHeader+"x\nnope\ty\n"), "line 2"},
		{"a line too long", gz(t, long), "too long"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseEnterprises(bytes.NewReader(tc.in)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAnAristaIsSelectedByItsEnterprise(t *testing.T) {
	if name, ok := ForObjectID(codec.MustParseOID("1.3.6.1.4.1.30065.1.3011.7020.1964.48")); !ok || name != "arista-eos" {
		t.Errorf("%q %v", name, ok)
	}
}

// Every table a sysObjectID selects ships in this binary: a recompile that
// lost one fails here.
func TestEveryTableTheObjectIDsNameIsShipped(t *testing.T) {
	for _, e := range byObjectID {
		if _, err := Load(e.table); err != nil {
			t.Error(err)
		}
	}
}
