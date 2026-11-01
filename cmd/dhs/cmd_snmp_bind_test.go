package main

// One output line of get/set/walk: the name from the compiled MIBs, and
// an enumerated INTEGER shown as its label with the number.

import (
	"bytes"
	"context"
	"net"
	"strings"
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

// An address is shown as one, however the wire carried it: the syntax says.
func TestWriteBindShowsAnAddress(t *testing.T) {
	table, err := mib.Load("ateme-dr5000")
	if err != nil {
		t.Fatal(err)
	}
	name := codec.MustParseOID("1.3.6.1.4.1.27338.5.3.2.2.2.1.0") // InetAddress
	var b bytes.Buffer
	writeBind(&b, codec.VarBind{Name: name, Value: codec.Bytes([]byte{10, 6, 255, 114})}, table)
	if want := "dr5000ChannelConfigurationInputIpAddress.0\tOCTET STRING\t10.6.255.114\n"; b.String() != want {
		t.Errorf("%q", b.String())
	}
	b.Reset()
	writeBind(&b, codec.VarBind{Name: mib.SysDescr, Value: codec.String("box")}, table)
	if want := "sysDescr.0\tOCTET STRING\tbox\n"; b.String() != want {
		t.Errorf("%q", b.String())
	}
}

// A name after the host is what was asked for: get printed the system group
// over it, and walk the whole tree.
func TestANameAfterTheHostIsRead(t *testing.T) {
	agent := startTestAgent(t)
	out := captureStdout(t, func() {
		if err := runSNMPGet(context.Background(), []string{agent, "sysName.0"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "sysName.0") || strings.Contains(out, "sysDescr.0") {
		t.Errorf("get printed %q", out)
	}
	out = captureStdout(t, func() {
		if err := runSNMPWalk(context.Background(), []string{agent, "sysName.0"}); err != nil {
			t.Fatal(err)
		}
	})
	if strings.Contains(out, "sysDescr.0") {
		t.Errorf("walk printed %q", out)
	}
}

// startTestAgent answers every request with a system group: a GET with the
// names it was asked, a walk with sysName.0 and then the end of the tree.
func startTestAgent(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			req, err := codec.Decode(buf[:n])
			if err != nil {
				continue
			}
			var binds []codec.VarBind
			for _, vb := range req.PDU.VarBinds {
				switch {
				case req.PDU.Type != codec.PDUTypeGet && vb.Name.Compare(mib.SysName) < 0:
					binds = append(binds, codec.VarBind{Name: mib.SysName, Value: codec.String("box")})
				case req.PDU.Type != codec.PDUTypeGet:
					binds = append(binds, codec.VarBind{Name: vb.Name, Value: codec.EndOfMIBView()})
				case vb.Name.Compare(mib.SysObjectID) == 0:
					binds = append(binds, codec.VarBind{Name: vb.Name, Value: codec.ObjectID(mib.DHSAgent)})
				default:
					binds = append(binds, codec.VarBind{Name: vb.Name, Value: codec.String("box")})
				}
			}
			raw, err := codec.Encode(codec.Message{Version: req.Version, Community: req.Community,
				PDU: &codec.PDU{Type: codec.PDUTypeResponse, RequestID: req.PDU.RequestID, VarBinds: binds}})
			if err != nil {
				continue
			}
			_, _ = conn.WriteTo(raw, from)
		}
	}()
	return conn.LocalAddr().String()
}
