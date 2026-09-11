package mib

// The compiled table, read two ways: a small hand-written one that pins
// each rule, and the embedded one checked against OIDs the plant's own
// devices answered with (docs/testbed.md).

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const smallRows = TableHeader + "\n" +
	"1.3.6.1.4.1.1773\ttandberg\tTANDBERG-SMI\tobject-identifier\t\t\t\t\t\n" +
	"1.3.6.1.4.1.1773.1.3.200.1.11\tcontrolMode\tETV-RX1290-MIB\tobject-type\tINTEGER\t\tread-write\tfp=1;snmp=4\t\n" +
	"1.3.6.1.4.1.1773.1.3.200.2.6.4\tuserAudio3\tETV-RX1290-MIB\tobject-identifier\t\t\t\t\t\n" +
	"1.3.6.1.4.1.1773.1.3.200.2.6.4\tuserLsdPid\tETV-TT1260-MIB\tobject-type\tINTEGER\tPIDNumber\tread-only\t\t\n" +
	"1.3.6.1.4.1.7995.1.3.1.389.1.1\tiqmux42Entry\tSNELL-IQMUX42-CMD\tobject-type\t\t\tnot-accessible\t\tslot,param\n" +
	"1.3.6.1.9.1\tshared\tA-MIB\tobject-type\tINTEGER\t\tread-only\t\t\n" +
	"1.3.6.1.9.2\tshared\tB-MIB\tobject-type\tINTEGER\t\tread-only\t\t\n"

func small(t *testing.T) *Table {
	t.Helper()
	tb, err := ParseTable(bytes.NewReader(gz(t, smallRows)))
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func TestParseTable(t *testing.T) {
	tb := small(t)
	if tb.Len() != 7 {
		t.Fatalf("Len = %d", tb.Len())
	}
	obj, rest := tb.Lookup(oid("1.3.6.1.4.1.1773.1.3.200.1.11.0"))
	if obj == nil || obj.Name != "controlMode" || rest.String() != "0" {
		t.Fatalf("Lookup = %+v %v", obj, rest)
	}
	if obj.Access != "read-write" || obj.Base != "INTEGER" || len(obj.Enums) != 2 {
		t.Errorf("controlMode = %+v", obj)
	}
	entry, _ := tb.Lookup(oid("1.3.6.1.4.1.7995.1.3.1.389.1.1"))
	if strings.Join(entry.Index, ",") != "slot,param" || entry.Kind != "object-type" {
		t.Errorf("entry = %+v", entry)
	}
	// Rows share one copy of a module name.
	a, _ := tb.Lookup(oid("1.3.6.1.4.1.1773.1.3.200.1.11"))
	b, _ := tb.Lookup(oid("1.3.6.1.4.1.1773.1.3.200.2.6.4"))
	if a.Module != b.Module {
		t.Errorf("modules = %q %q", a.Module, b.Module)
	}
}

func TestParseTableRefusals(t *testing.T) {
	long := TableHeader + "\n" + strings.Repeat("x", 2<<20) + "\n"
	for _, tc := range []struct {
		name string
		in   []byte
		want string
	}{
		{"not gzip", []byte("plain text"), "table:"},
		{"empty", gz(t, ""), "first line"},
		{"another layout", gz(t, "# dhs-snmp-mib-table v2\n"), "first line"},
		{"a short row", gz(t, TableHeader+"\n1.3\tx\n"), "line 2: 2 columns"},
		{"a bad OID", gz(t, TableHeader+"\nnope\tx\tM\tk\t\t\t\t\t\n"), "line 2"},
		{"an enum with no number", gz(t, TableHeader+"\n1.3\tx\tM\tk\t\t\t\ta=b\t\n"), `"a=b" has no number`},
		{"a row longer than any real one", gz(t, long), "too long"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseTable(bytes.NewReader(tc.in)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want %q", err, tc.want)
			}
		})
	}
}

// Two devices, one OID, two names: the default is the compiler's first,
// and a caller that knows the device gets the device's.
func TestPreferPicksTheDevicesName(t *testing.T) {
	tb := small(t)
	o := oid("1.3.6.1.4.1.1773.1.3.200.2.6.4.0")
	if got := tb.Name(o); got != "userAudio3.0" {
		t.Errorf("default = %q", got)
	}
	if got := tb.Name(o, "ETV-TT1260-MIB"); got != "userLsdPid.0" {
		t.Errorf("TT1260 = %q", got)
	}
	if got := tb.Name(o, "NOT-LOADED", "ETV-TT1260-MIB"); got != "userLsdPid.0" {
		t.Errorf("the first preference that has a row wins: %q", got)
	}
	if got := tb.Name(o, "NOT-LOADED"); got != "userAudio3.0" {
		t.Errorf("a preference with no row falls back: %q", got)
	}
}

func TestNameFallsBack(t *testing.T) {
	tb := small(t)
	if got := tb.Name(oid("1.3.6.1.4.1.1773.9")); got != "tandberg.9" {
		t.Errorf("= %q", got)
	}
	if got := tb.Name(oid("1.3.6.1.4.1.1773")); got != "tandberg" {
		t.Errorf("an exact name has no suffix: %q", got)
	}
	if got := tb.Name(oid("1.3.6.1.5")); got != "1.3.6.1.5" {
		t.Errorf("= %q, want the dotted form", got)
	}
	if obj, rest := tb.Lookup(oid("1.3.6.1.5")); obj != nil || rest != nil {
		t.Errorf("Lookup = %+v %v", obj, rest)
	}
}

func TestTableResolve(t *testing.T) {
	tb := small(t)
	for _, tc := range []struct{ in, want string }{
		{"controlMode", "1.3.6.1.4.1.1773.1.3.200.1.11"},
		{"controlMode.0", "1.3.6.1.4.1.1773.1.3.200.1.11.0"},
		{"iqmux42Entry.32774.11", "1.3.6.1.4.1.7995.1.3.1.389.1.1.32774.11"},
		{"ETV-TT1260-MIB::userLsdPid.0", "1.3.6.1.4.1.1773.1.3.200.2.6.4.0"},
		{"B-MIB::shared.3", "1.3.6.1.9.2.3"},
	} {
		got, err := tb.Resolve(tc.in)
		if err != nil || got.String() != tc.want {
			t.Errorf("Resolve(%q) = %s, %v; want %s", tc.in, got, err, tc.want)
		}
	}
	for _, tc := range []struct{ in, want string }{
		{"nothing", "not a standard name"},
		{"A-MIB::controlMode", "not a standard name"},
		{"shared", "A-MIB::shared, B-MIB::shared"},
		{"controlMode.x", `index arc "x"`},
	} {
		if _, err := tb.Resolve(tc.in); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Resolve(%q) = %v, want %q", tc.in, err, tc.want)
		}
	}
}

func TestEnumName(t *testing.T) {
	obj, _ := small(t).Lookup(oid("1.3.6.1.4.1.1773.1.3.200.1.11.0"))
	if n, ok := obj.EnumName(4); !ok || n != "snmp" {
		t.Errorf("4 = %q %v", n, ok)
	}
	if _, ok := obj.EnumName(9); ok {
		t.Error("a value the MIB does not name has no label")
	}
}

func TestABadEmbeddedTablePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a table this binary cannot read must panic")
		}
	}()
	mustLoad([]byte("not a table"))
}

// The embedded table, against what the plant's devices actually answered:
// the Snell frame at 10.6.255.113 (IQMUX42 in slot 11) and the IRDs'
// control gate. These OIDs came off the wire, so a regenerated table that
// renames them has broken something an operator reads.
func TestTheEmbeddedTableNamesThePlant(t *testing.T) {
	tb := Compiled()
	if tb.Len() < 70000 {
		t.Fatalf("only %d rows; was the table regenerated from a partial set?", tb.Len())
	}
	for _, tc := range []struct{ oid, want string }{
		// The column is .32774; the arc after it is the slot.
		{"1.3.6.1.4.1.7995.1.3.1.389.1.1.32774.11", "iqmux42SetupSerialNumber.11"},
		{"1.3.6.1.4.1.7995.1.3.1.562.1.1.32769.1", "iqdbe00AudioSetupFirmware.1"},
		{"1.3.6.1.4.1.1773.1.3.200.1.11.0", "controlMode.0"},
		{"1.3.6.1.4.1.7995.1.3.1.532", "moduleIQDMX31"},
	} {
		if got := Name(oid(tc.oid)); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.oid, got, tc.want)
		}
	}
	if got := tb.Name(oid("1.3.6.1.4.1.1773.1.3.200.2.6.4.0"), "ETV-TT1260-MIB"); got != "userLsdPid.0" {
		t.Errorf("the TT1260's name for .2.6.4 = %q", got)
	}
	got, err := Resolve("controlMode.0")
	if err != nil || got.String() != "1.3.6.1.4.1.1773.1.3.200.1.11.0" {
		t.Errorf("Resolve(controlMode.0) = %s, %v", got, err)
	}
}

func TestDescribe(t *testing.T) {
	name, obj := Describe(SysDescr)
	if name != "sysDescr.0" || obj == nil || obj.Base != "OCTET STRING" {
		t.Errorf("sysDescr = %q %+v", name, obj)
	}
	name, obj = Describe(oid("1.3.6.1.4.1.1773.1.3.200.1.11.0"))
	if l, _ := obj.EnumName(4); name != "controlMode.0" || l != "snmp" {
		t.Errorf("controlMode = %q, label %q", name, l)
	}
	if name, obj := Describe(oid("2.999.1")); name != "2.999.1" || obj != nil {
		t.Errorf("uncovered = %q %+v", name, obj)
	}
}

func TestResolveRoutesByForm(t *testing.T) {
	if got, err := Resolve(".1.3.6"); err != nil || got.String() != "1.3.6" {
		t.Errorf("leading dot = %s %v", got, err)
	}
	if _, err := Resolve(""); err == nil {
		t.Error("an empty OID must be refused")
	}
}
