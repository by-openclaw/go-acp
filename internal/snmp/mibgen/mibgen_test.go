package mibgen

// The writer's contract is that a manager's compiler accepts what it
// writes and reads back what was described. The round trip below proves
// the second with this repo's own compiler; tools on the Linux host
// (net-snmp's snmptranslate) check the first — see internal/snmp/CLAUDE.md.

import (
	"errors"
	"strings"
	"testing"
	"time"

	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/smi"
)

var (
	pen     = codec.MustParseOID("1.3.6.1.4.1.54981")
	product = pen.Append(1)
	agent   = product.Append(1)
	modOID  = pen.Append(2)
	objs    = modOID.Append(1)
)

func full() Module {
	return Module{
		Name: "TEST-MIB", Identity: "testMIB", OID: modOID,
		Organization: "Org", ContactInfo: "contact\nline two", Description: "A module.",
		Revisions: []Revision{
			{Date: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC), Description: "Second."},
			{Date: time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC), Description: "First."},
		},
		Branches: []Branch{
			{Name: "root", OID: pen},
			{Name: "products", OID: product, Description: "Products."},
			{Name: "agent", OID: agent, Description: "The agent."},
			{Name: "agentEvents", OID: agent.Append(0)},
			{Name: "things", OID: objs},
		},
		Objects: []Object{
			{Name: "tInt", OID: objs.Append(1), Syntax: codec.TypeInteger, Access: ReadWrite, Units: "dB", Description: "An integer."},
			{Name: "tStr", OID: objs.Append(2), Syntax: codec.TypeOctetString, Description: "A string."},
			{Name: "tOid", OID: objs.Append(3), Syntax: codec.TypeOID, Description: "An OID."},
			{Name: "tIP", OID: objs.Append(4), Syntax: codec.TypeIPAddress, Description: "An address."},
			{Name: "tC32", OID: objs.Append(5), Syntax: codec.TypeCounter32, Description: "A counter."},
			{Name: "tG32", OID: objs.Append(6), Syntax: codec.TypeGauge32, Description: "A gauge."},
			{Name: "tTicks", OID: objs.Append(7), Syntax: codec.TypeTimeTicks, Description: "Ticks."},
			{Name: "tC64", OID: objs.Append(8), Syntax: codec.TypeCounter64, Description: "A big counter."},
		},
		Notifications: []Notification{
			{Name: "tEvent", OID: agent.Append(0, 1), Objects: []string{"tInt", "tStr"}, Description: "An event."},
			{Name: "tPing", OID: agent.Append(0, 2), Description: "No objects."},
		},
	}
}

func TestRenderShape(t *testing.T) {
	text, err := Render(full())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"TEST-MIB DEFINITIONS ::= BEGIN\n\nIMPORTS\n" +
			"    MODULE-IDENTITY, NOTIFICATION-TYPE, OBJECT-IDENTITY, OBJECT-TYPE, Counter32, Counter64, Gauge32, Integer32, IpAddress, TimeTicks, enterprises\n" +
			"        FROM SNMPv2-SMI\n" +
			"    MODULE-COMPLIANCE, NOTIFICATION-GROUP, OBJECT-GROUP\n" +
			"        FROM SNMPv2-CONF;\n",
		// The identity comes first, even though its parent is defined
		// after it; LAST-UPDATED is the newest revision.
		"testMIB MODULE-IDENTITY\n    LAST-UPDATED \"202609110000Z\"",
		"    REVISION    \"202501020304Z\"",
		"        \"contact\n         line two\"",
		"    ::= { root 2 }\n",
		"root OBJECT IDENTIFIER ::= { enterprises 54981 }\n",
		"agent OBJECT-IDENTITY\n    STATUS      current\n",
		"agentEvents OBJECT IDENTIFIER ::= { agent 0 }\n",
		"tInt OBJECT-TYPE\n    SYNTAX      Integer32\n    UNITS       \"dB\"\n    MAX-ACCESS  read-write\n",
		"tStr OBJECT-TYPE\n    SYNTAX      OCTET STRING\n    MAX-ACCESS  read-only\n",
		"tEvent NOTIFICATION-TYPE\n    OBJECTS     { tInt, tStr }\n",
		"tPing NOTIFICATION-TYPE\n    STATUS      current\n",
		"testConformance OBJECT IDENTIFIER ::= { testMIB 2 }\n",
		"testObjectGroup OBJECT-GROUP\n    OBJECTS     { tInt, tStr, tOid, tIP, tC32, tG32, tTicks, tC64 }\n",
		"testNotificationGroup NOTIFICATION-GROUP\n    NOTIFICATIONS { tEvent, tPing }\n",
		"        MANDATORY-GROUPS { testObjectGroup, testNotificationGroup }\n    ::= { testCompliances 1 }\n",
		"\nEND\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q\n--- got ---\n%s", want, text)
		}
	}
	// The identity is written once, not again among the nodes.
	if strings.Count(text, "testMIB MODULE-IDENTITY") != 1 {
		t.Error("the MODULE-IDENTITY is written more than once")
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	a, _ := Render(full())
	b, _ := Render(full())
	if a != b {
		t.Error("two renders of one module differ")
	}
}

// The generated module, read back by this repo's own compiler, names every
// node at the OID it was described at, with nothing unresolved.
func TestRoundTripThroughSMI(t *testing.T) {
	text, err := Render(full())
	if err != nil {
		t.Fatal(err)
	}
	stub := `SNMPv2-SMI DEFINITIONS ::= BEGIN
  org OBJECT IDENTIFIER ::= { iso 3 }
  dod OBJECT IDENTIFIER ::= { org 6 }
  internet OBJECT IDENTIFIER ::= { dod 1 }
  private OBJECT IDENTIFIER ::= { internet 4 }
  enterprises OBJECT IDENTIFIER ::= { private 1 }
  Integer32 ::= INTEGER (-2147483648..2147483647)
  IpAddress ::= OCTET STRING (SIZE (4))
  Counter32 ::= INTEGER (0..4294967295)
  Gauge32 ::= INTEGER (0..4294967295)
  TimeTicks ::= INTEGER (0..4294967295)
  Counter64 ::= INTEGER (0..18446744073709551615)
END
SNMPv2-CONF DEFINITIONS ::= BEGIN END
`
	var mods []*smi.Module
	var findings []smi.Finding
	for _, src := range []struct{ file, text string }{{"stub", stub}, {"TEST-MIB", text}} {
		ms, f, err := smi.Parse(src.file, []byte(src.text))
		if err != nil {
			t.Fatal(err)
		}
		mods = append(mods, ms...)
		findings = append(findings, f...)
	}
	c := smi.Compile(mods)
	findings = append(findings, c.Findings...)
	if len(findings) != 0 {
		t.Fatalf("the compiler reported %v on\n%s", findings, text)
	}

	got := map[string]smi.Object{}
	for _, o := range c.Objects {
		if o.Module == "TEST-MIB" {
			got[o.Name] = o
		}
	}
	m := full()
	check := func(name string, oid codec.OID, kind string) {
		t.Helper()
		o, ok := got[name]
		if !ok {
			t.Fatalf("%s did not come back", name)
		}
		if o.Dotted() != oid.String() || o.Kind.String() != kind {
			t.Errorf("%s = %s %s, want %s %s", name, o.Dotted(), o.Kind, oid, kind)
		}
	}
	check(m.Identity, m.OID, "module-identity")
	for _, b := range m.Branches {
		kind := "object-identifier"
		if b.Description != "" {
			kind = "object-identity"
		}
		check(b.Name, b.OID, kind)
	}
	for _, o := range m.Objects {
		check(o.Name, o.OID, "object-type")
		if got[o.Name].Access != o.Access.String() {
			t.Errorf("%s access = %q", o.Name, got[o.Name].Access)
		}
	}
	for _, n := range m.Notifications {
		check(n.Name, n.OID, "notification")
	}
	check("testObjectGroup", m.OID.Append(2, 1, 1), "group")
	check("testNotificationGroup", m.OID.Append(2, 1, 2), "group")
	check("testCompliance", m.OID.Append(2, 2, 1), "compliance")
}

// A module with nothing to conform to carries no conformance section and
// imports nothing from SNMPv2-CONF; an identity not ending in MIB is used
// whole as the prefix.
func TestNoConformanceWithoutObjects(t *testing.T) {
	m := full()
	m.Objects, m.Notifications = nil, nil
	text, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "SNMPv2-CONF") || strings.Contains(text, "Conformance") {
		t.Errorf("conformance without anything to conform to:\n%s", text)
	}

	m = full()
	m.Identity = "testModule"
	text, err = Render(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "testModuleCompliance MODULE-COMPLIANCE") {
		t.Errorf("prefix:\n%s", text)
	}
}

func TestRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(m *Module)
		want string
	}{
		{"a lower-case module name", func(m *Module) { m.Name = "test-mib" }, "not an SMI module name"},
		{"no organization", func(m *Module) { m.Organization = " " }, "ORGANIZATION has no text"},
		{"a quote in the description", func(m *Module) { m.Description = `say "hi"` }, "double quote"},
		{"no revision", func(m *Module) { m.Revisions = nil }, "no REVISION"},
		{"an empty revision", func(m *Module) { m.Revisions[1].Description = "" }, "revision 1 has no text"},
		{"revisions oldest first", func(m *Module) { m.Revisions[0], m.Revisions[1] = m.Revisions[1], m.Revisions[0] }, "newest first"},
		{"an upper-case identity", func(m *Module) { m.Identity = "TestMIB" }, "not an SMI identifier"},
		{"an identity outside enterprises", func(m *Module) { m.OID = codec.MustParseOID("1.3.6.1.2.1.99") }, "not under enterprises"},
		{"enterprises itself", func(m *Module) { m.OID = enterprises }, "not under enterprises"},
		{"a branch description with a quote", func(m *Module) { m.Branches[1].Description = `"` }, "double quote"},
		{"a name used twice", func(m *Module) { m.Branches[4].Name = "root" }, "defined twice"},
		{"an OID used twice", func(m *Module) { m.Branches[4].OID = pen }, "both at"},
		{"a hyphen at the end", func(m *Module) { m.Branches[4].Name = "things-" }, "not an SMI identifier"},
		{"two hyphens", func(m *Module) { m.Branches[4].Name = "th--ings" }, "not an SMI identifier"},
		{"an underscore", func(m *Module) { m.Branches[4].Name = "th_ings" }, "not an SMI identifier"},
		{"a name too long", func(m *Module) { m.Branches[4].Name = "t" + strings.Repeat("x", 64) }, "not an SMI identifier"},
		{"an Opaque object", func(m *Module) { m.Objects[0].Syntax = codec.TypeOpaque }, "cannot declare"},
		{"an object with no description", func(m *Module) { m.Objects[0].Description = "" }, "tInt has no text"},
		{"units with a quote", func(m *Module) { m.Objects[0].Units = `"` }, "UNITS"},
		{"an object at a taken OID", func(m *Module) { m.Objects[0].OID = objs }, "both at"},
		{"a notification with no description", func(m *Module) { m.Notifications[0].Description = "" }, "tEvent has no text"},
		{"a notification carrying a stranger", func(m *Module) { m.Notifications[0].Objects = []string{"sysDescr"} }, "not an object in TEST-MIB"},
		{"a notification at a taken OID", func(m *Module) { m.Notifications[0].OID = agent }, "both at"},
		{"a node with no parent", func(m *Module) { m.Objects[0].OID = objs.Append(9, 1) }, "add a Branch there"},
		{"an identity with no parent", func(m *Module) { m.OID = codec.MustParseOID("1.3.6.1.4.1.1.2") }, "add a Branch there"},
		{"a branch where conformance goes", func(m *Module) { m.Branches = append(m.Branches, Branch{Name: "x", OID: modOID.Append(2)}) }, "both at"},
		{"a branch where the object group goes", func(m *Module) {
			m.Branches = append(m.Branches, Branch{Name: "x", OID: modOID.Append(2, 1, 1)})
		}, "both at"},
		{"a branch where the notification group goes", func(m *Module) {
			m.Branches = append(m.Branches, Branch{Name: "x", OID: modOID.Append(2, 1, 2)})
		}, "both at"},
		{"a branch where the compliance goes", func(m *Module) {
			m.Branches = append(m.Branches, Branch{Name: "x", OID: modOID.Append(2, 2, 1)})
		}, "both at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := full()
			m.Revisions = append([]Revision(nil), m.Revisions...)
			tc.edit(&m)
			if _, err := Render(m); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want %q", err, tc.want)
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWrite(t *testing.T) {
	var b strings.Builder
	if err := Write(&b, full()); err != nil || !strings.HasPrefix(b.String(), "TEST-MIB DEFINITIONS") {
		t.Errorf("= %v", err)
	}
	if err := Write(failingWriter{}, full()); err == nil {
		t.Error("a writer that refuses must be reported")
	}
	m := full()
	m.Name = ""
	b.Reset()
	if err := Write(&b, m); err == nil || b.Len() != 0 {
		t.Error("an invalid module must be refused with nothing written")
	}
}

func TestAccessString(t *testing.T) {
	if ReadOnly.String() != "read-only" || ReadWrite.String() != "read-write" {
		t.Error("access names")
	}
}
