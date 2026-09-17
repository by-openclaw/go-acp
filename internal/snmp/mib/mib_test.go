package mib

// The vocabulary. What is worth pinning is that the OIDs are the ones
// the RFCs and the vendors' own MIBs say, because an agent that serves
// sysDescr at the wrong OID serves nothing a manager will find.

import (
	"strings"
	"testing"
)

// The RFC 1213 system group, checked against the numbers rather than
// against each other — a typo copied twice is still a typo.
func TestTheStandardOIDs(t *testing.T) {
	for _, tc := range []struct {
		got  interface{ String() string }
		want string
	}{
		{Internet, "1.3.6.1"},
		{Mgmt2, "1.3.6.1.2.1"},
		{Enterprises, "1.3.6.1.4.1"},
		{SNMPv2, "1.3.6.1.6"},
		{System, "1.3.6.1.2.1.1"},
		{SysDescr, "1.3.6.1.2.1.1.1.0"},
		{SysObjectID, "1.3.6.1.2.1.1.2.0"},
		{SysUpTime, "1.3.6.1.2.1.1.3.0"},
		{SysContact, "1.3.6.1.2.1.1.4.0"},
		{SysName, "1.3.6.1.2.1.1.5.0"},
		{SysLocation, "1.3.6.1.2.1.1.6.0"},
		{SysServices, "1.3.6.1.2.1.1.7.0"},
		{SNMPGroup, "1.3.6.1.2.1.11"},
		{SNMPInPkts, "1.3.6.1.2.1.11.1.0"},
		{SNMPOutPkts, "1.3.6.1.2.1.11.2.0"},
		{SNMPInBadVersions, "1.3.6.1.2.1.11.3.0"},
		{SNMPInBadCommunityNames, "1.3.6.1.2.1.11.4.0"},
		{SNMPInBadCommunityUses, "1.3.6.1.2.1.11.5.0"},
		{SNMPInASNParseErrs, "1.3.6.1.2.1.11.6.0"},
		{SNMPEnableAuthenTraps, "1.3.6.1.2.1.11.30.0"},
	} {
		if got := tc.got.String(); got != tc.want {
			t.Errorf("= %s, want %s", got, tc.want)
		}
	}
}

// TWO vendor roots, because this plant has two. Code that assumed one
// would work against the IRDs and quietly mis-address the Snell frames.
func TestBothVendorRoots(t *testing.T) {
	if got := Tandberg.String(); got != "1.3.6.1.4.1.1773" {
		t.Errorf("Tandberg = %s, want IANA enterprise 1773", got)
	}
	if !TandbergCommon.HasPrefix(Tandberg) {
		t.Error("the chassis branch must be under the vendor root")
	}
	if got := SnellWilcox.String(); got != "1.3.6.1.4.1.7995" {
		t.Errorf("Snell & Wilcox = %s, want IANA enterprise 7995", got)
	}
	if !SnellProductReg.HasPrefix(SnellWilcox) || !SnellGeneric.HasPrefix(SnellWilcox) {
		t.Error("the Snell sub-trees must be under the Snell root")
	}
	if Tandberg.Compare(SnellWilcox) == 0 {
		t.Error("the two vendor roots are not the same tree")
	}
	if !DHS.HasPrefix(Enterprises) {
		t.Error("our own sub-tree must be under enterprises")
	}
}

// Both forms get typed, so an operator pasting either does not have to
// know which this tool wants.
func TestResolve(t *testing.T) {
	got, err := Resolve("sysDescr.0")
	if err != nil || got.Compare(SysDescr) != 0 {
		t.Errorf("by name = %s, %v", got, err)
	}
	got, err = Resolve("1.3.6.1.4.1.7995.1.2.3")
	if err != nil || got.String() != "1.3.6.1.4.1.7995.1.2.3" {
		t.Errorf("by number = %s, %v", got, err)
	}
	if _, err := Resolve("not an oid at all"); err == nil {
		t.Error("nonsense must be refused")
	}
}

// Names are for output. A standard OID gets its standard name, one the
// compiled MIBs cover gets the deepest name over it and the arcs left,
// and one nothing covers renders as its numbers rather than as nothing.
func TestName(t *testing.T) {
	if got := Name(SysDescr); got != "sysDescr.0" {
		t.Errorf("= %q", got)
	}
	if got := Name(oid("1.3.6.1.4.1.7995.9.9")); got != "snellWilcoxRoot.9.9" {
		t.Errorf("= %q, want the Snell root and the rest", got)
	}
	if got := Name(oid("2.999.1")); got != "2.999.1" {
		t.Errorf("= %q, want the dotted form", got)
	}
}

// The name table is built FROM the OIDs, so a corrected OID cannot leave
// a stale name pointing at the old one.
func TestTheNameTableAgreesWithTheOIDs(t *testing.T) {
	for name, o := range Standard() {
		if got := Name(o); got != name {
			t.Errorf("%s resolves to %s but renders as %s", name, o, got)
		}
	}
}

// Standard hands back a copy: a caller that edited a shared map would
// change what every other caller resolves.
func TestStandardIsACopy(t *testing.T) {
	m := Standard()
	delete(m, "sysDescr.0")
	m["sysDescr.0"] = oid("9.9")
	if got, err := Resolve("sysDescr.0"); err != nil || got.Compare(SysDescr) != 0 {
		t.Errorf("the caller's edit reached the table: %s", got)
	}
}

// sysServices is a bitmask, and application-layer is bit 7 — value 64,
// not 7.
func TestSysServicesApplication(t *testing.T) {
	if SysServicesApplication != 64 {
		t.Errorf("= %d, want bit 7 of the RFC 1213 bitmask", SysServicesApplication)
	}
}

func TestABadLiteralInOurOwnSourcePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a bad OID literal here must panic")
		}
	}()
	oid("nonsense")
}

func TestNamesAreLowerCamel(t *testing.T) {
	for name := range Standard() {
		if strings.ToLower(name[:1]) != name[:1] {
			t.Errorf("%q does not read as an SMI name", name)
		}
	}
}
