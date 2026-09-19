package mib

import "dhs/internal/snmp/codec"

var (
	etvRoot       = codec.MustParseOID("1.3.6.1.4.1.1773")
	etvAlarmTraps = codec.MustParseOID("1.3.6.1.4.1.1773.1.1.11")
)

// ETVAlarmTrapName names an Ericsson/Tandberg (ETV, enterprise 1773) alarm
// notification that the device emits as a v1 enterprise-specific trap under
// its own product OID rather than at the notification's defined OID.
//
// The RX1290 and TT1260 define no notifications under their product subtree;
// they raise the shared ETV-AlarmTrap-MIB notifications, and on v1 those
// arrive as enterprise=<product>, generic=enterpriseSpecific, specific=N.
// N is the ETV-AlarmTrap `traps` sub-id, so specific 5 is alarmMajor
// (1.3.6.1.4.1.1773.1.1.11.5) and specific 101 is eventNotify. RFC 3584
// would map the v1 trap to <product>.0.N — an OID no MIB defines — which is
// why a plain lookup of the trap OID renders it numerically (e.g. rx1290.0.5).
//
// It returns a name and true only for an enterprise-specific trap whose
// enterprise is under the ETV tree and whose mapped OID names an exact
// object; otherwise ("", false), so the caller keeps its numeric fallback
// for coldStart, dhsTestNotification, and genuinely product-specific traps.
func ETVAlarmTrapName(enterprise codec.OID, generic codec.GenericTrap, specific int) (string, bool) {
	if generic != codec.EnterpriseSpecific || specific < 0 {
		return "", false
	}
	if !enterprise.HasPrefix(etvRoot) {
		return "", false
	}
	obj, rest := Compiled().Lookup(etvAlarmTraps.Append(uint32(specific)))
	if obj == nil || len(rest) != 0 {
		return "", false // no exact ETV-AlarmTrap notification at this sub-id
	}
	return obj.Name, true
}
