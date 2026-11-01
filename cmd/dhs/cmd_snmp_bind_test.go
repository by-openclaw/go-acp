package main

// One output line of get/set/walk: the name from the compiled MIBs, and
// an enumerated INTEGER shown as its label with the number.

import (
	"bytes"
	"context"
	"testing"

	"dhs/internal/consumer/compliance"
	"dhs/internal/snmp/codec"
	snmpcons "dhs/internal/snmp/consumer"
	"dhs/internal/snmp/mib"
)

func TestWriteBind(t *testing.T) {
	controlMode := codec.MustParseOID("1.3.6.1.4.1.1773.1.3.200.1.11.0")
	shared := codec.MustParseOID("1.3.6.1.4.1.1773.1.3.200.2.6.4.0")
	for _, tc := range []struct {
		name  string
		vb    codec.VarBind
		table string
		want  string
	}{
		{"an enumerated value gets its label", codec.VarBind{Name: controlMode, Value: codec.Int(4)}, "ericsson-tt1260",
			"controlMode.0\tINTEGER\tsnmp(4)\n"},
		{"a value the MIB does not name stays a number", codec.VarBind{Name: controlMode, Value: codec.Int(9)}, "ericsson-tt1260",
			"controlMode.0\tINTEGER\t9\n"},
		{"a string is never labelled", codec.VarBind{Name: controlMode, Value: codec.String("4")}, "ericsson-tt1260",
			"controlMode.0\tOCTET STRING\t4\n"},
		{"the device's table names it", codec.VarBind{Name: shared, Value: codec.Int(1)}, "ericsson-tt1260",
			"userLsdPid.0\tINTEGER\t1\n"},
		{"nothing covers it", codec.VarBind{Name: codec.MustParseOID("2.999.1"), Value: codec.Int(1)}, "",
			"2.999.1\tINTEGER\t1\n"},
		// The agent's own identity, named from DHS-MIB.
		{"an OID value is named", codec.VarBind{Name: mib.SysObjectID, Value: codec.ObjectID(mib.DHSAgent)}, "",
			"sysObjectID.0\t" + codec.TypeOID.String() + "\tdhsAgent\n"},
		{"an OID value nothing names stays dotted", codec.VarBind{Name: mib.SysObjectID, Value: codec.ObjectID(codec.MustParseOID("2.999"))}, "",
			"sysObjectID.0\t" + codec.TypeOID.String() + "\t2.999\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			var table *mib.Table
			if tc.table != "" {
				var err error
				if table, err = mib.Load(tc.table); err != nil {
					t.Fatal(err)
				}
			}
			writeBind(&b, tc.vb, table)
			if b.String() != tc.want {
				t.Errorf("= %q, want %q", b.String(), tc.want)
			}
		})
	}
}

// A received notification is named from the MIBs, after its number.
func TestTrapLine(t *testing.T) {
	named := snmpcons.Trap{Version: codec.Version2c, Community: "public", TrapOID: mib.DHSTestNotification}
	if got, want := trapLine(named), named.String()+" dhsTestNotification"; got != want {
		t.Errorf("= %q, want %q", got, want)
	}
	unnamed := snmpcons.Trap{Version: codec.Version2c, Community: "public", TrapOID: codec.MustParseOID("2.999.1")}
	if got := trapLine(unnamed); got != unnamed.String() {
		t.Errorf("= %q, want the line unchanged", got)
	}
}

func TestSNMPTableRefusesAnUnknownName(t *testing.T) {
	f := snmpFlags{mibTable: "absent"}
	if _, err := f.table(context.Background(), nil, &compliance.Profile{}); err == nil {
		t.Error("an unknown --mib must be refused")
	}
}
