// Package mib holds the OID vocabulary: the standard groups every agent
// must serve, and the vendor roots this plant's devices answer on.
//
// Names only. This package knows that 1.3.6.1.2.1.1.1.0 is sysDescr.0
// and that 1.3.6.1.4.1.7995 is Snell & Wilcox; it does not know what
// value either holds, does not open a socket, and does not decide what
// an agent serves. internal/snmp/provider does that, and takes what it
// serves from here so an OID literal appears once.
//
// # Where the vendor tables come from
//
// The per-device tables are GENERATED, offline, from a checkout of
// github.com/by-protocol/mib by tools/mibc, and committed. Parsing MIB
// source is a compiler problem — lexer, grammar, IMPORTS resolution
// across a set that mixes SMIv1 and SMIv2 — and the shipped binary has
// no business doing it: see internal/snmp/CLAUDE.md, "MIB parsing
// happens OFFLINE, never in the binary". What is hand-written here is
// only the standard groups, which are small, fixed since 1991, and
// imported by every vendor MIB anyway.
package mib

import "dhs/internal/snmp/codec"

// oid is ParseOID for the constants below. It panics, which is what a
// bad OID literal in our own source deserves.
func oid(s string) codec.OID { return codec.MustParseOID(s) }

// The tree's landmarks, as a manager writes them.
var (
	// Internet is 1.3.6.1 — the root of everything below.
	Internet = oid("1.3.6.1")
	// Mgmt2 is 1.3.6.1.2.1, the mib-2 subtree RFC 1213 defines.
	Mgmt2 = oid("1.3.6.1.2.1")
	// Enterprises is 1.3.6.1.4.1, under which every vendor sits.
	Enterprises = oid("1.3.6.1.4.1")
	// SNMPv2 is 1.3.6.1.6, where the v3 framework's own objects live.
	SNMPv2 = oid("1.3.6.1.6")
)

// The RFC 1213 §3.7 system group. Every one of these is MANDATORY: a
// manager discovers what a device IS by reading them, and an agent that
// omits sysObjectID.0 is a device an NMS cannot classify however much
// else it serves.
var (
	System = oid("1.3.6.1.2.1.1")

	SysDescr    = oid("1.3.6.1.2.1.1.1.0")
	SysObjectID = oid("1.3.6.1.2.1.1.2.0")
	SysUpTime   = oid("1.3.6.1.2.1.1.3.0")
	SysContact  = oid("1.3.6.1.2.1.1.4.0")
	SysName     = oid("1.3.6.1.2.1.1.5.0")
	SysLocation = oid("1.3.6.1.2.1.1.6.0")
	SysServices = oid("1.3.6.1.2.1.1.7.0")
)

// The RFC 3418 §5 snmp group counters an NMS reads to tell "nobody is
// polling us" from "we are answering wrong".
var (
	SNMPGroup = oid("1.3.6.1.2.1.11")

	SNMPInPkts              = oid("1.3.6.1.2.1.11.1.0")
	SNMPInBadVersions       = oid("1.3.6.1.2.1.11.3.0")
	SNMPInBadCommunityNames = oid("1.3.6.1.2.1.11.4.0")
	SNMPInBadCommunityUses  = oid("1.3.6.1.2.1.11.5.0")
	SNMPInASNParseErrs      = oid("1.3.6.1.2.1.11.6.0")
	SNMPOutPkts             = oid("1.3.6.1.2.1.11.2.0")
	SNMPEnableAuthenTraps   = oid("1.3.6.1.2.1.11.30.0")
)

// SysServicesApplication is the sysServices value for a device that
// offers an application-layer service and nothing lower — bit 7 of RFC
// 1213's bitmask, which is 2^(7-1). This agent presents a protocol
// gateway, not a router or a bridge, so this is what it reports.
const SysServicesApplication = 64

// Vendor roots the devices in docs/testbed.md answer on. Both are here
// because there are TWO: code that assumed a single vendor root would
// work against the IRDs and quietly mis-address the Snell frames.
var (
	// Tandberg is Tandberg Television / Ericsson, IANA enterprise 1773.
	// The TT1260 and RX1290 both report sysObjectID 1773.1.3.200.
	Tandberg = oid("1.3.6.1.4.1.1773")
	// TandbergCommon is 1773.1.1, the chassis branch: network config at
	// .1.1.1, trap destinations at .1.2.1, and a per-slot card table at
	// .1.3.1.
	TandbergCommon = oid("1.3.6.1.4.1.1773.1.1")

	// SnellWilcox is Snell & Wilcox, IANA enterprise 7995, from
	// SNELL-WILCOX-SMI.mib.
	SnellWilcox = oid("1.3.6.1.4.1.7995")
	// SnellProductReg is 7995.1, the branch sysObjectID values are
	// assigned from (SNELL-WILCOX-PRODUCT-REG).
	SnellProductReg = oid("1.3.6.1.4.1.7995.1")
	// SnellGeneric is 7995.2, the sub-tree of textual conventions and
	// objects shared across Snell products.
	SnellGeneric = oid("1.3.6.1.4.1.7995.2")
)

// DHS is the enterprise sub-tree this implementation's own objects hang
// off: BY-SYSTEMS SPRL's IANA Private Enterprise Number, 54981. It is the
// number usm.Enterprise builds engine IDs from, so an agent's
// sysObjectID and its engine identity name the same organisation. Arcs
// under it are assigned in the generated DHS MIB, never ad hoc in code.
var DHS = oid("1.3.6.1.4.1.54981")

// Name returns the name for an OID: the standard one when this package
// defines it, else the deepest compiled name that covers it with the
// remaining arcs, else the dotted form.
//
// It exists for output, not for dispatch: a CLI column that says
// "sysDescr.0" is readable and one that says "1.3.6.1.2.1.1.1.0" is a
// thing an operator has to look up. Nothing branches on the result.
func Name(o codec.OID) string {
	if n, ok := names[o.String()]; ok {
		return n
	}
	return Compiled().Name(o)
}

// Describe is [Name] for a caller that knows which device it is talking
// to: prefer lists the modules to name from first where two devices name
// one OID differently. It also returns the compiled object, nil when
// nothing covers o, so an enumerated value can be labelled.
func Describe(o codec.OID, prefer ...string) (string, *Object) {
	obj, rest := Compiled().Lookup(o, prefer...)
	if n, ok := names[o.String()]; ok {
		return n, obj
	}
	if obj == nil {
		return o.String(), nil
	}
	return withSuffix(obj.Name, rest), obj
}

// names is the reverse index for [Name]. Built from the variables above
// rather than written twice, so a corrected OID cannot leave a stale
// name pointing at it.
var names = func() map[string]string {
	m := map[string]string{}
	for name, o := range Standard() {
		m[o.String()] = name
	}
	return m
}()

// Standard returns the standard objects by name, which is what a CLI
// resolving `--oid sysDescr.0` looks in.
//
// A fresh map per call: a caller that edited a shared one would change
// what every other caller resolves.
func Standard() map[string]codec.OID {
	return map[string]codec.OID{
		"sysDescr.0":                SysDescr,
		"sysObjectID.0":             SysObjectID,
		"sysUpTime.0":               SysUpTime,
		"sysContact.0":              SysContact,
		"sysName.0":                 SysName,
		"sysLocation.0":             SysLocation,
		"sysServices.0":             SysServices,
		"snmpInPkts.0":              SNMPInPkts,
		"snmpOutPkts.0":             SNMPOutPkts,
		"snmpInBadVersions.0":       SNMPInBadVersions,
		"snmpInBadCommunityNames.0": SNMPInBadCommunityNames,
		"snmpInBadCommunityUses.0":  SNMPInBadCommunityUses,
		"snmpInASNParseErrs.0":      SNMPInASNParseErrs,
		"snmpEnableAuthenTraps.0":   SNMPEnableAuthenTraps,
		"system":                    System,
		"snmp":                      SNMPGroup,
		"mib-2":                     Mgmt2,
		"internet":                  Internet,
		"enterprises":               Enterprises,
	}
}

// Resolve turns what an operator typed into an OID: a standard name, a
// dotted numeric form, or a name from the compiled MIBs — controlMode.0,
// or ETV-TT1260-MIB::controlMode.0 where a bare name is ambiguous.
//
// All of them, because all of them get typed. A manager's own
// documentation says sysDescr.0, a vendor's says 1.3.6.1.4.1.7995.1.2.3.4,
// and the vendor's MIB says controlMode, and an operator pasting any of
// them should not have to know which this tool wants.
func Resolve(s string) (codec.OID, error) {
	if o, ok := Standard()[s]; ok {
		return o, nil
	}
	if s == "" || s[0] == '.' || (s[0] >= '0' && s[0] <= '9') {
		return codec.ParseOID(s)
	}
	return Compiled().Resolve(s)
}
