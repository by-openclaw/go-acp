package provider

// DHS-MIB is generated from the served tree, so the tests are about the
// seam: what the tree hands the generator, and that the module names the
// notification at the OID the trap sender actually puts on the wire.

import (
	"strings"
	"testing"

	"dhs/internal/snmp/codec"
	"dhs/internal/snmp/mib"
	"dhs/internal/snmp/mibgen"
)

func named(o Object, name, descr string) Object {
	o.Name, o.Description = name, descr
	return o
}

func TestDefinitions(t *testing.T) {
	root := mib.DHSObjects
	m := NewMIB()
	m.MustRegister(
		// Outside the root: not ours to define.
		Scalar(mib.SysDescr, codec.String("agent")),
		// Structural: nothing to define.
		Object{OID: root.Append(9), Access: NotAccessible},
		named(Scalar(root.Append(1, 0), codec.Int(1)), "dhsOne", "One."),
		named(Writable(root.Append(2, 0), codec.TypeOctetString,
			func() codec.Value { return codec.String("") },
			func(codec.Value) error { return nil }), "dhsTwo", "Two."),
	)
	defs, err := m.Definitions(mib.DHS)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 {
		t.Fatalf("defs = %+v", defs)
	}
	one, two := defs[0], defs[1]
	if one.Name != "dhsOne" || one.OID.String() != root.Append(1).String() ||
		one.Syntax != codec.TypeInteger || one.Access != mibgen.ReadOnly || one.Description != "One." {
		t.Errorf("one = %+v", one)
	}
	if two.Access != mibgen.ReadWrite || two.Syntax != codec.TypeOctetString {
		t.Errorf("two = %+v", two)
	}
	// The definition's OID is its own: editing it leaves the served tree
	// answering where it did.
	one.OID[0] = 99
	if _, ok := m.Get(root.Append(1, 0)); !ok {
		t.Error("the definition aliased the served object's OID")
	}
}

func TestDefinitionsRefuse(t *testing.T) {
	root := mib.DHSObjects
	for _, tc := range []struct {
		name string
		obj  Object
		want string
	}{
		{"an object with no name", Scalar(root.Append(3, 0), codec.Int(1)), "no Name"},
		{"a table cell", named(Scalar(root.Append(4, 1), codec.Int(1)), "dhsCell", "A cell."), "not a scalar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMIB()
			m.MustRegister(tc.obj)
			if _, err := m.Definitions(mib.DHS); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("= %v, want %q", err, tc.want)
			}
			if _, err := DHSModule(m, ""); err == nil {
				t.Error("DHSModule must refuse a tree it cannot define")
			}
		})
	}
}

func TestDHSModule(t *testing.T) {
	tree := NewMIB()
	tree.MustRegister(SystemGroup(SystemInfo{}, nil)...)
	m, err := DHSModule(tree, "")
	if err != nil {
		t.Fatal(err)
	}
	if m.ContactInfo != DHSOrganization || m.Organization != DHSOrganization {
		t.Errorf("contact = %q", m.ContactInfo)
	}
	// The system group is RFC 1213's, not ours: nothing to define.
	if len(m.Objects) != 0 {
		t.Errorf("objects = %+v", m.Objects)
	}
	if m, _ = DHSModule(tree, "noc@example.org"); m.ContactInfo != "noc@example.org" {
		t.Errorf("contact = %q", m.ContactInfo)
	}
	if _, err := mibgen.Render(m); err != nil {
		t.Fatalf("the module does not render: %v", err)
	}
}

// The notification the module defines is the one the sender sends: a v1
// trap from dhsAgent, generic 6, specific 1, maps (RFC 3584 §3.1) to the
// v2c snmpTrapOID the module names dhsTestNotification — and sysObjectID
// is the same dhsAgent, so a manager names the device and the event from
// one module.
func TestTheModuleNamesWhatTheAgentSends(t *testing.T) {
	tree := NewMIB()
	tree.MustRegister(SystemGroup(SystemInfo{}, nil)...)
	m, err := DHSModule(tree, "")
	if err != nil {
		t.Fatal(err)
	}
	sent := Notification{Enterprise: mib.DHSAgent, Generic: codec.EnterpriseSpecific, Specific: 1}.TrapOID()
	if len(m.Notifications) != 1 || m.Notifications[0].OID.Compare(sent) != 0 {
		t.Errorf("module notification %v, sender sends %s", m.Notifications, sent)
	}
	obj, _ := tree.Get(mib.SysObjectID)
	if obj.Get().OID.Compare(mib.DHSAgent) != 0 {
		t.Errorf("sysObjectID.0 = %s, want dhsAgent", obj.Get().OID)
	}
}

// Served objects under our enterprise appear in the module, in its object
// group, with the conformance to match.
func TestServedObjectsAreDefined(t *testing.T) {
	tree := NewMIB()
	tree.MustRegister(named(Scalar(mib.DHSObjects.Append(1, 0), codec.Int(7)), "dhsSessions", "Open sessions."))
	m, err := DHSModule(tree, "")
	if err != nil {
		t.Fatal(err)
	}
	text, err := mibgen.Render(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"dhsSessions OBJECT-TYPE", "::= { dhsObjects 1 }", "OBJECTS     { dhsSessions }"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q", want)
		}
	}
}
