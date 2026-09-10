package main

// One output line of get/set/walk: the name from the compiled MIBs, and
// an enumerated INTEGER shown as its label with the number.

import (
	"bytes"
	"testing"

	"dhs/internal/snmp/codec"
)

func TestWriteBind(t *testing.T) {
	controlMode := codec.MustParseOID("1.3.6.1.4.1.1773.1.3.200.1.11.0")
	shared := codec.MustParseOID("1.3.6.1.4.1.1773.1.3.200.2.6.4.0")
	for _, tc := range []struct {
		name   string
		vb     codec.VarBind
		prefer []string
		want   string
	}{
		{"an enumerated value gets its label", codec.VarBind{Name: controlMode, Value: codec.Int(4)}, nil,
			"controlMode.0\tINTEGER\tsnmp(4)\n"},
		{"a value the MIB does not name stays a number", codec.VarBind{Name: controlMode, Value: codec.Int(9)}, nil,
			"controlMode.0\tINTEGER\t9\n"},
		{"a string is never labelled", codec.VarBind{Name: controlMode, Value: codec.String("4")}, nil,
			"controlMode.0\tOCTET STRING\t4\n"},
		{"--mib picks the device's name", codec.VarBind{Name: shared, Value: codec.Int(1)}, []string{"ETV-TT1260-MIB"},
			"userLsdPid.0\tINTEGER\t1\n"},
		{"nothing covers it", codec.VarBind{Name: codec.MustParseOID("2.999.1"), Value: codec.Int(1)}, nil,
			"2.999.1\tINTEGER\t1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			writeBind(&b, tc.vb, tc.prefer)
			if b.String() != tc.want {
				t.Errorf("= %q, want %q", b.String(), tc.want)
			}
		})
	}
}

func TestSNMPFlagsModules(t *testing.T) {
	f := snmpFlags{prefer: " ETV-TT1260-MIB, ,SNELL-IQMUX42-CMD "}
	got := f.modules()
	if len(got) != 2 || got[0] != "ETV-TT1260-MIB" || got[1] != "SNELL-IQMUX42-CMD" {
		t.Errorf("= %q", got)
	}
	if (&snmpFlags{}).modules() != nil {
		t.Error("no --mib is no preference")
	}
}
